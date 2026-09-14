package redact_test

import (
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog/redact"
)

func TestRedact_Transform_RunsBeforeKeyRules(t *testing.T) {
	r := redact.MustNew(redact.Transform(func(event map[string]any) {
		delete(event, "query") // drop it entirely before the normal walk
		event["password"] = "still-masked-by-key-rule"
	}))
	event := map[string]any{"query": "select 1", "name": "alice"}
	r.Apply(event)
	if _, ok := event["query"]; ok {
		t.Error("transform's delete was not applied")
	}
	if event["password"] != "[REDACTED]" {
		t.Errorf("key rule did not run on transform's output: %v", event["password"])
	}
}

func TestRedact_Transform_PanicDoesNotStopRedaction(t *testing.T) {
	r := redact.MustNew(redact.Transform(func(map[string]any) { panic("boom") }))
	event := map[string]any{"password": "secret"}
	r.Apply(event)
	if event["password"] != "[REDACTED]" {
		t.Errorf("redaction skipped after transform panic: %v", event["password"])
	}
}

func TestRedact_MaxDepth(t *testing.T) {
	r := redact.MustNew(redact.MaxDepth(2))
	event := map[string]any{
		"a": map[string]any{
			"b": map[string]any{
				"c": map[string]any{"name": "too-deep"},
			},
		},
	}
	r.Apply(event)
	b := event["a"].(map[string]any)["b"].(map[string]any)
	if b["c"] != "[REDACTED:DEPTH]" {
		t.Errorf("depth limit not enforced: %v", b["c"])
	}
}

func TestRedact_MaxStringScan(t *testing.T) {
	r := redact.MustNew(redact.MaxStringScan(10))
	event := map[string]any{"note": strings.Repeat("x", 20)}
	r.Apply(event)
	if event["note"] != "[REDACTED:TOO_LARGE]" {
		t.Errorf("string scan limit not enforced: %v", event["note"])
	}
}

func TestRedact_Replacement_Custom(t *testing.T) {
	r := redact.MustNew(redact.Replacement("***"))
	event := map[string]any{"password": "secret"}
	r.Apply(event)
	if event["password"] != "***" {
		t.Errorf("custom replacement not used: %v", event["password"])
	}
}

func TestRedact_Default_MatchesMustNew(t *testing.T) {
	a, b := redact.Default(), redact.MustNew()
	if a.Fingerprint() != b.Fingerprint() {
		t.Error("Default() and MustNew() have different effective configs")
	}
}

func TestRedact_Disabled_IsNoop(t *testing.T) {
	r := redact.Disabled()
	event := map[string]any{"password": "secret"}
	r.Apply(event)
	if event["password"] != "secret" {
		t.Errorf("Disabled() redactor changed the event: %v", event["password"])
	}
}
