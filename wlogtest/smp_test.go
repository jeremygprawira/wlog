package wlogtest_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestWlogtest_SMP9_Silent proves a test logger writes nothing at all, so a test reads its own
// report instead of a line per event.
func TestWlogtest_SMP9_Silent(t *testing.T) {
	// Capture stdout before the logger is built, because the sink writes at emit time.
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	wlog.Set(ctx, "http", map[string]any{"status": 500})
	end()

	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	if len(out) != 0 {
		t.Errorf("wlogtest.New wrote %q to stdout, want silence", out)
	}
	if rec.Count() != 1 {
		t.Errorf("the recorder holds %d events, want 1", rec.Count())
	}
}

// TestWlogtest_SMP9_DottedPath proves RequireField reads a dotted path and compares values
// with reflect.DeepEqual, so a slice or a map no longer panics on the comparison.
func TestWlogtest_SMP9_DottedPath(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	wlog.Set(ctx, "http", map[string]any{"status": 500})
	wlog.Set(ctx, "order_id", "ord-1")
	wlog.Set(ctx, "tags", []string{"a", "b"})
	end()

	rec.RequireField(t, "http.status", 500)
	rec.RequireField(t, "order_id", "ord-1")
	// A slice and a map compare by value, and a number of another width still matches.
	rec.RequireField(t, "tags", []any{"a", "b"})
	rec.RequireField(t, "http", map[string]any{"status": 500})
	rec.RequireField(t, "http.status", 500.0)
}

// ExampleRecorder_RequireField shows the common assertion: one field, by a dotted path.
func ExampleRecorder_RequireField() {
	log, rec := wlogtest.New(&testing.T{})
	ctx, end := wlog.Start(log.WithContext(context.Background()), "http.request")
	wlog.Set(ctx, "http", map[string]any{"status": 200})
	end()

	rec.RequireField(&testing.T{}, "http.status", 200)
	// Output:
}

// ExampleRecorder_RequireCount shows counting the events a piece of code emitted.
func ExampleRecorder_RequireCount() {
	log, rec := wlogtest.New(&testing.T{})
	audit := func() {
		ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
		_ = ctx
		end()
	}
	audit()

	rec.RequireCount(&testing.T{}, 1)
	// Output:
}

// ExampleRecorder_Events shows reading every event for a hand-written assertion.
func ExampleRecorder_Events() {
	log, rec := wlogtest.New(&testing.T{})
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	wlog.Set(ctx, "order_id", "ord-1")
	end()

	for _, event := range rec.Events() {
		fmt.Println(event["order_id"], event["operation"])
	}
	// Output: ord-1 op
}
