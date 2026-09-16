package wlogstd_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremygprawira/wlog"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
)

func TestMiddleware_ClientIPUserAgentBytesIn(t *testing.T) {
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON))
	handler := wlogstd.Middleware(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.ContentLength = 7
	req.Header.Set("User-Agent", "curl/8.0")
	req.RemoteAddr = "203.0.113.9:54321"
	rec := httptest.NewRecorder()
	out := captureStdout(t, func() { handler.ServeHTTP(rec, req) })

	var got map[string]any
	_ = json.Unmarshal([]byte(out), &got)
	httpField := got["http"].(map[string]any)

	if httpField["user_agent"] != "curl/8.0" {
		t.Errorf("http.user_agent = %v, want curl/8.0", httpField["user_agent"])
	}
	if httpField["client_ip"] != "203.0.113.9" {
		t.Errorf("http.client_ip = %v, want 203.0.113.9 (port stripped)", httpField["client_ip"])
	}
	if httpField["bytes_in"] != float64(7) {
		t.Errorf("http.bytes_in = %v, want 7", httpField["bytes_in"])
	}
}

func TestMiddleware_ClientIP_PrefersForwardedHeader(t *testing.T) {
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON))
	handler := wlogstd.Middleware(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")
	rec := httptest.NewRecorder()
	out := captureStdout(t, func() { handler.ServeHTTP(rec, req) })

	var got map[string]any
	_ = json.Unmarshal([]byte(out), &got)
	httpField := got["http"].(map[string]any)
	if httpField["client_ip"] != "203.0.113.9" {
		t.Errorf("client_ip = %v, want 203.0.113.9 (first X-Forwarded-For entry)", httpField["client_ip"])
	}
}
