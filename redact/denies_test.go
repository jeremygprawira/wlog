package redact_test

import (
	"testing"

	"github.com/jeremygprawira/wlog/redact"
)

// TestRedactor_Denies proves the key check a linter needs: a denied key name reports
// true, a safe name reports false, and the check follows the active config.
func TestRedactor_Denies(t *testing.T) {
	defaults := redact.Default()
	for _, key := range []string{"password", "stripe_api_key", "user_pin", "x_api_key", "authorization"} {
		if !defaults.Denies(key) {
			t.Errorf("Denies(%q) = false, want true", key)
		}
	}
	for _, key := range []string{"order_id", "http.status", "service.name", "duration_ms"} {
		if defaults.Denies(key) {
			t.Errorf("Denies(%q) = true, want false", key)
		}
	}

	removed, err := defaults.With(redact.RemoveKeys("password"))
	if err != nil {
		t.Fatalf("With: %v", err)
	}
	if removed.Denies("password") {
		t.Error("Denies(password) = true after RemoveKeys, want false")
	}
}
