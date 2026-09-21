// This file runs the calls conformance suite against the producer path, and checks the call
// record and the trace header that only this adapter has.
package wlogamqp

import (
	"context"
	"strings"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	callsconformance "github.com/jeremygprawira/wlog/internal/conformance/calls"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestAmqp_C1_CallsConformance proves that the producer path passes every scenario of the calls
// suite.
func TestAmqp_C1_CallsConformance(t *testing.T) {
	callsconformance.Run(conformance.Tester{T: t}, callsFactory{})
}

// callsFactory records the call the suite describes, with the real trace headers. The suite
// names the call and its result, because a publish is not an http, db, or cache call. The record
// of PublishWithContext has its own test below.
type callsFactory struct{}

// Call records one publish call and writes the trace headers of the context.
func (callsFactory) Call(ctx context.Context, _ *wlog.Logger, call wlog.Call, result wlog.CallResult) error {
	ctx, end := wlog.StartCall(ctx, call)
	withTraceHeaders(ctx, amqp.Table{})
	end(result)
	return result.Err
}

// TestAmqp_C1_PublishCallRecord proves that one publish records one queue call and writes a
// traceparent whose span id is the span id of that call.
func TestAmqp_C1_PublishCallRecord(t *testing.T) {
	pub := &fakePublisher{}
	log, rec := wlogtest.New(t)
	ctx, end := tracedContext(t, log)

	if err := PublishWithContext(ctx, pub, "orders", "orders.created", false, false, amqp.Publishing{
		MessageId: "msg-1",
		Body:      []byte("payload"),
	}); err != nil {
		t.Fatalf("PublishWithContext: %v", err)
	}
	end()

	record := firstCall(t, rec.Last())
	for key, want := range map[string]any{
		"kind": "queue", "system": "rabbitmq", "operation": "publish", "target": "orders.created", "status": "ok",
	} {
		if record[key] != want {
			t.Errorf("calls[0].%s = %v, want %v", key, record[key], want)
		}
	}
	parts := strings.Split(pub.last().Headers["traceparent"].(string), "-")
	if len(parts) != 4 || parts[1] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("traceparent = %v, want the trace id of the unit", pub.last().Headers["traceparent"])
	}
	if parts[2] != record["span_id"] {
		t.Errorf("traceparent span = %q, want the span id of the call %v", parts[2], record["span_id"])
	}
}

// TestAmqp_C1_PublishError proves that a publish the channel refuses records the error and hands
// it back.
func TestAmqp_C1_PublishError(t *testing.T) {
	pub := &fakePublisher{err: errString("publish refused")}
	log, rec := wlogtest.New(t)
	ctx, end := tracedContext(t, log)

	err := PublishWithContext(ctx, pub, "orders", "orders.created", false, false, amqp.Publishing{})
	if err == nil || err.Error() != "publish refused" {
		t.Fatalf("PublishWithContext returned %v, want the error of the channel", err)
	}
	end()

	if record := firstCall(t, rec.Last()); record["error"] == nil {
		t.Error("calls[0].error = nil, want the error of the channel")
	}
}

// tracedContext starts one event with a trace, and returns its context and the end func.
func tracedContext(t *testing.T, log *wlog.Logger) (context.Context, func()) {
	t.Helper()
	ctx := log.WithContext(context.Background())
	ctx = propagate.Extract(ctx, propagate.HeaderCarrier{
		"Traceparent": {"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"},
	})
	return wlog.Start(ctx, "op")
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
