// This file runs the http conformance suite against the Fiber v3 adapter, and checks the
// behavior that only Fiber v3 has: FullPath with Matched, a handler error that reaches the
// app's error handler, a 404 with an empty route, the SkipUnmatchedRoutes report, and the
// one-line Setup.
package wlogfiber3_test

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttpadaptor"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	httpconformance "github.com/jeremygprawira/wlog/internal/conformance/http"
	wlogfiber3 "github.com/jeremygprawira/wlog/middleware/fiber3"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestFiber3_Conformance proves that the Fiber v3 adapter passes every scenario of the
// http suite.
func TestFiber3_Conformance(t *testing.T) {
	httpconformance.Run(conformance.Tester{T: t}, fiber3Factory{})
}

// fiber3Factory serves the suite through the Fiber v3 middleware.
type fiber3Factory struct{}

// Build returns the Fiber app with the middleware.
func (fiber3Factory) Build(log *wlog.Logger, routes httpconformance.Routes, settings httpconformance.Settings) http.Handler {
	app := fiber.New()
	opts := []wlogfiber3.Option{wlogfiber3.SkipPaths("/skip")}
	if settings.CaptureAll {
		opts = append(opts, wlogfiber3.CaptureAll())
	}
	if settings.MaxBody > 0 {
		opts = append(opts, wlogfiber3.MaxBodyCapture(settings.MaxBody))
	}
	app.Use(wlogfiber3.Middleware(log, opts...))

	app.All("/ok", wrap(routes.OK))
	app.All("/skip", wrap(routes.OK))
	app.All("/orders/:id", wrap(routes.Order))
	app.All("/status/:code", wrap(routes.Status))
	app.All("/panic", wrap(routes.Panic))
	app.All("/stream", wrap(routes.Stream))
	app.All("/fail", wrap(routes.Fail))
	// The suite gives every adapter the plain net/http 404 body, so the factory answers
	// an unmatched path with the same body. Fiber matches the exact routes first.
	app.All("/*", func(c fiber.Ctx) error {
		c.Set("Content-Type", "text/plain; charset=utf-8")
		return c.Status(http.StatusNotFound).SendString("404 page not found\n")
	})

	handler := app.Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := newRequestCtx(r)
		handler(ctx)
		copyResponse(w, ctx)
	})
}

// wrap turns one net/http handler of the suite into a Fiber handler. The request carries
// the event context, so a handler adds its fields to the open event.
func wrap(h http.HandlerFunc) fiber.Handler {
	return func(c fiber.Ctx) error {
		var request http.Request
		if err := fasthttpadaptor.ConvertRequest(c.RequestCtx(), &request, true); err != nil {
			return err
		}
		request = *request.WithContext(c.Context())
		recorder := httptest.NewRecorder()
		h(recorder, &request)
		c.Status(recorder.Code)
		for name, values := range recorder.Header() {
			for _, value := range values {
				c.Set(name, value)
			}
		}
		if body := recorder.Body.Bytes(); len(body) > 0 {
			_, _ = c.Write(body)
		}
		return nil
	}
}

// newRequestCtx builds the fasthttp context a server would hand to a Fiber handler.
func newRequestCtx(r *http.Request) *fasthttp.RequestCtx {
	ctx := &fasthttp.RequestCtx{}
	ctx.Request.Header.SetMethod(r.Method)
	ctx.Request.Header.SetProtocol(r.Proto)
	ctx.Request.SetRequestURI(r.URL.RequestURI())
	ctx.Request.Header.SetHost(r.Host)
	for name, values := range r.Header {
		for _, value := range values {
			ctx.Request.Header.Add(name, value)
		}
	}
	if r.Body != nil {
		body, _ := io.ReadAll(r.Body)
		ctx.Request.SetBody(body)
	}
	if addr, err := net.ResolveTCPAddr("tcp", r.RemoteAddr); err == nil {
		ctx.SetRemoteAddr(addr)
	}
	return ctx
}

// copyResponse copies a fasthttp response into the recorder the suite reads.
func copyResponse(w http.ResponseWriter, ctx *fasthttp.RequestCtx) {
	for name, value := range ctx.Response.Header.All() {
		w.Header().Add(string(name), string(value))
	}
	w.WriteHeader(ctx.Response.StatusCode())
	_, _ = w.Write(ctx.Response.Body())
}

// TestFiber3_HTTP4_HandlerErrorStatus proves that an error a handler returns reaches the
// app's error handler, so the event carries the final status.
func TestFiber3_HTTP4_HandlerErrorStatus(t *testing.T) {
	log, rec := wlogtest.New(t)
	app := fiber.New()
	app.Use(wlogfiber3.Middleware(log))
	app.Get("/fail", func(fiber.Ctx) error {
		return fiber.NewError(http.StatusBadGateway, "the upstream failed")
	})

	app.Handler()(newRequestCtx(getRequest("/fail")))

	httpFields, _ := rec.Last()["http"].(map[string]any)
	if !conformance.Equal(httpFields["status"], http.StatusBadGateway) {
		t.Errorf("http.status = %v, want 502", httpFields["status"])
	}
	if rec.Last()["level"] != "error" {
		t.Errorf("level = %v, want error", rec.Last()["level"])
	}
}

