// This file tests the batch helpers of package work: the lag of a queued message, the
// size and failures of a batch, the skipped tick of a ticker, and the flush of a short
// lived runtime.
package work_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/wlogtest"
	"github.com/jeremygprawira/wlog/work"
)

// syncBuffer is a buffer a writer goroutine and a test may share.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write appends one line.
func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// String returns what the writer goroutine has written so far.
func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestWork_LagMs proves that StartedAt in the past gives lag_ms, and that timestamp stays
// the moment the work started processing.
func TestWork_LagMs(t *testing.T) {
	log, rec := wlogtest.New(t)
	start := time.Now()
	unit := work.Unit{
		Kind:      work.KindMessage,
		Fields:    map[string]any{"system": "kafka", "operation": "process", "destination": "orders"},
		StartedAt: start.Add(-2 * time.Second),
	}

	_, h := work.Start(context.Background(), log, unit)
	h.End(nil)

	got := rec.Last()
	messaging, _ := got["messaging"].(map[string]any)
	lag, _ := messaging["lag_ms"].(float64)
	if lag < 1900 || lag > 2600 {
		t.Errorf("messaging.lag_ms = %v, want about 2000", messaging["lag_ms"])
	}
	stamp, err := time.Parse(time.RFC3339Nano, got["timestamp"].(string))
	if err != nil {
		t.Fatalf("timestamp = %v: %v", got["timestamp"], err)
	}
	if stamp.Before(start.Add(-time.Second)) {
		t.Errorf("timestamp = %v, want the processing start %v and not the enqueue time", stamp, start)
	}
}

// TestWork_BatchEvent proves that the parent event of a batch holds the batch size and the
// failures, and that every message event links to the parent.
func TestWork_BatchEvent(t *testing.T) {
	log, rec := wlogtest.New(t)
	fields := map[string]any{"system": "kafka", "operation": "process", "destination": "orders"}

	parentCtx, parent := work.BatchEvent(context.Background(), log, work.Unit{Kind: work.KindMessage, Fields: fields}, 3)
	for i := 0; i < 3; i++ {
		_, h := work.Start(parentCtx, log, work.Unit{Kind: work.KindMessage, Fields: fields})
		if i == 0 {
			h.End(errors.New("bad message"))
			parent.Failed()
			continue
		}
		h.End(nil)
	}
	parent.End(nil)

	events := rec.Events()
	if len(events) != 4 {
		t.Fatalf("events = %d, want the parent and three messages", len(events))
	}
	parentEvent := events[3]
	messaging, _ := parentEvent["messaging"].(map[string]any)
	if numberOf(messaging["batch_size"]) != 3 {
		t.Errorf("messaging.batch_size = %v, want 3", messaging["batch_size"])
	}
	if numberOf(messaging["batch_failures"]) != 1 {
		t.Errorf("messaging.batch_failures = %v, want 1", messaging["batch_failures"])
	}

	parentID, _ := parentEvent["event_id"].(string)
	for i, child := range events[:3] {
		trace, _ := child["trace"].(map[string]any)
		if trace["parent_event_id"] != parentID {
			t.Errorf("message %d parent_event_id = %v, want the batch event %s", i, trace["parent_event_id"], parentID)
		}
	}
}

// TestWork_TickerSkipsBusyTick proves that a ticker emits one job event per tick as a job
// of system ticker, and that a tick which arrives while the previous run is still going is
// recorded in the next event.
func TestWork_TickerSkipsBusyTick(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var runs atomic.Int64
	done := make(chan struct{})
	go func() {
		defer close(done)
		work.Ticker(ctx, log, "reaper", 5*time.Millisecond, func(context.Context) error {
			runs.Add(1)
			// The run outlives a tick, so the ticker has to skip one.
			time.Sleep(12 * time.Millisecond)
			return nil
		})
	}()

	deadline := time.After(3 * time.Second)
	for runs.Load() < 2 {
		select {
		case <-deadline:
			t.Fatal("the ticker never ran twice")
		case <-time.After(2 * time.Millisecond):
		}
	}
	cancel()
	<-done

	var skipped int64
	var jobs int
	for _, event := range rec.Events() {
		if event["kind"] != "job" {
			continue
		}
		jobs++
		job, _ := event["job"].(map[string]any)
		if job["system"] != "ticker" || job["name"] != "reaper" {
			t.Errorf("job = %v, want system ticker and the name", job)
		}
		if ticker, ok := job["ticker"].(map[string]any); ok {
			skipped += int64(numberOf(ticker["skipped"]))
		}
	}
	if jobs < 2 {
		t.Errorf("job events = %d, want one per tick", jobs)
	}
	if skipped == 0 {
		t.Error("job.ticker.skipped is 0, want the tick that arrived during a run")
	}
}

