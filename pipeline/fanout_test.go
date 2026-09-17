package pipeline_test

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/pipeline"
)

// panickyDrain panics on every Send. A panic in a drain the caller supplied must
// never climb out of FanOut's own goroutine and kill the process.
type panickyDrain struct{ calls atomic.Int64 }

// Send panics after counting the call, so the test can see the drain was reached.
func (p *panickyDrain) Send(context.Context, map[string]any) {
	p.calls.Add(1)
	panic("drain boom")
}

// TestPipeline_PIPE2_FanOutPanicSurvives proves that a panicking drain hurts
// neither the process nor its siblings, and that the same drain keeps receiving
// events after the panic.
func TestPipeline_PIPE2_FanOutPanicSurvives(t *testing.T) {
	p := &panickyDrain{}
	reached := make(chan map[string]any, 4)
	sibling := wlog.DrainFunc(func(_ context.Context, e map[string]any) { reached <- e })

	fanned := pipeline.FanOut(p, sibling)
	fanned.Send(context.Background(), mkEvent(1))

	select {
	case <-reached:
	case <-time.After(time.Second):
		t.Fatal("a panicking sibling blocked delivery to a healthy drain")
	}
	waitFor(t, time.Second, func() bool { return p.calls.Load() >= 1 })

	// The recovered worker must keep draining the queue, not exit on the first panic.
	fanned.Send(context.Background(), mkEvent(2))
	waitFor(t, time.Second, func() bool { return p.calls.Load() >= 2 })
	closeDrain(t, fanned)
}

// TestPipeline_PIPE2_FanOutGoroutinesBounded proves that a hung drain costs one
// queue and one goroutine, not one goroutine per event. The old FanOut started
// 10,000 goroutines for these 10,000 events and leaked every one of them.
func TestPipeline_PIPE2_FanOutGoroutinesBounded(t *testing.T) {
	hang := wlog.DrainFunc(func(context.Context, map[string]any) { select {} })
	before := runtime.NumGoroutine()
	fanned := pipeline.FanOut(hang)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 10000; i++ {
			fanned.Send(context.Background(), mkEvent(i))
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Send blocked on a hung drain with a full queue")
	}

	// Give a leaking design time to spin up its per-event goroutines before the count.
	time.Sleep(100 * time.Millisecond)
	if after := runtime.NumGoroutine(); after > before+50 {
		t.Fatalf("goroutines grew from %d to %d for 10000 events", before, after)
	}
}

// lifecycleDrain records Flush and Close calls, so a test can see FanOut forward
// them to every drain.
type lifecycleDrain struct {
	sends   atomic.Int64
	flushes atomic.Int64
	closes  atomic.Int64
}

// Send counts the event.
func (d *lifecycleDrain) Send(context.Context, map[string]any) { d.sends.Add(1) }

// Flush counts the flush and reports success.
func (d *lifecycleDrain) Flush(context.Context) error { d.flushes.Add(1); return nil }

// Close counts the close and reports success.
func (d *lifecycleDrain) Close(context.Context) error { d.closes.Add(1); return nil }

// TestPipeline_PIPE2_FanOutCloses proves that Flush and Close reach every drain
// inside a FanOut. Without it, Logger.Close never stops or flushes a pipeline.Wrap
// that a FanOut holds.
func TestPipeline_PIPE2_FanOutCloses(t *testing.T) {
	a, b := &lifecycleDrain{}, &lifecycleDrain{}
	fanned := pipeline.FanOut(a, b)
	fanned.Send(context.Background(), mkEvent(1))
	waitFor(t, time.Second, func() bool { return a.sends.Load() == 1 && b.sends.Load() == 1 })

	f, ok := fanned.(interface{ Flush(context.Context) error })
	if !ok {
		t.Fatal("FanOut does not implement Flush")
	}
	if err := f.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	c, ok := fanned.(interface{ Close(context.Context) error })
	if !ok {
		t.Fatal("FanOut does not implement Close")
	}
	if err := c.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if a.flushes.Load() != 1 || b.flushes.Load() != 1 {
		t.Errorf("flush counts = %d, %d; want 1, 1", a.flushes.Load(), b.flushes.Load())
	}
	if a.closes.Load() != 1 || b.closes.Load() != 1 {
		t.Errorf("close counts = %d, %d; want 1, 1", a.closes.Load(), b.closes.Load())
	}
}
