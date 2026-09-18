// This file tests the pretty console from inside the package, because the golden comes
// from the layout of SPEC-core-v2 and not from a rendered run of the code under test.
package wlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// prettyGoldenEvent is one request that failed, with a call, a folded log record, a
// trace, a service, and a user key, so one golden fixes the whole layout.
func prettyGoldenEvent() map[string]any {
	return map[string]any{
		"timestamp":   "2026-01-01T00:00:00Z",
		"level":       "error",
		"summary":     "POST /orders/{id} 502 in 840.2ms: PAYMENT_DECLINED card declined (fix: Ask the customer for another card.) (order_id=4821)",
		"operation":   "POST /orders/{id}",
		"kind":        "request",
		"outcome":     "error",
		"duration_ms": 840.2,
		"event_id":    "0191f0b9-1c2f-7a3d-8e4f-0a1b2c3d4e5f",
		"service":     map[string]any{"name": "shop", "version": "1.2.3", "env": "prod"},
		"http": map[string]any{
			"method": "POST", "route": "/orders/{id}", "status": 502,
			"bytes_in": 76, "bytes_out": 37,
		},
		"trace": map[string]any{
			"trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
			"span_id":  "00f067aa0ba902b7", "request_id": "8a3692537f716fa0",
		},
		"error": map[string]any{
			"code": "PAYMENT_DECLINED", "message": "card declined",
			"why":    "The issuer rejected the charge.",
			"fix":    "Ask the customer for another card.",
			"link":   "https://docs.example.com/errors/PAYMENT_DECLINED",
			"caller": "orders/charge.go:88",
		},
		"calls": []any{map[string]any{
			"operation": "POST", "target": "api.stripe.com/v1/charges",
			"status": "402", "duration_ms": 801.5,
		}},
		"logs": []any{map[string]any{
			"level": "info", "msg": "cache miss",
			"attrs": map[string]any{"key": "order:4821"},
		}},
		"order_id": "4821",
	}
}

// TestPretty_BET15_Golden proves that one rendered event matches the layout of
// SPEC-core-v2: the sentence without its fix, the error block, one tree line per group,
// one line per call and per log record, and the user keys last.
func TestPretty_BET15_Golden(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "pretty", "request.txt"))
	if err != nil {
		t.Fatal(err)
	}
	got := renderPretty(prettyGoldenEvent(), false)
	if string(got) != string(want) {
		t.Errorf("the pretty layout does not match the spec\n got:\n%s\nwant:\n%s", got, want)
	}
}

// TestPretty_ColorEscape proves that the level alone is colored, and that a caller may
// turn color off for a plain terminal or a file.
func TestPretty_ColorEscape(t *testing.T) {
	colored := string(renderPretty(prettyGoldenEvent(), true))
	if !strings.Contains(colored, "\033[31mERROR\033[0m") {
		t.Errorf("the colored line does not color the level: %q", strings.SplitN(colored, "\n", 2)[0])
	}
	if strings.Contains(string(renderPretty(prettyGoldenEvent(), false)), "\033[") {
		t.Error("the plain line carries an escape")
	}
}
