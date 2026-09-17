package audit_test

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/audit"
	"github.com/jeremygprawira/wlog/redact"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// beforeAfter is the struct the diff test walks through its JSON tags.
type beforeAfter struct {
	Name   string `json:"name"`
	Amount int    `json:"amount"`
	Secret string `json:"secret"`
}

// TestAudit_Diff proves only changed fields come back, in from and to form, and that two
// equal values give an empty map.
func TestAudit_Diff(t *testing.T) {
	before := beforeAfter{Name: "order", Amount: 100, Secret: "a"}
	after := beforeAfter{Name: "order", Amount: 120, Secret: "b"}

	diff, err := audit.Diff(before, after)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(diff) != 2 {
		t.Fatalf("diff = %v, want two changed fields", diff)
	}
	amount, _ := diff["amount"].(map[string]any)
	if amount["from"] != float64(100) && amount["from"] != 100 {
		t.Errorf("amount.from = %v, want 100", amount["from"])
	}
	if amount["to"] != float64(120) && amount["to"] != 120 {
		t.Errorf("amount.to = %v, want 120", amount["to"])
	}
	if _, present := diff["name"]; present {
		t.Errorf("unchanged field name is in the diff: %v", diff)
	}

	equal, err := audit.Diff(before, before)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(equal) != 0 {
		t.Errorf("Diff of equal values = %v, want empty", equal)
	}
	empty, err := audit.Diff(nil, nil)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !reflect.DeepEqual(empty, map[string]any{}) {
		t.Errorf("Diff(nil, nil) = %v, want empty", empty)
	}
}

// TestAudit_Diff_Nested proves a nested change keeps the nested shape, so a path denylist
// entry can match it.
func TestAudit_Diff_Nested(t *testing.T) {
	before := map[string]any{"user": map[string]any{"plan": "free"}, "count": 1}
	after := map[string]any{"user": map[string]any{"plan": "pro"}, "count": 1}

	diff, err := audit.Diff(before, after)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	user, ok := diff["user"].(map[string]any)
	if !ok {
		t.Fatalf("diff = %v, want a nested user object", diff)
	}
	entry, ok := user["plan"].(map[string]any)
	if !ok {
		t.Fatalf("user = %v, want a plan entry", user)
	}
	if entry["from"] != "free" || entry["to"] != "pro" {
		t.Errorf("user.plan = %v, want free to pro", entry)
	}
	if _, flat := diff["user.plan"]; flat {
		t.Errorf("diff still holds a dotted key: %v", diff)
	}
}

// TestAudit_AUD7_DiffPathRedacted proves the diff is a nested tree, so a path denylist
// entry masks a value inside it. A flattened dotted key never matched such an entry,
// which leaked the value (gate G1).
func TestAudit_AUD7_DiffPathRedacted(t *testing.T) {
	redactor, err := redact.Default().With(redact.AddKeys("changes.user.creds.nik"))
	if err != nil {
		t.Fatalf("redact.With: %v", err)
	}
	log, rec := wlogtest.New(t, wlog.WithRedactor(redactor))

	before := map[string]any{"user": map[string]any{"creds": map[string]any{"nik": "1111", "bank": "bca"}}}
	after := map[string]any{"user": map[string]any{"creds": map[string]any{"nik": "2222", "bank": "bca"}}}

	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	diff, err := audit.Diff(before, after)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	wlog.Set(ctx, "changes", diff)
	end()

	user, ok := diff["user"].(map[string]any)
	if !ok {
		t.Fatalf("Diff returned a flat tree: %v", diff)
	}
	creds, ok := user["creds"].(map[string]any)
	if !ok {
		t.Fatalf("Diff flattened the nested object: %v", diff)
	}
	nik, ok := creds["nik"].(map[string]any)
	if !ok || nik["to"] != "2222" {
		t.Fatalf("creds.nik = %v, want the change with its nested path", creds["nik"])
	}

	// The denylist names changes.user.creds.nik, so neither the old nor the new value
	// may survive to the event. With a flat dotted key the entry could not match, and
	// the raw value was emitted (gate G1).
	event := fmt.Sprint(rec.Last())
	if strings.Contains(event, "2222") || strings.Contains(event, "1111") {
		t.Errorf("the denied path leaked into the event: %v", rec.Last())
	}
	if !strings.Contains(event, "[REDACTED]") {
		t.Errorf("the denied value was not masked: %v", rec.Last())
	}
}

// TestAudit_AUD11_DiffEdgeCases proves Diff rejects input it cannot compare, reports a
// type change at its own path instead of an empty diff, and keeps the nested shape.
func TestAudit_AUD11_DiffEdgeCases(t *testing.T) {
	if _, err := audit.Diff(1, 2); err == nil {
		t.Error("Diff accepted two scalars")
	}
	if _, err := audit.Diff([]int{1}, []int{2}); err == nil {
		t.Error("Diff accepted two slices")
	}
	if _, err := audit.Diff("before", map[string]any{"a": 1}); err == nil {
		t.Error("Diff accepted a scalar against an object")
	}

	// A type change is a change, not an empty diff and not "1 to 1".
	diff, err := audit.Diff(map[string]any{"n": 1}, map[string]any{"n": map[string]any{"a": 1}})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	entry, ok := diff["n"].(map[string]any)
	if !ok {
		t.Fatalf("diff = %v, want an n entry", diff)
	}
	if entry["from"] != 1 {
		t.Errorf("n.from = %v, want 1", entry["from"])
	}
	to, ok := entry["to"].(map[string]any)
	if !ok || to["a"] != 1 {
		t.Errorf("n.to = %v, want the object {a:1}", entry["to"])
	}

	// The tree stays nested, so a path entry can match it.
	nested, err := audit.Diff(
		map[string]any{"user": map[string]any{"plan": "free"}},
		map[string]any{"user": map[string]any{"plan": "pro"}},
	)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	user, ok := nested["user"].(map[string]any)
	if !ok {
		t.Fatalf("diff = %v, want a nested user object", nested)
	}
	plan, ok := user["plan"].(map[string]any)
	if !ok || plan["from"] != "free" || plan["to"] != "pro" {
		t.Errorf("user.plan = %v, want free to pro", user["plan"])
	}
	if _, flat := nested["user.plan"]; flat {
		t.Errorf("diff still holds a dotted key: %v", nested)
	}
}
