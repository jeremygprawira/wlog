// This file runs the http conformance suite against the fasthttp adapter through a
// fasthttp context built by hand, and checks the behavior that only fasthttp has: the
// event context a handler reads, the panic policy, an unknown size for a body stream, and
// values that survive the reuse of the fasthttp buffers.
package wlogfasthttp_test

import (
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttpadaptor"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	httpconformance "github.com/jeremygprawira/wlog/internal/conformance/http"
	wlogfasthttp "github.com/jeremygprawira/wlog/middleware/fasthttp"
	"github.com/jeremygprawira/wlog/middleware/httpcore"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestFastHTTP_Conformance proves that the fasthttp adapter passes every scenario of the
// http suite.
func TestFastHTTP_Conformance(t *testing.T) {
	httpconformance.Run(conformance.Tester{T: t}, fasthttpFactory{})
}

// fasthttpFactory serves the suite through the fasthttp middleware. The suite drives an
// http.Handler, so the factory builds a fasthttp context per request, and copies the
// fasthttp response back to the recorder the suite reads.
type fasthttpFactory struct{}

// Build returns the middleware around the suite's route table.
func (fasthttpFactory) Build(log *wlog.Logger, routes httpconformance.Routes, settings httpconformance.Settings) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", routes.OK)
	mux.HandleFunc("/skip", routes.OK)
	mux.HandleFunc("/orders/{id}", routes.Order)
	mux.HandleFunc("/status/{code}", routes.Status)
	mux.HandleFunc("/panic", routes.Panic)
	mux.HandleFunc("/stream", routes.Stream)
	mux.HandleFunc("/fail", routes.Fail)

	opts := []wlogfasthttp.Option{
		wlogfasthttp.Core(httpcore.SkipPaths("/skip")),
		wlogfasthttp.RouteFunc(routeOf),
	}
	if settings.CaptureAll {
		opts = append(opts, wlogfasthttp.Core(httpcore.CaptureAll()))
	}
	if settings.MaxBody > 0 {
		opts = append(opts, wlogfasthttp.Core(httpcore.MaxBody(settings.MaxBody)))
	}

	handler := wlogfasthttp.Middleware(log, opts...)(func(ctx *fasthttp.RequestCtx) {
		var request http.Request
		if err := fasthttpadaptor.ConvertRequest(ctx, &request, true); err != nil {
			ctx.Error("bad request", fasthttp.StatusBadRequest)
			return
		}
		request = *request.WithContext(wlogfasthttp.Context(ctx))
		writer := &ctxWriter{ctx: ctx}
		mux.ServeHTTP(writer, &request)
		writer.flush()
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := newRequestCtx(r)
		handler(ctx)
		copyResponse(w, ctx)
	})
}

// routeOf reports the suite's route template for one request path.
func routeOf(ctx *fasthttp.RequestCtx) string {
	path := string(ctx.Path())
	switch {
	case path == "/ok" || path == "/skip" || path == "/panic" || path == "/stream" || path == "/fail":
		return path
	case strings.HasPrefix(path, "/orders/"):
		return "/orders/{id}"
	case strings.HasPrefix(path, "/status/"):
		return "/status/{code}"
	}
	return ""
}

// ctxWriter writes one net/http response into a fasthttp context, with the response rules
// of net/http: a status set without a write still reaches the wire, and a header reaches
// the context once the handler flushes it.
type ctxWriter struct {
	ctx    *fasthttp.RequestCtx
	header http.Header
	status int
}

// Header returns the response header map.
func (w *ctxWriter) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}

