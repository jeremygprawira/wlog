package wlogecho_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	wlogecho "github.com/jeremygprawira/wlog/middleware/echo"
	"github.com/jeremygprawira/wlog/wlogtest"
)

type echoAdapter struct{}

func (echoAdapter) Build(log *wlog.Logger, routes conformance.Routes) http.Handler {
	e := echo.New()
	e.Use(wlogecho.Middleware(log, wlogecho.SkipPaths("/skip")))
	e.GET("/ok", func(c echo.Context) error { routes.OK(c.Response(), c.Request()); return nil })
	e.GET("/panic", func(c echo.Context) error { routes.Panic(c.Response(), c.Request()); return nil })
	e.POST("/echo", func(c echo.Context) error { routes.Echo(c.Response(), c.Request()); return nil })
	e.GET("/skip", func(c echo.Context) error { routes.OK(c.Response(), c.Request()); return nil })
	return e
}

func TestConformance(t *testing.T) {
	conformance.Run(t, echoAdapter{})
}

func TestEcho_HandlerError_ReachesWlogErrorAndEchoErrorHandler(t *testing.T) {
	log, rec := wlogtest.New(t)
	e := echo.New()
	wantErr := errors.New("boom")
	e.HTTPErrorHandler = func(err error, c echo.Context) {
		if !c.Response().Committed {
			c.String(http.StatusTeapot, "handled: "+err.Error())
		}
	}
	e.Use(wlogecho.Middleware(log))
	e.GET("/fail", func(c echo.Context) error { return wantErr })

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
	if httpFields["status"] != http.StatusTeapot {
		t.Errorf("event http.status = %v, want %d (captured after HTTPErrorHandler wrote the real response)", httpFields["status"], http.StatusTeapot)
	}
}

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
