// This file tests the shared conformance harness: what Normalize removes, and what Diff
// names when two events differ.
package conformance_test

import (
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog/internal/conformance"
)

// TestConformance_HTTP22_NormalizeRemovesRunValues proves that Normalize removes the values
// that change between runs, and keeps the ones that do not.
func TestConformance_HTTP22_NormalizeRemovesRunValues(t *testing.T) {
	event := map[string]any{
		"timestamp":   "2026-01-01T00:00:00Z",
		"event_id":    "0191f0b9-1c2f-7a3d-8e4f-0a1b2c3d4e5f",
		"level":       "info",
		"duration_ms": 8.25,
		"summary":     "GET /ok 200 in 8.25ms",
		"service":     map[string]any{"name": "shop", "instance": "i-1"},
		"trace": map[string]any{
			"trace_id":   "4bf92f3577b34da6a3ce929d0e0e4736",
			"span_id":    "00f067aa0ba902b7",
			"request_id": "req-1",
		},
		"http": map[string]any{"host": "127.0.0.1:54321", "status": 200},
		"job":  map[string]any{"lag_ms": 12.5},
		"error": map[string]any{
			"code": "INTERNAL", "message": "panic: boom",
			"caller": "exchange.go:108", "stack": "goroutine 20 [running]:\n...",
		},
	}

	got := conformance.Normalize(event)

	for _, key := range []string{"timestamp", "event_id", "duration_ms"} {
		if _, present := got[key]; present {
			t.Errorf("Normalize kept %q: %v", key, got)
		}
	}
	trace, _ := got["trace"].(map[string]any)
	for _, key := range []string{"trace_id", "span_id"} {
		if _, present := trace[key]; present {
			t.Errorf("Normalize kept trace.%s: %v", key, trace)
		}
	}
	if trace["request_id"] != "req-1" {
		t.Errorf("trace.request_id = %v, want the request id kept", trace["request_id"])
	}
	service, _ := got["service"].(map[string]any)
	if _, present := service["instance"]; present {
		t.Errorf("Normalize kept service.instance: %v", service)
	}
	if service["name"] != "shop" {
		t.Errorf("service.name = %v, want the name kept", service["name"])
	}
	if job, _ := got["job"].(map[string]any); job["lag_ms"] != nil {
		t.Errorf("Normalize kept a nested _ms key: %v", got["job"])
	}
	errorGroup, _ := got["error"].(map[string]any)
	for _, key := range []string{"caller", "stack"} {
		if _, present := errorGroup[key]; present {
			t.Errorf("Normalize kept error.%s: %v", key, errorGroup)
		}
	}
	if errorGroup["code"] != "INTERNAL" || errorGroup["message"] != "panic: boom" {
		t.Errorf("Normalize dropped a stable error field: %v", errorGroup)
	}
	if got["summary"] != "GET /ok 200 in {d}" {
		t.Errorf("summary = %q, want the duration text replaced", got["summary"])
	}
	if httpFields, _ := got["http"].(map[string]any); httpFields["host"] != "127.0.0.1" {
		t.Errorf("http.host = %v, want the port removed", httpFields["host"])
	}

	// The original event is untouched, so a suite can normalize one event twice.
	if event["duration_ms"] != 8.25 {
		t.Errorf("Normalize changed the event it was given: %v", event)
	}
}

// TestConformance_HTTP22_DiffNamesFields proves that Diff names each field that differs,
// and that two equal events give an empty difference.
func TestConformance_HTTP22_DiffNamesFields(t *testing.T) {
	want := map[string]any{"level": "info", "http": map[string]any{"status": 200, "route": "/ok"}}
	got := map[string]any{"level": "warn", "http": map[string]any{"status": 503, "extra": true}}

	diff := conformance.Diff(want, got)
	for _, fragment := range []string{"level: want info, got warn", "http.status: want 200, got 503", "http.extra"} {
		if !strings.Contains(diff, fragment) {
			t.Errorf("Diff does not name %q:\n%s", fragment, diff)
		}
	}
	if missing := conformance.Diff(want, want); missing != "" {
		t.Errorf("Diff of two equal events = %q, want an empty string", missing)
	}
}

// TestConformance_HTTP22_NormalizeCanonicalRoute proves that Normalize rewrites every
// route to the braced form, so a suite compares the shape of a route and not the dialect
// of one router.
func TestConformance_HTTP22_NormalizeCanonicalRoute(t *testing.T) {
	got := conformance.Normalize(map[string]any{
		"operation": "POST /orders/:id",
		"summary":   "POST /orders/:id 201 in 0.5ms",
		"http":      map[string]any{"route": "/orders/:id"},
	})

	if got["operation"] != "POST /orders/{id}" {
		t.Errorf("operation = %q, want the braced route", got["operation"])
	}
	httpFields, _ := got["http"].(map[string]any)
	if httpFields["route"] != "/orders/{id}" {
		t.Errorf("http.route = %q, want the braced route", httpFields["route"])
	}
	if got["summary"] != "POST /orders/{id} 201 in {d}" {
		t.Errorf("summary = %q, want the braced route and the duration placeholder", got["summary"])
	}
}
