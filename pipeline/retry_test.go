package pipeline_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog/pipeline"
)

func TestPipeline_Retry_SucceedsAfterFailures(t *testing.T) {
	sender := &fakeSender{failN: 2}
	drain := pipeline.Wrap(sender,
		pipeline.BatchSize(3), pipeline.BatchInterval(time.Hour),
		pipeline.MaxAttempts(3), pipeline.InitialDelay(2*time.Millisecond), pipeline.MaxDelay(5*time.Millisecond),
	)
	defer func() { _ = drain.(interface{ Close(context.Context) error }).Close(context.Background()) }()

	for i := 0; i < 3; i++ {
		drain.Send(context.Background(), mkEvent(i))
	}

	waitFor(t, time.Second, func() bool { return len(sender.allEvents()) == 3 })
}

// dropCapture accumulates every OnDropped call (there can be more than one — a
// hanging Sender under overflow drops one event per call, for instance).
type dropCapture struct {
	mu    sync.Mutex
	calls [][]map[string]any
	errs  []error
}

func (d *dropCapture) record(events []map[string]any, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, events)
	d.errs = append(d.errs, err)
}

// get returns the most recent call.
func (d *dropCapture) get() ([]map[string]any, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.calls) == 0 {
		return nil, nil
	}
	return d.calls[len(d.calls)-1], d.errs[len(d.errs)-1]
}

// first returns the first call ever recorded.
func (d *dropCapture) first() ([]map[string]any, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.calls) == 0 {
		return nil, nil
	}
	return d.calls[0], d.errs[0]
}

func (d *dropCapture) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.calls)
}

func TestPipeline_Retry_ExhaustedCallsOnDropped(t *testing.T) {
	sender := &fakeSender{failN: 100} // always fails
	drop := &dropCapture{}
	drain := pipeline.Wrap(sender,
		pipeline.BatchSize(2), pipeline.BatchInterval(time.Hour),
		pipeline.MaxAttempts(2), pipeline.InitialDelay(2*time.Millisecond), pipeline.MaxDelay(5*time.Millisecond),
		pipeline.OnDropped(drop.record),
	)
	defer func() { _ = drain.(interface{ Close(context.Context) error }).Close(context.Background()) }()

	drain.Send(context.Background(), mkEvent(1))
	drain.Send(context.Background(), mkEvent(2))

	waitFor(t, time.Second, func() bool { events, _ := drop.get(); return events != nil })
	events, err := drop.get()
	if len(events) != 2 {
		t.Errorf("dropped batch has %d events, want 2", len(events))
	}
	if err == nil {
		t.Error("OnDropped's err is nil after exhausting retries, want the last error")
	}
	if sender.callsMade() != 2 {
		t.Errorf("SendBatch called %d times, want MaxAttempts=2", sender.callsMade())
	}
}

// retryAfterSender fails with a RetryError that asks for a huge wait.
type retryAfterSender struct {
	after   time.Duration
	calls   atomic.Int64
	mu      sync.Mutex
	backoff []time.Duration
}

// SendBatch fails with the configured Retry-After.
func (s *retryAfterSender) SendBatch(context.Context, []map[string]any) error {
	s.calls.Add(1)
	return &retryErr{after: s.after}
}

// retryErr is a RetryError with a wait of its own.
type retryErr struct{ after time.Duration }

// Error returns a message.
func (r *retryErr) Error() string { return "retry me" }

// Retryable reports that another attempt may work.
func (r *retryErr) Retryable() bool { return true }

// RetryAfter returns the wait the caller asked for.
func (r *retryErr) RetryAfter() time.Duration { return r.after }

// TestPipeline_PIPE5_RetryAfterCapped proves that a Retry-After longer than
// MaxDelay is capped, so a hostile or a confused server cannot park the worker for
// hours.
func TestPipeline_PIPE5_RetryAfterCapped(t *testing.T) {
	sender := &retryAfterSender{after: time.Hour}
	w := pipeline.Wrap(sender, pipeline.BatchSize(1), pipeline.MaxAttempts(2),
		pipeline.MaxDelay(30*time.Millisecond), pipeline.InitialDelay(time.Millisecond))
	w.Send(context.Background(), map[string]any{"n": 1})

	start := time.Now()
	for sender.calls.Load() < 2 {
		if time.Since(start) > 3*time.Second {
			t.Fatalf("the worker waited %v for a second attempt, want the capped delay", time.Since(start))
		}
		time.Sleep(5 * time.Millisecond)
	}
	elapsed := time.Since(start)
	if elapsed > time.Second {
		t.Errorf("the retry took %v, want it capped at MaxDelay", elapsed)
	}
	closeDrain(t, w)
}

// TestPipeline_PIPE5_RetryAfterOverflow proves that a huge Retry-After does not
// overflow into a negative wait, which would hot-loop the worker.
func TestPipeline_PIPE5_RetryAfterOverflow(t *testing.T) {
	sender := &retryAfterSender{after: time.Duration(1<<63 - 1)}
	w := pipeline.Wrap(sender, pipeline.BatchSize(1), pipeline.MaxAttempts(3),
		pipeline.MaxDelay(20*time.Millisecond), pipeline.InitialDelay(time.Millisecond))
	w.Send(context.Background(), map[string]any{"n": 1})

	bedrock := time.After(3 * time.Second)
	for {
		if sender.calls.Load() >= 3 {
			break
		}
		select {
		case <-bedrock:
			t.Fatalf("the worker made %d attempts, want 3: an overflowed wait stopped it or spun", sender.calls.Load())
		case <-time.After(5 * time.Millisecond):
		}
	}
	closeDrain(t, w)
}

// TestPipeline_PIPE22_BackoffBounded proves that the exponential curve never
// overflows and that the wait, jitter included, stays inside MaxDelay.
func TestPipeline_PIPE22_BackoffBounded(t *testing.T) {
	sender := &retryAfterSender{}
	w := pipeline.Wrap(sender, pipeline.BatchSize(1), pipeline.MaxAttempts(40),
		pipeline.Backoff(pipeline.Exponential), pipeline.InitialDelay(time.Millisecond),
		pipeline.MaxDelay(50*time.Millisecond))
	w.Send(context.Background(), map[string]any{"n": 1})

	// Forty attempts on a curve that doubles from a millisecond would take
	// centuries without the cap, so finishing inside this window proves the cap.
	bedrock := time.After(5 * time.Second)
	for {
		if sender.calls.Load() >= 40 {
			break
		}
		select {
		case <-bedrock:
			t.Fatalf("the backoff was not capped: %d attempts in 5s", sender.calls.Load())
		case <-time.After(10 * time.Millisecond):
		}
	}
	closeDrain(t, w)
}
