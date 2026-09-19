// Package wlogfiber adapts http-core to Fiber v2, so a Fiber app gets the same wide event
// as a plain net/http app, in one call.
//
// Read top to bottom: Middleware opens one event per request through the shared fasthttp
// views, carries it on the Fiber user context, and reads the route from the pointer Fiber
// matched. A handler error goes to the app's error handler before the event emits. Setup
// installs Middleware with the default Logger.
package wlogfiber

import (
	"bytes"
	"net/http"
	"runtime/debug"

	"github.com/gofiber/fiber/v2"

	"github.com/jeremygprawira/wlog"
	wlogfasthttp "github.com/jeremygprawira/wlog/middleware/fasthttp"
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

// Middleware returns Fiber middleware that emits one wide event per request through log.
// It reads only what Fiber owns: the route it matched, and the response it wrote.
//
// A handler reads the event context with c.UserContext, and a request that adds to an
// open event joins that event instead of starting one.
func Middleware(log *wlog.Logger, opts ...Option) fiber.Handler {
	core := httpcore.New(log, opts...)
	return func(c *fiber.Ctx) error {
		view := wlogfasthttp.RequestView{Ctx: c.Context()}
		if core.Skip(view) {
			return c.Next()
		}

		// Fiber fills the route while the chain runs. The middleware keeps the route it
		// runs under, so a different pointer after the chain means an endpoint matched.
		before := c.Route()
		ctx, x := core.Start(c.UserContext(), view)
		c.SetUserContext(ctx)
		if trace, ok := propagate.FromContext(ctx); ok && trace.RequestID != "" {
			c.Set("X-Request-ID", trace.RequestID)
		}
		if core.CapturesBody(view) {
			body, _, truncated := core.ReadBody(bytes.NewReader(c.Body()))
			x.RequestBody(body, truncated, false)
			defer core.ReturnBody(body)
		}

		// The chain runs under the panic policy. Fiber does not recover, so an emitted
		// event must not wait for the caller.
		again := func() (again any) {
			defer func() {
				value := recover()
				if value == nil {
					return
				}
				x.Panic(value, debug.Stack())
				if core.PanicPolicy() == httpcore.Repanic {
					again = value
					return
				}
				// Fiber keeps no written flag, so a bare 200 and an untouched
				// response look alike here.
				if c.Response().StatusCode() == http.StatusOK && len(c.Response().Body()) == 0 {
					_ = c.Status(http.StatusInternalServerError)
				}
			}()
			if err := c.Next(); err != nil {
				// Fiber's own logger hands a handler error to the app's error
				// handler, and the middleware then returns nil.
				_ = c.App().ErrorHandler(c, err)
			}
			return nil
		}()

		after := c.Route()
		template := ""
		if after != before {
			template = after.Path
		}
		x.Route(template, template != "")
		x.End(wlogfasthttp.ResponseView{Ctx: c.Context()}, nil)
		if again != nil {
			panic(again)
		}
		return nil
	}
}

// Setup installs Middleware with the default Logger on a Fiber v2 app. Call it before any
// route, so the middleware wraps the whole chain.
func Setup(app *fiber.App) {
	app.Use(Middleware(wlog.Default()))
}
