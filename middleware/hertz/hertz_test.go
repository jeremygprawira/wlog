// This file runs the http conformance suite against the hertz adapter, and checks the
// behavior that only hertz has: a redirect event, an error a handler aborts with, and the
// one-line Setup.
//
// The factory drives the tracer the way the hertz protocol server does, because hertz
// runs its tracers in the protocol server and not in engine.ServeHTTP.
package wloghertz_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/network"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	httpconformance "github.com/jeremygprawira/wlog/internal/conformance/http"
	wloghertz "github.com/jeremygprawira/wlog/middleware/hertz"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestHertz_Conformance proves that the hertz adapter passes every scenario of the http
// suite.
func TestHertz_Conformance(t *testing.T) {
	httpconformance.Run(conformance.Tester{T: t}, hertzFactory{})
}

// hertzFactory builds the adapter around a hertz engine of the suite's route table.
type hertzFactory struct{}

// Build returns the engine as an http.Handler. The routes come from the suite, and the
// wrapper hands each request to the tracer as a hertz server would.
func (hertzFactory) Build(log *wlog.Logger, routes httpconformance.Routes, settings httpconformance.Settings) http.Handler {
	h := server.New()
	// hertz answers an unmatched path with its own 404 body, and the suite's golden
	// carries the plain net/http body, so the factory writes that body.
	h.NoRoute(func(_ context.Context, ctx *app.RequestContext) {
		ctx.SetContentType("text/plain; charset=utf-8")
		ctx.SetStatusCode(http.StatusNotFound)
		ctx.SetBodyString("404 page not found\n")
	})

	opts := []wloghertz.Option{wloghertz.SkipPaths("/skip")}
	if settings.CaptureAll {
		opts = append(opts, wloghertz.CaptureAll())
	}
	if settings.MaxBody > 0 {
		opts = append(opts, wloghertz.MaxBodyCapture(settings.MaxBody))
	}
	h.GetTracer().Append(wloghertz.Tracer(log, opts...))
	h.Use(wloghertz.Middleware())

	h.Handle(http.MethodGet, "/ok", adapt(routes.OK))
	h.Handle(http.MethodPost, "/orders/:id", adapt(routes.Order))
	h.Handle(http.MethodGet, "/panic", adapt(routes.Panic))
	h.Handle(http.MethodGet, "/status/:code", adapt(routes.Status))
	h.Handle(http.MethodGet, "/stream", adapt(routes.Stream))
	h.Handle(http.MethodGet, "/fail", adapt(routes.Fail))
	h.Handle(http.MethodGet, "/skip", adapt(routes.OK))
	h.Handle(http.MethodHead, "/ok", adapt(routes.OK))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { serveHandler(h, w, r) })
}

// useAdapter wires the adapter around log into a hertz server with no extra options.
func useAdapter(h *server.Hertz, log *wlog.Logger) {
	h.GetTracer().Append(wloghertz.Tracer(log))
	h.Use(wloghertz.Middleware())
}

// TestHertz_HTTP7_RedirectStatus proves that a redirect gives one event with status 301.
func TestHertz_HTTP7_RedirectStatus(t *testing.T) {
	log, rec := wlogtest.New(t)
	h := server.New()
	useAdapter(h, log)
	h.GET("/ok", func(_ context.Context, ctx *app.RequestContext) {
		ctx.SetStatusCode(http.StatusOK)
	})

	serve(t, h, getRequest("/ok/"))

	if count := rec.Count(); count != 1 {
		t.Fatalf("events = %d, want 1", count)
	}
	httpFields, _ := rec.Last()["http"].(map[string]any)
	if !conformance.Equal(httpFields["status"], http.StatusMovedPermanently) {
		t.Errorf("http.status = %v, want 301", httpFields["status"])
	}
}

// TestHertz_HTTP4_AbortErrorRecorded proves that an error a handler aborts with reaches
// the event with the final status.
func TestHertz_HTTP4_AbortErrorRecorded(t *testing.T) {
	log, rec := wlogtest.New(t)
	h := server.New()
	useAdapter(h, log)
	h.GET("/fail", func(_ context.Context, ctx *app.RequestContext) {
		ctx.AbortWithError(http.StatusBadRequest, errors.New("the order is not payable"))
	})

	serve(t, h, getRequest("/fail"))

	event := rec.Last()
	httpFields, _ := event["http"].(map[string]any)
	if !conformance.Equal(httpFields["status"], http.StatusBadRequest) {
		t.Errorf("http.status = %v, want 400", httpFields["status"])
	}
	if event["level"] != "warn" {
		t.Errorf("level = %v, want warn", event["level"])
	}
	errorFields, _ := event["error"].(map[string]any)
	if errorFields["message"] != "the order is not payable" {
		t.Errorf("error.message = %v, want the abort message", errorFields["message"])
	}
}

