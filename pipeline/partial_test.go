// This file tests the PartialError path: a Sender that accepts part of a batch
// names the indexes to send again and the indexes it refused for good.
package pipeline_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog/pipeline"
)

// partialSender returns one PartialError on the first call, then records every batch
// it receives.
type partialSender struct {
	mu      sync.Mutex
	err     *pipeline.PartialError
	calls   int
	batches [][]map[string]any
}

// SendBatch fails the first call with the PartialError, then records the batch.
func (s *partialSender) SendBatch(_ context.Context, events []map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.calls == 1 {
		return s.err
	}
	cp := make([]map[string]any, len(events))
	copy(cp, events)
	s.batches = append(s.batches, cp)
	return nil
}

// lastBatch returns the most recent batch, or nil when none arrived.
func (s *partialSender) lastBatch() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.batches) == 0 {
		return nil
	}
	return s.batches[len(s.batches)-1]
}

// callsMade returns how many times SendBatch ran.
func (s *partialSender) callsMade() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// TestPipeline_PartialError_RetryAndDropByIndex proves criterion 1: the worker drops
// the events the backend refused and sends again only the events it named.
func TestPipeline_PartialError_RetryAndDropByIndex(t *testing.T) {
	sender := &partialSender{err: &pipeline.PartialError{
		Retry:   []int{1},
		Dropped: []int{2},
		Reason:  "status_400",
	}}
	drop := &dropCapture{}
	drain := pipeline.Wrap(sender,
		pipeline.BatchSize(3), pipeline.BatchInterval(time.Hour),
		pipeline.MaxAttempts(3), pipeline.InitialDelay(time.Millisecond),
		pipeline.MaxDelay(5*time.Millisecond),
		pipeline.OnDropped(drop.record),
	)
	defer closeDrain(t, drain)

	for i := 0; i < 3; i++ {
		drain.Send(context.Background(), mkEvent(i))
	}

	// OnDropped receives event 2 alone.
	waitFor(t, time.Second, func() bool { return drop.count() > 0 })
	events, err := drop.first()
	if len(events) != 1 || events[0]["i"] != 2 {
		t.Fatalf("OnDropped got %v, want event 2 alone", events)
	}
	var pe *pipeline.PartialError
	if !errors.As(err, &pe) {
		t.Fatalf("OnDropped err = %v, want the PartialError", err)
	}

	// The next attempt sends event 1 alone.
	waitFor(t, time.Second, func() bool { return sender.lastBatch() != nil })
	got := sender.lastBatch()
	if len(got) != 1 || got[0]["i"] != 1 {
		t.Fatalf("the second attempt sent %v, want event 1 alone", got)
	}
}

// TestPipeline_PartialError_IgnoresIndexOutsideBatch proves that an index the batch
// cannot hold names nothing, so no event is dropped or sent again.
func TestPipeline_PartialError_IgnoresIndexOutsideBatch(t *testing.T) {
	sender := &partialSender{err: &pipeline.PartialError{
		Retry:   []int{5, -1},
		Dropped: []int{9},
		Reason:  "status_400",
	}}
	drop := &dropCapture{}
	drain := pipeline.Wrap(sender,
		pipeline.BatchSize(2), pipeline.BatchInterval(time.Hour),
		pipeline.MaxAttempts(3), pipeline.InitialDelay(time.Millisecond),
		pipeline.OnDropped(drop.record),
	)
	defer closeDrain(t, drain)

	drain.Send(context.Background(), mkEvent(0))
	drain.Send(context.Background(), mkEvent(1))

	// No index names an event of this batch, so the batch stops here.
	waitFor(t, time.Second, func() bool { return sender.callsMade() >= 1 })
	time.Sleep(30 * time.Millisecond)
	if got := drop.count(); got != 0 {
		t.Errorf("OnDropped ran %d times, want 0 for indexes outside the batch", got)
	}
	if got := sender.callsMade(); got != 1 {
		t.Errorf("SendBatch ran %d times, want 1: no index needs another try", got)
	}
}

