// This file tests the ordered JSON writer from inside the package. Every golden here
// is written by hand from the reserved key table of SPEC-core-v2, so a change to the
// writer has to argue with the spec rather than with a previous run.
package wlog

import (
	"os"
	"path/filepath"
	"testing"
)

// TestShape_BET13_KeyOrderGolden proves the writer lays out one event per kind in the
// table order, with the empty values left out.
func TestShape_BET13_KeyOrderGolden(t *testing.T) {
	trace := map[string]any{
		"trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
		"span_id":  "00f067aa0ba902b7",
	}
	identity := map[string]any{
		"timestamp": "2026-01-01T00:00:00Z", "level": "info",
		"event_id": "0191f0b9-1c2f-7a3d-8e4f-0a1b2c3d4e5f", "outcome": "success",
	}
	with := func(key string, value any, duration float64) map[string]any {
		out := map[string]any{"trace": trace, "wlog": map[string]any{"schema_version": 2}}
		for k, v := range identity {
			out[k] = v
		}
		out[key] = value
		out["duration_ms"] = duration
		return out
	}

	service := map[string]any{"name": "shop", "version": "1.2.3", "env": "prod"}
	work := with("kind", "work", 12.5)
	work["operation"] = "checkout"
	work["service"] = service
	work["order_id"] = "A-1"
	work["wlog"] = map[string]any{"schema_version": 2, "redact_fingerprint": "3f2a91"}

	request := with("kind", "request", 8.25)
	request["operation"] = "GET /orders/{id}"
	request["service"] = service
	request["http"] = map[string]any{"method": "GET", "route": "/orders/{id}", "status": 200}
	request["order_id"] = "A-1"

	rpc := with("kind", "rpc", 3.5)
	rpc["operation"] = "grpc OrderService/Get"
	rpc["rpc"] = map[string]any{"system": "grpc", "service": "OrderService", "method": "Get", "status_code": 0}

	message := with("kind", "message", 2.125)
	message["operation"] = "process orders.created"
	message["messaging"] = map[string]any{"system": "kafka", "operation": "process", "destination": "orders"}

	job := with("kind", "job", 1500.75)
	job["operation"] = "reindex"
	job["job"] = map[string]any{"name": "reindex", "attempt": 1}

	command := with("kind", "command", 42.5)
	command["operation"] = "wlog map"
	command["cli"] = map[string]any{"name": "wlog map"}

	function := with("kind", "function", 6.75)
	function["operation"] = "handler"
	function["faas"] = map[string]any{"name": "handler", "cold_start": false}

	// A log line holds a message and no duration, because it records no work.
	logLine := map[string]any{
		"timestamp": "2026-01-01T00:00:00Z", "level": "warn", "operation": "retry",
		"kind": "log", "outcome": "success", "message": "attempt 2",
		"event_id": "0191f0b9-1c2f-7a3d-8e4f-0a1b2c3d4e5f",
		"wlog":     map[string]any{"schema_version": 2},
	}

	for _, tc := range []struct {
		kind  string
		event map[string]any
	}{
		{"work", work}, {"request", request}, {"rpc", rpc}, {"message", message},
		{"job", job}, {"command", command}, {"function", function}, {"log", logLine},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			got, err := encodeEvent(tc.event)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata", "shape", tc.kind+".json"))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want) {
				t.Errorf("the %s layout does not match the spec\n got: %s\nwant: %s", tc.kind, got, want)
			}
		})
	}
}
