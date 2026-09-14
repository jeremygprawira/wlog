package pipeline_test

import (
	"context"
	"sync"
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
	defer drain.(interface{ Close(context.Context) error }).Close(context.Background())

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
	defer drain.(interface{ Close(context.Context) error }).Close(context.Background())

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
