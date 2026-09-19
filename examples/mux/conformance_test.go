// This file runs the shared http conformance suite against gorilla/mux, proving that
// http-std is framework-agnostic: only the route function changes.
package main

import (
	"net/http"
	"testing"

	"github.com/gorilla/mux"
	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	httpconformance "github.com/jeremygprawira/wlog/internal/conformance/http"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
)

// muxFactory builds the middleware around a gorilla/mux router of the suite's route
// table.
type muxFactory struct{}

// Build returns the middleware around the router.
func (muxFactory) Build(log *wlog.Logger, routes httpconformance.Routes, settings httpconformance.Settings) http.Handler {
	router := mux.NewRouter()
	router.HandleFunc("/ok", routes.OK)
	router.HandleFunc("/orders/{id}", routes.Order)
	router.HandleFunc("/panic", routes.Panic)
	router.HandleFunc("/status/{code}", routes.Status)
	router.HandleFunc("/stream", routes.Stream)
	router.HandleFunc("/fail", routes.Fail)
	router.HandleFunc("/skip", routes.OK)

	opts := []wlogstd.Option{wlogstd.WithRouteFunc(muxTemplate(router)), wlogstd.SkipPaths("/skip")}
	if settings.CaptureAll {
		opts = append(opts, wlogstd.CaptureAll())
	}
	if settings.MaxBody > 0 {
		opts = append(opts, wlogstd.MaxBody(settings.MaxBody))
	}
	return wlogstd.Middleware(log, opts...)(router)
}

// muxTemplate returns the route template the router matches for one request, and an empty
// string for a path no route matches. The middleware wraps the router, so the request it
// reads carries no route of its own: the router matches the request a second time here.
func muxTemplate(router *mux.Router) func(*http.Request) string {
	return func(r *http.Request) string {
		match := &mux.RouteMatch{}
		if router.Match(r, match) && match.Route != nil {
			if template, err := match.Route.GetPathTemplate(); err == nil {
				return template
			}
		}
		return ""
	}
}

// TestConformance runs the shared http suite against the mux example.
func TestConformance(t *testing.T) {
	httpconformance.Run(conformance.Tester{T: t}, muxFactory{})
}
