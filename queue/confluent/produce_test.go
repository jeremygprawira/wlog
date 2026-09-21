//go:build cgo

// This file runs the calls conformance suite against the producer path, and checks the call
// record and the delivery report that only confluent has.
package wlogconfluent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	callsconformance "github.com/jeremygprawira/wlog/internal/conformance/calls"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestConfluent_C1_CallsConformance proves that the producer path passes every scenario of
// the calls suite.
func TestConfluent_C1_CallsConformance(t *testing.T) {
	callsconformance.Run(conformance.Tester{T: t}, callsFactory{})
}

// callsFactory records the call the suite describes, with the real header injection. The
// suite names the call and its result, because a Kafka produce is not an http, db, or cache
// call, and the delivery report of a fake cannot carry the error the suite hands in. The
// record of the Produce wrapper has its own tests below.
type callsFactory struct{}

// Call records one produce call with the trace headers of the context.
func (callsFactory) Call(ctx context.Context, _ *wlog.Logger, call wlog.Call, result wlog.CallResult) error {
	ctx, end := wlog.StartCall(ctx, call)
	msg := message(call.Target, 0, 0)
	msg.Headers = withTraceHeaders(ctx, msg.Headers)
	end(result)
	return result.Err
}

// TestConfluent_C1_ProduceCallRecord proves that one produce records one queue call, adds the
// trace headers of the event, and leaves the header slice of the caller alone.
func TestConfluent_C1_ProduceCallRecord(t *testing.T) {
	sender := &fakeSender{}
	log, rec := wlogtest.New(t)

	ctx := log.WithContext(context.Background())
	ctx = propagate.Extract(ctx, propagate.HeaderCarrier{
		"Traceparent": {"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"},
	})
	ctx, end := wlog.Start(ctx, "op")
	sent := message("orders", 2, 9)
	sent.Headers = []kafka.Header{{Key: "x-tenant", Value: []byte("acme")}}
	original := sent.Headers
	if err := Produce(ctx, sender, sent, make(chan kafka.Event, 1)); err != nil {
		t.Fatalf("Produce: %v", err)
	}
	end()

	record := firstCall(t, rec.Last())
	for key, want := range map[string]any{
		"kind": "queue", "system": "kafka", "operation": "publish", "target": "orders", "status": "ok",
	} {
		if record[key] != want {
			t.Errorf("calls[0].%s = %v, want %v", key, record[key], want)
		}
	}
	headers := headerMap(sender.last())
	if headers["x-tenant"] != "acme" {
		t.Errorf("x-tenant = %q, want the header the caller set", headers["x-tenant"])
	}
	parts := strings.Split(headers["traceparent"], "-")
	if len(parts) != 4 || parts[1] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("traceparent = %q, want the trace id of the unit", headers["traceparent"])
	}
	if parts[2] != record["span_id"] {
		t.Errorf("traceparent span = %q, want the span id of the call %v", parts[2], record["span_id"])
	}
	if len(original) != 1 || string(original[0].Value) != "acme" {
		t.Errorf("the header slice of the caller changed: %v", original)
	}
}

// TestConfluent_C1_ProduceDeliveryError proves that a report which carries an error ends the
// call with that error and hands the error back.
func TestConfluent_C1_ProduceDeliveryError(t *testing.T) {
	sender := &fakeSender{reportErr: errString("delivery failed")}
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")

	err := Produce(ctx, sender, message("orders", 0, 0), make(chan kafka.Event, 1))
	if err == nil || err.Error() != "delivery failed" {
		t.Fatalf("Produce returned %v, want the error of the report", err)
	}
	end()

	record := firstCall(t, rec.Last())
	if record["error"] == nil {
		t.Errorf("calls[0].error = nil, want the error of the report")
	}
}

// firstCall returns the first call record of one event.
func firstCall(t *testing.T, event map[string]any) map[string]any {
	t.Helper()
	if event == nil {
		t.Fatal("no event recorded")
	}
	calls, _ := event["calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	record, _ := calls[0].(map[string]any)
	return record
}

// headerMap returns the headers of one message as text.
func headerMap(msg *kafka.Message) map[string]string {
	out := map[string]string{}
	if msg == nil {
		return out
	}
	for _, header := range msg.Headers {
		out[header.Key] = string(header.Value)
	}
	return out
}

// TestConfluent_C1_ProduceRefused proves that a sender which refuses the message ends the call
// with that error, and that a delivery event of type kafka.Error does the same.
func TestConfluent_C1_ProduceRefused(t *testing.T) {
	t.Run("sender error", func(t *testing.T) {
		sender := &fakeSender{sendErr: errString("produce refused")}
		log, rec := wlogtest.New(t)
		ctx, end := wlog.Start(log.WithContext(context.Background()), "op")

		err := Produce(ctx, sender, &kafka.Message{Timestamp: time.Now()}, make(chan kafka.Event, 1))
		if err == nil || err.Error() != "produce refused" {
			t.Fatalf("Produce returned %v, want the error of the sender", err)
		}
		end()
		record := firstCall(t, rec.Last())
		if _, present := record["target"]; present {
			t.Errorf("target = %v, want no target for a message with no topic", record["target"])
		}
		if record["error"] == nil {
			t.Error("calls[0].error = nil, want the error of the sender")
		}
	})

	t.Run("kafka error event", func(t *testing.T) {
		sender := &fakeSender{eventErr: kafka.NewError(kafka.ErrMsgTimedOut, "timed out", false)}
		log, _ := wlogtest.New(t)
		ctx, end := wlog.Start(log.WithContext(context.Background()), "op")

		err := Produce(ctx, sender, message("orders", 0, 0), make(chan kafka.Event, 1))
		if err == nil {
			t.Fatal("Produce returned nil, want the error of the delivery event")
		}
		end()
	})
}
