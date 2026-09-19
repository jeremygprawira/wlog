// Package wlogstd is wlog's net/http middleware, and the adapter that every other
// net/http-based adapter builds on. One request becomes one wlog request event, with the
// method, the route, the status, the bytes, and the duration captured by default.
//
// The pipeline lives in http-core, so this package only names it: Middleware is
// httpcore.NetHTTP, and Setup wires it around a ServeMux.
package wlogstd

import (
	"net/http"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/middleware/httpcore"
)

// Middleware wraps next so every request becomes one wlog request event. Build the
// returned middleware once, at startup.
func Middleware(log *wlog.Logger, opts ...Option) func(http.Handler) http.Handler {
	all := append([]Option{httpcore.RouteFunc(route)}, opts...)
	return httpcore.NetHTTP(log, all...)
}

// Setup wraps mux with the middleware of the default Logger, so an app with no options
// gets one event per request.
func Setup(mux *http.ServeMux) http.Handler {
	return Middleware(wlog.Default())(mux)
}
