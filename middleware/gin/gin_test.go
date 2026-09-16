package wloggin_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	wloggin "github.com/jeremygprawira/wlog/middleware/gin"
	"github.com/jeremygprawira/wlog/wlogtest"
)

type ginAdapter struct{}

func (ginAdapter) Build(log *wlog.Logger, routes conformance.Routes) http.Handler {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(wloggin.Middleware(log, wloggin.SkipPaths("/skip")))
	r.GET("/ok", func(c *gin.Context) { routes.OK(c.Writer, c.Request) })
	r.GET("/panic", func(c *gin.Context) { routes.Panic(c.Writer, c.Request) })
	r.POST("/echo", func(c *gin.Context) { routes.Echo(c.Writer, c.Request) })
	r.GET("/skip", func(c *gin.Context) { routes.OK(c.Writer, c.Request) })
	return r
}

func TestConformance(t *testing.T) {
	conformance.Run(t, ginAdapter{})
}

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
	httpFields := rec.Last()["http"].(map[string]any)
	if got := httpFields["status"]; got != int64(http.StatusTeapot) && got != http.StatusTeapot {
		t.Errorf("event http.status = %v (%T), want %d", got, got, http.StatusTeapot)
	}
}

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

	httpFields := rec.Last()["http"].(map[string]any)
	if httpFields["route"] != "/users/:id" {
		t.Errorf("http.route = %v, want /users/:id", httpFields["route"])
	}
}
