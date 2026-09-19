// This file runs the http conformance suite against huma next to the chi adapter, and
// checks the one field huma owns: the operation id.
//
// The suite's routes go through the huma adapter, so every scenario runs the huma
// middleware chain. The adapter registers each suite handler as a raw operation, because
// the suite drives net/http handlers and huma operations carry typed inputs.
package wloghuma_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	httpconformance "github.com/jeremygprawira/wlog/internal/conformance/http"
	wlogchi "github.com/jeremygprawira/wlog/middleware/chi"
	wloghuma "github.com/jeremygprawira/wlog/middleware/huma"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestHuma_Conformance proves that huma next to the chi adapter passes every scenario of
// the http suite.
func TestHuma_Conformance(t *testing.T) {
	httpconformance.Run(conformance.Tester{T: t}, humaFactory{})
}

// humaFactory builds the adapter around a chi router with a huma API on it.
type humaFactory struct{}

// Build returns the chi router with the chi middleware, and the suite's routes registered
// through the huma adapter.
func (humaFactory) Build(log *wlog.Logger, routes httpconformance.Routes, settings httpconformance.Settings) http.Handler {
	r := chi.NewRouter()
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("404 page not found\n"))
	})

	opts := []wlogchi.Option{wlogchi.SkipPaths("/skip")}
	if settings.CaptureAll {
		opts = append(opts, wlogchi.CaptureAll())
	}
	if settings.MaxBody > 0 {
		opts = append(opts, wlogchi.MaxBodyCapture(settings.MaxBody))
	}
	r.Use(wlogchi.Middleware(log, opts...))

	api := humachi.New(r, huma.DefaultConfig("conformance", "1.0.0"))
	api.UseMiddleware(wloghuma.Middleware())

	registerRaw(api, http.MethodGet, "/ok", routes.OK)
	registerRaw(api, http.MethodPost, "/orders/{id}", routes.Order)
	registerRaw(api, http.MethodGet, "/panic", routes.Panic)
	registerRaw(api, http.MethodGet, "/status/{code}", routes.Status)
	registerRaw(api, http.MethodGet, "/stream", routes.Stream)
	registerRaw(api, http.MethodGet, "/fail", routes.Fail)
	registerRaw(api, http.MethodGet, "/skip", routes.OK)
	registerRaw(api, http.MethodHead, "/ok", routes.OK)
	return r
}

// TestHuma_HTTP_OperationID proves that the huma middleware records the operation id of
// the matched operation, and that the chi adapter supplies the route.
func TestHuma_HTTP_OperationID(t *testing.T) {
	log, rec := wlogtest.New(t)
	r := chi.NewRouter()
	r.Use(wlogchi.Middleware(log))
	api := humachi.New(r, huma.DefaultConfig("test", "1.0.0"))
	api.UseMiddleware(wloghuma.Middleware())
	huma.Register(api, huma.Operation{
		OperationID: "list-orders",
		Method:      http.MethodGet,
		Path:        "/orders/{id}",
	}, func(context.Context, *orderInput) (*orderOutput, error) {
		return &orderOutput{}, nil
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/orders/42", nil))

	if count := rec.Count(); count != 1 {
		t.Fatalf("events = %d, want 1", count)
	}
	httpFields, _ := rec.Last()["http"].(map[string]any)
	if httpFields["operation_id"] != "list-orders" {
		t.Errorf("http.operation_id = %v, want list-orders", httpFields["operation_id"])
	}
	if httpFields["route"] != "/orders/{id}" {
		t.Errorf("http.route = %v, want /orders/{id}", httpFields["route"])
	}
}

// orderInput is the typed input of the test operation.
type orderInput struct {
	ID string `path:"id"`
}

// orderOutput is the typed output of the test operation.
type orderOutput struct {
	Body struct {
		ID string `json:"id"`
	}
}

// registerRaw registers one net/http handler of the suite as a huma operation, with the
// huma middleware chain around it. The suite's golden events carry no operation id, so a
// raw operation carries none.
func registerRaw(api huma.API, method, path string, h http.HandlerFunc) {
	op := &huma.Operation{Method: method, Path: path}
	api.Adapter().Handle(op, api.Middlewares().Handler(func(ctx huma.Context) {
		target := ctx.URL()
		req, err := http.NewRequestWithContext(ctx.Context(), ctx.Method(), target.String(), ctx.BodyReader())
		if err != nil {
			ctx.SetStatus(http.StatusInternalServerError)
			return
		}
		req.Host = ctx.Host()
		ctx.EachHeader(func(name, value string) { req.Header.Add(name, value) })

		rec := httptest.NewRecorder()
		h(rec, req)
		for name, values := range rec.Header() {
			for _, value := range values {
				ctx.AppendHeader(name, value)
			}
		}
		ctx.SetStatus(rec.Code)
		_, _ = ctx.BodyWriter().Write(rec.Body.Bytes())
	}))
}
