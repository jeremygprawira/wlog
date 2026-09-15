// Package examples holds the parity test for the HTTP example apps. The apps
// themselves are small main packages under this directory; this test rebuilds the same
// route on each framework and checks every stack reports identical core fields.
package examples

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
	echo4 "github.com/labstack/echo/v4"
	echo5 "github.com/labstack/echo/v5"

	"github.com/jeremygprawira/wlog"
	wlogecho "github.com/jeremygprawira/wlog/middleware/echo"
	wlogecho5 "github.com/jeremygprawira/wlog/middleware/echo5"
	wloggin "github.com/jeremygprawira/wlog/middleware/gin"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// Each stack builds the same GET /parity route, sets one shared field, and returns 200.
// The route has no method or path parameter, so the route template matches on every
// framework and the events can be compared field for field.

func nethttpStack(log *wlog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/parity", func(w http.ResponseWriter, r *http.Request) {
		wlog.Set(r.Context(), "parity", "yes")
		w.WriteHeader(http.StatusOK)
	})
	return wlogstd.Middleware(log)(mux)
}

func echoStack(log *wlog.Logger) http.Handler {
	e := echo4.New()
	e.Use(wlogecho.Middleware(log))
	e.GET("/parity", func(c echo4.Context) error {
		wlog.Set(c.Request().Context(), "parity", "yes")
		return c.NoContent(http.StatusOK)
	})
	return e
}

func echo5Stack(log *wlog.Logger) http.Handler {
	e := echo5.New()
	e.Use(wlogecho5.Middleware(log))
	e.GET("/parity", func(c *echo5.Context) error {
		wlog.Set(c.Request().Context(), "parity", "yes")
		return c.NoContent(http.StatusOK)
	})
	return e
}

func ginStack(log *wlog.Logger) http.Handler {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(wloggin.Middleware(log))
	r.GET("/parity", func(c *gin.Context) {
		wlog.Set(c.Request.Context(), "parity", "yes")
		c.Status(http.StatusOK)
	})
	return r
}

// parityFields pulls the fields every stack must agree on, leaving out the ones that
// are legitimately per-request (timestamp, duration, request id).
func parityFields(event map[string]any) map[string]any {
	httpFields, _ := event["http"].(map[string]any)
	return map[string]any{
		"level":       event["level"],
		"operation":   event["operation"],
		"outcome":     event["outcome"],
		"parity":      event["parity"],
		"http.method": httpFields["method"],
		"http.route":  httpFields["route"],
		"http.status": httpFields["status"],
	}
}

// TestHTTPParity sends one request through each of the four HTTP stacks and fails if any
// event's core fields differ from the first stack's.
func TestHTTPParity(t *testing.T) {
	stacks := []struct {
		name  string
		build func(*wlog.Logger) http.Handler
	}{
		{"nethttp", nethttpStack},
		{"echo", echoStack},
		{"echo5", echo5Stack},
		{"gin", ginStack},
	}

	var reference map[string]any
	var referenceName string
	for _, stack := range stacks {
		log, rec := wlogtest.New(t)
		handler := stack.build(log)

		req := httptest.NewRequest(http.MethodGet, "/parity", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", stack.name, w.Code)
		}
		event := rec.Last()
		if event == nil {
			t.Fatalf("%s: no event recorded", stack.name)
		}
		fields := parityFields(event)

		if reference == nil {
			reference, referenceName = fields, stack.name
			continue
		}
		if !reflect.DeepEqual(fields, reference) {
			t.Errorf("%s fields = %v, want the same as %s: %v", stack.name, fields, referenceName, reference)
		}
	}
}
