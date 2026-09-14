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

type dropCapture struct {
	mu     sync.Mutex
	events []map[string]any
	err    error
}

func (d *dropCapture) record(events []map[string]any, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.events = events
	d.err = err
}

func (d *dropCapture) get() ([]map[string]any, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.events, d.err
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
