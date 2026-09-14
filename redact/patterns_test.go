package redact_test

import (
	"testing"

	"github.com/jeremygprawira/wlog/redact"
)

func TestRedact_Builtin_CreditCard(t *testing.T) {
	r := redact.MustNew()

	event := map[string]any{"note": "card 4111111111111111 on file"}
	r.Apply(event)
	if event["note"] != "card ****1111 on file" {
		t.Errorf("valid Luhn card: got %q", event["note"])
	}

	// One digit off from the number above: fails Luhn, must not be treated as a card.
	event2 := map[string]any{"note": "id 4111111111111112 not a card"}
	r.Apply(event2)
	if event2["note"] != "id 4111111111111112 not a card" {
		t.Errorf("non-Luhn digits got masked: %q", event2["note"])
	}
}

func TestRedact_Builtin_Email(t *testing.T) {
	r := redact.MustNew()
	event := map[string]any{"contact": "alice@example.com"}
	r.Apply(event)
	if event["contact"] != "a***@***.com" {
		t.Errorf("email: got %q", event["contact"])
	}
}

func TestRedact_Builtin_JWT(t *testing.T) {
	r := redact.MustNew()
	jwt := "eyJhbGciOiJIUzI1NiIs.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
	event := map[string]any{"authz": jwt}
	r.Apply(event)
	if event["authz"] != "eyJ***.***" {
		t.Errorf("jwt: got %q", event["authz"])
	}
}

func TestRedact_Builtin_Bearer(t *testing.T) {
	r := redact.MustNew()
	event := map[string]any{"header": "Bearer sk_live_abc123def456"}
	r.Apply(event)
	if event["header"] != "Bearer ***" {
		t.Errorf("bearer: got %q", event["header"])
	}
}

func TestRedact_Builtin_UnmatchedStringsUntouched(t *testing.T) {
	r := redact.MustNew()
	event := map[string]any{"greeting": "hello world"}
	r.Apply(event)
	if event["greeting"] != "hello world" {
		t.Errorf("unrelated string got changed: %q", event["greeting"])
	}
}
