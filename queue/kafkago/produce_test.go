// This file runs the calls conformance suite against the producer path, and checks what
// only a Kafka writer has: the trace headers of each message, and the async call that ends
// when the broker reports its batch.
package wlogkafkago

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	callsconformance "github.com/jeremygprawira/wlog/internal/conformance/calls"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestKafka_C1_CallsConformance proves that the producer path passes every scenario of the
// calls suite.
func TestKafka_C1_CallsConformance(t *testing.T) {
	callsconformance.Run(conformance.Tester{T: t}, newCallsFactory())
}

// callsFactory makes one produce call per scenario and records the call the suite
// describes.
//
// The suite names the call and its result, because a Kafka write is not an http, db, or
// cache call, and the write itself cannot fail with the error the suite hands in. The
// factory therefore writes through the header path of the adapter, and records the call
// and the result the suite described. The record of the adapter's own path has its own
// test below.
type callsFactory struct {
	w *kafka.Writer
}

// newCallsFactory builds the factory around one writer with a fake broker.
func newCallsFactory() *callsFactory {
	return &callsFactory{w: &kafka.Writer{
		Addr:         kafka.TCP("broker:9092"),
		Topic:        "conformance",
		Transport:    &fakeBroker{},
		BatchTimeout: time.Millisecond,
	}}
}

// Call makes one produce call with the trace headers of the context, and records the call
// the suite describes.
func (f *callsFactory) Call(ctx context.Context, _ *wlog.Logger, call wlog.Call, result wlog.CallResult) error {
	ctx, end := wlog.StartCall(ctx, call)
	msg := kafka.Message{Value: []byte("conformance")}
	msg.Headers = withTraceHeaders(ctx, msg.Headers)
	if err := f.w.WriteMessages(ctx, msg); err != nil {
		end(wlog.CallResult{Err: err})
		return err
	}
	end(result)
	return result.Err
}

// TestKafka_C1_ProducerCallRecord proves that one write records one queue call with the
// topic as its target, and that each message carries a traceparent whose span id is the
// span id of that call.
func TestKafka_ProducerCallRecord(t *testing.T) {
	broker := &fakeBroker{}
	w := &kafka.Writer{
		Addr:         kafka.TCP("broker:9092"),
		Topic:        "orders",
		Transport:    broker,
		BatchTimeout: time.Millisecond,
	}
	p := Writer(w)
	log, rec := wlogtest.New(t)

	ctx := log.WithContext(context.Background())
	ctx = propagate.Extract(ctx, propagate.HeaderCarrier{
		"Traceparent": {"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"},
	})
	ctx, end := wlog.Start(ctx, "op")
	sent := kafka.Message{Value: []byte("payload"), Headers: []kafka.Header{{Key: "x-tenant", Value: []byte("acme")}}}
	if err := p.WriteMessages(ctx, sent); err != nil {
		t.Fatalf("WriteMessages: %v", err)
	}
	end()

	calls, _ := rec.Last()["calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	record, _ := calls[0].(map[string]any)
	for key, want := range map[string]any{
		"kind": "queue", "system": "kafka", "operation": "publish", "target": "orders", "status": "ok",
	} {
		if record[key] != want {
			t.Errorf("calls[0].%s = %v, want %v", key, record[key], want)
		}
	}

	msgs := broker.messages()
	if len(msgs) != 1 {
		t.Fatalf("records = %d, want 1", len(msgs))
	}
	headers := map[string]string{}
	for _, header := range msgs[0].Headers {
		headers[header.Key] = string(header.Value)
	}
	if headers["x-tenant"] != "acme" {
		t.Errorf("x-tenant = %q, want the header the caller set", headers["x-tenant"])
	}
	if len(sent.Headers) != 1 || string(sent.Headers[0].Value) != "acme" {
		t.Errorf("the caller's message headers changed: %v", sent.Headers)
	}
	parts := strings.Split(headers["traceparent"], "-")
	if len(parts) != 4 || parts[1] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("traceparent = %q, want the trace id of the unit", headers["traceparent"])
	}
	if parts[2] != record["span_id"] {
		t.Errorf("traceparent span = %q, want the span id of the call %v", parts[2], record["span_id"])
	}
}

// TestKafka_C1_AsyncEndsInCompletion proves that an async write ends its call when the
// broker reports the batch, and not when WriteMessages returns.
func TestKafka_AsyncEndsInCompletion(t *testing.T) {
	broker := &fakeBroker{hold: make(chan struct{})}
	broker.failWith(errString("broker refused the batch"))
	w := &kafka.Writer{
		Addr:         kafka.TCP("broker:9092"),
		Topic:        "orders",
		Async:        true,
		MaxAttempts:  1,
		Transport:    broker,
		BatchTimeout: time.Millisecond,
	}
	p := Writer(w)
	// The test wraps the dispatcher, so it knows when the batch result arrived.
	dispatched := make(chan struct{})
	inner := w.Completion
	w.Completion = func(msgs []kafka.Message, err error) {
		inner(msgs, err)
		close(dispatched)
	}
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")

	if err := p.WriteMessages(ctx, kafka.Message{Value: []byte("payload")}); err != nil {
		t.Fatalf("WriteMessages returned %v, want nil in the async mode", err)
	}
	close(broker.hold)
	<-dispatched
	end()

	calls, _ := rec.Last()["calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	record, _ := calls[0].(map[string]any)
	if record["error"] == nil {
		t.Errorf("calls[0].error = nil, want the broker error the Completion reported")
	}
}

// TestKafka_C1_CallEndWaitsForEveryBatch proves that a call ends after its last batch
// reports, and that an error of any batch wins. kafka-go reports one Completion per partition
// batch, so the first report must not decide the call.
func TestKafka_CallEndWaitsForEveryBatch(t *testing.T) {
	var results []wlog.CallResult
	finished := &callEnd{end: func(result wlog.CallResult) { results = append(results, result) }}
	finished.expect(3)

	finished.report(nil, 1)
	if len(results) != 0 {
		t.Fatalf("the call ended after one of three messages")
	}
	finished.report(errString("refused"), 2)
	if len(results) != 1 {
		t.Fatalf("results = %d, want one", len(results))
	}
	if results[0].Err == nil {
		t.Error("the call result is ok, want the error of the failed batch")
	}
}
