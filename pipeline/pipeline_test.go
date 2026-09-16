package pipeline_test

import (
	"context"
	"sync"
	"testing"
	"time"

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
