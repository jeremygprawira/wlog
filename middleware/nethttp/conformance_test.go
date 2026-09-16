// The adapter registers plain paths, because the conformance suite tests the
// middleware and not the router. A pattern carries no meaning here, and plain
// paths behave the same on every toolchain.
package wlogstd_test

import (
	"net/http"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
)

// wlogstdAdapter implements conformance.Adapter for the net/http middleware.
type wlogstdAdapter struct{}

// Build wires the routes of the suite into a ServeMux.
func (wlogstdAdapter) Build(log *wlog.Logger, routes conformance.Routes) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", routes.OK)
	mux.HandleFunc("/panic", routes.Panic)
	mux.HandleFunc("/echo", routes.Echo)
	mux.HandleFunc("/skip", routes.OK)
	return wlogstd.Middleware(log, wlogstd.SkipPaths("/skip"))(mux)
}

// TestConformance runs the shared adapter suite against the middleware.
func TestConformance(t *testing.T) {
	conformance.Run(t, wlogstdAdapter{})
}
