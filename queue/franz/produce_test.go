// This file runs the calls conformance suite against the producer path, and checks the
// produce hooks and the fetch hook.
package wlogfranz

import (
	"context"
	"strings"
	"testing"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	callsconformance "github.com/jeremygprawira/wlog/internal/conformance/calls"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestFranz_C1_CallsConformance proves that the producer path passes every scenario of the
// calls suite.
func TestFranz_C1_CallsConformance(t *testing.T) {
	callsconformance.Run(conformance.Tester{T: t}, callsFactory{})
}

// callsFactory records the call the suite describes, with the real header injection. The
// suite names the call and its result, because a Kafka produce is not an http, db, or cache
// call. The record of the produce hooks has its own test below.
type callsFactory struct{}

// Call records one produce call with the trace headers of the context.
func (callsFactory) Call(ctx context.Context, _ *wlog.Logger, call wlog.Call, result wlog.CallResult) error {
	ctx, end := wlog.StartCall(ctx, call)
	r := &kgo.Record{Topic: call.Target, Value: []byte("conformance")}
	r.Headers = withTraceHeaders(ctx, r.Headers)
	end(result)
	return result.Err
}

// TestFranz_C1_ProduceHooksRecordCall proves that the produce hooks record one queue call
// and add the trace headers of the record context.
func TestFranz_C1_ProduceHooksRecordCall(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx := log.WithContext(context.Background())
	ctx = propagate.Extract(ctx, propagate.HeaderCarrier{
		"Traceparent": {"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"},
	})
	ctx, end := wlog.Start(ctx, "op")
	r := &kgo.Record{Topic: "orders", Value: []byte("payload"), Context: ctx}

	h := hooks{}
	h.OnProduceRecordBuffered(r)
	h.OnProduceRecordUnbuffered(r, nil)
	end()

	record := firstCall(t, rec.Last())
	for key, want := range map[string]any{
		"kind": "queue", "system": "kafka", "operation": "publish", "target": "orders", "status": "ok",
	} {
		if record[key] != want {
			t.Errorf("calls[0].%s = %v, want %v", key, record[key], want)
		}
	}
	parts := strings.Split(headerMap(r)["traceparent"], "-")
	if len(parts) != 4 || parts[1] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("traceparent = %q, want the trace id of the unit", headerMap(r)["traceparent"])
	}
	if parts[2] != record["span_id"] {
		t.Errorf("traceparent span = %q, want the span id of the call %v", parts[2], record["span_id"])
	}
}

// TestFranz_C1_FetchHookSetsTrace proves that the fetch hook reads the trace headers of a
// record into its context, so the event of Record joins the trace of the producer.
func TestFranz_C1_FetchHookSetsTrace(t *testing.T) {
	r := &kgo.Record{
		Topic: "orders",
		Headers: []kgo.RecordHeader{{
			Key:   "traceparent",
			Value: []byte("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"),
		}},
	}

	hooks{}.OnFetchRecordBuffered(r)

	trace, ok := propagate.FromContext(r.Context)
	if !ok {
		t.Fatal("the record context carries no trace")
	}
	if trace.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace id = %q, want the trace id of the header", trace.TraceID)
	}
}

// TestFranz_C1_HooksImplementTheInterfaces proves at build time that one Hooks value covers
// both sides of a client.
func TestFranz_C1_HooksImplementTheInterfaces(t *testing.T) {
	hook := Hooks()
	for name, ok := range map[string]bool{
		"produce buffered":   implements[kgo.HookProduceRecordBuffered](hook),
		"produce unbuffered": implements[kgo.HookProduceRecordUnbuffered](hook),
		"fetch buffered":     implements[kgo.HookFetchRecordBuffered](hook),
	} {
		if !ok {
			t.Errorf("Hooks does not implement the %s hook", name)
		}
	}
}

// implements reports whether one value satisfies the hook interface T.
func implements[T any](value any) bool {
	_, ok := value.(T)
	return ok
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

// headerMap returns the headers of one record as text.
func headerMap(r *kgo.Record) map[string]string {
	out := map[string]string{}
	for _, header := range r.Headers {
		out[header.Key] = string(header.Value)
	}
	return out
}
