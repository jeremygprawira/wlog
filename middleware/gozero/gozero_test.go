// This file runs the http conformance suite against the go-zero router option, and checks
// the two go-zero paths that write outside the handler: the timeout answer and the plain
// 404 body.
package wloggozero_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zeromicro/go-zero/rest/handler"
	"github.com/zeromicro/go-zero/rest/router"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	httpconformance "github.com/jeremygprawira/wlog/internal/conformance/http"
	wloggozero "github.com/jeremygprawira/wlog/middleware/gozero"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestGoZero_Conformance proves that the go-zero router option passes every scenario of
// the http suite.
func TestGoZero_Conformance(t *testing.T) {
	httpconformance.Run(conformance.Tester{T: t}, gozeroFactory{})
}

// gozeroFactory builds the router option around a go-zero router of the suite's route
// table.
type gozeroFactory struct{}

// Build returns the wrapper with the middleware.
func (gozeroFactory) Build(log *wlog.Logger, routes httpconformance.Routes, settings httpconformance.Settings) http.Handler {
	opts := []wloggozero.Option{wloggozero.SkipPaths("/skip")}
	if settings.CaptureAll {
		opts = append(opts, wloggozero.CaptureAll())
	}
	if settings.MaxBody > 0 {
		opts = append(opts, wloggozero.MaxBodyCapture(settings.MaxBody))
	}
	r := wloggozero.RouterOption(log, router.NewRouter(), opts...)
	// The suite's golden carries the plain net/http 404 body, so the factory gives the
	// router the same body.
	r.SetNotFoundHandler(http.HandlerFunc(http.NotFound))

	register := func(method, path string, h http.HandlerFunc) {
		if err := r.Handle(method, path, h); err != nil {
			panic(err)
		}
	}
	register(http.MethodGet, "/ok", routes.OK)
	// go-zero loads each method into its own tree, so HEAD needs its own route.
	register(http.MethodHead, "/ok", routes.OK)
	register(http.MethodPost, "/orders/:id", routes.Order)
	register(http.MethodGet, "/panic", routes.Panic)
	register(http.MethodGet, "/status/:code", routes.Status)
	register(http.MethodGet, "/stream", routes.Stream)
	register(http.MethodGet, "/fail", routes.Fail)
	register(http.MethodGet, "/skip", routes.OK)
	return r
}

// TestGoZero_C8_TimeoutAnswers503 proves that a route which times out logs the 503 the
// timeout middleware wrote, and that the event emits before the handler returns.
func TestGoZero_C8_TimeoutAnswers503(t *testing.T) {
	log, rec := wlogtest.New(t)
	r := wloggozero.RouterOption(log, router.NewRouter())
	slow := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})
	if err := r.Handle(http.MethodGet, "/slow", handler.TimeoutHandler(50*time.Millisecond)(slow)); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	start := time.Now()
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/slow", nil))
	elapsed := time.Since(start)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", recorder.Code)
	}
	if elapsed > 150*time.Millisecond {
		t.Errorf("the answer took %v, want the timeout at 50ms", elapsed)
	}
	if rec.Count() != 1 {
		t.Fatalf("events = %d, want 1", rec.Count())
	}
	httpFields, _ := rec.Last()["http"].(map[string]any)
	if !conformance.Equal(httpFields["status"], http.StatusServiceUnavailable) {
		t.Errorf("http.status = %v, want 503", httpFields["status"])
	}
	if rec.Last()["level"] != "error" {
		t.Errorf("level = %v, want error", rec.Last()["level"])
	}
}

// TestGoZero_C1_NotFoundBlank proves that an unmatched path logs status 404 with an empty
// route, as the shared table asks.
func TestGoZero_C1_NotFoundBlank(t *testing.T) {
	log, rec := wlogtest.New(t)
	r := wloggozero.RouterOption(log, router.NewRouter())
	if err := r.Handle(http.MethodGet, "/ok", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})); err != nil {
		t.Fatalf("Handle: %v", err)
	}

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
