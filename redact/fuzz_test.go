package redact_test

import (
	"testing"

	"github.com/jeremygprawira/wlog/redact"
)

// FuzzRedact_NeverLeaks is gate G1: a value stored under a denylisted key must never
// survive Apply, for any value the fuzzer can produce (empty, huge, unicode, or one of
// the seeded real-shaped secrets below). The key itself is never fuzzed — it is always
// one of the real default denylist entries, so this checks the one guarantee that has
// no known false negative (unlike value-pattern scanning, which is best-effort).
func FuzzRedact_NeverLeaks(f *testing.F) {
	f.Add("plain secret value")
	f.Add("")
	f.Add("4111111111111111")
	f.Add("alice@example.com")
	f.Add("eyJhbGciOiJIUzI1NiIs.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U")
	f.Add("Bearer sk_live_abc123")
	f.Add("+62 812-3456-7890")
	f.Add("🔑unicode🔑")

	r := redact.Default()
	keys := r.Keys()

	f.Fuzz(func(t *testing.T, value string) {
		for _, key := range keys {
			event := map[string]any{key: value}
			r.Apply(event)
			if got := event[key]; got != "[REDACTED]" {
				t.Fatalf("denylisted key %q leaked: value=%q got=%v", key, value, got)
			}
		}
	})
}

// FuzzRedact_ReplaceFuncPanic covers the panic path of ReplaceFunc: a mask function that
// panics must fall back to the fixed replacement, never to the raw value (gate G1).
func FuzzRedact_ReplaceFuncPanic(f *testing.F) {
	f.Add("plain secret value")
	f.Add("4111111111111111")
	f.Add("")

	r := redact.MustNew(redact.ReplaceFunc(func(string) string { panic("bad mask") }))
	keys := r.Keys()

	f.Fuzz(func(t *testing.T, value string) {
		for _, key := range keys {
			event := map[string]any{key: value}
			r.Apply(event)
			if got := event[key]; got != "[REDACTED]" {
				t.Fatalf("panicking ReplaceFunc leaked for key %q: got=%v", key, got)
			}
		}
	})
}
