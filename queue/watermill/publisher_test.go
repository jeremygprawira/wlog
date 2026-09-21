// This file runs the calls conformance suite against the publisher path, and checks the call
// record and the trace metadata that only the decorator has.
package wlogwatermill

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/ThreeDotsLabs/watermill/message"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	callsconformance "github.com/jeremygprawira/wlog/internal/conformance/calls"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestWatermill_C1_CallsConformance proves that the publisher path passes every scenario of
// the calls suite.
func TestWatermill_C1_CallsConformance(t *testing.T) {
	callsconformance.Run(conformance.Tester{T: t}, callsFactory{})
}

// callsFactory records the call the suite describes, with the real trace metadata. The suite
// names the call and its result, because a publish is not an http, db, or cache call. The
// record of the decorator has its own test below.
type callsFactory struct{}

// Call records one publish call and writes the trace metadata of the context.
func (callsFactory) Call(ctx context.Context, _ *wlog.Logger, call wlog.Call, result wlog.CallResult) error {
	ctx, end := wlog.StartCall(ctx, call)
	withTraceMetadata(ctx, message.NewMessage("", nil))
	end(result)
	return result.Err
}

// TestWatermill_C1_PublisherCallRecord proves that one publish records one queue call and
// writes a traceparent whose span id is the span id of that call.
func TestWatermill_C1_PublisherCallRecord(t *testing.T) {
	pub := &fakePublisher{}
	decorated, err := PublisherDecorator()(pub)
	if err != nil {
		t.Fatalf("PublisherDecorator: %v", err)
	}
	log, rec := wlogtest.New(t)

	ctx := log.WithContext(context.Background())
	ctx = propagate.Extract(ctx, propagate.HeaderCarrier{
		"Traceparent": {"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"},
	})
	ctx, end := wlog.Start(ctx, "op")
	msg := message.NewMessage("msg-1", nil)
	msg.SetContext(ctx)
	if err := decorated.Publish("orders", msg); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	end()

	record := firstCall(t, rec.Last())
	for key, want := range map[string]any{
		"kind": "queue", "system": "watermill", "operation": "publish", "target": "orders", "status": "ok",
	} {
		if record[key] != want {
			t.Errorf("calls[0].%s = %v, want %v", key, record[key], want)
		}
	}
	parts := strings.Split(msg.Metadata.Get("traceparent"), "-")
	if len(parts) != 4 || parts[1] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("traceparent = %q, want the trace id of the unit", msg.Metadata.Get("traceparent"))
	}
	if parts[2] != record["span_id"] {
		t.Errorf("traceparent span = %q, want the span id of the call %v", parts[2], record["span_id"])
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

// fakePublisher records every publish and closes nothing.
type fakePublisher struct {
	mu       sync.Mutex
	messages []*message.Message
}

// Publish records one batch of messages.
func (p *fakePublisher) Publish(_ string, messages ...*message.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.messages = append(p.messages, messages...)
	return nil
}

// Close closes nothing.
func (*fakePublisher) Close() error { return nil }
