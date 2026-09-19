// This file runs the http conformance suite against the Echo v5 adapter, and checks the
// behavior that only Echo has: a returned error reaches wlog, and an error after a
// committed response writes no second body.
package wlogecho5_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	echo "github.com/labstack/echo/v5"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	httpconformance "github.com/jeremygprawira/wlog/internal/conformance/http"
	wlogecho5 "github.com/jeremygprawira/wlog/middleware/echo5"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestEcho5_Conformance proves that the Echo v5 adapter passes every scenario of the http
// suite.
func TestEcho5_Conformance(t *testing.T) {
	httpconformance.Run(conformance.Tester{T: t}, echoFactory{})
}

// echoFactory builds the adapter around an Echo router of the suite's route table.
type echoFactory struct{}

// Build returns the Echo server with the middleware.
func (echoFactory) Build(log *wlog.Logger, routes httpconformance.Routes, settings httpconformance.Settings) http.Handler {
	e := echo.New()
	// The suite's golden carries the plain net/http 404 body, so the factory gives Echo
	// the same body.
	e.RouteNotFound("/*", func(c *echo.Context) error {
		c.Response().Header().Set(echo.HeaderContentType, "text/plain; charset=utf-8")
		c.Response().WriteHeader(http.StatusNotFound)
		_, _ = c.Response().Write([]byte("404 page not found\n"))
		return nil
	})

	opts := []wlogecho5.Option{wlogecho5.SkipPaths("/skip")}
	if settings.CaptureAll {
		opts = append(opts, wlogecho5.CaptureAll())
	}
	if settings.MaxBody > 0 {
		opts = append(opts, wlogecho5.MaxBodyCapture(settings.MaxBody))
	}
	e.Use(wlogecho5.Middleware(log, opts...))

	wrap := func(h http.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			h(c.Response(), c.Request())
			return nil
		}
	}
	e.Any("/ok", wrap(routes.OK))
	e.Any("/orders/:id", wrap(routes.Order))
	e.Any("/panic", wrap(routes.Panic))
	e.Any("/status/:code", wrap(routes.Status))
	e.Any("/stream", wrap(routes.Stream))
	e.Any("/fail", wrap(routes.Fail))
	e.Any("/skip", wrap(routes.OK))
	return e
}

// TestEcho5_HTTP5_NoSecondBody proves that an error after a committed response writes no
// second body.
func TestEcho5_HTTP5_NoSecondBody(t *testing.T) {
	log, rec := wlogtest.New(t)
	e := echo.New()
	e.HTTPErrorHandler = func(c *echo.Context, _ error) {
		_ = c.String(http.StatusInternalServerError, "late")
	}
	e.Use(wlogecho5.Middleware(log))
	e.GET("/late", func(c *echo.Context) error {
		_ = c.String(http.StatusOK, "first")
		return errors.New("late failure")
	})

	req := httptest.NewRequest(http.MethodGet, "/late", nil)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)

	if w.Body.String() != "first" {
		t.Errorf("body = %q, want the first body only", w.Body.String())
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want the committed 200", w.Code)
	}
	httpFields, _ := rec.Last()["http"].(map[string]any)
	if !conformance.Equal(httpFields["status"], http.StatusOK) {
		t.Errorf("event http.status = %v, want 200", httpFields["status"])
	}
}

// TestEcho5_HandlerError_ReachesWlogErrorAndEchoErrorHandler proves that a returned error
// reaches wlog.Error and then flows to Echo's own HTTPErrorHandler unchanged.
func TestEcho5_HandlerError_ReachesWlogErrorAndEchoErrorHandler(t *testing.T) {
	log, rec := wlogtest.New(t)
	e := echo.New()
	wantErr := errors.New("boom")
	e.HTTPErrorHandler = func(c *echo.Context, err error) {
		_ = c.String(http.StatusTeapot, "handled: "+err.Error())
	}
	e.Use(wlogecho5.Middleware(log))
	e.GET("/fail", func(c *echo.Context) error { return wantErr })

	req := httptest.NewRequest(http.MethodGet, "/fail", nil)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)

	if w.Code != http.StatusTeapot {
		t.Errorf("status = %d, want %d (Echo's HTTPErrorHandler should still run)", w.Code, http.StatusTeapot)
	}
	if w.Body.String() != "handled: boom" {
		t.Errorf("body = %q, want %q", w.Body.String(), "handled: boom")
	}

	rec.RequireErrorCode(t, "INTERNAL")
	httpFields := rec.Last()["http"].(map[string]any)
	if !conformance.Equal(httpFields["status"], http.StatusTeapot) {
		t.Errorf("event http.status = %v, want %d", httpFields["status"], http.StatusTeapot)
	}
}

// TestEcho5_Route_UsesEchoPathTemplate proves that the route is Echo's own path template.
func TestEcho5_Route_UsesEchoPathTemplate(t *testing.T) {
	log, rec := wlogtest.New(t)
	e := echo.New()
	e.Use(wlogecho5.Middleware(log))
	e.GET("/users/:id", func(c *echo.Context) error {
		wlog.Set(c.Request().Context(), "x", 1)
		return c.NoContent(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)

	httpFields := rec.Last()["http"].(map[string]any)
	if httpFields["route"] != "/users/:id" {
		t.Errorf("http.route = %v, want /users/:id", httpFields["route"])
	}
}
