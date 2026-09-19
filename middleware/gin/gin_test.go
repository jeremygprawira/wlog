// This file runs the http conformance suite against the Gin adapter, and checks the
// behavior that only Gin has: a panic stops the chain, a status set before the first write
// reaches the wire, a 404 keeps its own status, and an error recorded before a panic
// reaches the event.
package wloggin_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	httpconformance "github.com/jeremygprawira/wlog/internal/conformance/http"
	wloggin "github.com/jeremygprawira/wlog/middleware/gin"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestGin_Conformance proves that the Gin adapter passes every scenario of the http suite.
func TestGin_Conformance(t *testing.T) {
	httpconformance.Run(conformance.Tester{T: t}, ginFactory{})
}

// ginFactory builds the adapter around a Gin engine of the suite's route table.
type ginFactory struct{}

// Build returns the Gin engine with the middleware.
func (ginFactory) Build(log *wlog.Logger, routes httpconformance.Routes, settings httpconformance.Settings) http.Handler {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// The suite's golden carries the plain net/http 404 body, so the factory gives Gin
	// the same body.
	r.NoRoute(func(c *gin.Context) {
		c.Header("Content-Type", "text/plain; charset=utf-8")
		c.Status(http.StatusNotFound)
		_, _ = c.Writer.Write([]byte("404 page not found\n"))
	})

	opts := []wloggin.Option{wloggin.SkipPaths("/skip")}
	if settings.CaptureAll {
		opts = append(opts, wloggin.CaptureAll())
	}
	if settings.MaxBody > 0 {
		opts = append(opts, wloggin.MaxBodyCapture(settings.MaxBody))
	}
	r.Use(wloggin.Middleware(log, opts...))

	wrap := func(h http.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) { h(c.Writer, c.Request) }
	}
	r.Any("/ok", wrap(routes.OK))
	r.Any("/orders/:id", wrap(routes.Order))
	r.Any("/panic", wrap(routes.Panic))
	r.Any("/status/:code", wrap(routes.Status))
	r.Any("/stream", wrap(routes.Stream))
	r.Any("/fail", wrap(routes.Fail))
	r.Any("/skip", wrap(routes.OK))
	return r
}

// TestGin_HTTP2_PanicStopsChain proves that a panic in a middleware stops the chain, and
// that the handler never runs.
func TestGin_HTTP2_PanicStopsChain(t *testing.T) {
	log, rec := wlogtest.New(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(wloggin.Middleware(log))
	r.Use(func(*gin.Context) { panic("boom") })
	ran := false
	r.GET("/ok", func(c *gin.Context) {
		ran = true
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if ran {
		t.Error("the handler ran after a panicking middleware")
	}
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
	if rec.Last()["level"] != "error" {
		t.Errorf("level = %v, want error", rec.Last()["level"])
	}
}

// TestGin_HTTP3_StatusBeforeWrite proves that a status set before the first write reaches
// both the wire and the event.
func TestGin_HTTP3_StatusBeforeWrite(t *testing.T) {
	log, rec := wlogtest.New(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(wloggin.Middleware(log))
	r.GET("/teapot", func(c *gin.Context) {
		c.Status(http.StatusTeapot)
		_, _ = c.Writer.WriteString("short and stout")
	})

	req := httptest.NewRequest(http.MethodGet, "/teapot", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusTeapot {
		t.Errorf("status = %d, want 418", w.Code)
	}
	httpFields, _ := rec.Last()["http"].(map[string]any)
	if !conformance.Equal(httpFields["status"], http.StatusTeapot) {
		t.Errorf("event http.status = %v, want 418", httpFields["status"])
	}
}

// TestGin_HTTP4_404Status proves that an unmatched path logs its real 404 status and level
// warn, with no route.
func TestGin_HTTP4_404Status(t *testing.T) {
	log, rec := wlogtest.New(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(wloggin.Middleware(log))

	req := httptest.NewRequest(http.MethodGet, "/nope", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
	event := rec.Last()
	httpFields, _ := event["http"].(map[string]any)
	if !conformance.Equal(httpFields["status"], http.StatusNotFound) {
		t.Errorf("event http.status = %v, want 404", httpFields["status"])
	}
	if event["level"] != "warn" {
		t.Errorf("level = %v, want warn", event["level"])
	}
	if httpFields["route"] != "" {
		t.Errorf("http.route = %v, want an empty route", httpFields["route"])
	}
}

// TestGin_HTTP12_ErrorsBeforePanic proves that an error Gin recorded before a panic still
// reaches the event.
func TestGin_HTTP12_ErrorsBeforePanic(t *testing.T) {
	log, rec := wlogtest.New(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(wloggin.Middleware(log))
	r.GET("/boom", func(c *gin.Context) {
		_ = c.Error(errors.New("the first error"))
		panic("boom")
	})

	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	errorsField, _ := rec.Last()["errors"].([]any)
	found := false
	for _, item := range errorsField {
		if entry, ok := item.(map[string]any); ok && entry["message"] == "the first error" {
			found = true
		}
	}
	if !found {
		t.Errorf("errors = %v, want the error recorded before the panic", rec.Last()["errors"])
	}
}

// TestGin_HandlerError_ReachesWlogError proves that an error recorded with c.Error reaches
// the event's error group.
func TestGin_HandlerError_ReachesWlogError(t *testing.T) {
	log, rec := wlogtest.New(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	wantErr := errors.New("boom")
	r.Use(wloggin.Middleware(log))
	r.GET("/fail", func(c *gin.Context) {
		_ = c.Error(wantErr)
		c.String(http.StatusTeapot, "handled")
	})

	req := httptest.NewRequest(http.MethodGet, "/fail", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	rec.RequireErrorCode(t, "INTERNAL")
	httpFields, _ := rec.Last()["http"].(map[string]any)
	if !conformance.Equal(httpFields["status"], http.StatusTeapot) {
		t.Errorf("event http.status = %v, want %d", httpFields["status"], http.StatusTeapot)
	}
}

// TestGin_Route_UsesFullPathTemplate proves that the route is Gin's own full path
// template.
func TestGin_Route_UsesFullPathTemplate(t *testing.T) {
	log, rec := wlogtest.New(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(wloggin.Middleware(log))
	r.GET("/users/:id", func(c *gin.Context) {
		wlog.Set(c.Request.Context(), "x", 1)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	httpFields, _ := rec.Last()["http"].(map[string]any)
	if httpFields["route"] != "/users/:id" {
		t.Errorf("http.route = %v, want /users/:id", httpFields["route"])
	}
}
