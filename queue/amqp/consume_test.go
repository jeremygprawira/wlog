// This file runs the work conformance suite against the consumer path, and checks the ack rule
// and the fields that only RabbitMQ has.
package wlogamqp

import (
	"context"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	workconformance "github.com/jeremygprawira/wlog/internal/conformance/work"
	"github.com/jeremygprawira/wlog/wlogtest"
	"github.com/jeremygprawira/wlog/work"
)

// TestAmqp_C1_WorkConformance proves that the consumer event path passes every scenario of the
// work suite.
func TestAmqp_C1_WorkConformance(t *testing.T) {
	workconformance.Run(conformance.Tester{T: t}, workFactory{})
}

// workFactory runs one unit through the path Consume uses. The suite supplies the unit, because
// a delivery carries no job, rpc, command, or function field.
type workFactory struct{}

// Process runs one unit of work and returns what the handler returned.
func (workFactory) Process(log *wlog.Logger, unit work.Unit, handler func(context.Context) error) error {
	return process(context.Background(), log, unit, handler)
}

// TestAmqp_C1_ConsumeAcksAfterSuccess proves that the loop acks a delivery after the handler
// returns nil, and records the fields of the delivery.
func TestAmqp_ConsumeAcksAfterSuccess(t *testing.T) {
	acknowledger := &fakeAcknowledger{}
	delivery := amqp.Delivery{
		Acknowledger: acknowledger,
		MessageId:    "msg-1",
		Timestamp:    time.Now().Add(-2 * time.Second),
		Redelivered:  true,
		Exchange:     "orders",
		RoutingKey:   "orders.created",
		Headers: amqp.Table{
			"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		},
	}
	log, rec := wlogtest.New(t)

	if err := Consume(context.Background(), log, closed(delivery), "orders", func(context.Context, amqp.Delivery) error { return nil }); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if acknowledger.acks != 1 || acknowledger.nacks != 0 {
		t.Errorf("acks/nacks = %d/%d, want 1/0", acknowledger.acks, acknowledger.nacks)
	}

	got := rec.Last()
	if got["operation"] != "process orders" {
		t.Errorf("operation = %v, want process orders", got["operation"])
	}
	messaging, _ := got["messaging"].(map[string]any)
	for key, want := range map[string]any{
		"system": "rabbitmq", "operation": "process", "destination": "orders",
		"message_id": "msg-1", "redelivered": true,
	} {
		if messaging[key] != want {
			t.Errorf("messaging.%s = %v, want %v", key, messaging[key], want)
		}
	}
	if lag, _ := messaging["lag_ms"].(float64); lag < 1900 || lag > 2100 {
		t.Errorf("messaging.lag_ms = %v, want about 2000", messaging["lag_ms"])
	}
	rabbitmq, _ := messaging["rabbitmq"].(map[string]any)
	for key, want := range map[string]any{"exchange": "orders", "routing_key": "orders.created"} {
		if rabbitmq[key] != want {
			t.Errorf("messaging.rabbitmq.%s = %v, want %v", key, rabbitmq[key], want)
		}
	}
	trace, _ := got["trace"].(map[string]any)
	if trace["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace.trace_id = %v, want the trace id of the header", trace["trace_id"])
	}
}

// TestAmqp_C1_ConsumeNacksFailedDelivery proves that a failed handler nacks the delivery, with a
// requeue by default and without one when RequeueOn does not match.
func TestAmqp_ConsumeNacksFailedDelivery(t *testing.T) {
	failure := errString("handler failed")

	t.Run("requeue by default", func(t *testing.T) {
		acknowledger := &fakeAcknowledger{}
		log, rec := wlogtest.New(t)
		err := Consume(context.Background(), log, closed(amqp.Delivery{Acknowledger: acknowledger}), "orders",
			func(context.Context, amqp.Delivery) error { return failure })
		if err != nil {
			t.Fatalf("Consume: %v", err)
		}
		if acknowledger.nacks != 1 || !acknowledger.requeue {
			t.Errorf("nacks/requeue = %d/%v, want 1/true", acknowledger.nacks, acknowledger.requeue)
		}
		if level := rec.Last()["level"]; level != "error" {
			t.Errorf("level = %v, want error", level)
		}
	})

	t.Run("no requeue when RequeueOn does not match", func(t *testing.T) {
		acknowledger := &fakeAcknowledger{}
		log, _ := wlogtest.New(t)
		err := Consume(context.Background(), log, closed(amqp.Delivery{Acknowledger: acknowledger}), "orders",
			func(context.Context, amqp.Delivery) error { return failure }, RequeueOn(errString("another failure")))
		if err != nil {
			t.Fatalf("Consume: %v", err)
		}
		if acknowledger.nacks != 1 || acknowledger.requeue {
			t.Errorf("nacks/requeue = %d/%v, want 1/false", acknowledger.nacks, acknowledger.requeue)
		}
	})

	t.Run("requeue when RequeueOn matches", func(t *testing.T) {
		acknowledger := &fakeAcknowledger{}
		log, _ := wlogtest.New(t)
		err := Consume(context.Background(), log, closed(amqp.Delivery{Acknowledger: acknowledger}), "orders",
			func(context.Context, amqp.Delivery) error { return failure }, RequeueOn(failure))
		if err != nil {
			t.Fatalf("Consume: %v", err)
		}
		if acknowledger.nacks != 1 || !acknowledger.requeue {
			t.Errorf("nacks/requeue = %d/%v, want 1/true", acknowledger.nacks, acknowledger.requeue)
		}
	})
}

// closed returns a delivery channel that holds one delivery and is closed.
func closed(delivery amqp.Delivery) <-chan amqp.Delivery {
	deliveries := make(chan amqp.Delivery, 1)
	deliveries <- delivery
	close(deliveries)
	return deliveries
}

// errString is the plain error a scenario returns, so the test names no error library.
type errString string

// Error returns the text of the error.
func (e errString) Error() string { return string(e) }

// process runs one unit of work through the event path with a recovered panic, so the
// conformance suite continues after the panic scenario. The real entries record a panic and
// raise it again, which is the rule of the track spec.
func process(ctx context.Context, log *wlog.Logger, u work.Unit, handler func(context.Context) error) error {
	return work.Run(ctx, log, u, handler, work.RecoverPanics())
}
