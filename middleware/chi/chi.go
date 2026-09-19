// Package wlogchi adapts http-core to go-chi/chi, so a chi app gets the same wide event as
// a plain net/http app, in one call.
//
// Read top to bottom: Middleware drives one event per request through http-core, and reads
// the route from chi's RouteContext after the chain returns, because chi fills the pattern
// while it routes. Setup installs Middleware with the default Logger on a chi router.
package wlogchi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/middleware/httpcore"
)

// Option is an http-core option, re-exported so a caller needs one import.
type Option = httpcore.Option

// The http-core options this adapter passes through.
var (
	SkipPaths        = httpcore.SkipPaths
	CaptureAll       = httpcore.CaptureAll
	CaptureHeaders   = httpcore.CaptureHeaders
	CaptureBody      = httpcore.CaptureBody
	MaxBodyCapture   = httpcore.MaxBody
	BodyContentTypes = httpcore.BodyTypes
	WithUserFunc     = httpcore.User
)

// Middleware returns chi middleware that emits one wide event per request through log. It
// runs the http-core pipeline around the chain, and reads only what chi owns: the route
// pattern that chi matched.
func Middleware(log *wlog.Logger, opts ...Option) func(http.Handler) http.Handler {
	all := append(append([]Option{}, opts...), httpcore.RouteFunc(route))
	return httpcore.NetHTTP(log, all...)
}

// Setup installs Middleware with the default Logger on a chi router. Call it before any
// route, so the middleware wraps the whole chain.
func Setup(r chi.Router) {
	r.Use(Middleware(wlog.Default()))
}

// route returns the route pattern chi matched for one finished request. It answers an
// empty string when chi holds no route context, which happens outside a chi router.
//
// A wildcard pattern stays as chi reports it. http-core blanks a /* pattern on a 404 and
// on a 405, so an unmatched path never names a route.
func route(r *http.Request) string {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil {
		return ""
	}
	return rctx.RoutePattern()
}
