// Package wlogecho adapts http-std's middleware to Echo v4, so an Echo app gets the same
// wide event as a plain net/http app, in one call.
package wlogecho

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/jeremygprawira/wlog"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
)

// Option is exactly http-std's Option, re-exported so a caller only needs this package.
type Option = wlogstd.Option

// Re-exported http-std options. WithRouteFunc is not included: this package already sets
// it to Echo's own c.Path(), so overriding it here would only be useful for a route
// naming scheme other than Echo's own, which a caller can still get to via the
// middleware/nethttp package directly.
var (
	WithUserFunc     = wlogstd.WithUserFunc
	SkipPaths        = wlogstd.SkipPaths
	CaptureHeaders   = wlogstd.CaptureHeaders
	CaptureQuery     = wlogstd.CaptureQuery
	CaptureCookies   = wlogstd.CaptureCookies
	CaptureBody      = wlogstd.CaptureBody
	MaxBodyCapture   = wlogstd.MaxBodyCapture
	BodyContentTypes = wlogstd.BodyContentTypes
)

// Middleware wraps http-std's Middleware for Echo. It runs http-std's whole pipeline
// (capture, panic recovery, plugin hooks) around the framework's own request handling,
// translating only what differs: the route name (Echo's c.Path(), not r.Pattern) and the
// response writer (Echo's own c.Response(), not a raw http.ResponseWriter).
func Middleware(log *wlog.Logger, opts ...Option) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// route reads c.Path() directly rather than anything on the *http.Request
			// http-std hands it, since a request mutation inside the wrapped handler
			// (see h below) would not be visible to http-std's own copy of the
			// request: net/http's WithContext returns a copy, so a change inside next
			// never reaches the caller's variable (the same reason H1 read the route
			// from the request net/http's own router actually dispatched to, not the
			// original).
			routeFunc := func(*http.Request) string { return c.Path() }
			allOpts := append([]wlogstd.Option{wlogstd.WithRouteFunc(routeFunc)}, opts...)
			mw := wlogstd.Middleware(log, allOpts...)

			h := func(w http.ResponseWriter, r *http.Request) {
				c.SetRequest(r)
				c.Response().Writer = w
				if err := next(c); err != nil {
					// Reported and handled here, before http-std's own deferred
					// stages run, so the event's recorded status matches whatever
					// Echo's error handler actually writes to the client.
					wlog.Error(r.Context(), err)
					c.Echo().HTTPErrorHandler(err, c)
				}
			}
			mw(http.HandlerFunc(h)).ServeHTTP(c.Response().Writer, c.Request())
			return nil
		}
	}
}
