// This file proves the buffer rules: how large a batch may grow, what a zero
// option means, and what the counters report.
package pipeline_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog/pipeline"
)

// recordingSender keeps every batch it receives.
type recordingSender struct {
	mu      sync.Mutex
	batches [][]map[string]any
}

// SendBatch records the batch.
func (s *recordingSender) SendBatch(_ context.Context, batch []map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.batches = append(s.batches, batch)
	return nil
}

// largest returns the size of the biggest batch it saw.
func (s *recordingSender) largest() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	largest := 0
	for _, batch := range s.batches {
		largest = max(largest, len(batch))
	}
	return largest
}

// total returns how many events it received.
func (s *recordingSender) total() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	for _, batch := range s.batches {
		total += len(batch)
	}
	return total
}

// TestPipeline_PIPE10_BatchSizeCap proves that one batch never holds more events
// than BatchSize, however fast they arrive.
func TestPipeline_PIPE10_BatchSizeCap(t *testing.T) {
	sender := &recordingSender{}
	w := pipeline.Wrap(sender, pipeline.BatchSize(2), pipeline.BatchInterval(10*time.Millisecond))
	ctx := context.Background()

	for i := 0; i < 7; i++ {
		w.Send(ctx, map[string]any{"n": i})
	}
	deadline := time.After(3 * time.Second)
	for sender.total() < 7 {
		select {
		case <-deadline:
			t.Fatalf("only %d of 7 events arrived", sender.total())
		case <-time.After(5 * time.Millisecond):
		}
	}
	if got := sender.largest(); got > 2 {
		t.Errorf("the largest batch held %d events, want at most 2", got)
	}
	closeDrain(t, w)
}

// TestPipeline_PIPE11_OptionClamps proves that a zero option cannot make the
// pipeline drop every event or spin: the values clamp to something usable.
func TestPipeline_PIPE11_OptionClamps(t *testing.T) {
	sender := &recordingSender{}
	w := pipeline.Wrap(sender,
		pipeline.BatchSize(0),
		pipeline.MaxBuffer(0),
		pipeline.MaxAttempts(0),
		pipeline.MaxDelay(0),
		pipeline.InitialDelay(0),
		pipeline.BatchInterval(0),
	)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		w.Send(ctx, map[string]any{"n": i})
	}
	closeDrain(t, w)

	// A clamped buffer may still drop an event, but every event is accounted for:
	// what the sender did not receive, the counter reports.
	stats, ok := w.(interface{ Stats() pipeline.Stats })
	if !ok {
		t.Fatal("the wrapped drain has no Stats")
	}
	got := stats.Stats()
	if got.Sent+got.Dropped != 3 {
		t.Errorf("sent %d and dropped %d, want 3 events accounted for", got.Sent, got.Dropped)
	}
	if sender.total() == 0 {
		t.Error("a zero option lost every event")
	}
}

// TestPipeline_PIPE24_StatsAndTimer proves that the worker wakes on its own timer,
// so one buffered event reaches the sender without another Send, and that the
// counters report what happened.
func TestPipeline_PIPE24_StatsAndTimer(t *testing.T) {
	sender := &recordingSender{}
	w := pipeline.Wrap(sender, pipeline.BatchSize(10), pipeline.BatchInterval(20*time.Millisecond))

	w.Send(context.Background(), map[string]any{"n": 1})

	deadline := time.After(2 * time.Second)
	for sender.total() == 0 {
		select {
		case <-deadline:
			t.Fatal("the worker never woke on its timer")
		case <-time.After(5 * time.Millisecond):
		}
	}

	stats, ok := w.(interface{ Stats() pipeline.Stats })
	if !ok {
		t.Fatal("the wrapped drain has no Stats")
	}
	got := stats.Stats()
	if got.Sent != 1 {
		t.Errorf("Stats().Sent = %d, want 1", got.Sent)
	}
	if got.Dropped != 0 {
		t.Errorf("Stats().Dropped = %d, want 0", got.Dropped)
	}
	if got.Buffered != 0 {
		t.Errorf("Stats().Buffered = %d, want 0 after the flush", got.Buffered)
	}

	// A refused batch counts as a drop.
	failing := pipeline.Wrap(&failingSender{}, pipeline.BatchSize(1), pipeline.MaxAttempts(1))
	failing.Send(context.Background(), map[string]any{"n": 1})
	dropDeadline := time.After(2 * time.Second)
	for {
		s, ok := failing.(interface{ Stats() pipeline.Stats })
		if !ok {
			t.Fatal("the wrapped drain has no Stats")
		}
		if s.Stats().Dropped > 0 {
			break
		}
		select {
		case <-dropDeadline:
			t.Fatal("a refused batch was not counted")
		case <-time.After(5 * time.Millisecond):
		}
	}
	closeDrain(t, failing)
	closeDrain(t, w)
}