// TestHertz_Setup_InstallsTracer proves that Setup wires the default Logger and the
// middleware into a hertz server, so one request becomes one event.
func TestHertz_Setup_InstallsTracer(t *testing.T) {
	log, rec := wlogtest.New(t)
	previous := wlog.Default()
	wlog.SetDefault(log)
	t.Cleanup(func() { wlog.SetDefault(previous) })

	h := server.New()
	wloghertz.Setup(h)
	h.GET("/ok", func(_ context.Context, ctx *app.RequestContext) {
		ctx.SetStatusCode(http.StatusOK)
	})

	serve(t, h, getRequest("/ok"))

	if count := rec.Count(); count != 1 {
		t.Fatalf("events = %d, want 1", count)
	}
	httpFields, _ := rec.Last()["http"].(map[string]any)
	if httpFields["route"] != "/ok" {
		t.Errorf("http.route = %v, want /ok", httpFields["route"])
	}
}

// adapt runs one net/http handler of the suite as a hertz handler. The handler reads the
// event context, so a handler adds its own fields to the open event.
func adapt(h http.HandlerFunc) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		req, err := http.NewRequestWithContext(c, string(ctx.Method()), string(ctx.URI().RequestURI()), bytes.NewReader(ctx.Request.Body()))
		if err != nil {
			ctx.SetStatusCode(http.StatusInternalServerError)
			return
		}
		req.Host = string(ctx.Host())
		req.RemoteAddr = ctx.RemoteAddr().String()
		rec := httptest.NewRecorder()
		h(rec, req)
		for name, values := range rec.Header() {
			for _, value := range values {
				ctx.Response.Header.Add(name, value)
			}
		}
		ctx.Response.SetStatusCode(rec.Code)
		ctx.Response.SetBody(rec.Body.Bytes())
	}
}

// serve drives one request through the tracer and returns the response. It repeats what
// the hertz protocol server does around engine.ServeHTTP.
func serve(t *testing.T, h *server.Hertz, r *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	ctx := newContext(h, r)
	tracer := h.GetTracer()
	c := tracer.DoStart(context.Background(), ctx)
	h.ServeHTTP(c, ctx)
	tracer.DoFinish(c, ctx, nil)

	rec := httptest.NewRecorder()
	ctx.Response.Header.VisitAll(func(name, value []byte) {
		rec.Header().Add(string(name), string(value))
	})
	rec.WriteHeader(ctx.Response.StatusCode())
	_, _ = rec.Write(ctx.Response.Body())
	return rec
}

// serveHandler drives one request inside an http.Handler, which the conformance suite
// asks for.
func serveHandler(h *server.Hertz, w http.ResponseWriter, r *http.Request) {
	ctx := newContext(h, r)
	tracer := h.GetTracer()
	c := tracer.DoStart(context.Background(), ctx)
	h.ServeHTTP(c, ctx)
	tracer.DoFinish(c, ctx, nil)
	ctx.Response.Header.VisitAll(func(name, value []byte) {
		w.Header().Add(string(name), string(value))
	})
	w.WriteHeader(ctx.Response.StatusCode())
	_, _ = w.Write(ctx.Response.Body())
}

// newContext copies one net/http request into a hertz request context, with the client
// address the suite asks for.
func newContext(h *server.Hertz, r *http.Request) *app.RequestContext {
	ctx := h.NewContext()
	ctx.Request.Header.SetMethod(r.Method)
	ctx.Request.Header.SetProtocol("HTTP/1.1")
	ctx.Request.SetRequestURI(r.URL.RequestURI())
	ctx.Request.Header.SetHost(r.Host)
	for name, values := range r.Header {
		for i, value := range values {
			if i == 0 {
				ctx.Request.Header.Set(name, value)
			} else {
				ctx.Request.Header.Add(name, value)
			}
		}
	}
	if r.Body != nil {
		body, _ := io.ReadAll(r.Body)
		ctx.Request.SetBody(body)
	}
	if r.ContentLength >= 0 {
		ctx.Request.Header.SetContentLength(int(r.ContentLength))
	}
	ctx.SetConn(fixedConn{remote: &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 1234}})
	return ctx
}

// fixedConn answers the client address of one request, because a context outside a real
// server carries no connection.
type fixedConn struct {
	network.Conn
	remote net.Addr
}

// RemoteAddr returns the client address of the test request.
func (c fixedConn) RemoteAddr() net.Addr { return c.remote }

// getRequest builds the plain request the focused tests drive.
func getRequest(path string) *http.Request {
	request, _ := http.NewRequest(http.MethodGet, path, nil)
	request.Host = "example.com"
	request.RemoteAddr = "192.0.2.1:1234"
	return request
}
