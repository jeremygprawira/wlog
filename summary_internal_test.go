// This file tests the summary builder from inside the package, where a test can hand it
// the event shape of each kind that SPEC-core-v2 fixes. Every expectation is the
// template of that spec.
package wlog

import (
	"strings"
	"testing"
)

// TestShape_BET5_SummaryPerKind proves the template of each kind.
func TestShape_BET5_SummaryPerKind(t *testing.T) {
	base := map[string]any{"level": "info", "outcome": "success", "duration_ms": 12.8}
	with := func(kind string, extra map[string]any) map[string]any {
		out := map[string]any{"kind": kind}
		for k, v := range base {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}

	for _, tc := range []struct {
		name  string
		event map[string]any
		want  string
	}{
		{"work", with("work", map[string]any{"operation": "checkout"}), "checkout success in 12.8ms"},
		{"request", with("request", map[string]any{"http": map[string]any{"method": "GET", "route": "/orders/{id}", "status": 200}}), "GET /orders/{id} 200 in 12.8ms"},
		{"request unmatched", with("request", map[string]any{"http": map[string]any{"method": "POST", "status": 404}}), "POST unmatched 404 in 12.8ms"},
		{"rpc", with("rpc", map[string]any{"rpc": map[string]any{"system": "grpc", "service": "OrderService", "method": "Get", "status_code": 0}}), "grpc OrderService/Get 0 in 12.8ms"},
		{"message", with("message", map[string]any{"messaging": map[string]any{"operation": "process", "destination": "orders", "delivery_count": 3}}), "process orders success in 12.8ms delivery 3"},
		{"job", with("job", map[string]any{"job": map[string]any{"name": "reindex", "attempt": 2}}), "job reindex success in 12.8ms attempt 2"},
		{"command", with("command", map[string]any{"cli": map[string]any{"path": "wlog map", "exit_code": 0}}), "wlog map exit 0 in 12.8ms"},
		{"function", with("function", map[string]any{"faas": map[string]any{"name": "handler", "trigger": "http", "cold_start": true}}), "function handler http success in 12.8ms cold start"},
		{"log", map[string]any{"kind": "log", "level": "warn", "message": "attempt 2"}, "attempt 2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := defaultSummary(tc.event, "[REDACTED]"); got != tc.want {
				t.Errorf("summary = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestShape_BET5_CatalogErrorSummary proves the error suffix, the id keys, the masked
// and long part rules, and the cap.
func TestShape_BET5_CatalogErrorSummary(t *testing.T) {
	event := map[string]any{
		"kind": "work", "operation": "refund", "outcome": "error", "level": "error",
		"duration_ms": 843.5,
		"error": map[string]any{
			"code": "PAYMENT_DECLINED", "message": "card declined",
			"fix": "Ask the customer for another card",
		},
		"order_id": "A-1", "user_id": "U-9", "tenant_id": "T-7",
	}
	got := defaultSummary(event, "[REDACTED]")
	want := "refund error in 843.5ms: PAYMENT_DECLINED card declined (fix: Ask the customer for another card) (order_id=A-1) (tenant_id=T-7)"
	if got != want {
		t.Errorf("summary = %q\n         want %q", got, want)
	}

	// A masked part, a long part, and an absent part are all left out.
	hidden := map[string]any{
		"kind": "work", "operation": "refund", "outcome": "error", "duration_ms": 2.5,
		"error":    map[string]any{"code": "E1", "message": "[REDACTED]", "fix": strings.Repeat("f", 70)},
		"user_id":  strings.Repeat("u", 70),
		"order_id": "A-1",
	}
	got = defaultSummary(hidden, "[REDACTED]")
	if strings.Contains(got, "REDACTED") || strings.Contains(got, "uuu") || strings.Contains(got, "fff") {
		t.Errorf("summary kept a masked or long part: %q", got)
	}
	if got != "refund error in 2.5ms: E1 (order_id=A-1)" {
		t.Errorf("summary = %q", got)
	}

	// The cap holds for a wide event.
	wide := map[string]any{"kind": "work", "operation": strings.Repeat("x", 60), "duration_ms": 1.0}
	wide["error"] = map[string]any{"code": "E", "message": strings.Repeat("y", 60)}
	if got := defaultSummary(wide, "[REDACTED]"); len([]rune(got)) > maxSummary {
		t.Errorf("summary is %d characters, want at most %d", len([]rune(got)), maxSummary)
	}
}

// TestShape_BET5_SummaryDurationText proves the three forms a duration takes.
func TestShape_BET5_SummaryDurationText(t *testing.T) {
	for _, tc := range []struct {
		ms   float64
		want string
	}{
		{0.84, "0.84ms"}, {12.8, "12.8ms"}, {1240, "1.24s"}, {0, ""},
	} {
		if got := durationText(tc.ms); got != tc.want {
			t.Errorf("durationText(%v) = %q, want %q", tc.ms, got, tc.want)
		}
	}
}
