// Package wloghertz adapts http-core to hertz, so a hertz app gets the same wide event as
// a plain net/http app, in one call.
//
// Read top to bottom: Tracer opens one event per request in Start, and reads the route and
// the status in Finish. Middleware recovers a panic and records it on the open event.
// Setup installs both with the default Logger. The two view types translate one
// *app.RequestContext into what http-core reads.
//
// A hertz server runs its tracers in the protocol server, so Setup appends the tracer
// before Spin. This is the whole setup:
//
//	func main() {
//		h := server.New()
//		wloghertz.Setup(h)
//		h.GET("/orders/:id", handler)
//		h.Spin()
//	}
package wloghertz

import (
	"bytes"
	"context"
	"net/http"
	"runtime/debug"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/tracer"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/middleware/httpcore"
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
)

// exchangeKey is the RequestContext user key that carries the open Exchange of one
// request. The value starts with a NUL byte, so an app key never meets it.
const exchangeKey = "\x00wlog.hertz.exchange"

// Tracer returns the hertz tracer that emits one wide event per request through log. Pass
// it to server.WithTracer, or call Setup for the default Logger.
func Tracer(log *wlog.Logger, opts ...Option) tracer.Tracer {
	return &hertzTracer{core: httpcore.New(log, opts...)}
}

// Setup installs Tracer and Middleware with the default Logger on a hertz server. Call it
// after server.New and before Spin, so it sees every request.
func Setup(h *server.Hertz) {
	h.GetTracer().Append(Tracer(wlog.Default()))
	h.Use(Middleware())
}

// Middleware returns a hertz handler that recovers a panic, records it on the open event,
// and answers 500. A request that wlog skipped stays silent. Register it with Setup.
func Middleware() app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		defer func() {
			value := recover()
			if value == nil {
				return
			}
			if x := exchangeOf(ctx); x != nil {
				x.Panic(value, debug.Stack())
			}
			if code := ctx.Response.StatusCode(); (code == 0 || code == http.StatusOK) &&
				!ctx.Response.IsBodyStream() && len(ctx.Response.Body()) == 0 {
				ctx.SetStatusCode(http.StatusInternalServerError)
			}
		}()
		ctx.Next(c)
	}
}

// hertzTracer drives one event per request through http-core.
type hertzTracer struct {
	core *httpcore.Core
}

// Start opens the event of one request, copies the fields the policy allows, and stores
// the exchange for Middleware and Finish. A skipped request starts nothing.
func (t *hertzTracer) Start(ctx context.Context, c *app.RequestContext) context.Context {
	view := RequestView{Ctx: c}
	if t.core.Skip(view) {
		return ctx
	}
	eventCtx, x := t.core.Start(ctx, view)
	c.Set(exchangeKey, x)
	if t.core.EchoesRequestID() {
		if trace, ok := propagate.FromContext(eventCtx); ok && trace.RequestID != "" {
			c.Response.Header.Set("X-Request-ID", trace.RequestID)
		}
	}
	if t.core.CapturesBody(view) {
		body, _, truncated := t.core.ReadBody(bytes.NewReader(c.Request.Body()))
		x.RequestBody(body, truncated, false)
		t.core.ReturnBody(body)
	}
	return eventCtx
}

// Finish reads the route and the status of the finished request, records the last error a
// handler set, and emits the event. The status is right here, because hertz wrote its
// default bodies before this call.
func (t *hertzTracer) Finish(_ context.Context, c *app.RequestContext) {
	x := exchangeOf(c)
	if x == nil {
		return
	}
	template := c.FullPath()
	x.Route(template, template != "")
	x.End(ResponseView{Ctx: c}, lastError(c))
}

// exchangeOf returns the open Exchange of one request, or nil when wlog skipped it.
func exchangeOf(c *app.RequestContext) *httpcore.Exchange {
	if value, ok := c.Get(exchangeKey); ok {
		if x, ok := value.(*httpcore.Exchange); ok {
			return x
		}
	}
	return nil
}

// lastError returns the last error a hertz handler set with Error or AbortWithError.
func lastError(c *app.RequestContext) error {
	if n := len(c.Errors); n > 0 {
		return c.Errors[n-1].Err
	}
	return nil
}
