// This file runs the HTTP conformance suite against the net/http adapter, and against a
// broken adapter that must fail it.
package httpconformance_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	httpconformance "github.com/jeremygprawira/wlog/internal/conformance/http"
	"github.com/jeremygprawira/wlog/middleware/httpcore"
)

// TestConformance_HTTP22_NetHTTPPasses proves that the net/http adapter passes every
// scenario of the suite.
func TestConformance_HTTP22_NetHTTPPasses(t *testing.T) {
	httpconformance.Run(tester{t}, netHTTPFactory{})
}

// TestConformance_HTTP22_BrokenAdapterFails proves that an adapter which serves every path
// with one handler fails the scenarios it breaks, with a readable report.
func TestConformance_HTTP22_BrokenAdapterFails(t *testing.T) {
	broken := &capture{}
	httpconformance.Run(broken, brokenFactory{})

	if len(broken.failures) == 0 {
		t.Fatal("the broken adapter passed the suite")
	}
	for _, name := range []string{"RouteTemplate", "UnmatchedRoute"} {
		if !broken.reports(name) {
			t.Errorf("the broken adapter did not fail %s:\n%s", name, strings.Join(broken.failures, "\n"))
		}
	}
}

// tester adapts *testing.T to the suite's TB.
type tester struct{ *testing.T }

// Run runs one scenario with an adapted tester.
func (t tester) Run(name string, fn func(httpconformance.TB)) bool {
	return t.T.Run(name, func(sub *testing.T) { fn(tester{sub}) })
}

// capture records the failures of one suite run, so a test can read them.
type capture struct {
	failures []string
}

// Helper does nothing, because a capture holds no test state.
func (c *capture) Helper() {}

// Errorf records one failure.
func (c *capture) Errorf(format string, args ...any) {
	c.failures = append(c.failures, fmt.Sprintf(format, args...))
}

// Fatalf records one failure. It does not stop the run, because the suite reports with
// Errorf and returns.
func (c *capture) Fatalf(format string, args ...any) {
	c.failures = append(c.failures, fmt.Sprintf(format, args...))
}

// Run runs one scenario against the same capture.
func (c *capture) Run(_ string, fn func(httpconformance.TB)) bool {
	fn(c)
	return true
}

// reports reports whether one scenario name appears in the recorded failures.
func (c *capture) reports(name string) bool {
	for _, failure := range c.failures {
		if strings.Contains(failure, name) {
			return true
		}
	}
	return false
}

// netHTTPFactory builds the net/http adapter around a mux of the route table.
type netHTTPFactory struct{}

// Build returns the middleware around the mux.
func (netHTTPFactory) Build(log *wlog.Logger, routes httpconformance.Routes) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", routes.OK)
	mux.HandleFunc("/orders/", routes.Order)
	mux.HandleFunc("/panic", routes.Panic)
	mux.HandleFunc("/status/", routes.Status)
	mux.HandleFunc("/stream", routes.Stream)
	mux.HandleFunc("/fail", routes.Fail)
	return httpcore.NetHTTP(log,
		httpcore.RouteFunc(template),
		httpcore.SkipPaths("/skip"),
	)(mux)
}

// template returns the route template a router would report, and an empty string for a
// path no route matches. The suite cannot use a real mux pattern, because the new ServeMux
// patterns need a Go 1.22 language version.
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

// brokenFactory builds an adapter that never reports a route, which breaks every scenario
// that names one.
type brokenFactory struct{}

// Build returns one handler for every path.
func (brokenFactory) Build(log *wlog.Logger, _ httpconformance.Routes) http.Handler {
	return httpcore.NetHTTP(log)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}
