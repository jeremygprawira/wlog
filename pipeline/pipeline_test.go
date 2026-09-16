package pipeline_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/pipeline"
)

// fakeSender records every batch it receives, optionally failing the first N calls.
type fakeSender struct {
	mu      sync.Mutex
	batches [][]map[string]any
	failN   int
	calls   int
	hang    bool
	hangCh  chan struct{}
}

func (f *fakeSender) SendBatch(ctx context.Context, events []map[string]any) error {
	f.mu.Lock()
	f.calls++
	call := f.calls
	f.mu.Unlock()

	if f.hang {
		<-f.hangCh
		return nil
	}
	if call <= f.failN {
		return errFake
	}
	cp := make([]map[string]any, len(events))
	copy(cp, events)

	f.mu.Lock()
	f.batches = append(f.batches, cp)
	f.mu.Unlock()
	return nil
}

func (f *fakeSender) callsMade() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeSender) allEvents() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []map[string]any
	for _, b := range f.batches {
		out = append(out, b...)
	}
	return out
}

var errFake = &fakeError{"fake send failure"}

type fakeError struct{ msg string }

func (e *fakeError) Error() string { return e.msg }

func mkEvent(i int) map[string]any { return map[string]any{"i": i} }

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", timeout)
}

func TestPipeline_FlushesOnBatchSize(t *testing.T) {
	sender := &fakeSender{}
	drain := pipeline.Wrap(sender, pipeline.BatchSize(5), pipeline.BatchInterval(time.Hour))
	closer := drain.(interface{ Close(context.Context) error })
	defer func() { _ = closer.Close(context.Background()) }()

	for i := 0; i < 5; i++ {
		drain.Send(context.Background(), mkEvent(i))
	}

	waitFor(t, time.Second, func() bool { return len(sender.allEvents()) == 5 })
}

func TestPipeline_FlushesOnInterval(t *testing.T) {
	sender := &fakeSender{}
	drain := pipeline.Wrap(sender, pipeline.BatchSize(50), pipeline.BatchInterval(30*time.Millisecond))
	closer := drain.(interface{ Close(context.Context) error })
	defer func() { _ = closer.Close(context.Background()) }()

	drain.Send(context.Background(), mkEvent(1))
	drain.Send(context.Background(), mkEvent(2))

	waitFor(t, time.Second, func() bool { return len(sender.allEvents()) == 2 })
}

func TestPipeline_Close_FlushesPending(t *testing.T) {
	sender := &fakeSender{}
	drain := pipeline.Wrap(sender, pipeline.BatchSize(50), pipeline.BatchInterval(time.Hour))

	drain.Send(context.Background(), mkEvent(1))
	drain.Send(context.Background(), mkEvent(2))
	drain.Send(context.Background(), mkEvent(3))

	closer := drain.(interface{ Close(context.Context) error })
	if err := closer.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if len(sender.allEvents()) != 3 {
		t.Errorf("Close did not flush pending events: got %d, want 3", len(sender.allEvents()))
	}
}

// TestPipeline_PIPE4_FlushKeepsWorker proves that Flush sends what is buffered and
// leaves the worker running, so a later event still arrives.
func TestPipeline_PIPE4_FlushKeepsWorker(t *testing.T) {
	rec := &countingSender{}
	w := pipeline.Wrap(rec, pipeline.BatchSize(100), pipeline.BatchInterval(time.Hour))
	ctx := context.Background()

	w.Send(ctx, map[string]any{"n": 1})
	flusher, _ := w.(interface{ Flush(context.Context) error })
	if flusher == nil {
		t.Fatal("the wrapped drain has no Flush")
	}
	if err := flusher.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got := rec.count(); got != 1 {
		t.Fatalf("after Flush the sender saw %d events, want 1", got)
	}

	w.Send(ctx, map[string]any{"n": 2})
	if err := flusher.Flush(ctx); err != nil {
		t.Fatalf("second Flush: %v", err)
	}
	if got := rec.count(); got != 2 {
		t.Errorf("the worker stopped after Flush: %d events", got)
	}
	closeDrain(t, w)
}

// TestPipeline_PIPE4_SendAfterCloseDropped proves that a Send after Close reports
// the event instead of buffering it forever.
func TestPipeline_PIPE4_SendAfterCloseDropped(t *testing.T) {
	var mu sync.Mutex
	var dropped int

	w := pipeline.Wrap(&countingSender{}, pipeline.BatchSize(1),
		pipeline.OnDropped(func([]map[string]any, error) {
			mu.Lock()
			defer mu.Unlock()
			dropped++
		}))
	closeDrain(t, w)

	w.Send(context.Background(), map[string]any{"n": 1})

	mu.Lock()
	defer mu.Unlock()
	if dropped == 0 {
		t.Error("a Send after Close was accepted silently")
	}
}

// slowSender blocks in SendBatch until the context ends.
type slowSender struct{ calls atomic.Int64 }

// SendBatch waits for the context, which is how a Close with a deadline cancels a
// send that is already in flight.
func (s *slowSender) SendBatch(ctx context.Context, _ []map[string]any) error {
	s.calls.Add(1)
	<-ctx.Done()
	return ctx.Err()
}

// TestPipeline_PIPE6_CloseCancelsSend proves that Close ends an in-flight send at
// the deadline of its context instead of waiting forever.
func TestPipeline_PIPE6_CloseCancelsSend(t *testing.T) {
	sender := &slowSender{}
	w := pipeline.Wrap(sender, pipeline.BatchSize(1), pipeline.MaxAttempts(1), pipeline.MaxDelay(time.Millisecond))
	w.Send(context.Background(), map[string]any{"n": 1})

	// The worker picks the batch up, then blocks inside SendBatch.
	for sender.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- closeWith(ctx, w) }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("Close returned nil, want the deadline error of a stuck send")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close waited for a send that the deadline should have cancelled")
	}
}

// closeWith closes a wrapped drain with a context.
func closeWith(ctx context.Context, d wlog.Drain) error {
	c, ok := d.(interface{ Close(context.Context) error })
	if !ok {
		return errors.New("no Close")
	}
	return c.Close(ctx)
}

// countingSender counts the events it received.
type countingSender struct {
	mu     sync.Mutex
	events int
}

// SendBatch counts a batch.
func (c *countingSender) SendBatch(_ context.Context, batch []map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events += len(batch)
	return nil
}

// count returns the number of events received.
func (c *countingSender) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.events
}
