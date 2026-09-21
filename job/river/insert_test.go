// This file runs the calls conformance suite against the insert path, and checks the call
// record and the trace metadata of one insert.
package wlogriver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/riverqueue/river/rivertype"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	callsconformance "github.com/jeremygprawira/wlog/internal/conformance/calls"
	"github.com/jeremygprawira/wlog/propagate"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestRiver_C1_CallsConformance proves that the insert path passes every scenario of the
// calls suite.
func TestRiver_C1_CallsConformance(t *testing.T) {
	callsconformance.Run(conformance.Tester{T: t}, callsFactory{})
}

// callsFactory records the call the suite describes, with the real trace metadata. The suite
// names the call and its result, because an insert is not an http, db, or cache call.
type callsFactory struct{}

// Call records one insert call and writes the trace metadata of the context.
func (callsFactory) Call(ctx context.Context, _ *wlog.Logger, call wlog.Call, result wlog.CallResult) error {
	ctx, end := wlog.StartCall(ctx, call)
	_ = withTraceMetadata(ctx, nil)
	end(result)
	return result.Err
}

// TestRiver_C1_InsertWritesTraceMetadata proves that one insert records one queue call, and
// writes a traceparent into the job metadata whose span id is the span id of that call.
func TestRiver_C1_InsertWritesTraceMetadata(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := tracedContext(t, log)
	params := &rivertype.JobInsertParams{Kind: "reindex", Metadata: []byte(`{"user_id":"u1"}`)}

	if _, err := InsertMiddleware().InsertMany(ctx, []*rivertype.JobInsertParams{params}, innerOK); err != nil {
		t.Fatalf("InsertMany returned %v", err)
	}
	end()

	record := firstCall(t, rec.Last())
	for key, want := range map[string]any{
		"kind": "queue", "system": "river", "operation": "insert",
		"target": "reindex", "status": "ok",
	} {
		if record[key] != want {
			t.Errorf("calls[0].%s = %v, want %v", key, record[key], want)
		}
	}
	fields := map[string]any{}
	if err := json.Unmarshal(params.Metadata, &fields); err != nil {
		t.Fatalf("metadata is not a JSON object: %v", err)
	}
	if fields["user_id"] != "u1" {
		t.Errorf("metadata.user_id = %v, want the key the caller wrote", fields["user_id"])
	}
	checkTraceSpan(t, fmt.Sprint(fields["traceparent"]), record["span_id"])
}

// TestRiver_C1_InsertKeepsBadMetadata proves that metadata which does not parse stays as the
// caller wrote it, because a trace helper never damages an insert.
func TestRiver_C1_InsertKeepsBadMetadata(t *testing.T) {
	log, _ := wlogtest.New(t)
	ctx, end := tracedContext(t, log)
	params := &rivertype.JobInsertParams{Kind: "reindex", Metadata: []byte("not json")}

	if _, err := InsertMiddleware().InsertMany(ctx, []*rivertype.JobInsertParams{params}, innerOK); err != nil {
		t.Fatalf("InsertMany returned %v", err)
	}
	end()

	if string(params.Metadata) != "not json" {
		t.Errorf("metadata = %q, want the bytes of the caller", params.Metadata)
	}
}

// TestRiver_C1_InsertErrorIsReturned proves that an insert error comes back unchanged, and
// the call records it.
func TestRiver_C1_InsertErrorIsReturned(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := tracedContext(t, log)
	failure := errString("insert refused")

	_, err := InsertMiddleware().InsertMany(ctx, []*rivertype.JobInsertParams{{Kind: "reindex"}}, func(context.Context) ([]*rivertype.JobInsertResult, error) {
		return nil, failure
	})
	if !errors.Is(err, failure) {
		t.Fatalf("InsertMany returned %v, want the error of the insert", err)
	}
	end()

	if record := firstCall(t, rec.Last()); record["error"] == nil {
		t.Error("calls[0].error = nil, want the error of the insert")
	}
}

// innerOK is the inner insert of a batch that succeeds.
func innerOK(context.Context) ([]*rivertype.JobInsertResult, error) { return nil, nil }

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
