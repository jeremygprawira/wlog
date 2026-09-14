package redact_test

import (
	"testing"

	"github.com/jeremygprawira/wlog/redact"
)

func withKeys(t *testing.T, keys ...string) *redact.Redactor {
	t.Helper()
	r, err := redact.New(redact.ReplaceKeys(keys...))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return r
}

func TestRedact_Path_AnchoredToRoot(t *testing.T) {
	r := withKeys(t, "http.request.headers.cookie")

	rootLevel := map[string]any{"cookie": "should-not-mask"}
	r.Apply(rootLevel)
	if rootLevel["cookie"] != "should-not-mask" {
		t.Errorf("unanchored cookie got masked: %v", rootLevel["cookie"])
	}

	nested := map[string]any{
		"http": map[string]any{
			"request": map[string]any{
				"headers": map[string]any{"cookie": "secret", "accept": "json"},
			},
		},
	}
	r.Apply(nested)
	headers := nested["http"].(map[string]any)["request"].(map[string]any)["headers"].(map[string]any)
	if headers["cookie"] != "[REDACTED]" {
		t.Errorf("anchored cookie not masked: %v", headers["cookie"])
	}
	if headers["accept"] != "json" {
		t.Errorf("sibling field masked: %v", headers["accept"])
	}
}

func TestRedact_Path_Glob(t *testing.T) {
	r := withKeys(t, "user.*")
	event := map[string]any{
		"user": map[string]any{"password": "secret", "name": "alice"},
	}
	r.Apply(event)
	user := event["user"].(map[string]any)
	if user["password"] != "[REDACTED]" || user["name"] != "[REDACTED]" {
		t.Errorf("user.* did not mask every direct child: %+v", user)
	}
}

func TestRedact_LeafGlob(t *testing.T) {
	r := withKeys(t, "*_pin", "x-*-secret")
	event := map[string]any{
		"login_pin":    "1234",
		"x-app-secret": "shh",
		"login_pinned": "not-a-match",
	}
	r.Apply(event)
	if event["login_pin"] != "[REDACTED]" {
		t.Errorf("*_pin: got %v", event["login_pin"])
	}
	if event["x-app-secret"] != "[REDACTED]" {
		t.Errorf("x-*-secret: got %v", event["x-app-secret"])
	}
	if event["login_pinned"] != "not-a-match" {
		t.Errorf("*_pin over-matched: %v", event["login_pinned"])
	}
}

func TestRedact_Path_ArrayInheritsParentPath(t *testing.T) {
	r := withKeys(t, "items.card_number")
	event := map[string]any{
		"items": []any{
			map[string]any{"card_number": "4111111111111111", "id": "1"},
			map[string]any{"card_number": "4222222222222222", "id": "2"},
		},
	}
	r.Apply(event)
	items := event["items"].([]any)
	for _, item := range items {
		m := item.(map[string]any)
		if m["card_number"] != "[REDACTED]" {
			t.Errorf("array element card_number not masked: %v", m["card_number"])
		}
		if m["id"] == "[REDACTED]" {
			t.Errorf("array element id should not be masked")
		}
	}
}