// TestPipeline_PartialError_BothListsCountsAsDropped proves that an index in both
// lists is dropped, because a permanent refusal wins over a retry.
func TestPipeline_PartialError_BothListsCountsAsDropped(t *testing.T) {
	sender := &partialSender{err: &pipeline.PartialError{
		Retry:   []int{0},
		Dropped: []int{0, 1},
		Reason:  "status_400",
	}}
	drop := &dropCapture{}
	drain := pipeline.Wrap(sender,
		pipeline.BatchSize(2), pipeline.BatchInterval(time.Hour),
		pipeline.MaxAttempts(3), pipeline.InitialDelay(time.Millisecond),
		pipeline.OnDropped(drop.record),
	)
	defer closeDrain(t, drain)

	drain.Send(context.Background(), mkEvent(0))
	drain.Send(context.Background(), mkEvent(1))

	waitFor(t, time.Second, func() bool { return drop.count() > 0 })
	events, _ := drop.first()
	if len(events) != 2 {
		t.Fatalf("OnDropped got %v, want both events", events)
	}
	if sender.callsMade() != 1 {
		t.Errorf("SendBatch ran %d times, want 1: event 0 is dropped, not retried", sender.callsMade())
	}
}

// TestPipeline_PartialError_CountsDroppedInStats proves that a refused event counts as
// dropped and an event in neither list counts as sent.
func TestPipeline_PartialError_CountsDroppedInStats(t *testing.T) {
	sender := &partialSender{err: &pipeline.PartialError{
		Dropped: []int{2},
		Reason:  "status_400",
	}}
	drain := pipeline.Wrap(sender,
		pipeline.BatchSize(3), pipeline.BatchInterval(time.Hour),
		pipeline.MaxAttempts(3), pipeline.InitialDelay(time.Millisecond),
	)
	defer closeDrain(t, drain)

	for i := 0; i < 3; i++ {
		drain.Send(context.Background(), mkEvent(i))
	}

	waitFor(t, time.Second, func() bool {
		return drain.(interface{ Stats() pipeline.Stats }).Stats().Dropped == 1
	})
	stats := drain.(interface{ Stats() pipeline.Stats }).Stats()
	if stats.Dropped != 1 {
		t.Errorf("Stats.Dropped = %d, want 1", stats.Dropped)
	}
	if stats.Sent != 2 {
		t.Errorf("Stats.Sent = %d, want 2 for the events in neither list", stats.Sent)
	}
}

// typedNilSender returns a typed-nil *PartialError as its error. The error interface is
// not nil, and the pointer inside it is, which is what a Sender bug looks like.
type typedNilSender struct {
	mu    sync.Mutex
	calls int
}

// SendBatch returns a typed-nil *PartialError on every call.
func (s *typedNilSender) SendBatch(_ context.Context, _ []map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	var pe *pipeline.PartialError
	return pe
}

// callsMade returns how many times SendBatch ran.
func (s *typedNilSender) callsMade() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// TestPipeline_P11_TypedNilPartialErrorDoesNotPanic proves that a Sender which returns a
// typed-nil *PartialError does not kill the worker goroutine. The error names nothing, so
// the batch follows the normal retry policy and is reported when it runs out of attempts.
func TestPipeline_P11_TypedNilPartialErrorDoesNotPanic(t *testing.T) {
	sender := &typedNilSender{}
	drop := &dropCapture{}
	drain := pipeline.Wrap(sender,
		pipeline.BatchSize(1), pipeline.BatchInterval(time.Hour),
		pipeline.MaxAttempts(3), pipeline.InitialDelay(time.Millisecond),
		pipeline.OnDropped(drop.record),
	)
	defer closeDrain(t, drain)

	drain.Send(context.Background(), mkEvent(0))

	waitFor(t, time.Second, func() bool { return drop.count() > 0 })
	if got := sender.callsMade(); got != 3 {
		t.Errorf("SendBatch ran %d times, want 3: the typed nil is a plain failure", got)
	}
	events, err := drop.first()
	if len(events) != 1 {
		t.Errorf("OnDropped got %v, want the one event", events)
	}
	if err == nil {
		t.Error("OnDropped got a nil error, want the typed nil the Sender returned")
	}
}
