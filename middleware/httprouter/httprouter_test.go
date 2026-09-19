// This file runs the http conformance suite against the httprouter wrapper, and checks
// the paths that run no handler: a trailing-slash redirect and a method that a route does
// not allow.
package wloghttprouter_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/julienschmidt/httprouter"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	httpconformance "github.com/jeremygprawira/wlog/internal/conformance/http"
	wloghttprouter "github.com/jeremygprawira/wlog/middleware/httprouter"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestHTTPRouter_Conformance proves that the httprouter wrapper passes every scenario of
// the http suite.
func TestHTTPRouter_Conformance(t *testing.T) {
	httpconformance.Run(conformance.Tester{T: t}, httprouterFactory{})
}

// httprouterFactory builds the wrapper around a httprouter router of the suite's route
// table.
type httprouterFactory struct{}

// Build returns the wrapper with the middleware.
func (httprouterFactory) Build(log *wlog.Logger, routes httpconformance.Routes, settings httpconformance.Settings) http.Handler {
	inner := httprouter.New()
	// The suite's golden carries the plain net/http 404 body, so the factory gives
	// httprouter the same body.
	inner.NotFound = http.HandlerFunc(http.NotFound)

	opts := []wloghttprouter.Option{wloghttprouter.SkipPaths("/skip")}
	if settings.CaptureAll {
		opts = append(opts, wloghttprouter.CaptureAll())
	}
	if settings.MaxBody > 0 {
		opts = append(opts, wloghttprouter.MaxBodyCapture(settings.MaxBody))
	}
	r := wloghttprouter.New(log, inner, opts...)

	wrap := func(h http.HandlerFunc) httprouter.Handle {
		return func(w http.ResponseWriter, req *http.Request, _ httprouter.Params) { h(w, req) }
	}
	r.GET("/ok", wrap(routes.OK))
	// httprouter registers each method on its own, so HEAD needs its own route.
	r.HEAD("/ok", wrap(routes.OK))
	r.POST("/orders/:id", wrap(routes.Order))
	r.GET("/panic", wrap(routes.Panic))
	r.GET("/status/:code", wrap(routes.Status))
	r.GET("/stream", wrap(routes.Stream))
	r.GET("/fail", wrap(routes.Fail))
	r.GET("/skip", wrap(routes.OK))
	return r
}

// TestHTTPRouter_C1_RouteTemplate proves that the route is the registered path.
func TestHTTPRouter_C1_RouteTemplate(t *testing.T) {
	log, rec := wlogtest.New(t)
	inner := httprouter.New()
	r := wloghttprouter.New(log, inner)
	r.GET("/users/:id", func(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
		w.WriteHeader(http.StatusOK)
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users/42", nil))

	httpFields, _ := rec.Last()["http"].(map[string]any)
	if httpFields["route"] != "/users/:id" {
		t.Errorf("http.route = %v, want /users/:id", httpFields["route"])
	}
	if rec.Last()["operation"] != "GET /users/:id" {
		t.Errorf("operation = %v, want GET /users/:id", rec.Last()["operation"])
	}
}

// TestHTTPRouter_C1_RedirectKeepsNoRoute proves that a trailing-slash redirect gives one
// event with status 301 and an empty route, because no handler ran.
func TestHTTPRouter_C1_RedirectKeepsNoRoute(t *testing.T) {
	log, rec := wlogtest.New(t)
	inner := httprouter.New()
	r := wloghttprouter.New(log, inner)
	r.GET("/ok", func(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
		w.WriteHeader(http.StatusOK)
	})

	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ok/", nil))

	if recorder.Code != http.StatusMovedPermanently {
		t.Errorf("status = %d, want 301", recorder.Code)
	}
	event := rec.Last()
	httpFields, _ := event["http"].(map[string]any)
	if !conformance.Equal(httpFields["status"], http.StatusMovedPermanently) {
		t.Errorf("http.status = %v, want 301", httpFields["status"])
	}
	if httpFields["route"] != "" {
		t.Errorf("http.route = %v, want an empty route", httpFields["route"])
	}
}

// TestHTTPRouter_C1_MethodNotAllowedBlank proves that a method no route allows gives
// status 405 with an empty route.
func TestHTTPRouter_C1_MethodNotAllowedBlank(t *testing.T) {
	log, rec := wlogtest.New(t)
	inner := httprouter.New()
	r := wloghttprouter.New(log, inner)
	r.GET("/ok", func(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
		w.WriteHeader(http.StatusOK)
	})

	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/ok", nil))

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", recorder.Code)
	}
	httpFields, _ := rec.Last()["http"].(map[string]any)
	if httpFields["route"] != "" {
		t.Errorf("http.route = %v, want an empty route", httpFields["route"])
	}
	if rec.Last()["level"] != "warn" {
		t.Errorf("level = %v, want warn", rec.Last()["level"])
	}
}
