// This file runs the http conformance suite against the chi adapter, and checks the
// behavior that only chi has: the route pattern from chi's RouteContext, a blank route on
// an unmatched path and on a method that no route allows, and the one-line Setup.
package wlogchi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	httpconformance "github.com/jeremygprawira/wlog/internal/conformance/http"
	wlogchi "github.com/jeremygprawira/wlog/middleware/chi"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestChi_Conformance proves that the chi adapter passes every scenario of the http
// suite.
func TestChi_Conformance(t *testing.T) {
	httpconformance.Run(conformance.Tester{T: t}, chiFactory{})
}

// chiFactory builds the adapter around a chi router of the suite's route table.
type chiFactory struct{}

// Build returns the chi router with the middleware.
func (chiFactory) Build(log *wlog.Logger, routes httpconformance.Routes, settings httpconformance.Settings) http.Handler {
	r := chi.NewRouter()
	// chi answers an unmatched path with no trailing newline, and the suite's golden
	// carries the plain net/http body, so the factory gives chi the same 404 body.
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("404 page not found\n"))
	})

	opts := []wlogchi.Option{wlogchi.SkipPaths("/skip")}
	if settings.CaptureAll {
		opts = append(opts, wlogchi.CaptureAll())
	}
	if settings.MaxBody > 0 {
		opts = append(opts, wlogchi.MaxBodyCapture(settings.MaxBody))
	}
	r.Use(wlogchi.Middleware(log, opts...))

	r.Handle("/ok", routes.OK)
	r.Handle("/orders/{id}", routes.Order)
	r.Handle("/panic", routes.Panic)
	r.Handle("/status/{code}", routes.Status)
	r.Handle("/stream", routes.Stream)
	r.Handle("/fail", routes.Fail)
	r.Handle("/skip", routes.OK)
	return r
}

// TestChi_HTTP15_UnmatchedRouteBlank proves that an unmatched path logs status 404 with an
// empty route, as the shared table asks.
func TestChi_HTTP15_UnmatchedRouteBlank(t *testing.T) {
	log, rec := wlogtest.New(t)
	r := chi.NewRouter()
	r.Use(wlogchi.Middleware(log))
	r.Get("/ok", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/nope", nil))

	event := rec.Last()
	httpFields, _ := event["http"].(map[string]any)
	if httpFields["route"] != "" {
		t.Errorf("http.route = %v, want an empty route", httpFields["route"])
	}
	if !conformance.Equal(httpFields["status"], http.StatusNotFound) {
		t.Errorf("http.status = %v, want 404", httpFields["status"])
	}
	if event["operation"] != "GET unmatched" {
		t.Errorf("operation = %v, want GET unmatched", event["operation"])
	}
}

// TestChi_HTTP15_MethodNotAllowedBlank proves that a method no route allows logs status
// 405 with an empty route.
func TestChi_HTTP15_MethodNotAllowedBlank(t *testing.T) {
	log, rec := wlogtest.New(t)
	r := chi.NewRouter()
	r.Use(wlogchi.Middleware(log))
	r.Get("/ok", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/ok", nil))

	event := rec.Last()
	httpFields, _ := event["http"].(map[string]any)
	if httpFields["route"] != "" {
		t.Errorf("http.route = %v, want an empty route", httpFields["route"])
	}
	if !conformance.Equal(httpFields["status"], http.StatusMethodNotAllowed) {
		t.Errorf("http.status = %v, want 405", httpFields["status"])
	}
	if event["level"] != "warn" {
		t.Errorf("level = %v, want warn", event["level"])
	}
}

// TestChi_Route_UsesRoutePattern proves that the route is chi's own route pattern.
func TestChi_Route_UsesRoutePattern(t *testing.T) {
	log, rec := wlogtest.New(t)
	r := chi.NewRouter()
	r.Use(wlogchi.Middleware(log))
	r.Get("/users/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users/42", nil))

	httpFields, _ := rec.Last()["http"].(map[string]any)
	if httpFields["route"] != "/users/{id}" {
		t.Errorf("http.route = %v, want /users/{id}", httpFields["route"])
	}
	if rec.Last()["operation"] != "GET /users/{id}" {
		t.Errorf("operation = %v, want GET /users/{id}", rec.Last()["operation"])
	}
}

// TestChi_Setup_InstallsMiddleware proves that Setup wires the default Logger into a chi
// router, so one request becomes one event.
func TestChi_Setup_InstallsMiddleware(t *testing.T) {
	log, rec := wlogtest.New(t)
	previous := wlog.Default()
	wlog.SetDefault(log)
	t.Cleanup(func() { wlog.SetDefault(previous) })

	r := chi.NewRouter()
	wlogchi.Setup(r)
	r.Get("/ok", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ok", nil))

	if count := rec.Count(); count != 1 {
		t.Fatalf("events = %d, want 1", count)
	}
	httpFields, _ := rec.Last()["http"].(map[string]any)
	if httpFields["route"] != "/ok" {
		t.Errorf("http.route = %v, want /ok", httpFields["route"])
	}
}
