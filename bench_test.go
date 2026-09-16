package wlog_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/jeremygprawira/wlog"
)

// BenchmarkCore_StartSetEmit measures Start, 10 Set calls, and emit with no drains —
// SPEC-core.md's criterion 10: <= 20us p50, half of SPEC.md's 50us total request
// budget (leaving room for HTTP capture on top, in a later module).
func BenchmarkCore_StartSetEmit(b *testing.B) {
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON))

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
