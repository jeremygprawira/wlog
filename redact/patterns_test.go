package redact_test

import (
	"strings"
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

// TestRedact_RED5_UnknownPatternName proves that an unknown or duplicate pattern
// name is an error rather than a silent no-op.
func TestRedact_RED5_UnknownPatternName(t *testing.T) {
	if _, err := redact.New(redact.EnablePatterns("no_such_pattern")); err == nil {
		t.Error("EnablePatterns accepted an unknown name")
	}
	if _, err := redact.New(redact.RemovePatterns("no_such_pattern")); err == nil {
		t.Error("RemovePatterns accepted an unknown name")
	}
	if _, err := redact.New(
		redact.AddPatterns(redact.Pattern{Name: "email", Regex: `x+`}),
	); err == nil {
		t.Error("AddPatterns accepted a duplicate name")
	}
}

// TestRedact_RED6_NewPatterns proves the four new built-in patterns mask their
// examples, and that a value with the shape of a token prefix is not missed.
func TestRedact_RED6_NewPatterns(t *testing.T) {
	r := redact.MustNew()

	cases := map[string]string{
		"database_url":  "postgres://app:hunter2@db.internal:5432/orders",
		"proxy_url":     "https://user:s3cret@proxy.internal/",
		"authorization": "Basic dXNlcjpwYXNzd29yZA==",
		"stripe_key":    "sk_live_51H8xQeK2eZvKYlo2C",
		"aws_key":       "AKIAIOSFODNN7EXAMPLE",
		"github_token":  "ghp_1234567890abcdefghijklmnopqrstuv",
	}
	for key, value := range cases {
		event := map[string]any{key: value}
		r.Apply(event)
		got, _ := event[key].(string)
		if got == value {
			t.Errorf("%s = %q was not masked", key, got)
		}
		for _, secret := range []string{"hunter2", "s3cret", "dXNlcjpwYXNzd29yZA==", "51H8xQeK2eZvKYlo2C", "IOSFODNN7EXAMPLE", "1234567890abcdefghijklmnopqrstuv"} {
			if strings.Contains(got, secret) {
				t.Errorf("%s = %q still holds %q", key, got, secret)
			}
		}
	}

	// A query string that names a denylisted parameter masks the value only.
	event := map[string]any{"url": "https://api.example.com/v1/orders?api_key=sk-should-vanish&page=2"}
	r.Apply(event)
	got, _ := event["url"].(string)
	if strings.Contains(got, "sk-should-vanish") {
		t.Errorf("url = %q kept the api_key value", got)
	}
	if !strings.Contains(got, "page=2") {
		t.Errorf("url = %q lost the harmless parameter", got)
	}
}
