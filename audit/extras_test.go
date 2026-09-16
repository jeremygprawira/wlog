package audit_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/audit"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// recordOf returns event["audit"] as a map.
func recordOf(t *testing.T, event map[string]any) map[string]any {
	t.Helper()
	record, ok := event["audit"].(map[string]any)
	if !ok {
		t.Fatalf("no audit field in %v", event)
	}
	return record
}

// TestAudit_Extras proves the new record fields reach the event, and that Version
// defaults to 1. One event holds one audit record, so each case uses its own event.
func TestAudit_Extras(t *testing.T) {
	log, rec := wlogtest.New(t)

	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "op")
	audit.Do(ctx, audit.Record{
		Action:         "invoice.refund",
		Version:        2,
		IdempotencyKey: "key-1",
		Context:        map[string]any{"ticket": "T-9"},
	})
	end()

	record := recordOf(t, rec.Last())
	if record["version"] != float64(2) && record["version"] != 2 {
		t.Errorf("version = %v, want 2", record["version"])
	}
	if record["idempotency_key"] != "key-1" {
		t.Errorf("idempotency_key = %v, want key-1", record["idempotency_key"])
	}
	contextMap, _ := record["context"].(map[string]any)
	if contextMap["ticket"] != "T-9" {
		t.Errorf("context = %v, want ticket T-9", record["context"])
	}

	ctx = log.WithContext(context.Background())
	ctx, end = wlog.Start(ctx, "op")
	audit.Do(ctx, audit.Record{Action: "invoice.read"})
	end()

	if got := recordOf(t, rec.Last())["version"]; got != float64(1) && got != 1 {
		t.Errorf("default version = %v, want 1", got)
	}
}

// TestAudit_Deny proves a refused action records outcome "denied".
func TestAudit_Deny(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "op")
	audit.Deny(ctx, audit.Record{Action: "invoice.refund", Outcome: "success"})
	end()

	if got := recordOf(t, rec.Last())["outcome"]; got != "denied" {
		t.Errorf("outcome = %v, want denied", got)
	}
}

// TestAudit_Only proves Only emits a standalone audit event and leaves the request
// event without an audit field.
func TestAudit_Only(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "op")
	audit.Only(ctx, audit.Record{Action: "invoice.refund"})
	end()

	byOperation := map[string]map[string]any{}
	for _, event := range rec.Events() {
		byOperation[event["operation"].(string)] = event
	}
	standalone := byOperation["audit.invoice.refund"]
	if standalone == nil {
		t.Fatalf("no standalone audit event: %v", byOperation)
	}
	if _, ok := standalone["audit"]; !ok {
		t.Errorf("standalone event has no audit field: %v", standalone)
	}
	parent := byOperation["op"]
	if parent == nil {
		t.Fatalf("no request event: %v", byOperation)
	}
	if _, ok := parent["audit"]; ok {
		t.Errorf("Only put the audit record on the request event: %v", parent)
	}
}

// TestAudit_Wrap proves Wrap records the outcome from fn, and keeps the error's code.
// Each case uses its own event, because one event holds one audit record.
func TestAudit_Wrap(t *testing.T) {
	log, rec := wlogtest.New(t)

	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "op")
	if err := audit.Wrap(ctx, audit.Record{Action: "invoice.refund"}, func() error { return nil }); err != nil {
		t.Fatalf("Wrap returned %v", err)
	}
	end()
	if got := recordOf(t, rec.Last())["outcome"]; got != "success" {
		t.Errorf("success outcome = %v, want success", got)
	}

	wantErr := errors.New("card declined")
	ctx = log.WithContext(context.Background())
	ctx, end = wlog.Start(ctx, "op")
	if err := audit.Wrap(ctx, audit.Record{Action: "invoice.refund"}, func() error { return wantErr }); !errors.Is(err, wantErr) {
		t.Fatalf("Wrap returned %v, want the function's error", err)
	}
	end()
	record := recordOf(t, rec.Last())
	if record["outcome"] != "error" {
		t.Errorf("error outcome = %v, want error", record["outcome"])
	}
	if record["error_code"] == nil || record["error_code"] == "" {
		t.Errorf("error_code missing from %v", record)
	}
}
