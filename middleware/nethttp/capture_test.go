package wlogstd_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremygprawira/wlog"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
)

func TestMiddleware_CapturesHeadersQueryCookies(t *testing.T) {
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON))
	handler := wlogstd.Middleware(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest(http.MethodGet, "/search?q=shoes", nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer secret-token")
	req.AddCookie(&http.Cookie{Name: "session", Value: "abc123"})
	rec := httptest.NewRecorder()

	out := captureStdout(t, func() { handler.ServeHTTP(rec, req) })

	var got map[string]any
	json.Unmarshal([]byte(out), &got)
	httpField := got["http"].(map[string]any)

	headers := httpField["request_headers"].(map[string]any)
	if headers["Accept"] != "application/json" {
		t.Errorf("headers = %v", headers)
	}
	if headers["Authorization"] != "[REDACTED]" {
		t.Errorf("Authorization header not redacted: %v", headers["Authorization"])
	}

	query := httpField["request_query"].(map[string]any)
	if query["q"] != "shoes" {
		t.Errorf("query = %v", query)
	}

	cookies := httpField["request_cookies"].(map[string]any)
	if cookies["session"] != "[REDACTED]" {
		t.Errorf("session cookie not redacted: %v", cookies["session"])
	}
}

func TestMiddleware_CaptureTogglesOff(t *testing.T) {
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON))
	handler := wlogstd.Middleware(log,
		wlogstd.CaptureHeaders(false),
		wlogstd.CaptureQuery(false),
		wlogstd.CaptureCookies(false),
	)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest(http.MethodGet, "/search?q=shoes", nil)
	req.Header.Set("Accept", "application/json")
	req.AddCookie(&http.Cookie{Name: "session", Value: "abc123"})
	rec := httptest.NewRecorder()

	out := captureStdout(t, func() { handler.ServeHTTP(rec, req) })

	var got map[string]any
	json.Unmarshal([]byte(out), &got)
	httpField := got["http"].(map[string]any)
	if _, ok := httpField["request_headers"]; ok {
		t.Error("request_headers present despite CaptureHeaders(false)")
	}
	if _, ok := httpField["request_query"]; ok {
		t.Error("request_query present despite CaptureQuery(false)")
	}
	if _, ok := httpField["request_cookies"]; ok {
		t.Error("request_cookies present despite CaptureCookies(false)")
	}
}

func TestMiddleware_SkipPaths_EmitsNothing(t *testing.T) {
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON))
	called := false
	handler := wlogstd.Middleware(log, wlogstd.SkipPaths("/health"))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }),
	)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	out := captureStdout(t, func() { handler.ServeHTTP(rec, req) })

	if out != "" {
		t.Errorf("expected no event for a skipped path, got %q", out)
	}
	if !called {
		t.Error("the handler itself must still run for a skipped path")
	}
}
