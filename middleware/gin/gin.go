// Package wloggin adapts http-std's middleware to Gin, so a Gin app gets the same wide
// event as a plain net/http app, in one call.
package wloggin

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/jeremygprawira/wlog"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
)

// Option is exactly http-std's Option, re-exported so a caller only needs this package.
type Option = wlogstd.Option

// Re-exported http-std options. WithRouteFunc is not included: this package already sets
// it to Gin's own c.FullPath(), so overriding it here would only be useful for a route
// naming scheme other than Gin's own, which a caller can still get to via the
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

// captureWriter is Gin's ResponseWriter pointed at http-std's capturing writer. Gin
// writes through Write, WriteHeader and WriteString, so routing those three to out is
// enough to capture status, byte count and body. Every other method (Status, Size,
// Hijack, Flush, CloseNotify, Pusher) is inherited from Gin's own writer, which http-std
// forwards to underneath, so those stay correct.
type captureWriter struct {
	gin.ResponseWriter
	out http.ResponseWriter
}

func (w *captureWriter) WriteHeader(code int) { w.out.WriteHeader(code) }

func (w *captureWriter) Write(b []byte) (int, error) { return w.out.Write(b) }

func (w *captureWriter) WriteString(s string) (int, error) { return w.out.Write([]byte(s)) }

// Middleware wraps http-std's Middleware around Gin's request handling. It runs http-std's
// whole pipeline (capture, panic recovery, plugin hooks) around the Gin handler chain, and
// translates only what differs: the route name (Gin's c.FullPath(), not r.Pattern), and
// the response writer (Gin's c.Writer pointed at http-std's capturing writer).
func Middleware(log *wlog.Logger, opts ...Option) gin.HandlerFunc {
	return func(c *gin.Context) {
		// route reads c.FullPath() directly rather than anything on the *http.Request
		// http-std hands it, since a request mutation inside the wrapped handler
		// (see h below) would not be visible to http-std's own copy of the request:
		// net/http's WithContext returns a copy, so a change inside next never reaches
		// the caller's variable.
		routeFunc := func(*http.Request) string { return c.FullPath() }
		allOpts := append([]wlogstd.Option{wlogstd.WithRouteFunc(routeFunc)}, opts...)
		mw := wlogstd.Middleware(log, allOpts...)

		h := func(w http.ResponseWriter, r *http.Request) {
			original := c.Writer
			c.Request = r
			c.Writer = &captureWriter{ResponseWriter: original, out: w}
			c.Next()
			c.Writer = original
			// Reported here, before http-std's own deferred stages run, so the event
			// closes with every error Gin recorded, in the order it recorded them.
			for _, e := range c.Errors {
				wlog.Error(r.Context(), e.Err)
			}
		}
		mw(http.HandlerFunc(h)).ServeHTTP(c.Writer, c.Request)
	}
}
