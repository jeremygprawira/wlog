// This file runs the work conformance suite against the consumer path, and checks the fields and
// the receive rule that only Pub/Sub has.
package wlogpubsub

import (
	"context"
	"testing"
	"time"

	"cloud.google.com/go/pubsub/v2"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	workconformance "github.com/jeremygprawira/wlog/internal/conformance/work"
	"github.com/jeremygprawira/wlog/wlogtest"
	"github.com/jeremygprawira/wlog/work"
)

// TestPubsub_C1_WorkConformance proves that the consumer event path passes every scenario of the
// work suite.
func TestPubsub_C1_WorkConformance(t *testing.T) {
	workconformance.Run(conformance.Tester{T: t}, workFactory{})
}

// workFactory runs one unit through the path Receive uses. The suite supplies the unit, because a
// Pub/Sub message carries no job, rpc, command, or function field.
type workFactory struct{}

// Process runs one unit of work and returns what the handler returned.
func (workFactory) Process(log *wlog.Logger, unit work.Unit, handler func(context.Context) error) error {
	return process(context.Background(), log, unit, handler)
}

// TestPubsub_C1_ReceiveFields proves that one message fills the messaging group, the delivery
// count, the lag, and the trace carrier, including the googclient prefix of the client.
func TestPubsub_C1_ReceiveFields(t *testing.T) {
	attempt := 3
	msg := &pubsub.Message{
		ID:              "msg-1",
		PublishTime:     time.Now().Add(-2 * time.Second),
		DeliveryAttempt: &attempt,
		Attributes: map[string]string{
			"googclient_traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		},
	}
	sub := &fakeSubscriber{id: "orders-sub", messages: []*pubsub.Message{msg}}
	log, rec := wlogtest.New(t)

	if err := Receive(context.Background(), log, sub, func(context.Context, *pubsub.Message) error { return nil }); err != nil {
		t.Fatalf("Receive: %v", err)
	}

	got := rec.Last()
	if got == nil {
		t.Fatal("no event recorded")
	}
	if got["operation"] != "process orders-sub" {
		t.Errorf("operation = %v, want process orders-sub", got["operation"])
	}
	messaging, _ := got["messaging"].(map[string]any)
	for key, want := range map[string]any{
		"system": "gcp_pubsub", "operation": "process", "destination": "orders-sub",
		"message_id": "msg-1", "delivery_count": 3,
	} {
		if !conformance.Equal(messaging[key], want) {
			t.Errorf("messaging.%s = %v, want %v", key, messaging[key], want)
		}
	}
	if lag, _ := messaging["lag_ms"].(float64); lag < 1900 || lag > 2100 {
		t.Errorf("messaging.lag_ms = %v, want about 2000", messaging["lag_ms"])
	}
	trace, _ := got["trace"].(map[string]any)
	if trace["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace.trace_id = %v, want the trace id of the googclient attribute", trace["trace_id"])
	}
}

// TestPubsub_C1_FailedHandlerRecordsError proves that a failed handler records an error event and
// leaves the message to its acknowledgement rule. The ack itself needs a live service, which is an
// integration step.
func TestPubsub_C1_FailedHandlerRecordsError(t *testing.T) {
	sub := &fakeSubscriber{id: "orders-sub", messages: []*pubsub.Message{{ID: "msg-1"}}}
	log, rec := wlogtest.New(t)

	err := Receive(context.Background(), log, sub, func(context.Context, *pubsub.Message) error {
		return errString("handler failed")
	})
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if level := rec.Last()["level"]; level != "error" {
		t.Errorf("level = %v, want error", level)
	}
}

// errString is the plain error a scenario returns, so the test names no error library.
type errString string

// Error returns the text of the error.
func (e errString) Error() string { return string(e) }
