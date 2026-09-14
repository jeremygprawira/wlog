package redact_test

import (
	"testing"

	"github.com/jeremygprawira/wlog/redact"
)

func TestRedact_AddPatterns_Replacement(t *testing.T) {
	r := redact.MustNew(redact.AddPatterns(redact.Pattern{
		Name:        "midtrans_key",
		Regex:       `SB-Mid-server-\w+`,
		Replacement: "[MIDTRANS]",
	}))
	event := map[string]any{"key": "using SB-Mid-server-abc123 now"}
	r.Apply(event)
	if event["key"] != "using [MIDTRANS] now" {
		t.Errorf("got %q", event["key"])
	}
}

func TestRedact_AddPatterns_ReplaceFunc(t *testing.T) {
	r := redact.MustNew(redact.AddPatterns(redact.Pattern{
		Name:  "order_ref",
		Regex: `ORD-(\d+)`,
		Replace: func(m redact.Match) string {
			return "ORD-***" + "(" + m.Groups[0][:1] + ")"
		},
	}))
	event := map[string]any{"ref": "ORD-4821"}
	r.Apply(event)
	if event["ref"] != "ORD-***(4)" {
		t.Errorf("got %q", event["ref"])
	}
}

func TestRedact_AddPatterns_ReplacePanic_YieldsRedacted(t *testing.T) {
	r := redact.MustNew(redact.AddPatterns(redact.Pattern{
		Name:  "boom",
		Regex: `BOOM\d+`,
		Replace: func(m redact.Match) string {
			panic("nope")
		},
	}))
	event := map[string]any{"x": "see BOOM123 here"}
	r.Apply(event)
	if event["x"] != "see [REDACTED] here" {
		t.Errorf("panic did not fall back to [REDACTED]: %q", event["x"])
	}
}

func TestRedact_RemovePatterns_Builtin(t *testing.T) {
	r := redact.MustNew(redact.RemovePatterns("ipv4"))
	event := map[string]any{"remote": "192.168.1.100"}
	r.Apply(event)
	if event["remote"] != "192.168.1.100" {
		t.Errorf("ipv4 still masked after RemovePatterns: %q", event["remote"])
	}
}

func TestRedact_NoBuiltinPatterns(t *testing.T) {
	r := redact.MustNew(redact.NoBuiltinPatterns())
	event := map[string]any{"contact": "alice@example.com"}
	r.Apply(event)
	if event["contact"] != "alice@example.com" {
		t.Errorf("email masked despite NoBuiltinPatterns: %q", event["contact"])
	}
}

func TestRedact_Patterns_Errors(t *testing.T) {
	if _, err := redact.New(redact.AddPatterns(redact.Pattern{Name: "bad", Regex: `(unterminated`})); err == nil {
		t.Error("expected error for invalid regex")
	}
	if _, err := redact.New(redact.AddPatterns(redact.Pattern{Name: "email", Regex: `x`})); err == nil {
		t.Error("expected error for duplicate pattern name")
	}
	if _, err := redact.New(redact.RemovePatterns("not_a_pattern")); err == nil {
		t.Error("expected error removing an unknown pattern")
	}
}
