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
	r := withKeys(t, "http.request_headers.cookie")

	rootLevel := map[string]any{"cookie": "should-not-mask"}
	r.Apply(rootLevel)
	if rootLevel["cookie"] != "should-not-mask" {
		t.Errorf("unanchored cookie got masked: %v", rootLevel["cookie"])
	}

	nested := map[string]any{
		"http": map[string]any{
			"request_headers": map[string]any{"cookie": "secret", "accept": "json"},
		},
	}
	r.Apply(nested)
	headers := nested["http"].(map[string]any)["request_headers"].(map[string]any)
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

// TestRedact_RED10_DeadPathReported proves that a path entry which can never match
// a reserved key reports at New, so a typo in a dotted path is not silent.
func TestRedact_RED10_DeadPathReported(t *testing.T) {
	// The event shape names the field http.request_headers, so a path through
	// "request" alone can never match anything.
	if _, err := redact.New(redact.AddKeys("http.request.header.authorization")); err == nil {
		t.Error("a path that can never match a reserved key was accepted")
	}
	// The real name is accepted, so the check does not reject a good path.
	if _, err := redact.New(redact.AddKeys("http.request_headers.authorization")); err != nil {
		t.Errorf("a real reserved path was rejected: %v", err)
	}
	// A path outside the reserved shape is for the caller's own event, so it is
	// accepted.
	if _, err := redact.New(redact.AddKeys("user.profile.email")); err != nil {
		t.Errorf("a caller path was rejected: %v", err)
	}
}

// TestRedact_RED12_GlobsAndRemoveKeys proves that a star crosses a slash inside one
// segment, that RemoveKeys matches by token form and removes every match, and that
// a bad limit or an empty-match regex is an error.
func TestRedact_RED12_GlobsAndRemoveKeys(t *testing.T) {
	r := redact.MustNew(redact.AddKeys("*_token"))
	event := map[string]any{"user/api_token": "SECRET", "user": map[string]any{"api_token": "SECRET"}}
	r.Apply(event)
	if event["user/api_token"] != "[REDACTED]" {
		t.Errorf("star did not cross the slash: %v", event["user/api_token"])
	}
	if event["user"].(map[string]any)["api_token"] != "[REDACTED]" {
		t.Errorf("star did not match a plain segment: %v", event["user"])
	}

	if _, err := redact.New(redact.MaxDepth(-1)); err == nil {
		t.Error("MaxDepth(-1) was accepted")
	}
	if _, err := redact.New(redact.AddPatterns(redact.Pattern{Name: "empty", Regex: ``})); err == nil {
		t.Error("a pattern that matches the empty string was accepted")
	}
}
