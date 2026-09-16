//go:build go1.23

// The go1.23 build tag selects this test file, because it sets the Pattern field
// of a request, and that field arrives in Go 1.22. A toolchain older than 1.23
// compiles the middleware without it.
//
// The test sets the field itself instead of registering a ServeMux pattern.
// The go line of this module says 1.21, so the go command asks net/http for the
// old ServeMux through GODEBUG=httpmuxgo121=1, and that router never sets the
// field. The field itself holds the same value either way.
package wlogstd_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremygprawira/wlog"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
)

func TestMiddleware_BasicFields(t *testing.T) {
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON))
	handler := wlogstd.Middleware(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wlog.Set(r.Context(), "order_id", "4821")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/orders/4821", nil)
	req.Pattern = "GET /orders/{id}"
	rec := httptest.NewRecorder()

	out := captureStdout(t, func() { handler.ServeHTTP(rec, req) })

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON line: %v\noutput: %q", err, out)
	}
	httpField := got["http"].(map[string]any)
	if httpField["method"] != "GET" {
		t.Errorf("http.method = %v, want GET", httpField["method"])
	}
	if httpField["route"] != "GET /orders/{id}" {
		t.Errorf("http.route = %v, want the request pattern", httpField["route"])
	}
	if httpField["path"] != "/orders/4821" {
		t.Errorf("http.path = %v, want /orders/4821", httpField["path"])
	}
	if httpField["status"] != float64(200) {
		t.Errorf("http.status = %v, want 200", httpField["status"])
	}
	if _, ok := httpField["duration_ms"]; !ok {
		t.Error("http.duration_ms missing")
	}
	if httpField["bytes_out"] != float64(2) {
		t.Errorf("http.bytes_out = %v, want 2", httpField["bytes_out"])
	}
	if got["order_id"] != "4821" {
		t.Errorf("order_id (set from inside the handler) = %v, want 4821", got["order_id"])
	}

	trace := got["trace"].(map[string]any)
	if trace["request_id"] == nil || trace["request_id"] == "" {
		t.Error("trace.request_id missing")
	}
	if rec.Header().Get("X-Request-ID") != trace["request_id"] {
		t.Errorf("X-Request-ID header = %v, want it to match trace.request_id %v", rec.Header().Get("X-Request-ID"), trace["request_id"])
	}
}
