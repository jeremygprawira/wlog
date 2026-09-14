package wlogstd_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
)

func TestMiddleware_CapturesBodies(t *testing.T) {
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON))
	var handlerSawBody string
	handler := wlogstd.Middleware(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		handlerSawBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	}))

	req := httptest.NewRequest(http.MethodPost, "/orders", bytes.NewBufferString(`{"item":"shoes"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	out := captureStdout(t, func() { handler.ServeHTTP(rec, req) })

	if handlerSawBody != `{"item":"shoes"}` {
		t.Errorf("handler did not see the full request body: %q", handlerSawBody)
	}

	var got map[string]any
	json.Unmarshal([]byte(out), &got)
	httpField := got["http"].(map[string]any)
	reqBody := httpField["request_body"].(map[string]any)
	if reqBody["item"] != "shoes" {
		t.Errorf("request_body = %v", reqBody)
	}
	respBody := httpField["response_body"].(map[string]any)
	if respBody["status"] != "ok" {
		t.Errorf("response_body = %v", respBody)
	}
}

func TestMiddleware_Body_ContentTypeFilter(t *testing.T) {
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON))
	handler := wlogstd.Middleware(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte{0xFF, 0xD8, 0xFF})
	}))

	req := httptest.NewRequest(http.MethodPost, "/upload", bytes.NewBufferString("binary-ish"))
	req.Header.Set("Content-Type", "application/octet-stream")
	rec := httptest.NewRecorder()
	out := captureStdout(t, func() { handler.ServeHTTP(rec, req) })

	var got map[string]any
	json.Unmarshal([]byte(out), &got)
	httpField := got["http"].(map[string]any)
	if _, ok := httpField["request_body"]; ok {
		t.Error("request_body captured for a non-allowed content type")
	}
	if _, ok := httpField["response_body"]; ok {
		t.Error("response_body captured for a non-allowed content type")
	}
}

func TestMiddleware_Body_Cap(t *testing.T) {
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON))
	big := strings.Repeat("x", 200)
	handler := wlogstd.Middleware(log, wlogstd.MaxBodyCapture(10))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			if len(b) != len(big) {
				t.Errorf("handler saw a truncated body: got %d bytes, want %d", len(b), len(big))
			}
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(big))
		}),
	)

	req := httptest.NewRequest(http.MethodPost, "/echo", bytes.NewBufferString(big))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	out := captureStdout(t, func() { handler.ServeHTTP(rec, req) })

	var got map[string]any
	json.Unmarshal([]byte(out), &got)
	httpField := got["http"].(map[string]any)
	reqBody := httpField["request_body"].(map[string]any)
	if raw, ok := reqBody["raw"].(string); !ok || len(raw) != 10 {
		t.Errorf("request_body not capped at 10 bytes: %v", reqBody)
	}
}

func TestMiddleware_CaptureBodyOff(t *testing.T) {
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON))
	handler := wlogstd.Middleware(log, wlogstd.CaptureBody(false))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"ok":true}`))
		}),
	)

	req := httptest.NewRequest(http.MethodPost, "/x", bytes.NewBufferString(`{"a":1}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	out := captureStdout(t, func() { handler.ServeHTTP(rec, req) })

	var got map[string]any
	json.Unmarshal([]byte(out), &got)
	httpField := got["http"].(map[string]any)
	if _, ok := httpField["request_body"]; ok {
		t.Error("request_body captured despite CaptureBody(false)")
	}
	if _, ok := httpField["response_body"]; ok {
		t.Error("response_body captured despite CaptureBody(false)")
	}
}
