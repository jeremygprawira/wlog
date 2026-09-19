// This file runs the http conformance suite against the Echo v4 adapter, and checks the
// behavior that only Echo has: a returned error reaches wlog, and a panic updates the
// response Echo reports.
package wlogecho_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	httpconformance "github.com/jeremygprawira/wlog/internal/conformance/http"
	wlogecho "github.com/jeremygprawira/wlog/middleware/echo"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestEcho_Conformance proves that the Echo adapter passes every scenario of the http
// suite.
func TestEcho_Conformance(t *testing.T) {
	httpconformance.Run(conformance.Tester{T: t}, echoFactory{})
}

// echoFactory builds the adapter around an Echo router of the suite's route table.
type echoFactory struct{}

// Build returns the Echo server with the middleware.
func (echoFactory) Build(log *wlog.Logger, routes httpconformance.Routes, settings httpconformance.Settings) http.Handler {
	e := echo.New()
	// Echo answers an unmatched path with a JSON body, and the suite's golden carries the
	// plain net/http body, so the factory gives Echo the same 404 body.
	e.RouteNotFound("/*", func(c echo.Context) error {
		c.Response().Header().Set(echo.HeaderContentType, "text/plain; charset=utf-8")
		c.Response().WriteHeader(http.StatusNotFound)
		_, _ = c.Response().Write([]byte("404 page not found\n"))
		return nil
	})

	opts := []wlogecho.Option{wlogecho.SkipPaths("/skip")}
	if settings.CaptureAll {
		opts = append(opts, wlogecho.CaptureAll())
	}
	if settings.MaxBody > 0 {
		opts = append(opts, wlogecho.MaxBodyCapture(settings.MaxBody))
	}
	e.Use(wlogecho.Middleware(log, opts...))

	wrap := func(h http.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
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

// TestEcho_HTTP11_HTTPError400Warn proves that a 400 error the handler returns gives level
// warn and a status of 400.
func TestEcho_HTTP11_HTTPError400Warn(t *testing.T) {
	log, rec := wlogtest.New(t)
	e := echo.New()
	e.Use(wlogecho.Middleware(log))
	e.GET("/bad", func(echo.Context) error {
		return echo.NewHTTPError(http.StatusBadRequest, "bad request")
	})

	req := httptest.NewRequest(http.MethodGet, "/bad", nil)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if rec.Last()["level"] != "warn" {
		t.Errorf("level = %v, want warn", rec.Last()["level"])
	}
	httpFields, _ := rec.Last()["http"].(map[string]any)
	if !conformance.Equal(httpFields["status"], http.StatusBadRequest) {
		t.Errorf("event http.status = %v, want 400", httpFields["status"])
	}
}

// TestEcho_HTTP12_PanicUpdatesResponse proves that a panic in the handler gives a 500 on
// the wire and in the event.
func TestEcho_HTTP12_PanicUpdatesResponse(t *testing.T) {
	log, rec := wlogtest.New(t)
	e := echo.New()
	e.Use(wlogecho.Middleware(log))
	e.GET("/panic", func(echo.Context) error { panic("boom") })

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
	httpFields, _ := rec.Last()["http"].(map[string]any)
	if !conformance.Equal(httpFields["status"], http.StatusInternalServerError) {
		t.Errorf("event http.status = %v, want 500", httpFields["status"])
	}
	if rec.Last()["level"] != "error" {
		t.Errorf("level = %v, want error", rec.Last()["level"])
	}
}

// TestEcho_HandlerError_ReachesWlogErrorAndEchoErrorHandler proves that a returned error
// reaches wlog.Error and then flows to Echo's own HTTPErrorHandler unchanged.
func TestEcho_HandlerError_ReachesWlogErrorAndEchoErrorHandler(t *testing.T) {
	log, rec := wlogtest.New(t)
	e := echo.New()
	wantErr := errors.New("boom")
	e.HTTPErrorHandler = func(err error, c echo.Context) {
		if !c.Response().Committed {
			_ = c.String(http.StatusTeapot, "handled: "+err.Error())
		}
	}
	e.Use(wlogecho.Middleware(log))
	e.GET("/fail", func(echo.Context) error { return wantErr })

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
	if got := httpFields["status"]; got != int64(http.StatusTeapot) && got != http.StatusTeapot {
		t.Errorf("event http.status = %v (%T), want %d", got, got, http.StatusTeapot)
	}
}

// TestEcho_Route_UsesEchoPathTemplate proves that the route is Echo's own path template.
func TestEcho_Route_UsesEchoPathTemplate(t *testing.T) {
	log, rec := wlogtest.New(t)
	e := echo.New()
	e.Use(wlogecho.Middleware(log))
	e.GET("/users/:id", func(c echo.Context) error {
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
