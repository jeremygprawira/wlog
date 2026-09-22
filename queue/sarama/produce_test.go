// This file runs the calls conformance suite against the producer path, and checks what
// only the sarama wrappers have: the trace headers, the call record, and the async result.
package wlogsarama

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/IBM/sarama"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	callsconformance "github.com/jeremygprawira/wlog/internal/conformance/calls"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestSarama_C1_CallsConformance proves that the producer path passes every scenario of the
// calls suite.
func TestSarama_C1_CallsConformance(t *testing.T) {
	callsconformance.Run(conformance.Tester{T: t}, &callsFactory{p: &fakeSyncProducer{}})
}

// callsFactory makes one produce call per scenario and records the call the suite
// describes. The suite names the call and its result, because a Kafka write is not an http,
// db, or cache call. The record of the wrapper's own path has its own test below.
type callsFactory struct{ p *fakeSyncProducer }

// Call makes one produce call with the trace headers of the context, and records the call
// the suite describes.
func (f *callsFactory) Call(ctx context.Context, _ *wlog.Logger, call wlog.Call, result wlog.CallResult) error {
	ctx, end := wlog.StartCall(ctx, call)
	msg := &sarama.ProducerMessage{Topic: call.Target, Value: sarama.ByteEncoder("conformance")}
	msg.Headers = withTraceHeaders(ctx, msg.Headers)
	if _, _, err := f.p.SendMessage(msg); err != nil {
		end(wlog.CallResult{Err: err})
		return err
	}
	end(result)
	return result.Err
}

// TestSarama_C1_SyncProducerCallRecord proves that one send records one queue call, adds the
// trace headers of the event, and leaves the headers of the caller alone.
func TestSarama_SyncProducerCallRecord(t *testing.T) {
	fake := &fakeSyncProducer{}
	p := SyncProducer(fake)
	log, rec := wlogtest.New(t)

	ctx := log.WithContext(context.Background())
	ctx = propagate.Extract(ctx, propagate.HeaderCarrier{
		"Traceparent": {"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"},
	})
	ctx, end := wlog.Start(ctx, "op")
	sent := &sarama.ProducerMessage{
		Topic: "orders", Value: sarama.ByteEncoder("payload"),
		Headers: []sarama.RecordHeader{{Key: []byte("x-tenant"), Value: []byte("acme")}},
	}
	original := sent.Headers
	if _, _, err := p.SendMessage(ctx, sent); err != nil {
		t.Fatalf("SendMessage: %v", err)
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
	headers := headerMap(fake.last())
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

// TestSarama_C1_AsyncProducerCallRecord proves that an async send records one call that ends
// when the producer reports the message on Successes.
func TestSarama_AsyncProducerCallRecord(t *testing.T) {
	fake := newFakeAsyncProducer()
	cfg := sarama.NewConfig()
	cfg.Producer.Return.Successes = true
	p, err := AsyncProducer(fake, cfg)
	if err != nil {
		t.Fatalf("AsyncProducer: %v", err)
	}
	log, rec := wlogtest.New(t)

	ctx := log.WithContext(context.Background())
	ctx = propagate.Extract(ctx, propagate.HeaderCarrier{
		"Traceparent": {"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"},
	})
	ctx, end := wlog.Start(ctx, "op")
	msg := &sarama.ProducerMessage{Topic: "orders", Value: sarama.ByteEncoder("payload")}
	p.Send(ctx, msg)

	queued := <-fake.input
	fake.successes <- queued
	select {
	case got := <-p.Successes():
		if got != msg {
			t.Errorf("Successes gave %v, want the sent message", got)
		}
	case <-time.After(time.Second):
		t.Fatal("the wrapper reported no success")
	}
	end()

	record := firstCall(t, rec.Last())
	if record["status"] != "ok" {
		t.Errorf("calls[0].status = %v, want ok", record["status"])
	}
	if headerMap(msg)["traceparent"] == "" {
		t.Error("the async message carries no traceparent")
	}
}

// TestSarama_C1_AsyncErrorEndsTheCall proves that a failure report ends the call with the
// error of the broker.
func TestSarama_C1_AsyncErrorEndsTheCall(t *testing.T) {
	fake := newFakeAsyncProducer()
	cfg := sarama.NewConfig()
	cfg.Producer.Return.Successes = true
	p, err := AsyncProducer(fake, cfg)
	if err != nil {
		t.Fatalf("AsyncProducer: %v", err)
	}
	log, rec := wlogtest.New(t)

	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "op")
	msg := &sarama.ProducerMessage{Topic: "orders", Value: sarama.ByteEncoder("payload")}
	p.Send(ctx, msg)

	queued := <-fake.input
	fake.failures <- &sarama.ProducerError{Msg: queued, Err: errString("refused")}
	select {
	case got := <-p.Errors():
		if got == nil {
			t.Fatal("Errors gave nothing")
		}
	case <-time.After(time.Second):
		t.Fatal("the wrapper reported no failure")
	}
	end()

	record := firstCall(t, rec.Last())
	if record["error"] == nil {
		t.Error("calls[0].error = nil, want the error of the broker")
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
func headerMap(msg *sarama.ProducerMessage) map[string]string {
	out := map[string]string{}
	if msg == nil {
		return out
	}
	for _, header := range msg.Headers {
		out[string(header.Key)] = string(header.Value)
	}
	return out
}
