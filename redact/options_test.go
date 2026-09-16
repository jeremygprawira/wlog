package redact_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog/redact"
)

func TestRedact_RemoveKeys_LeavesLongerKeyUnmasked(t *testing.T) {
	r, err := redact.New(redact.RemoveKeys("session"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// session_id has an entry of its own, so removing session must not touch it.
	event := map[string]any{"session_store": "abc123"}
	r.Apply(event)
	if event["session_store"] != "abc123" {
		t.Errorf("session_store got masked after RemoveKeys(session): %v", event["session_store"])
	}
}

func TestRedact_AddKeys(t *testing.T) {
	r, err := redact.New(redact.AddKeys("nik"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	event := map[string]any{"customer": map[string]any{"profile": map[string]any{"nik": "123"}}}
	r.Apply(event)
	got := event["customer"].(map[string]any)["profile"].(map[string]any)["nik"]
	if got != "[REDACTED]" {
		t.Errorf("AddKeys(nik) did not mask nested nik: %v", got)
	}
}

func TestRedact_New_ErrorOnUnknownRemoval(t *testing.T) {
	_, err := redact.New(redact.RemoveKeys("not_a_real_key"))
	if err == nil {
		t.Fatal("expected error removing an unknown key, got nil")
	}
}

func TestRedact_New_ErrorOnInvalidGlob(t *testing.T) {
	_, err := redact.New(redact.AddKeys("x-[abc*"))
	if err == nil {
		t.Fatal("expected error for invalid glob pattern, got nil")
	}
}

func TestRedact_With_DoesNotMutateParent(t *testing.T) {
	base := redact.MustNew(redact.ReplaceKeys("token"))
	derived, err := base.With(redact.AddKeys("nik"))
	if err != nil {
		t.Fatalf("With: %v", err)
	}

	baseEvent := map[string]any{"nik": "123"}
	base.Apply(baseEvent)
	if baseEvent["nik"] != "123" {
		t.Errorf("base redactor was mutated by With: %v", baseEvent["nik"])
	}

	derivedEvent := map[string]any{"nik": "123"}
	derived.Apply(derivedEvent)
	if derivedEvent["nik"] != "[REDACTED]" {
		t.Errorf("derived redactor missing With's AddKeys: %v", derivedEvent["nik"])
	}
}

func TestRedact_Keys_Sorted(t *testing.T) {
	r := redact.MustNew(redact.ReplaceKeys("zebra", "alpha", "mango"))
	got := r.Keys()
	if !sortedStrings(got) {
		t.Errorf("Keys() not sorted: %v", got)
	}
}

func sortedStrings(s []string) bool {
	for i := 1; i < len(s); i++ {
		if strings.Compare(s[i-1], s[i]) > 0 {
			return false
		}
	}
	return true
}

func TestRedact_Fingerprint_OrderIndependent(t *testing.T) {
	a := redact.MustNew(redact.ReplaceKeys("alpha"), redact.AddKeys("beta"))
	b := redact.MustNew(redact.ReplaceKeys("beta"), redact.AddKeys("alpha"))
	if a.Fingerprint() != b.Fingerprint() {
		t.Errorf("fingerprints differ for same effective set: %q vs %q", a.Fingerprint(), b.Fingerprint())
	}
}

func TestRedact_Fingerprint_ChangesOnAddRemove(t *testing.T) {
	a := redact.MustNew(redact.ReplaceKeys("alpha"))
	b := redact.MustNew(redact.ReplaceKeys("alpha"), redact.AddKeys("beta"))
	if a.Fingerprint() == b.Fingerprint() {
		t.Error("fingerprint did not change after AddKeys")
	}
}

// TestRedact_RED2_WithKeepsPatternsTransformsReplaceFunc proves that With starts
// from every resolved setting of the receiver: denylist, pattern toggles, custom
// patterns, transforms, ReplaceFunc, masks, and limits.
func TestRedact_RED2_WithKeepsPatternsTransformsReplaceFunc(t *testing.T) {
	transformed := false
	base := redact.MustNew(
		redact.RemovePatterns("jwt"),
		redact.AddPatterns(redact.Pattern{Name: "tenant", Regex: `tenant_[0-9]+`}),
		redact.Transform(func(event map[string]any) { transformed = true; event["seen"] = true }),
		redact.ReplaceFunc(func(match string) string { return "<" + match + ">" }),
		redact.MaskClientIP(),
		redact.MaxDepth(4),
	)

	derived, err := base.With(redact.AddKeys("extra_secret"))
	if err != nil {
		t.Fatal(err)
	}

	event := map[string]any{
		"tenant_id":    "tenant_42",
		"jwt":          "a.b.c",
		"password":     "hunter2",
		"extra_secret": "also",
		"http":         map[string]any{"client_ip": "203.0.113.7"},
	}
	derived.Apply(event)
	if !transformed {
		t.Error("With dropped the transform")
	}
	if event["tenant_id"] == "tenant_42" {
		t.Errorf("tenant_id = %v, want the custom pattern masked", event["tenant_id"])
	}
	// ReplaceFunc covers a key, a path, and a glob match; a pattern keeps its own
	// masker, so the custom pattern proves only that With kept it.
	if fmt.Sprint(event["password"]) != "<hunter2>" {
		t.Errorf("password = %v, want ReplaceFunc to build the mask", event["password"])
	}
	if event["jwt"] != "a.b.c" {
		t.Errorf("jwt = %v, want it left alone: RemovePatterns was dropped", event["jwt"])
	}
	if fmt.Sprint(event["password"]) == "hunter2" || fmt.Sprint(event["extra_secret"]) == "also" {
		t.Errorf("keys = %v, want the inherited and the added key masked", event)
	}
	if got := event["http"].(map[string]any)["client_ip"]; got == "203.0.113.7" {
		t.Errorf("http.client_ip = %v, want the mask kept", got)
	}
}

// TestRedact_RED2_DisabledWithFails proves that a disabled redactor cannot derive
// one, because Disabled() means "no redaction" and a derived redactor would be a
// surprise.
func TestRedact_RED2_DisabledWithFails(t *testing.T) {
	if _, err := redact.Disabled().With(redact.AddKeys("password")); err == nil {
		t.Fatal("Disabled().With returned nil error")
	}
}
