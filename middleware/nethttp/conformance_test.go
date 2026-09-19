// This file runs the shared http conformance suite against the net/http adapter, so the
// adapter and every adapter built on it prove one event shape.
package wlogstd_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	httpconformance "github.com/jeremygprawira/wlog/internal/conformance/http"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
)

// TestStd_Conformance proves that the net/http adapter passes every scenario of the http
// suite.
func TestStd_Conformance(t *testing.T) {
	httpconformance.Run(conformance.Tester{T: t}, stdFactory{})
}

// stdFactory builds the adapter around a mux of the suite's route table.
type stdFactory struct{}

// Build returns the middleware around the mux.
func (stdFactory) Build(log *wlog.Logger, routes httpconformance.Routes, settings httpconformance.Settings) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", routes.OK)
	mux.HandleFunc("/orders/", routes.Order)
	mux.HandleFunc("/panic", routes.Panic)
	mux.HandleFunc("/status/", routes.Status)
	mux.HandleFunc("/stream", routes.Stream)
	mux.HandleFunc("/fail", routes.Fail)
	mux.HandleFunc("/skip", routes.OK)

	opts := []wlogstd.Option{wlogstd.WithRouteFunc(template), wlogstd.SkipPaths("/skip")}
	if settings.CaptureAll {
		opts = append(opts, wlogstd.CaptureAll())
	}
	if settings.MaxBody > 0 {
		opts = append(opts, wlogstd.MaxBody(settings.MaxBody))
	}
	return wlogstd.Middleware(log, opts...)(mux)
}

// template returns the route template a router would report, and an empty string for a
// path no route matches. The suite cannot use a real mux pattern, because the new
// ServeMux patterns need a Go 1.22 language version.
func template(r *http.Request) string {
	switch {
	case r.URL.Path == "/ok", r.URL.Path == "/panic", r.URL.Path == "/stream", r.URL.Path == "/fail":
		return r.URL.Path
	case strings.HasPrefix(r.URL.Path, "/orders/"):
		return "/orders/{id}"
	case strings.HasPrefix(r.URL.Path, "/status/"):
		return "/status/{code}"
	default:
		return ""
	}
}
