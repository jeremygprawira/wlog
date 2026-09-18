package wlogstd_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
)

func TestMiddleware_Panic_Recovers500AndLogs(t *testing.T) {
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON))
	handler := wlogstd.Middleware(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("kaboom")
	}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	out := captureStdout(t, func() { handler.ServeHTTP(rec, req) })

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}

	var got map[string]any
	_ = json.Unmarshal([]byte(out), &got)
	if got["level"] != "error" {
		t.Errorf("level = %v, want error", got["level"])
	}
	errInfo := got["error"].(map[string]any)
	if !strings.Contains(errInfo["message"].(string), "kaboom") {
		t.Errorf("error.message = %v, want it to mention the panic value", errInfo["message"])
	}
	if errInfo["stack"] == nil || errInfo["stack"] == "" {
		t.Error("error.stack missing for a recovered panic")
	}

	// The process itself must keep serving — a second, normal request still works.
	req2 := httptest.NewRequest(http.MethodGet, "/y", nil)
	rec2 := httptest.NewRecorder()
	handler2 := wlogstd.Middleware(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	captureStdout(t, func() { handler2.ServeHTTP(rec2, req2) })
	if rec2.Code != http.StatusOK {
		t.Errorf("second request status = %d, want 200 (process must keep serving)", rec2.Code)
	}
}

func TestMiddleware_Traceparent(t *testing.T) {
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON))
	handler := wlogstd.Middleware(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	rec := httptest.NewRecorder()
	out := captureStdout(t, func() { handler.ServeHTTP(rec, req) })

	var got map[string]any
	_ = json.Unmarshal([]byte(out), &got)
	trace := got["trace"].(map[string]any)
	if trace["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace.trace_id = %v", trace["trace_id"])
	}
	if trace["span_id"] != "00f067aa0ba902b7" {
		t.Errorf("trace.span_id = %v", trace["span_id"])
	}
}

func TestMiddleware_Traceparent_InvalidIsIgnored(t *testing.T) {
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON))
	handler := wlogstd.Middleware(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("traceparent", "not-a-valid-header")
	rec := httptest.NewRecorder()
	out := captureStdout(t, func() { handler.ServeHTTP(rec, req) })

	var got map[string]any
	_ = json.Unmarshal([]byte(out), &got)
	// Core names the trace of every event of work, so an absent trace_id would be
	// wrong here. What matters is that the invalid header was not adopted.
	trace, _ := got["trace"].(map[string]any)
	if got := trace["trace_id"]; got == "not-a-valid-header" {
		t.Errorf("trace_id came from an invalid traceparent: %v", got)
	}
}

func TestMiddleware_WithUserFunc(t *testing.T) {
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON))
	handler := wlogstd.Middleware(log, wlogstd.WithUserFunc(func(r *http.Request) string {
		return r.Header.Get("X-User-ID")
	}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-User-ID", "u_42")
	rec := httptest.NewRecorder()
	out := captureStdout(t, func() { handler.ServeHTTP(rec, req) })

	var got map[string]any
	_ = json.Unmarshal([]byte(out), &got)
	user := got["user"].(map[string]any)
	if user["id"] != "u_42" {
		t.Errorf("user.id = %v, want u_42", user["id"])
	}
}

type hookPlugin struct {
	started, finished bool
}

func (*hookPlugin) Name() string { return "hooks" }
func (p *hookPlugin) OnStart(ctx context.Context, _ string) context.Context {
	p.started = true
	return ctx
}
func (p *hookPlugin) OnFinish(context.Context, wlog.Event) { p.finished = true }

func TestMiddleware_PluginRequestHooks(t *testing.T) {
	p := &hookPlugin{}
	log := wlog.New(wlog.WithFormat(wlog.FormatJSON), wlog.WithPlugins(p))
	handler := wlogstd.Middleware(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !p.started {
			t.Error("OnStart had not run before the handler")
		}
	}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	captureStdout(t, func() { handler.ServeHTTP(rec, req) })

	if !p.started {
		t.Error("OnStart was not called")
	}
	if !p.finished {
		t.Error("OnFinish was not called")
	}
}
