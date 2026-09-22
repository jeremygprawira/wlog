// This file runs the calls conformance suite against the send path, and checks the call record
// and the trace extension that only CloudEvents has.
package wlogcloudevents

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	callsconformance "github.com/jeremygprawira/wlog/internal/conformance/calls"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestCloudEvents_C1_CallsConformance proves that the send path passes every scenario of the
// calls suite.
func TestCloudEvents_C1_CallsConformance(t *testing.T) {
	callsconformance.Run(conformance.Tester{T: t}, callsFactory{})
}

// callsFactory records the call the suite describes, with the real trace extension. The suite
// names the call and its result, because a send is not an http, db, or cache call. The record of
// RecordSendingEvent has its own test below.
type callsFactory struct{}

// Call records one send call and writes the trace extensions of the context.
func (callsFactory) Call(ctx context.Context, _ *wlog.Logger, call wlog.Call, result wlog.CallResult) error {
	ctx, end := wlog.StartCall(ctx, call)
	EventDefaulter()(ctx, newEvent())
	end(result)
	return result.Err
}

// TestCloudEvents_C1_SendCallRecord proves that one sent event records one queue call and carries
// a traceparent whose span id is the span id of that call.
func TestCloudEvents_SendCallRecord(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := tracedContext(t, log)
	event := newEvent()

	_, sent := Observability(log).RecordSendingEvent(ctx, event)
	sent(nil)
	end()

	record := firstCall(t, rec.Last())
	for key, want := range map[string]any{
		"kind": "queue", "system": "cloudevents", "operation": "publish", "target": "orders.created",
		"status": "ack",
	} {
		if record[key] != want {
			t.Errorf("calls[0].%s = %v, want %v", key, record[key], want)
		}
	}
}

// TestCloudEvents_C1_SendTraceNamesTheCall proves that the send hook writes a traceparent
// whose span id is the span id of the open call. The hook writes it, because the SDK runs the
// defaulters before the hook.
func TestCloudEvents_SendTraceNamesTheCall(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := tracedContext(t, log)
	event := newEvent()

	_, sent := Observability(log).RecordSendingEvent(ctx, event)
	sent(nil)
	end()

	parts := strings.Split(fmt.Sprint(event.Extensions()["traceparent"]), "-")
	if len(parts) != 4 || parts[1] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("traceparent = %v, want the trace id of the unit", event.Extensions()["traceparent"])
	}
	record := firstCall(t, rec.Last())
	if parts[2] != record["span_id"] {
		t.Errorf("traceparent span = %q, want the span id of the call %v", parts[2], record["span_id"])
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
