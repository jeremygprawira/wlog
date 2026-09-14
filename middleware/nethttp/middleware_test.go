package wlogstd_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jeremygprawira/wlog"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
)

// captureStdout mirrors the root package's test helper (unexported, so duplicated
// here); see wlog_test.go for the original.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	fn()
	os.Stdout = orig
	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func TestMiddleware_BasicFields(t *testing.T) {
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		wlog.Set(r.Context(), "order_id", r.PathValue("id"))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	handler := wlogstd.Middleware(log)(mux)

	req := httptest.NewRequest(http.MethodGet, "/orders/4821", nil)
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
		t.Errorf("http.route = %v, want the ServeMux pattern", httpField["route"])
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

func TestMiddleware_ReusesIncomingRequestID(t *testing.T) {
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON))
	handler := wlogstd.Middleware(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "existing-id")
	rec := httptest.NewRecorder()

	out := captureStdout(t, func() { handler.ServeHTTP(rec, req) })

	var got map[string]any
	json.Unmarshal([]byte(out), &got)
	trace := got["trace"].(map[string]any)
	if trace["request_id"] != "existing-id" {
		t.Errorf("trace.request_id = %v, want existing-id (reused)", trace["request_id"])
	}
	if rec.Header().Get("X-Request-ID") != "existing-id" {
		t.Errorf("X-Request-ID not echoed: %v", rec.Header().Get("X-Request-ID"))
	}
}

func TestMiddleware_WithRouteFunc(t *testing.T) {
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON))
	handler := wlogstd.Middleware(log, wlogstd.WithRouteFunc(func(r *http.Request) string {
		return "custom:" + r.URL.Path
	}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	rec := httptest.NewRecorder()
	out := captureStdout(t, func() { handler.ServeHTTP(rec, req) })

	var got map[string]any
	json.Unmarshal([]byte(out), &got)
	httpField := got["http"].(map[string]any)
	if httpField["route"] != "custom:/anything" {
		t.Errorf("route = %v, want custom:/anything", httpField["route"])
	}
}

func TestMiddleware_NoPattern_FallsBackToPath(t *testing.T) {
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON))
	// A bare http.HandlerFunc (no ServeMux) never populates r.Pattern.
	handler := wlogstd.Middleware(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest(http.MethodGet, "/no-mux", nil)
	rec := httptest.NewRecorder()
	out := captureStdout(t, func() { handler.ServeHTTP(rec, req) })

	var got map[string]any
	json.Unmarshal([]byte(out), &got)
	httpField := got["http"].(map[string]any)
	if httpField["route"] != "/no-mux" {
		t.Errorf("route fallback = %v, want /no-mux", httpField["route"])
	}
}
