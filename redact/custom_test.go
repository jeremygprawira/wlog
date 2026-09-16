package redact_test

import (
	"strings"
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

// TestRedact_RED8_FingerprintCoversConfig proves that two redactors with any
// difference give two fingerprints, because the fingerprint names the denylist
// that masked an event.
func TestRedact_RED8_FingerprintCoversConfig(t *testing.T) {
	base := redact.MustNew()
	same := redact.MustNew()
	if base.Fingerprint() != same.Fingerprint() {
		t.Fatalf("two identical redactors differ: %s vs %s", base.Fingerprint(), same.Fingerprint())
	}

	variants := map[string]*redact.Redactor{
		"keys":            redact.MustNew(redact.AddKeys("custom_key")),
		"mask ip":         redact.MustNew(redact.MaskClientIP()),
		"replacement":     redact.MustNew(redact.Replacement("[gone]")),
		"max depth":       redact.MustNew(redact.MaxDepth(4)),
		"removed pattern": redact.MustNew(redact.RemovePatterns("jwt")),
		"custom pattern":  redact.MustNew(redact.AddPatterns(redact.Pattern{Name: "tenant", Regex: `tenant_[0-9]+`})),
		"transform":       redact.MustNew(redact.Transform(func(map[string]any) {})),
		"disabled":        redact.Disabled(),
	}
	for name, other := range variants {
		if other.Fingerprint() == base.Fingerprint() {
			t.Errorf("%s: the fingerprint did not change (%s)", name, other.Fingerprint())
		}
	}
}

// TestRedact_RED9_TransformPanicFailsClosed proves that a transform or a
// ReplaceFunc which panics masks the value it was given rather than passing it on.
func TestRedact_RED9_TransformPanicFailsClosed(t *testing.T) {
	r := redact.MustNew(
		redact.Transform(func(event map[string]any) { panic("transform boom") }),
		redact.ReplaceFunc(func(string) string { panic("replace boom") }),
	)
	event := map[string]any{"password": "hunter2", "note": "kept"}
	r.Apply(event)

	if event["password"] == "hunter2" {
		t.Errorf("password = %v, want the mask even though both hooks panicked", event["password"])
	}
}

// TestRedact_SPECG2_UnknownTypeMasked proves that a value the redactor cannot walk
// is replaced, and that a long integer is still scanned as a card.
func TestRedact_SPECG2_UnknownTypeMasked(t *testing.T) {
	r := redact.MustNew()
	event := map[string]any{
		"custom": struct{ A int }{A: 1},
		"number": int64(4111111111111111),
		"big":    uint64(4111111111111111),
	}
	r.Apply(event)
	if _, ok := event["custom"].(struct{ A int }); ok {
		t.Errorf("custom = %#v, want the mask: the redactor cannot walk that type", event["custom"])
	}
	for _, key := range []string{"number", "big"} {
		if text, ok := event[key].(string); !ok || !strings.Contains(text, "*") {
			t.Errorf("%s = %v, want the card pattern to scan it", key, event[key])
		}
	}
}
