package audit_test

import (
	"reflect"
	"testing"

	"github.com/jeremygprawira/wlog/audit"
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

	diff := audit.Diff(before, after)
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

	if equal := audit.Diff(before, before); len(equal) != 0 {
		t.Errorf("Diff of equal values = %v, want empty", equal)
	}
	if !reflect.DeepEqual(audit.Diff(nil, nil), map[string]any{}) {
		t.Errorf("Diff(nil, nil) = %v, want empty", audit.Diff(nil, nil))
	}
}

// TestAudit_Diff_Nested proves a nested change reports its dotted path.
func TestAudit_Diff_Nested(t *testing.T) {
	before := map[string]any{"user": map[string]any{"plan": "free"}, "count": 1}
	after := map[string]any{"user": map[string]any{"plan": "pro"}, "count": 1}

	diff := audit.Diff(before, after)
	entry, ok := diff["user.plan"].(map[string]any)
	if !ok {
		t.Fatalf("diff = %v, want a user.plan entry", diff)
	}
	if entry["from"] != "free" || entry["to"] != "pro" {
		t.Errorf("user.plan = %v, want free to pro", entry)
	}
}
