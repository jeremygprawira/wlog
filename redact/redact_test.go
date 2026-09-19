package redact_test

import (
	"testing"

	"github.com/jeremygprawira/wlog/redact"
)

func TestRedact_DefaultKeys_Mask(t *testing.T) {
	r := redact.MustNew()
	cases := []string{"authHeader", "X-Auth-Token", "user_password", "login_pin"}
	for _, key := range cases {
		event := map[string]any{key: "secret-value"}
		r.Apply(event)
		if event[key] != "[REDACTED]" {
			t.Errorf("key %q: got %v, want [REDACTED]", key, event[key])
		}
	}
}

func TestRedact_DefaultKeys_DoNotMask(t *testing.T) {
	r := redact.MustNew()
	cases := []string{"author", "concert", "tokenizer_version", "spin_count"}
	for _, key := range cases {
		event := map[string]any{key: "plain-value"}
		r.Apply(event)
		if event[key] != "plain-value" {
			t.Errorf("key %q: got %v, want unmasked", key, event[key])
		}
	}
}

func TestRedact_AnyDepth(t *testing.T) {
	r := redact.MustNew()
	event := map[string]any{
		"user": map[string]any{
			"profile": map[string]any{
				"password": "secret",
				"name":     "alice",
			},
		},
	}
	r.Apply(event)
	profile := event["user"].(map[string]any)["profile"].(map[string]any)
	if profile["password"] != "[REDACTED]" {
		t.Errorf("nested password: got %v, want [REDACTED]", profile["password"])
	}
	if profile["name"] != "alice" {
		t.Errorf("nested name: got %v, want unmasked", profile["name"])
	}
}

func TestRedact_Slices(t *testing.T) {
	r := redact.MustNew()
	event := map[string]any{
		"items": []any{
			map[string]any{"token": "t1", "id": "1"},
			map[string]any{"token": "t2", "id": "2"},
		},
	}
	r.Apply(event)
	items := event["items"].([]any)
	for _, item := range items {
		m := item.(map[string]any)
		if m["token"] != "[REDACTED]" {
			t.Errorf("slice item token: got %v, want [REDACTED]", m["token"])
		}
		if m["id"] == "[REDACTED]" {
			t.Errorf("slice item id should not be masked")
		}
	}
}

// TestRedact_RED3_OneWordKeysMasked proves that a key entry also matches its
// tokens joined with no separator, and that the default list names every key that
// RED-3 adds.
func TestRedact_RED3_OneWordKeysMasked(t *testing.T) {
	r := redact.MustNew()

	for _, key := range []string{
		"apikey", "api_key", "apiKey",
		"sessionid", "session_id", "sessionId",
		"sid", "jsessionid", "phpsessid", "connect.sid", "csrf", "xsrf",
		"passphrase", "access_key", "signing_key", "client_secret", "signature",
		"x_amz_signature", "dsn", "database_url",
	} {
		event := map[string]any{key: "SECRET-VALUE"}
		r.Apply(event)
		if event[key] == "SECRET-VALUE" {
			t.Errorf("key %q was not masked", key)
		}
	}
}

// TestRedact_IdentityFieldsNeverScanned proves that a value pattern never scans a
// reserved identity field. A UUID can hold a run of digits that passes the card checksum,
// so scanning an event id would sometimes hide half of it.
func TestRedact_IdentityFieldsNeverScanned(t *testing.T) {
	const card = "4111111111111111"
	event := map[string]any{
		"event_id": card,
		"trace": map[string]any{
			"trace_id":        card,
			"span_id":         card,
			"parent_span_id":  card,
			"parent_event_id": card,
		},
		"note": card,
	}
	redact.Default().Apply(event)

	if event["event_id"] != card {
		t.Errorf("event_id = %v, want it unchanged", event["event_id"])
	}
	trace := event["trace"].(map[string]any)
	for _, key := range []string{"trace_id", "span_id", "parent_span_id", "parent_event_id"} {
		if trace[key] != card {
			t.Errorf("trace.%s = %v, want it unchanged", key, trace[key])
		}
	}
	if event["note"] != "****1111" {
		t.Errorf("note = %v, want the same value masked outside an identity field", event["note"])
	}
}
