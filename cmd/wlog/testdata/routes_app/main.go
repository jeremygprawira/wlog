// Command routes_app is a fixture for the wlog map route tests: it registers through Echo,
// Gin, and mux groups, uses a constant route, and passes per-route middleware.
package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/mux"
	"github.com/jeremygprawira/wlog"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
	"github.com/labstack/echo/v4"
)

// healthPath is a constant route, which the map must resolve.
const healthPath = "/health"

func requireAuth(next echo.HandlerFunc) echo.HandlerFunc { return next }

func handleRole(c echo.Context) error {
	wlog.Set(c.Request().Context(), "role", c.Param("role"))
	return c.NoContent(http.StatusOK)
}

func handleOrder(c *gin.Context) {
	wlog.Set(c.Request.Context(), "order_id", c.Param("id"))
	c.Status(http.StatusOK)
}

func handleLogs(w http.ResponseWriter, r *http.Request) {
	wlog.Set(r.Context(), "scope", "logs")
	w.WriteHeader(http.StatusOK)
}

func main() {
	logger := wlog.New()

	// Echo: a group prefix, and per-route middleware AFTER the handler.
	e := echo.New()
	admin := e.Group("/admin")
	admin.DELETE("/users/:id/role", handleRole, requireAuth)

	// Gin: a group prefix, with middleware before the handler.
	g := gin.New()
	api := g.Group("/api")
	api.POST("/orders/:id", gin.Logger(), handleOrder)

	// mux: a subrouter prefix, a constant route, and two methods on one route.
	router := mux.NewRouter()
	adminRouter := router.PathPrefix("/admin").Subrouter()
	adminRouter.Methods(http.MethodDelete, http.MethodPost).Path("/logs").HandlerFunc(handleLogs)
	router.HandleFunc(healthPath, handleLogs)

	http.ListenAndServe(":8080", wlogstd.Middleware(logger)(router))
	_ = e
	_ = g
}
