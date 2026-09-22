// This file runs the calls conformance suite against the producer path, and checks the call
// record and the trace header of one enqueued task.
package wlogasynq

import (
	"context"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	callsconformance "github.com/jeremygprawira/wlog/internal/conformance/calls"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestAsynq_C1_CallsConformance proves that the producer path passes every scenario of the
// calls suite.
func TestAsynq_C1_CallsConformance(t *testing.T) {
	callsconformance.Run(conformance.Tester{T: t}, callsFactory{})
}

// callsFactory records the call the suite describes, with the real trace header. The suite
// names the call and its result, because an enqueue is not an http, db, or cache call.
type callsFactory struct{}

// Call records one enqueue call and writes the trace header of the context.
func (callsFactory) Call(ctx context.Context, _ *wlog.Logger, call wlog.Call, result wlog.CallResult) error {
	ctx, end := wlog.StartCall(ctx, call)
	_ = traceHeaders(ctx)
	end(result)
	return result.Err
}

// TestAsynq_C1_EnqueueCallRecord proves that one enqueue records one queue call, and the task
// carries a traceparent header whose span id is the span id of that call.
func TestAsynq_EnqueueCallRecord(t *testing.T) {
	client := &fakeClient{}
	log, rec := wlogtest.New(t)
	ctx, end := tracedContext(t, log)

	if _, err := Enqueue(ctx, client, "reindex:orders", []byte("payload")); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	end()

	record := firstCall(t, rec.Last())
	for key, want := range map[string]any{
		"kind": "queue", "system": "asynq", "operation": "enqueue",
		"target": "reindex:orders", "status": "ok",
	} {
		if record[key] != want {
			t.Errorf("calls[0].%s = %v, want %v", key, record[key], want)
		}
	}
	task := client.last()
	if task == nil {
		t.Fatal("Enqueue sent no task")
	}
	if task.Type() != "reindex:orders" || string(task.Payload()) != "payload" {
		t.Errorf("task = %s/%q, want reindex:orders/payload", task.Type(), task.Payload())
	}
	checkTraceSpan(t, task.Headers()["traceparent"], record["span_id"])
}

// TestAsynq_C1_EnqueueErrorIsReturned proves that a client error comes back unchanged, and
// the call records it.
func TestAsynq_EnqueueErrorIsReturned(t *testing.T) {
	client := &fakeClient{err: errString("queue refused")}
	log, rec := wlogtest.New(t)
	ctx, end := tracedContext(t, log)

	_, err := Enqueue(ctx, client, "reindex", nil)
	if err == nil || err.Error() != "queue refused" {
		t.Fatalf("Enqueue returned %v, want the error of the client", err)
	}
	end()

	if record := firstCall(t, rec.Last()); record["error"] == nil {
		t.Error("calls[0].error = nil, want the error of the client")
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

// checkTraceSpan proves that one traceparent text names the given span id.
func checkTraceSpan(t *testing.T, traceparent string, spanID any) {
	t.Helper()
	parts := strings.Split(traceparent, "-")
	if len(parts) != 4 || parts[1] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("traceparent = %q, want the trace id of the unit", traceparent)
	}
	if parts[2] != spanID {
		t.Errorf("traceparent span = %q, want the span id of the call %v", parts[2], spanID)
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
