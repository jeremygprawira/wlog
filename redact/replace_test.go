package redact_test

import (
	"testing"

	"github.com/jeremygprawira/wlog/redact"
)

// TestRedact_ReplaceFunc proves the mask comes from the function, so a caller can keep
// part of the value.
func TestRedact_ReplaceFunc(t *testing.T) {
	r := redact.MustNew(redact.ReplaceFunc(func(match string) string {
		if len(match) <= 4 {
			return "***"
		}
		return "***" + match[len(match)-4:]
	}))

	event := map[string]any{"password": "hunter2secret"}
	r.Apply(event)
	if event["password"] != "***cret" {
		t.Errorf("mask = %v, want ***cret", event["password"])
	}
}

// TestRedact_ReplaceFuncPanic proves a panicking function falls back to the fixed text.
func TestRedact_ReplaceFuncPanic(t *testing.T) {
	r := redact.MustNew(redact.ReplaceFunc(func(string) string { panic("bad mask") }))

	event := map[string]any{"password": "hunter2"}
	r.Apply(event)
	if event["password"] != "[REDACTED]" {
		t.Errorf("panic mask = %v, want [REDACTED]", event["password"])
	}
	if event["password"] == "hunter2" {
		t.Error("the raw value survived a panicking mask function")
	}
}
