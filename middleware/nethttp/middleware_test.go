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
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

func TestMiddleware_ReusesIncomingRequestID(t *testing.T) {
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON))
	handler := wlogstd.Middleware(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "existing-id")
	rec := httptest.NewRecorder()

	out := captureStdout(t, func() { handler.ServeHTTP(rec, req) })

	var got map[string]any
	_ = json.Unmarshal([]byte(out), &got)
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
	_ = json.Unmarshal([]byte(out), &got)
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
	_ = json.Unmarshal([]byte(out), &got)
	httpField := got["http"].(map[string]any)
	if httpField["route"] != "/no-mux" {
		t.Errorf("route fallback = %v, want /no-mux", httpField["route"])
	}
}