// TestFiber3_HTTP15_UnmatchedRouteBlank proves that an unmatched path logs status 404 with
// an empty route, as the shared table asks.
func TestFiber3_HTTP15_UnmatchedRouteBlank(t *testing.T) {
	log, rec := wlogtest.New(t)
	app := fiber.New()
	app.Use(wlogfiber3.Middleware(log))
	app.Get("/ok", func(c fiber.Ctx) error { return c.SendStatus(http.StatusOK) })

	app.Handler()(newRequestCtx(getRequest("/nope")))

	event := rec.Last()
	httpFields, _ := event["http"].(map[string]any)
	if httpFields["route"] != "" {
		t.Errorf("http.route = %v, want an empty route", httpFields["route"])
	}
	if !conformance.Equal(httpFields["status"], http.StatusNotFound) {
		t.Errorf("http.status = %v, want 404", httpFields["status"])
	}
	if event["level"] != "warn" {
		t.Errorf("level = %v, want warn", event["level"])
	}
}

// TestFiber3_Route_UsesFullPath proves that the route is Fiber's own full path.
func TestFiber3_Route_UsesFullPath(t *testing.T) {
	log, rec := wlogtest.New(t)
	app := fiber.New()
	app.Use(wlogfiber3.Middleware(log))
	app.Get("/users/:id", func(c fiber.Ctx) error { return c.SendStatus(http.StatusOK) })

	app.Handler()(newRequestCtx(getRequest("/users/42")))

	httpFields, _ := rec.Last()["http"].(map[string]any)
	if httpFields["route"] != "/users/:id" {
		t.Errorf("http.route = %v, want /users/:id", httpFields["route"])
	}
	if rec.Last()["operation"] != "GET /users/:id" {
		t.Errorf("operation = %v, want GET /users/:id", rec.Last()["operation"])
	}
}

// TestFiber3_Panic_RecoversWith500 proves that the default policy records the panic,
// answers 500, and emits the event.
func TestFiber3_Panic_RecoversWith500(t *testing.T) {
	log, rec := wlogtest.New(t)
	app := fiber.New()
	app.Use(wlogfiber3.Middleware(log))
	app.Get("/panic", func(fiber.Ctx) error { panic("conformance panic") })

	ctx := newRequestCtx(getRequest("/panic"))
	app.Handler()(ctx)

	if got := ctx.Response.StatusCode(); got != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", got)
	}
	if rec.Last()["level"] != "error" {
		t.Errorf("level = %v, want error", rec.Last()["level"])
	}
}

// TestFiber3_Setup_InstallsMiddleware proves that Setup wires the default Logger into a
// Fiber app, so one request becomes one event.
func TestFiber3_Setup_InstallsMiddleware(t *testing.T) {
	log, rec := wlogtest.New(t)
	previous := wlog.Default()
	wlog.SetDefault(log)
	t.Cleanup(func() { wlog.SetDefault(previous) })

	app := fiber.New()
	wlogfiber3.Setup(app)
	app.Get("/ok", func(c fiber.Ctx) error { return c.SendStatus(http.StatusOK) })

	app.Handler()(newRequestCtx(getRequest("/ok")))

	if count := rec.Count(); count != 1 {
		t.Fatalf("events = %d, want 1", count)
	}
	httpFields, _ := rec.Last()["http"].(map[string]any)
	if httpFields["route"] != "/ok" {
		t.Errorf("http.route = %v, want /ok", httpFields["route"])
	}
}

// TestFiber3_Setup_ReportsSkipUnmatchedRoutes proves that Setup reports a problem when the
// app hides 404 and 405 from the middleware chain, because those requests start no event.
func TestFiber3_Setup_ReportsSkipUnmatchedRoutes(t *testing.T) {
	var problems []wlog.Problem
	log, _ := wlogtest.New(t, wlog.OnProblem(func(p wlog.Problem) { problems = append(problems, p) }))
	previous := wlog.Default()
	wlog.SetDefault(log)
	t.Cleanup(func() { wlog.SetDefault(previous) })

	app := fiber.New(fiber.Config{SkipUnmatchedRoutes: true})
	wlogfiber3.Setup(app)

	if len(problems) != 1 {
		t.Fatalf("problems = %d, want 1: %v", len(problems), problems)
	}
	if problems[0].Code != "WLOG_INVALID_CONFIG" {
		t.Errorf("problem code = %q, want WLOG_INVALID_CONFIG", problems[0].Code)
	}
}

// getRequest builds the plain request the focused tests drive.
func getRequest(path string) *http.Request {
	request, _ := http.NewRequest(http.MethodGet, path, nil)
	request.Host = "example.com"
	request.RemoteAddr = "192.0.2.1:1234"
	return request
}
