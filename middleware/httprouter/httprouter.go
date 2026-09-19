// Package wloghttprouter adapts http-core to julienschmidt/httprouter, so a httprouter app
// gets the same wide event as a plain net/http app, in one call.
//
// Read top to bottom: New wraps one *httprouter.Router, and the wrapper's registration
// methods record the route template of each handler, because httprouter never reports the
// pattern it matched. ServeHTTP carries one holder per request, so the registration
// wrapper and the route reader share a template with no map and no lock.
package wloghttprouter

import (
	"context"
	"net/http"

	"github.com/julienschmidt/httprouter"

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

// Router is the wlog wrapper around one httprouter.Router. Register every route on the
// wrapper, and serve the wrapper as the app's handler.
type Router struct {
	inner    *httprouter.Router
	pipeline http.Handler
}

// New returns the wrapper around router. Every request becomes one wlog event, and the
// route template comes from the registration call that served it.
func New(log *wlog.Logger, router *httprouter.Router, opts ...Option) *Router {
	all := append(append([]Option{}, opts...), httpcore.RouteFunc(routeOf))
	return &Router{
		inner:    router,
		pipeline: httpcore.NetHTTP(log, all...)(router),
	}
}

// ServeHTTP runs one request through the pipeline, and carries the holder that records the
// route template of this request.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	req = req.WithContext(context.WithValue(req.Context(), routeKey{}, &routeHolder{}))
	r.pipeline.ServeHTTP(w, req)
}

// Handle registers one route, and records its template for every request it serves.
func (r *Router) Handle(method, path string, handle httprouter.Handle) {
	r.inner.Handle(method, path, record(path, handle))
}

// Handler registers one route with an http.Handler.
func (r *Router) Handler(method, path string, handler http.Handler) {
	r.inner.Handler(method, path, recordHTTP(path, handler))
}

// HandlerFunc registers one route with an http.HandlerFunc.
func (r *Router) HandlerFunc(method, path string, handler http.HandlerFunc) {
	r.Handler(method, path, handler)
}

// GET registers one GET route.
func (r *Router) GET(path string, handle httprouter.Handle) {
	r.Handle(http.MethodGet, path, handle)
}

// POST registers one POST route.
func (r *Router) POST(path string, handle httprouter.Handle) {
	r.Handle(http.MethodPost, path, handle)
}

// PUT registers one PUT route.
func (r *Router) PUT(path string, handle httprouter.Handle) {
	r.Handle(http.MethodPut, path, handle)
}

// PATCH registers one PATCH route.
func (r *Router) PATCH(path string, handle httprouter.Handle) {
	r.Handle(http.MethodPatch, path, handle)
}

// DELETE registers one DELETE route.
func (r *Router) DELETE(path string, handle httprouter.Handle) {
	r.Handle(http.MethodDelete, path, handle)
}

// HEAD registers one HEAD route.
func (r *Router) HEAD(path string, handle httprouter.Handle) {
	r.Handle(http.MethodHead, path, handle)
}

// OPTIONS registers one OPTIONS route.
func (r *Router) OPTIONS(path string, handle httprouter.Handle) {
	r.Handle(http.MethodOptions, path, handle)
}

// routeKey is the request context key that carries the route holder of one request.
type routeKey struct{}

// routeHolder carries the route template of one request from the registration wrapper to
// the route reader.
type routeHolder struct {
	pattern string
}

// record returns handle with the route template recorded on the holder of the request.
func record(pattern string, handle httprouter.Handle) httprouter.Handle {
	return func(w http.ResponseWriter, req *http.Request, params httprouter.Params) {
		setPattern(req, pattern)
		handle(w, req, params)
	}
}

// recordHTTP returns handler with the route template recorded on the holder of the
// request.
func recordHTTP(pattern string, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		setPattern(req, pattern)
		handler.ServeHTTP(w, req)
	})
}

// setPattern writes one template on the holder of the request.
func setPattern(req *http.Request, pattern string) {
	if holder, ok := req.Context().Value(routeKey{}).(*routeHolder); ok {
		holder.pattern = pattern
	}
}

// routeOf returns the route template of one finished request. A redirect, a 404, a 405,
// and an OPTIONS answer run no registered handler, so they carry no route.
func routeOf(req *http.Request) string {
	holder, ok := req.Context().Value(routeKey{}).(*routeHolder)
	if !ok {
		return ""
	}
	return holder.pattern
}
