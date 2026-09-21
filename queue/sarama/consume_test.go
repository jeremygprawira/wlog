// This file runs the work conformance suite against the consumer path, and checks the
// fields and the mark rule that only sarama has.
package wlogsarama

import (
	"context"
	"testing"
	"time"

	"github.com/IBM/sarama"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	workconformance "github.com/jeremygprawira/wlog/internal/conformance/work"
	"github.com/jeremygprawira/wlog/wlogtest"
	"github.com/jeremygprawira/wlog/work"
)

// TestSarama_C1_WorkConformance proves that the consumer event path passes every scenario
// of the work suite.
func TestSarama_C1_WorkConformance(t *testing.T) {
	workconformance.Run(conformance.Tester{T: t}, workFactory{})
}

// workFactory runs one unit through the path Handler uses. The suite supplies the unit,
// because a sarama message carries no job, rpc, command, or function field.
type workFactory struct{}

// Process runs one unit of work and returns what the handler returned.
func (workFactory) Process(log *wlog.Logger, unit work.Unit, handler func(context.Context) error) error {
	return process(context.Background(), log, unit, handler)
}

// TestSarama_C1_HandlerMarksAfterSuccess proves that the claim loop marks a message after
// the handler returns nil, and leaves a failed message unmarked.
func TestSarama_C1_HandlerMarksAfterSuccess(t *testing.T) {
	claim := &fakeClaim{
		topic: "orders", partition: 1, highWaterMark: 10,
		messages: []*sarama.ConsumerMessage{
			{Topic: "orders", Partition: 1, Offset: 7, Timestamp: time.Now()},
			{Topic: "orders", Partition: 1, Offset: 8, Timestamp: time.Now()},
		},
	}
	session := newFakeSession()
	log, rec := wlogtest.New(t)

	handler := Handler(log, func(_ context.Context, msg *sarama.ConsumerMessage) error {
		if msg.Offset == 8 {
			return errString("handler failed")
		}
		return nil
	})
	if err := handler.ConsumeClaim(session, claim); err != nil {
		t.Fatalf("ConsumeClaim: %v", err)
	}

	marked := session.markedOffsets()
	if len(marked) != 1 || marked[0] != 7 {
		t.Errorf("marked offsets = %v, want only 7", marked)
	}
	if count := len(rec.Events()); count != 2 {
		t.Errorf("events = %d, want 2", count)
	}
}

// TestSarama_C1_MessageFields proves that one message fills the messaging group and the
// kafka member fields.
func TestSarama_C1_MessageFields(t *testing.T) {
	msg := &sarama.ConsumerMessage{
		Topic: "orders", Partition: 2, Offset: 41, Timestamp: time.Now().Add(-2 * time.Second),
		Headers: []*sarama.RecordHeader{{
			Key:   []byte("traceparent"),
			Value: []byte("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"),
		}},
	}
	claim := &fakeClaim{topic: "orders", partition: 2, highWaterMark: 100}
	session := newFakeSession()
	log, rec := wlogtest.New(t)

	ctx, end := Message(log, session, claim, msg)
	if ctx == nil {
		t.Fatal("Message returned no context")
	}
	end(nil)

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
		"partition": 2, "offset": 41,
	} {
		if !conformance.Equal(messaging[key], want) {
			t.Errorf("messaging.%s = %v, want %v", key, messaging[key], want)
		}
	}
	if lag, _ := messaging["lag_ms"].(float64); lag < 1900 || lag > 2100 {
		t.Errorf("messaging.lag_ms = %v, want about 2000", messaging["lag_ms"])
	}
	kafkaFields, _ := messaging["kafka"].(map[string]any)
	for key, want := range map[string]any{"member_id": "member-1", "generation": 3, "offset_lag": 58} {
		if !conformance.Equal(kafkaFields[key], want) {
			t.Errorf("messaging.kafka.%s = %v, want %v", key, kafkaFields[key], want)
		}
	}
	trace, _ := got["trace"].(map[string]any)
	if trace["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace.trace_id = %v, want the trace id of the header", trace["trace_id"])
	}
}

// errString is the plain error a scenario returns, so the test names no error library.
type errString string

// Error returns the text of the error.
func (e errString) Error() string { return string(e) }
