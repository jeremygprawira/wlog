// Package wloggozero adapts http-core to go-zero's rest server, so a go-zero app gets the
// same wide event as a plain net/http app, in one call.
//
// Read top to bottom: RouterOption wraps one httpx.Router. The wrapper's Handle records
// the route template of each handler, because go-zero never reports the path it matched.
// ServeHTTP carries one holder per request, so the registration wrapper and the route
// reader share a template with no map and no lock. Pass the option to rest.WithRouter
// before rest.WithNotFoundHandler, so go-zero's own handlers land on this router.
//
// A route that times out still gives a correct event. go-zero's timeout middleware sits
// under this router, so it writes the 503 into the writer this pipeline reads, and the
// event emits with the 503 before the handler returns.
package wloggozero

import (
	"context"
	"net/http"

	"github.com/zeromicro/go-zero/rest/httpx"

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

// RouterOption returns the go-zero router that gives every request one wlog event. It
// wraps inner, so go-zero keeps its own routing. Pass the result to rest.WithRouter
// before any other option that touches the router.
//
//	server := rest.MustNewServer(rest.RestConf{...},
//		rest.WithRouter(wloggozero.RouterOption(log, router.NewRouter())),
//		rest.WithNotFoundHandler(http.NotFoundHandler()),
//	)
func RouterOption(log *wlog.Logger, inner httpx.Router, opts ...Option) httpx.Router {
	all := append(append([]Option{}, opts...), httpcore.RouteFunc(routeOf))
	return &router{
		inner:    inner,
		pipeline: httpcore.NetHTTP(log, all...)(inner),
	}
}

// router is the wlog wrapper around one go-zero router.
type router struct {
	inner    httpx.Router
	pipeline http.Handler
}

// ServeHTTP runs one request through the pipeline, and carries the holder that records the
// route template of this request.
func (r *router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	req = req.WithContext(context.WithValue(req.Context(), routeKey{}, &routeHolder{}))
	r.pipeline.ServeHTTP(w, req)
}

// Handle registers one route, and records its template for every request it serves. A
// route that times out keeps the template, because the timeout runs inside the handler.
func (r *router) Handle(method, path string, handler http.Handler) error {
	return r.inner.Handle(method, path, record(path, handler))
}

// SetNotFoundHandler replaces the not-found handler of the inner router.
func (r *router) SetNotFoundHandler(handler http.Handler) {
	r.inner.SetNotFoundHandler(handler)
}

// SetNotAllowedHandler replaces the method-not-allowed handler of the inner router.
func (r *router) SetNotAllowedHandler(handler http.Handler) {
	r.inner.SetNotAllowedHandler(handler)
}

// routeKey is the request context key that carries the route holder of one request.
type routeKey struct{}

// routeHolder carries the route template of one request from the registration wrapper to
// the route reader.
type routeHolder struct {
	pattern string
}

// record returns handler with the route template recorded on the holder of the request.
func record(pattern string, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if holder, ok := req.Context().Value(routeKey{}).(*routeHolder); ok {
			holder.pattern = pattern
		}
		handler.ServeHTTP(w, req)
	})
}

// routeOf returns the route template of one finished request, and an empty string for a
// 404, a 405, and every other answer that runs no registered handler.
func routeOf(req *http.Request) string {
	holder, ok := req.Context().Value(routeKey{}).(*routeHolder)
	if !ok {
		return ""
	}
	return holder.pattern
}
