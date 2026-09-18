package wlogstd_test

import (
	"context"
	"encoding/json"
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
	f, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer f.Close()

	orig := os.Stdout
	os.Stdout = f
	fn()
	os.Stdout = orig

	out, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	return string(out)
}

// flushWriter waits until log's writer has written every line it queued, so a test reads
// the output of the async writer.
func flushWriter(t *testing.T, log *wlog.Logger) {
	t.Helper()
	if err := log.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}
}

func TestMiddleware_ReusesIncomingRequestID(t *testing.T) {
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON))
	handler := wlogstd.Middleware(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "existing-id")
	rec := httptest.NewRecorder()

	out := captureStdout(t, func() {
		handler.ServeHTTP(rec, req)
		flushWriter(t, log)
	})

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
	out := captureStdout(t, func() {
		handler.ServeHTTP(rec, req)
		flushWriter(t, log)
	})

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
	out := captureStdout(t, func() {
		handler.ServeHTTP(rec, req)
		flushWriter(t, log)
	})

	var got map[string]any
	_ = json.Unmarshal([]byte(out), &got)
	httpField := got["http"].(map[string]any)
	if httpField["route"] != "/no-mux" {
		t.Errorf("route fallback = %v, want /no-mux", httpField["route"])
	}
}