// WriteHeader records the first status the handler sets.
func (w *ctxWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

// Write records a default status and writes the body to the fasthttp response.
func (w *ctxWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ctx.Response.BodyWriter().Write(body)
}

// flush copies the recorded status and headers into the fasthttp response.
func (w *ctxWriter) flush() {
	for name, values := range w.Header() {
		for _, value := range values {
			w.ctx.Response.Header.Add(name, value)
		}
	}
	status := w.status
	if status == 0 {
		status = http.StatusOK
	}
	w.ctx.SetStatusCode(status)
}

// newRequestCtx builds the fasthttp context a server would hand to a handler. It carries
// no Content-Length header, the same shape the suite gives every adapter.
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

// TestFastHTTP_Context_CarriesEvent proves that a handler reads the open event through
// Context, and that the route stays empty when no route function is set.
func TestFastHTTP_Context_CarriesEvent(t *testing.T) {
	log, rec := wlogtest.New(t)
	handler := wlogfasthttp.Middleware(log)(func(ctx *fasthttp.RequestCtx) {
		wlog.Set(wlogfasthttp.Context(ctx), "order_id", "A-1")
		ctx.SetStatusCode(http.StatusOK)
	})

	handler(newRequestCtx(getRequest("/ok")))

	if rec.Last()["order_id"] != "A-1" {
		t.Errorf("order_id = %v, want A-1", rec.Last()["order_id"])
	}
	httpFields, _ := rec.Last()["http"].(map[string]any)
	if httpFields["route"] != "" {
		t.Errorf("http.route = %v, want an empty route", httpFields["route"])
	}
}

// TestFastHTTP_Panic_RecoversWith500 proves that the default policy records the panic,
// answers 500, and emits the event.
func TestFastHTTP_Panic_RecoversWith500(t *testing.T) {
	log, rec := wlogtest.New(t)
	handler := wlogfasthttp.Middleware(log)(func(*fasthttp.RequestCtx) {
		panic("conformance panic")
	})

	ctx := newRequestCtx(getRequest("/panic"))
	handler(ctx)

	if got := ctx.Response.StatusCode(); got != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", got)
	}
	if rec.Last()["level"] != "error" {
		t.Errorf("level = %v, want error", rec.Last()["level"])
	}
	httpFields, _ := rec.Last()["http"].(map[string]any)
	if !conformance.Equal(httpFields["bytes_out"], 0) {
		t.Errorf("http.bytes_out = %v, want 0", httpFields["bytes_out"])
	}
}

// TestFastHTTP_Panic_RepanicEmitsThenPanics proves that Repanic emits the event first,
// and then lets the panic continue.
func TestFastHTTP_Panic_RepanicEmitsThenPanics(t *testing.T) {
	log, rec := wlogtest.New(t)
	handler := wlogfasthttp.Middleware(log, wlogfasthttp.PanicPolicy(httpcore.Repanic))(
		func(*fasthttp.RequestCtx) { panic("conformance panic") },
	)

	defer func() {
		value := recover()
		if value != "conformance panic" {
			t.Errorf("recovered %v, want the panic value", value)
		}
		if count := rec.Count(); count != 1 {
			t.Errorf("events = %d, want 1", count)
		}
		if rec.Last()["level"] != "error" {
			t.Errorf("level = %v, want error", rec.Last()["level"])
		}
	}()

	handler(newRequestCtx(getRequest("/panic")))
	t.Error("the panic did not continue")
}

// TestFastHTTP_BytesWritten_UnknownForBodyStream proves that a response body stream
// reports no size, and stays unread.
func TestFastHTTP_BytesWritten_UnknownForBodyStream(t *testing.T) {
	log, rec := wlogtest.New(t)
	handler := wlogfasthttp.Middleware(log)(func(ctx *fasthttp.RequestCtx) {
		ctx.Response.SetBodyStream(strings.NewReader("chunk"), -1)
	})

	ctx := newRequestCtx(getRequest("/stream"))
	handler(ctx)

	httpFields, _ := rec.Last()["http"].(map[string]any)
	if _, present := httpFields["bytes_out"]; present {
		t.Errorf("http.bytes_out = %v, want no size for a body stream", httpFields["bytes_out"])
	}
	if ctx.Response.IsBodyStream() == false {
		t.Error("the body stream was consumed")
	}
}

// TestFastHTTP_Views_SurviveBufferReuse proves that every value a view hands out is a
// copy, so a later write over a fasthttp buffer never changes it.
func TestFastHTTP_Views_SurviveBufferReuse(t *testing.T) {
	ctx := &fasthttp.RequestCtx{}
	ctx.Request.Header.SetMethod("POST")
	ctx.Request.Header.Set("X-Token", "first")
	ctx.Request.SetRequestURI("/orders/42")

	view := wlogfasthttp.RequestView{Ctx: ctx}
	method, token, path := view.Method(), view.Header("X-Token"), view.Path()
	seen := ""
	view.EachHeader(func(name, value string) {
		if strings.EqualFold(name, "X-Token") {
			seen = value
		}
	})

	ctx.Request.Header.SetMethod("GET")
	ctx.Request.Header.Set("X-Token", "second")
	ctx.Request.SetRequestURI("/orders/99")

	if method != "POST" {
		t.Errorf("method = %q, want POST", method)
	}
	if token != "first" {
		t.Errorf("header = %q, want first", token)
	}
	if path != "/orders/42" {
		t.Errorf("path = %q, want /orders/42", path)
	}
	if seen != "first" {
		t.Errorf("visited header = %q, want first", seen)
	}
}

// getRequest builds the plain request the focused tests drive.
func getRequest(path string) *http.Request {
	request, _ := http.NewRequest(http.MethodGet, path, nil)
	request.Host = "example.com"
	request.RemoteAddr = "192.0.2.1:1234"
	return request
}
