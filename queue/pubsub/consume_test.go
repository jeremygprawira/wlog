// This file runs the work conformance suite against the consumer path, and checks the fields and
// the receive rule that only Pub/Sub has.
package wlogpubsub

import (
	"context"
	"fmt"
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

// workFactory drives the real Receive path with a fake subscription, so a change that breaks
// the adapter fails the suite.
type workFactory struct{}

// Declare names the one kind a Pub/Sub consumer produces. Pub/Sub reports a delivery attempt.
func (workFactory) Declare() workconformance.Declaration {
	return workconformance.Declaration{
		Kinds: []work.Kind{work.KindMessage}, System: "gcp_pubsub", DeliveryCount: true,
	}
}

// Process runs one unit of work through Receive. The suite expects no panic from Process, so
// the panic of the handler, which Receive raises again, comes back as an error.
func (workFactory) Process(log *wlog.Logger, unit work.Unit, handler func(context.Context) error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic: %v", recovered)
		}
	}()
	destination, _ := unit.Fields["destination"].(string)
	msg := &pubsub.Message{ID: "msg-1", PublishTime: unit.StartedAt}
	if count, ok := unit.Fields["delivery_count"].(int); ok {
		msg.DeliveryAttempt = &count
	}
	sub := &fakeSubscriber{id: destination, messages: []*pubsub.Message{msg}}
	var handlerErr error
	_ = Receive(context.Background(), log, sub, func(ctx context.Context, _ *pubsub.Message) error {
		handlerErr = handler(ctx)
		return handlerErr
	})
	// The loop reports the read error that ended it. The suite asks for the result of the
	// handler, so the factory reports that one.
	return handlerErr
}

// TestPubsub_C1_ReceiveFields proves that one message fills the messaging group, the delivery
// count, the lag, and the trace carrier, including the googclient prefix of the client.
func TestPubsub_ReceiveFields(t *testing.T) {
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
func TestPubsub_FailedHandlerRecordsError(t *testing.T) {
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
