// Package wlogecho5 adapts http-std's middleware to Echo v5, so an Echo v5 app gets the
// same wide event as a plain net/http app, in one call.
package wlogecho5

import (
	"net/http"

	"github.com/labstack/echo/v5"

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
	CaptureAll       = wlogstd.CaptureAll
	CaptureHeaders   = wlogstd.CaptureHeaders
	CaptureBody      = wlogstd.CaptureBody
	MaxBodyCapture   = wlogstd.MaxBody
	BodyContentTypes = wlogstd.BodyTypes
)

// Middleware wraps http-std's Middleware for Echo v5. It runs http-std's whole pipeline
// (capture, panic recovery, plugin hooks) around the framework's own request handling,
// translating only what differs: the route name (Echo's c.Path(), not r.Pattern) and the
// response writer (Echo's c.Response(), swapped with http-std's wrapper via SetResponse).
func Middleware(log *wlog.Logger, opts ...Option) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			// route reads c.Path() directly rather than anything on the *http.Request
			// http-std hands it, since a request mutation inside the wrapped handler
			// (see h below) would not be visible to http-std's own copy of the
			// request: net/http's WithContext returns a copy, so a change inside next
			// never reaches the caller's variable.
			routeFunc := func(*http.Request) string { return c.Path() }
			allOpts := append([]wlogstd.Option{wlogstd.WithRouteFunc(routeFunc)}, opts...)
			mw := wlogstd.Middleware(log, allOpts...)

			h := func(w http.ResponseWriter, r *http.Request) {
				c.SetRequest(r)
				tracked := &trackingWriter{ResponseWriter: w}
				c.SetResponse(tracked)
				if err := next(c); err != nil {
					// Reported and handled here, before http-core's own deferred
					// stages run, so the event's recorded status matches whatever
					// Echo's error handler actually writes to the client.
					wlog.Error(r.Context(), err)
					// A committed response keeps its body: a second write would append
					// to a body the client already received a length for.
					if !tracked.wrote {
						c.Echo().HTTPErrorHandler(c, err)
					}
				}
			}
			mw(http.HandlerFunc(h)).ServeHTTP(c.Response(), c.Request())
			return nil
		}
	}
}

// trackingWriter remembers whether the handler already committed a response, so a later
// error never appends a second body.
type trackingWriter struct {
	http.ResponseWriter
	wrote bool
}

// WriteHeader records the commit and forwards it.
func (w *trackingWriter) WriteHeader(code int) {
	w.wrote = true
	w.ResponseWriter.WriteHeader(code)
}

// Write records the commit and forwards the bytes.
func (w *trackingWriter) Write(b []byte) (int, error) {
	w.wrote = true
	return w.ResponseWriter.Write(b)
}

// Unwrap returns the writer underneath, so http.ResponseController reaches it.
func (w *trackingWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
