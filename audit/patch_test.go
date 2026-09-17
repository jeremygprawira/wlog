package audit_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/audit"
	"github.com/jeremygprawira/wlog/redact"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestAudit_PAR26_PatchOps proves Patch returns RFC 6902 operations in a stable order,
// with each path escaped the way a JSON Pointer requires.
func TestAudit_PAR26_PatchOps(t *testing.T) {
	ops, err := audit.Patch(
		map[string]any{
			"amount": 100,
			"gone":   true,
			"name":   "order",
			"user":   map[string]any{"plan": "free"},
		},
		map[string]any{
			"added":  "x",
			"amount": 120,
			"name":   "order",
			"user":   map[string]any{"plan": "pro"},
		},
	)
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}

	want := []audit.Operation{
		{Op: "add", Path: "/added", Value: "x"},
		{Op: "replace", Path: "/amount", Value: 120},
		{Op: "remove", Path: "/gone"},
		{Op: "replace", Path: "/user/plan", Value: "pro"},
	}
	if len(ops) != len(want) {
		t.Fatalf("Patch returned %d operations, want %d: %v", len(ops), len(want), ops)
	}
	for i, op := range ops {
		if op != want[i] {
			t.Errorf("operation %d = %+v, want %+v", i, op, want[i])
		}
	}

	// A key with a slash or a tilde is escaped, so the pointer resolves to one key.
	escaped, err := audit.Patch(map[string]any{}, map[string]any{"a/b~c": 1})
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}
	if len(escaped) != 1 || escaped[0].Path != "/a~1b~0c" {
		t.Errorf("path = %v, want /a~1b~0c", escaped)
	}

	// Two values with no fields to compare are an error, exactly as Diff reports.
	if _, err := audit.Patch(1, 2); err == nil {
		t.Error("Patch accepted two scalars")
	}
}

// TestAudit_PAR26_PatchRedacted proves a denylist entry masks the value of an operation
// whose path it names, so a patch never carries a secret.
func TestAudit_PAR26_PatchRedacted(t *testing.T) {
	redactor, err := redact.Default().With(redact.AddKeys("user.creds.nik"))
	if err != nil {
		t.Fatalf("redact.With: %v", err)
	}

	before := map[string]any{"user": map[string]any{"creds": map[string]any{"nik": "1111", "bank": "bca"}}}
	after := map[string]any{"user": map[string]any{"creds": map[string]any{"nik": "2222", "bank": "bca"}}}

	ops, err := audit.Patch(before, after, audit.WithRedactor(redactor))
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}
	if len(ops) != 1 {
		t.Fatalf("Patch returned %v, want one operation", ops)
	}
	if ops[0].Path != "/user/creds/nik" {
		t.Errorf("path = %q, want /user/creds/nik", ops[0].Path)
	}
	if ops[0].Value != redactor.Replacement() {
		t.Errorf("value = %v, want the replacement text, because the path is denied", ops[0].Value)
	}
	if strings.Contains(ops[0].Path, "2222") {
		t.Error("the denied value reached the operation path")
	}

	// A path the denylist does not name keeps its value.
	plain, err := audit.Patch(map[string]any{"amount": 1}, map[string]any{"amount": 2}, audit.WithRedactor(redactor))
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}
	if len(plain) != 1 || plain[0].Value != 2 {
		t.Errorf("a value the denylist allows was masked: %v", plain)
	}
}

// TestAudit_PAR26_OnlyDrainShape proves OnlyDrain forwards only the audit fact, with the
// five keys a compliance backend needs, and nothing else.
func TestAudit_PAR26_OnlyDrainShape(t *testing.T) {
	var forwarded []map[string]any
	next := wlog.DrainFunc(func(_ context.Context, event map[string]any) {
		forwarded = append(forwarded, event)
	})
	only := audit.OnlyDrain(next)

	event := map[string]any{
		"timestamp": "2026-09-16T12:00:00Z",
		"event_id":  "evt-1",
		"service":   map[string]any{"name": "orders"},
		"trace":     map[string]any{"trace_id": "4bf9"},
		"audit":     []any{map[string]any{"action": "invoice.refund", "outcome": "success"}},
		// Everything below must not travel to the audit-only drain.
		"order_id": "ord-1",
		"level":    "info",
		"http":     map[string]any{"status": 200},
	}
	only.Send(context.Background(), event)

	// A plain event forwards nothing at all.
	only.Send(context.Background(), map[string]any{"level": "info", "operation": "op"})

	if len(forwarded) != 1 {
		t.Fatalf("OnlyDrain forwarded %d events, want only the one with an audit record", len(forwarded))
	}
	allowed := map[string]bool{"timestamp": true, "event_id": true, "service": true, "trace": true, "audit": true}
	for key := range forwarded[0] {
		if !allowed[key] {
			t.Errorf("the forwarded event carries %q, which is not part of the audit shape", key)
		}
	}
	for key := range allowed {
		if _, ok := forwarded[0][key]; !ok {
			t.Errorf("the forwarded event is missing %q", key)
		}
	}
	records, _ := forwarded[0]["audit"].([]any)
	if len(records) != 1 {
		t.Errorf("audit = %v, want the one record", forwarded[0]["audit"])
	}

	// The forwarded copy must not alias the original event's slice.
	event["audit"] = []any{map[string]any{"action": "other"}}
	if got, _ := forwarded[0]["audit"].([]any); len(got) != 1 {
		t.Error("only the forwarded event's audit records changed after the source was replaced")
	}
}

// TestAudit_PAR26_ChangesOnRecord proves a record can carry the patch that describes what
// changed, so a reader sees the change and not only its summary.
func TestAudit_PAR26_ChangesOnRecord(t *testing.T) {
	ops, err := audit.Patch(
		map[string]any{"amount": 100},
		map[string]any{"amount": 120},
	)
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}

	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "order.update")
	r := testRecord()
	r.Changes = ops
	audit.Do(ctx, r)
	end()

	changes, _ := recordOf(t, rec.Last())["changes"].([]any)
	if len(changes) != 1 {
		t.Fatalf("changes = %v, want the one operation", recordOf(t, rec.Last())["changes"])
	}
	op, _ := changes[0].(map[string]any)
	if op["op"] != "replace" || op["path"] != "/amount" {
		t.Errorf("change = %v, want a replace of /amount", changes[0])
	}
}
