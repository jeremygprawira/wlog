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

func TestPipeline_Overflow_DropsOldest(t *testing.T) {
	// hangCh is closed explicitly, before Close is ever called: Close waits for the
	// worker to finish its current flush, so calling it while the worker is stuck in
	// SendBatch (as it is throughout this test) would deadlock the test itself.
	hangCh := make(chan struct{})
	sender := &fakeSender{hang: true, hangCh: hangCh}

	drop := &dropCapture{}
	drain := pipeline.Wrap(sender,
		pipeline.BatchSize(1), pipeline.BatchInterval(time.Hour),
		pipeline.MaxBuffer(3),
		pipeline.OnDropped(drop.record),
	)

	// The first Send triggers a flush that hangs forever in sender.SendBatch,
	// holding one "in flight" batch slot. The next MaxBuffer(3) sends then fill
	// the buffer; anything beyond that must drop the oldest still-queued event.
	drain.Send(context.Background(), mkEvent(0))
	time.Sleep(20 * time.Millisecond) // let the hang start

	for i := 1; i <= 5; i++ {
		drain.Send(context.Background(), mkEvent(i))
	}

	waitFor(t, time.Second, func() bool { return drop.count() > 0 })
	events, _ := drop.first()
	if events[0]["i"] != 1 {
		t.Errorf("first dropped event = %v, want the oldest queued one (i=1)", events[0])
	}
	close(hangCh)
}

func TestPipeline_Overflow_NeverBlocksSend(t *testing.T) {
	hangCh := make(chan struct{})
	sender := &fakeSender{hang: true, hangCh: hangCh}

	drain := pipeline.Wrap(sender, pipeline.BatchSize(1), pipeline.MaxBuffer(2))

	done := make(chan struct{})
	go func() {
		for i := 0; i < 10000; i++ {
			drain.Send(context.Background(), mkEvent(i))
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Send blocked with a hanging Sender and a full buffer")
	}
	close(hangCh)
}

func TestPipeline_FanOut_DeliversToAll(t *testing.T) {
	var mu sync.Mutex
	var gotA, gotB bool
	a := wlog.DrainFunc(func(_ context.Context, e map[string]any) {
		mu.Lock()
		gotA = true
		mu.Unlock()
	})
	b := wlog.DrainFunc(func(_ context.Context, e map[string]any) {
		mu.Lock()
		gotB = true
		mu.Unlock()
	})

	fanned := pipeline.FanOut(a, b)
	fanned.Send(context.Background(), mkEvent(1))

	// FanOut.Send dispatches to each drain in its own goroutine and returns
	// immediately (gate G3), so delivery must be awaited, not checked synchronously
	// right after Send returns.
	waitFor(t, time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return gotA && gotB
	})
}

func TestPipeline_FanOut_OneHangingDrainDoesNotBlockOthers(t *testing.T) {
	hang := wlog.DrainFunc(func(context.Context, map[string]any) {
		select {} // hangs forever
	})
	fast := make(chan struct{}, 1)
	fastDrain := wlog.DrainFunc(func(context.Context, map[string]any) {
		fast <- struct{}{}
	})

	fanned := pipeline.FanOut(hang, fastDrain)
	go fanned.Send(context.Background(), mkEvent(1))

	select {
	case <-fast:
	case <-time.After(time.Second):
		t.Fatal("a hanging drain blocked delivery to a fast one")
	}
}

// closeDrain closes a wrapped drain, which Wrap returns as a wlog.Drain.
func closeDrain(t *testing.T, d wlog.Drain) {
	t.Helper()
	c, ok := d.(interface{ Close(context.Context) error })
	if !ok {
		t.Fatal("the wrapped drain has no Close")
	}
	if err := c.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// panickingSender panics in SendBatch, which the worker must survive.
type panickingSender struct{ calls atomic.Int64 }

// SendBatch panics on the first call and succeeds after it.
func (p *panickingSender) SendBatch(context.Context, []map[string]any) error {
	if p.calls.Add(1) == 1 {
		panic("sender boom")
	}
	return nil
}

// TestPipeline_PIPE1_SenderPanicSurvives proves that a panicking SendBatch never
// kills the process, and that the worker keeps sending later batches.
func TestPipeline_PIPE1_SenderPanicSurvives(t *testing.T) {
	sender := &panickingSender{}
	w := pipeline.Wrap(sender, pipeline.BatchSize(1), pipeline.BatchInterval(5*time.Millisecond))
	ctx := context.Background()

	w.Send(ctx, map[string]any{"n": 1})
	w.Send(ctx, map[string]any{"n": 2})

	deadline := time.After(2 * time.Second)
	for sender.calls.Load() < 2 {
		select {
		case <-deadline:
			t.Fatalf("the worker stopped after the panic: %d calls", sender.calls.Load())
		case <-time.After(5 * time.Millisecond):
		}
	}
	closeDrain(t, w)
}

// reentrantSender calls back into the pipeline from OnDropped, which deadlocks
// when the drop runs while the lock is held.
type reentrantSender struct {
	w interface {
		Send(context.Context, map[string]any)
	}
	seen   atomic.Int64
	events []map[string]any
	mu     sync.Mutex
}

// SendBatch returns an error, so the batch is dropped.
func (r *reentrantSender) SendBatch(context.Context, []map[string]any) error {
	return errors.New("refused")
}

// TestPipeline_PIPE3_OnDroppedPanicDoesNotLock proves that OnDropped runs after
// the lock is released, so a drop that calls back into the pipeline does not
// deadlock.
func TestPipeline_PIPE3_OnDroppedPanicDoesNotLock(t *testing.T) {
	r := &reentrantSender{}
	w := pipeline.Wrap(r, pipeline.BatchSize(1), pipeline.MaxAttempts(1),
		pipeline.OnDropped(func(batch []map[string]any, err error) {
			r.mu.Lock()
			r.events = append(r.events, batch...)
			r.mu.Unlock()
			r.seen.Add(1)
			// A reentrant Send must not deadlock on the buffer lock.
			r.w.Send(context.Background(), map[string]any{"nested": true})
		}))
	r.w = w

	w.Send(context.Background(), map[string]any{"n": 1})

	deadline := time.After(2 * time.Second)
	for r.seen.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("OnDropped never ran")
		case <-time.After(5 * time.Millisecond):
		}
	}
	closeDrain(t, w)
}

// TestPipeline_PIPE3_OnDroppedReentrant proves that a drop reported from the
// worker does not hold the buffer lock: a Send during the callback returns.
func TestPipeline_PIPE3_OnDroppedReentrant(t *testing.T) {
	blocking := make(chan struct{})
	w := pipeline.Wrap(&failingSender{}, pipeline.BatchSize(1), pipeline.MaxAttempts(1),
		pipeline.OnDropped(func([]map[string]any, error) { <-blocking }))

	w.Send(context.Background(), map[string]any{"n": 1})
	time.Sleep(20 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Send(context.Background(), map[string]any{"n": 2})
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Send blocked while OnDropped ran: the lock was held")
	}
	close(blocking)
	closeDrain(t, w)
}

// failingSender refuses every batch.
type failingSender struct{}

// SendBatch always fails.
func (failingSender) SendBatch(context.Context, []map[string]any) error { return errors.New("refused") }