// TestWork_FlushBounded proves that work.Flush reaches the writer before Run returns, so a
// short lived runtime, such as a command line tool, does not exit with the event queued.
func TestWork_FlushBounded(t *testing.T) {
	out := &syncBuffer{}
	log := wlog.New(wlog.WithWriter(out), wlog.WithFormat(wlog.FormatJSON))

	err := work.Run(context.Background(), log, work.Unit{Kind: work.KindWork, Operation: "short.lived"},
		func(context.Context) error { return nil }, work.Flush())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), "short.lived") {
		t.Errorf("the event was still queued when Run returned: %q", out.String())
	}
}

// numberOf reads a stored counter, treating a missing value as 0.
func numberOf(value any) float64 {
	switch number := value.(type) {
	case float64:
		return number
	case int:
		return float64(number)
	case int64:
		return float64(number)
	}
	return 0
}

// TestWork_BatchChildKeepsTheBatchTrace proves that a message with an empty carrier keeps
// the trace of its batch, so its parent span stays in the same trace.
func TestWork_BatchChildKeepsTheBatchTrace(t *testing.T) {
	log, rec := wlogtest.New(t)
	fields := map[string]any{"system": "kafka", "operation": "process", "destination": "orders"}
	parentCtx, parent := work.BatchEvent(context.Background(), log, work.Unit{Kind: work.KindMessage, Fields: fields}, 1)

	_, child := work.Start(parentCtx, log, work.Unit{
		Kind: work.KindMessage, Fields: fields, Carrier: propagate.MapCarrier{},
	})
	child.End(nil)
	parent.End(nil)

	events := rec.Events()
	if len(events) != 2 {
		t.Fatalf("events = %d, want the batch and its message", len(events))
	}
	// The child ends first, so it lands first.
	childTrace, _ := events[0]["trace"].(map[string]any)
	batchTrace, _ := events[1]["trace"].(map[string]any)
	if childTrace["trace_id"] != batchTrace["trace_id"] {
		t.Errorf("child trace_id = %v, want the batch trace id %v", childTrace["trace_id"], batchTrace["trace_id"])
	}
	if childTrace["parent_span_id"] != batchTrace["span_id"] {
		t.Errorf("child parent_span_id = %v, want the batch span id %v", childTrace["parent_span_id"], batchTrace["span_id"])
	}
}

// TestWork_BatchChildJoinsTheProducerTrace proves that a message whose carrier holds a
// traceparent joins the producer trace.
func TestWork_BatchChildJoinsTheProducerTrace(t *testing.T) {
	log, rec := wlogtest.New(t)
	fields := map[string]any{"system": "kafka", "operation": "process", "destination": "orders"}
	parentCtx, parent := work.BatchEvent(context.Background(), log, work.Unit{Kind: work.KindMessage, Fields: fields}, 1)

	_, child := work.Start(parentCtx, log, work.Unit{
		Kind: work.KindMessage, Fields: fields,
		Carrier: propagate.MapCarrier{"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"},
	})
	child.End(nil)
	parent.End(nil)

	childTrace, _ := rec.Events()[0]["trace"].(map[string]any)
	if childTrace["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("child trace_id = %v, want the producer trace id", childTrace["trace_id"])
	}
	if childTrace["parent_span_id"] != "00f067aa0ba902b7" {
		t.Errorf("child parent_span_id = %v, want the producer span id", childTrace["parent_span_id"])
	}
}

// TestWork_TickerEventContext proves that fn gets the context of its tick event, so a field
// it sets lands on the event.
func TestWork_TickerEventContext(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		work.Ticker(ctx, log, "reaper", time.Millisecond, func(tickCtx context.Context) error {
			wlog.Set(tickCtx, "tick_field", "yes")
			return nil
		})
	}()

	deadline := time.After(3 * time.Second)
	for len(rec.Events()) == 0 {
		select {
		case <-deadline:
			t.Fatal("the ticker never ran")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	<-done

	if got := rec.Events()[0]["tick_field"]; got != "yes" {
		t.Errorf("tick_field = %v, want the field fn set", got)
	}
}
