package redact_test

import (
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog/redact"
)

func TestRedact_RemoveKeys_LeavesLongerKeyUnmasked(t *testing.T) {
	r, err := redact.New(redact.RemoveKeys("session"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	event := map[string]any{"session_id": "abc123"}
	r.Apply(event)
	if event["session_id"] != "abc123" {
		t.Errorf("session_id got masked after RemoveKeys(session): %v", event["session_id"])
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
