// Package wloggin adapts http-core to Gin, so a Gin app gets the same wide event as a
// plain net/http app, in one call.
//
// Read top to bottom: Middleware drives one event per request and reads the status, the
// size, and the route from Gin's own writer; the two view types translate a Gin request
// and response into what http-core reads; Handler and WriteProblem cover the redirect and
// the problem response.
package wloggin

import (
	"io"
	"net/http"
	"runtime/debug"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/middleware/httpcore"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
	"github.com/jeremygprawira/wlog/propagate"
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
	WithRouteFunc    = httpcore.RouteFunc
)

// Middleware wraps the Gin chain so every request becomes one wlog request event. It runs
// the http-core pipeline around the chain, and reads only what Gin owns: the route from
// c.FullPath(), and the status and the size from c.Writer after the chain returns.
func Middleware(log *wlog.Logger, opts ...Option) gin.HandlerFunc {
	core := httpcore.New(log, opts...)
	return func(c *gin.Context) {
		view := ginRequest{r: c.Request}
		if core.Skip(view) {
			c.Next()
			return
		}
		ctx, x := core.Start(c.Request.Context(), view)
		c.Request = c.Request.WithContext(ctx)
		view.r = c.Request
		if trace, ok := propagate.FromContext(ctx); ok && trace.RequestID != "" {
			c.Writer.Header().Set("X-Request-ID", trace.RequestID)
		}

		if core.CapturesBody(view) {
			body, rest, truncated := core.ReadBody(c.Request.Body)
			if rest != nil {
				c.Request.Body = io.NopCloser(rest)
			}
			x.RequestBody(body, truncated, false)
			defer core.ReturnBody(body)
		}

		x.Route(c.FullPath(), c.FullPath() != "")
		func() {
			defer func() {
				// Every error Gin recorded reaches the event first, so a later panic
				// leaves them in errors[] and not in the single error field.
				for _, e := range c.Errors {
					wlog.Error(ctx, e.Err)
				}
				// A panic stops the chain and becomes a 500, which is what a Gin app
				// does with its own recovery middleware.
				if r := recover(); r != nil {
					x.Panic(r, debug.Stack())
					c.AbortWithStatus(http.StatusInternalServerError)
				}
			}()
			c.Next()
		}()

		// Commit the status Gin set but never wrote, so the size Gin counts is 0 and the
		// wire status matches the event.
		c.Writer.WriteHeaderNow()
		x.End(ginResponse{c: c}, nil)
	}
}

// Handler returns engine as an http.Handler with the default Logger around it, so a
// redirect Gin answers before its own chain still becomes one event.
func Handler(engine *gin.Engine) http.Handler {
	return wlogstd.Middleware(nil)(engine)
}

// WriteProblem writes err as one problem response, so a Gin app answers a fault with the
// same body every other adapter writes.
func WriteProblem(c *gin.Context, err error) {
	httpcore.WriteProblem(c.Writer, c.Request, err)
}

// ginRequest is the http-core view of one Gin request. Every method copies the value it
// returns, because Gin reuses the request after the handler.
type ginRequest struct {
	r *http.Request
}

// Method returns the request method.
func (v ginRequest) Method() string { return v.r.Method }

// Path returns the raw request path.
func (v ginRequest) Path() string { return v.r.URL.Path }

// Proto returns the protocol, such as HTTP/1.1.
func (v ginRequest) Proto() string { return v.r.Proto }

// Header returns the first value of one header. The Host header is a field of its own in
// net/http, so this view answers it from there.
func (v ginRequest) Header(name string) string {
	if strings.EqualFold(name, "Host") {
		return v.r.Host
	}
	return v.r.Header.Get(name)
}

// Scheme returns https for a TLS connection, and http otherwise.
func (v ginRequest) Scheme() string {
	if v.r.TLS != nil {
		return "https"
	}
	return "http"
}

// EachHeader visits every request header value.
func (v ginRequest) EachHeader(fn func(name, value string)) {
	for name, values := range v.r.Header {
		for _, value := range values {
			fn(name, value)
		}
	}
}

// EachQuery visits every query value.
func (v ginRequest) EachQuery(fn func(key, value string)) {
	for key, values := range v.r.URL.Query() {
		for _, value := range values {
			fn(key, value)
		}
	}
}

// EachCookie visits every request cookie.
func (v ginRequest) EachCookie(fn func(name, value string)) {
	for _, cookie := range v.r.Cookies() {
		fn(cookie.Name, cookie.Value)
	}
}

// RemoteAddr returns the connection address, as ip:port.
func (v ginRequest) RemoteAddr() string { return v.r.RemoteAddr }

// ContentLength returns the declared body size, or -1 when it is unknown.
func (v ginRequest) ContentLength() int64 { return v.r.ContentLength }

// ginResponse is the http-core view of one Gin response, read after the chain returns.
type ginResponse struct {
	c *gin.Context
}

// Status returns the status Gin wrote, or 200 when it wrote none.
func (v ginResponse) Status() int { return v.c.Writer.Status() }

// BytesWritten returns the number of body bytes Gin wrote, and -1 when Gin counted none
// and wrote no header. Gin reports -1 until a write, so a written header without a body
// counts as zero.
func (v ginResponse) BytesWritten() int64 {
	if size := v.c.Writer.Size(); size >= 0 {
		return int64(size)
	}
	if v.c.Writer.Written() {
		return 0
	}
	return -1
}

// EachHeader visits every response header value.
func (v ginResponse) EachHeader(fn func(name, value string)) {
	for name, values := range v.c.Writer.Header() {
		for _, value := range values {
			fn(name, value)
		}
	}
}
