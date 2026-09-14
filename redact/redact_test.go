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
