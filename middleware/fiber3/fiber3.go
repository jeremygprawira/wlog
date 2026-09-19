// Package wlogfiber3 adapts http-core to Fiber v3, so a Fiber app gets the same wide
// event as a plain net/http app, in one call.
//
// Read top to bottom: Middleware opens one event per request through the shared fasthttp
// views, carries it on the Fiber context, and reads the route Fiber matched. A handler
// error goes to the app's error handler before the event emits. Setup installs Middleware
// with the default Logger, and it reports a problem when Fiber hides unmatched paths.
package wlogfiber3

import (
	"bytes"
	"net/http"
	"runtime/debug"

	"github.com/gofiber/fiber/v3"

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
// A handler reads the event context with c.Context, and a request that adds to an open
// event joins that event instead of starting one.
func Middleware(log *wlog.Logger, opts ...Option) fiber.Handler {
	core := httpcore.New(log, opts...)
	return func(c fiber.Ctx) error {
		view := wlogfasthttp.RequestView{Ctx: c.RequestCtx()}
		if core.Skip(view) {
			return c.Next()
		}

		ctx, x := core.Start(c.Context(), view)
		c.SetContext(ctx)
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

		template := ""
		if c.Matched() {
			template = c.FullPath()
		}
		x.Route(template, template != "")
		x.End(wlogfasthttp.ResponseView{Ctx: c.RequestCtx()}, nil)
		if again != nil {
			panic(again)
		}
		return nil
	}
}

// Setup installs Middleware with the default Logger on a Fiber v3 app. Call it before any
// route, so the middleware wraps the whole chain.
//
// SkipUnmatchedRoutes makes Fiber answer a 404 or a 405 before the middleware chain, so
// those requests start no event. Setup reports that fault once, and the app still runs.
func Setup(app *fiber.App) {
	log := wlog.Default()
	if app.Config().SkipUnmatchedRoutes {
		log.Report(wlog.Problem{
			Code:    "WLOG_INVALID_CONFIG",
			Source:  "fiber3",
			Message: "SkipUnmatchedRoutes answers 404 and 405 before the middleware chain, so those requests start no event",
		})
	}
	app.Use(Middleware(log))
}
