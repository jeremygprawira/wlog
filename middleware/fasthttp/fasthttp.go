// Package wlogfasthttp adapts http-core to fasthttp, so a fasthttp app gets the same wide
// event as a plain net/http app, in one call.
//
// Read top to bottom: Middleware starts one event per request, copies the fields the
// policy allows through the views, and emits before it returns, because fasthttp reuses
// its buffers after that. Context returns the open event, so a handler adds its own
// fields. RequestView and ResponseView translate one *fasthttp.RequestCtx.
package wlogfasthttp

import (
	"bytes"
	"context"
	"net/http"
	"runtime/debug"

	"github.com/valyala/fasthttp"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/middleware/httpcore"
	"github.com/jeremygprawira/wlog/propagate"
)

// Option configures the middleware.
type Option func(*options)

// options holds the resolved settings: what http-core receives, the route function only
// fasthttp has, and the panic policy the adapter applies itself.
type options struct {
	core   []httpcore.Option
	route  func(*fasthttp.RequestCtx) string
	policy httpcore.Policy
}

// Core passes http-core options through, so a caller sets capture, skip, and proxy rules
// with the constructors every other adapter uses.
func Core(opts ...httpcore.Option) Option {
	return func(o *options) { o.core = append(o.core, opts...) }
}

// RouteFunc sets how the middleware reads the route template of one finished request. With
// no function, the route stays empty.
func RouteFunc(fn func(*fasthttp.RequestCtx) string) Option {
	return func(o *options) { o.route = fn }
}

// PanicPolicy sets what the middleware does with a panic in the handler chain. The default
// records the panic, writes 500, and emits.
func PanicPolicy(policy httpcore.Policy) Option {
	return func(o *options) { o.policy = policy }
}

// eventKey is the user value key that carries the event context of one request. A caller
// cannot build it, so it never meets a key of the app.
type eventKey struct{}

// Context returns the open event of one request, so a handler adds fields with wlog.Set
// and wlog.Error. It answers a background context when the request holds no event, which
// happens on a skipped path.
func Context(ctx *fasthttp.RequestCtx) context.Context {
	if value := ctx.UserValue(eventKey{}); value != nil {
		if event, ok := value.(context.Context); ok {
			return event
		}
	}
	return context.Background()
}

// Middleware returns fasthttp middleware that emits one wide event per request through log.
// It keeps every value it reads as a copy, so a reused fasthttp buffer never changes an
// event that already left.
func Middleware(log *wlog.Logger, opts ...Option) func(fasthttp.RequestHandler) fasthttp.RequestHandler {
	var cfg options
	for _, opt := range opts {
		opt(&cfg)
	}
	cfg.core = append(cfg.core, httpcore.PanicPolicy(cfg.policy))
	core := httpcore.New(log, cfg.core...)

	return func(next fasthttp.RequestHandler) fasthttp.RequestHandler {
		return func(ctx *fasthttp.RequestCtx) {
			view := RequestView{Ctx: ctx}
			if core.Skip(view) {
				next(ctx)
				return
			}

			eventCtx, x := core.Start(ctx, view)
			ctx.SetUserValue(eventKey{}, eventCtx)
			if trace, ok := propagate.FromContext(eventCtx); ok && trace.RequestID != "" {
				ctx.Response.Header.Set("X-Request-ID", trace.RequestID)
			}
			if core.CapturesBody(view) {
				body, _, truncated := core.ReadBody(bytes.NewReader(ctx.PostBody()))
				x.RequestBody(body, truncated, false)
				defer core.ReturnBody(body)
			}

			// The chain runs under the panic policy. A recorded panic stays on the
			// event, and the event still emits, because fasthttp reuses its buffers
			// after this function returns.
			again := func() (again any) {
				defer func() {
					value := recover()
					if value == nil {
						return
					}
					x.Panic(value, debug.Stack())
					if cfg.policy == httpcore.Repanic {
						again = value
						return
					}
					// fasthttp keeps no written flag, so a bare 200 and an
					// untouched response look alike here. A named 200 with a
					// body, and every other status, stay as the handler set
					// them.
					if ctx.Response.StatusCode() == http.StatusOK &&
						!ctx.Response.IsBodyStream() && len(ctx.Response.Body()) == 0 {
						ctx.SetStatusCode(http.StatusInternalServerError)
					}
				}()
				next(ctx)
				return nil
			}()

			if cfg.route != nil {
				template := cfg.route(ctx)
				x.Route(template, template != "")
			}
			x.End(ResponseView{Ctx: ctx}, nil)
			if again != nil {
				panic(again)
			}
		}
	}
}
