package wlog_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/jeremygprawira/wlog"
)

// countingMeasurer counts the measures it receives. It holds the last one, so the
// compiler cannot fold the call away.
type countingMeasurer struct {
	calls int
	last  wlog.Measure
}

// Name names the plugin.
func (countingMeasurer) Name() string { return "counting" }

// Measure counts one measurement.
func (m *countingMeasurer) Measure(_ context.Context, measure wlog.Measure) {
	m.calls++
	m.last = measure
}

// BenchmarkEmit_WithMeasurer measures the same work as BenchmarkCore_StartSetEmit with
// one Measurer installed. The budget is 1us p50 above the plain benchmark, so a metrics
// plugin stays off the request path. The bench gate watches this number.
func BenchmarkEmit_WithMeasurer(b *testing.B) {
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON), wlog.WithPlugins(&countingMeasurer{}))
	benchEmit(b, log)
}

// benchEmit runs the Start, 10 Set, emit loop of one logger against a drained stdout.
func benchEmit(b *testing.B, log *wlog.Logger) {
	b.Helper()
	// b.N JSON lines can exceed the OS pipe buffer (see TestCore_SetRedactor_
	// ConcurrentSwapsAndEmits for the same issue), so drain stdout concurrently
	// instead of not at all, or emit would block once the pipe fills.
	r, w, err := os.Pipe()
	if err != nil {
		b.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	drained := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, r)
		close(drained)
	}()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := log.WithContext(context.Background())
		ctx, end := wlog.Start(ctx, "bench.op")
		for j := 0; j < 10; j++ {
			wlog.Set(ctx, fmt.Sprintf("field_%d", j), j)
		}
		end()
	}
	b.StopTimer()

	os.Stdout = orig
	_ = w.Close()
	<-drained
}

// BenchmarkCore_StartSetEmit measures Start, 10 Set calls, and emit with no drains —
// SPEC-core.md's criterion 10: <= 20us p50, half of SPEC.md's 50us total request
// budget (leaving room for HTTP capture on top, in a later module).
func BenchmarkCore_StartSetEmit(b *testing.B) {
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON))
	benchEmit(b, log)
}
