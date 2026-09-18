// Package conformance is the shared contract every wlog HTTP adapter (net/http,
// gorilla/mux, and later Echo/Gin) must pass, per SPEC-http-std.md. One adapter today
// (wlogstd) implements Adapter; Run exercises the behavior every adapter must match,
// so a second adapter can be dropped in and immediately checked against the same bar.
package conformance

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jeremygprawira/wlog"
)

// Routes are the handlers every adapter must wire up the same way, at these fixed
// paths, so the suite can be framework-agnostic (no path-parameter syntax to agree on).
type Routes struct {
	OK    http.HandlerFunc // GET /ok — returns 200; may enrich the event via wlog.Set
	Panic http.HandlerFunc // GET /panic — panics
	Echo  http.HandlerFunc // POST /echo — reads the body, writes it back with the same Content-Type
}

// Adapter builds a full http.Handler wrapping Routes with wlog middleware, and must
// apply a "skip this path" rule (however that adapter spells it) to "/skip".
type Adapter interface {
	Build(log *wlog.Logger, routes Routes) http.Handler
}

// Run exercises Adapter against every conformance requirement. Call it from the
// adapter module's own tests: conformance.Run(t, myAdapter{}).
func Run(t *testing.T, adapter Adapter) {
	t.Helper()
	t.Run("BasicFields", func(t *testing.T) { testBasicFields(t, adapter) })
	t.Run("Panic", func(t *testing.T) { testPanic(t, adapter) })
	t.Run("Traceparent", func(t *testing.T) { testTraceparent(t, adapter) })
	t.Run("SetFromHandler", func(t *testing.T) { testSetFromHandler(t, adapter) })
	t.Run("Body", func(t *testing.T) { testBody(t, adapter) })
	t.Run("SkipPath", func(t *testing.T) { testSkipPath(t, adapter) })
}

// recorder is a minimal event collector shared by every sub-test below.
type recorder struct {
	mu     sync.Mutex
	events []map[string]any
}

func (r *recorder) drain() wlog.Drain {
	return wlog.DrainFunc(func(_ context.Context, e map[string]any) {
		r.mu.Lock()
		defer r.mu.Unlock()
		cp := make(map[string]any, len(e))
		for k, v := range e {
			cp[k] = v
		}
		r.events = append(r.events, cp)
	})
}

func (r *recorder) last() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.events) == 0 {
		return nil
	}
	return r.events[len(r.events)-1]
}

func newLogger(rec *recorder) *wlog.Logger {
	return wlog.New(wlog.WithFormat(wlog.FormatJSON), wlog.WithDrains(rec.drain()))
}

func okHandler(w http.ResponseWriter, r *http.Request) {
	wlog.Set(r.Context(), "handler_field", "value")
	w.WriteHeader(http.StatusOK)
}

func panicHandler(http.ResponseWriter, *http.Request) { panic("conformance panic") }

func echoHandler(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}

func routes() Routes {
	return Routes{OK: okHandler, Panic: panicHandler, Echo: echoHandler}
}

func testBasicFields(t *testing.T, adapter Adapter) {
	rec := &recorder{}
	h := adapter.Build(newLogger(rec), routes())

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	ev := rec.last()
	if ev == nil {
		t.Fatal("no event recorded")
	}
	httpField, _ := ev["http"].(map[string]any)
	if httpField == nil || httpField["method"] != http.MethodGet {
		t.Errorf("http.method missing or wrong: %v", ev["http"])
	}
	if httpField["status"] != int64(http.StatusOK) && httpField["status"] != float64(http.StatusOK) && httpField["status"] != http.StatusOK {
		t.Errorf("http.status = %v, want 200", httpField["status"])
	}
	trace, _ := ev["trace"].(map[string]any)
	if trace == nil || trace["request_id"] == "" || trace["request_id"] == nil {
		t.Error("trace.request_id missing")
	}
	if w.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID not echoed on the response")
	}
}

func testPanic(t *testing.T, adapter Adapter) {
	rec := &recorder{}
	h := adapter.Build(newLogger(rec), routes())

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
	ev := rec.last()
	if ev == nil || ev["level"] != "error" {
		t.Errorf("panic event level = %v, want error", ev["level"])
	}

	// The process must keep serving.
	req2 := httptest.NewRequest(http.MethodGet, "/ok", nil)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Errorf("request after a panic: status = %d, want 200", w2.Code)
	}
}

func testTraceparent(t *testing.T, adapter Adapter) {
	rec := &recorder{}
	h := adapter.Build(newLogger(rec), routes())

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	trace, _ := rec.last()["trace"].(map[string]any)
	if trace["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace_id = %v", trace["trace_id"])
	}

	rec2 := &recorder{}
	h2 := adapter.Build(newLogger(rec2), routes())
	req2 := httptest.NewRequest(http.MethodGet, "/ok", nil)
	req2.Header.Set("traceparent", "garbage")
	w2 := httptest.NewRecorder()
	h2.ServeHTTP(w2, req2)
	// Core names the trace of every event of work, so an absent trace_id is not the
	// rule here. An invalid header must simply not be adopted.
	trace2, _ := rec2.last()["trace"].(map[string]any)
	if got := trace2["trace_id"]; got == "garbage" {
		t.Errorf("trace_id came from an invalid traceparent: %v", got)
	}
}

func testSetFromHandler(t *testing.T, adapter Adapter) {
	rec := &recorder{}
	h := adapter.Build(newLogger(rec), routes())

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if rec.last()["handler_field"] != "value" {
		t.Errorf("handler_field = %v, want value (set from inside the handler)", rec.last()["handler_field"])
	}
}

func testBody(t *testing.T, adapter Adapter) {
	rec := &recorder{}
	h := adapter.Build(newLogger(rec), routes())

	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(`{"a":1}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	httpField, _ := rec.last()["http"].(map[string]any)
	reqBody, _ := httpField["request_body"].(map[string]any)
	if reqBody["a"] != float64(1) {
		t.Errorf("request_body = %v", httpField["request_body"])
	}
	respBody, _ := httpField["response_body"].(map[string]any)
	if respBody["a"] != float64(1) {
		t.Errorf("response_body = %v", httpField["response_body"])
	}

	var wireCheck map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &wireCheck); err != nil || wireCheck["a"] != float64(1) {
		t.Errorf("the actual response body was altered by capture: %q", w.Body.String())
	}
}

// testSkipPath expects the adapter to route GET /skip to a real handler (Build's
// convention: the OK handler, with its own skip-this-path option applied to "/skip")
// and confirms no event is recorded for it.
func testSkipPath(t *testing.T, adapter Adapter) {
	rec := &recorder{}
	h := adapter.Build(newLogger(rec), routes())

	req := httptest.NewRequest(http.MethodGet, "/skip", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code == http.StatusNotFound {
		t.Fatal("adapter does not route /skip to a handler; wire it (e.g. to the OK handler) with its skip-path option applied to \"/skip\"")
	}
	if len(rec.events) != 0 {
		t.Errorf("expected no event for a skipped path, got %d", len(rec.events))
	}
}
