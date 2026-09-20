// This file runs the work conformance suite against the consumer event path, and checks
// the two rules that only Kafka has: a failed handler does not commit its message, and one
// message fills the messaging group.
package wlogkafka

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	workconformance "github.com/jeremygprawira/wlog/internal/conformance/work"
	"github.com/jeremygprawira/wlog/wlogtest"
	"github.com/jeremygprawira/wlog/work"
)

// TestKafka_C1_WorkConformance proves that the consumer event path passes every scenario
// of the work suite.
func TestKafka_C1_WorkConformance(t *testing.T) {
	workconformance.Run(conformance.Tester{T: t}, workFactory{})
}

// workFactory runs one unit through the path Consume uses. The suite supplies the unit,
// because a Kafka message carries no job, rpc, command, or function field.
type workFactory struct{}

// Process runs one unit of work and returns what the handler returned.
func (workFactory) Process(log *wlog.Logger, unit work.Unit, handler func(context.Context) error) error {
	return process(context.Background(), log, unit, handler)
}

// TestKafka_C1_MessageFields proves that one message fills the messaging group, the offset
// lag, the time lag, and the trace carrier.
func TestKafka_C1_MessageFields(t *testing.T) {
	msg := kafka.Message{
		Topic: "orders", Partition: 3, Offset: 41, HighWaterMark: 100,
		Time: time.Now().Add(-2 * time.Second),
		Headers: []kafka.Header{{
			Key:   "traceparent",
			Value: []byte("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"),
		}},
	}
	r := &fakeReader{group: "workers", messages: []kafka.Message{msg}}
	log, rec := wlogtest.New(t)

	if err := Consume(context.Background(), log, r, func(context.Context, kafka.Message) error { return nil }); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	got := rec.Last()
	if got == nil {
		t.Fatal("no event recorded")
	}
	if got["operation"] != "process orders" {
		t.Errorf("operation = %v, want process orders", got["operation"])
	}
	messaging, _ := got["messaging"].(map[string]any)
	for key, want := range map[string]any{
		"system": "kafka", "operation": "process", "destination": "orders",
		"partition": 3, "offset": 41, "consumer_group": "workers",
	} {
		if !conformance.Equal(messaging[key], want) {
			t.Errorf("messaging.%s = %v, want %v", key, messaging[key], want)
		}
	}
	if lag, _ := messaging["lag_ms"].(float64); lag < 1900 || lag > 2100 {
		t.Errorf("messaging.lag_ms = %v, want about 2000", messaging["lag_ms"])
	}
	kafkaFields, _ := messaging["kafka"].(map[string]any)
	if !conformance.Equal(kafkaFields["offset_lag"], 58) {
		t.Errorf("messaging.kafka.offset_lag = %v, want 58", kafkaFields["offset_lag"])
	}
	trace, _ := got["trace"].(map[string]any)
	if trace["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace.trace_id = %v, want the trace id of the header", trace["trace_id"])
	}
}

// TestKafka_C2_CommitAfterSuccess proves that Consume commits the message after the
// handler returns nil.
func TestKafka_C2_CommitAfterSuccess(t *testing.T) {
	r := &fakeReader{group: "workers", messages: []kafka.Message{{Topic: "orders", Partition: 1, Offset: 7, HighWaterMark: 9}}}
	log, _ := wlogtest.New(t)

	if err := Consume(context.Background(), log, r, func(context.Context, kafka.Message) error { return nil }); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if got := r.committed(); got != 1 {
		t.Errorf("commits = %d, want 1", got)
	}
}

// TestKafka_C2_FailedHandlerDoesNotCommit proves that a failed handler leaves the message
// uncommitted, so the next fetch of the partition sees the same offset.
func TestKafka_C2_FailedHandlerDoesNotCommit(t *testing.T) {
	msg := kafka.Message{Topic: "orders", Partition: 1, Offset: 7, HighWaterMark: 9}
	r := &fakeReader{group: "workers", messages: []kafka.Message{msg, msg}}
	failure := errString("handler failed")
	handler := func(context.Context, kafka.Message) error { return failure }
	log, _ := wlogtest.New(t)

	if err := Consume(context.Background(), log, r, handler); err == nil || err.Error() != failure.Error() {
		t.Fatalf("Consume returned %v, want the handler error", err)
	}
	if got := r.committed(); got != 0 {
		t.Fatalf("commits = %d, want none after a failed handler", got)
	}

	// The caller fetches again, and the same offset comes back.
	if err := Consume(context.Background(), log, r, handler); err == nil {
		t.Fatal("Consume returned nil on the second fetch")
	}
	if got := r.lastOffset(); got != 7 {
		t.Errorf("second fetch offset = %d, want the uncommitted 7", got)
	}
}

// errString is the plain error a scenario returns, so the test names no error library.
type errString string

// Error returns the text of the error.
func (e errString) Error() string { return string(e) }

// fakeReader serves messages from a slice and records the commits. It satisfies Fetcher,
// because no test can build a real kafka.Reader without a broker.
type fakeReader struct {
	mu       sync.Mutex
	group    string
	messages []kafka.Message
	fetched  int
	commits  []kafka.Message
}

// FetchMessage returns the next message, and io.EOF when the slice ends.
func (r *fakeReader) FetchMessage(context.Context) (kafka.Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fetched >= len(r.messages) {
		return kafka.Message{}, io.EOF
	}
	msg := r.messages[r.fetched]
	r.fetched++
	return msg, nil
}

// CommitMessages records the committed messages.
func (r *fakeReader) CommitMessages(_ context.Context, msgs ...kafka.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commits = append(r.commits, msgs...)
	return nil
}

// Config returns the reader configuration, which carries the consumer group.
func (r *fakeReader) Config() kafka.ReaderConfig {
	return kafka.ReaderConfig{GroupID: r.group, Topic: "orders"}
}

// committed returns how many messages the reader committed.
func (r *fakeReader) committed() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.commits)
}

// lastOffset returns the offset of the message the reader served last, and -1 when it
// served none.
func (r *fakeReader) lastOffset() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fetched == 0 {
		return -1
	}
	return r.messages[r.fetched-1].Offset
}
