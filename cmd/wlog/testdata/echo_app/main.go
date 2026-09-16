// Command echo_app is a fixture for the wlog map tests. It uses Echo v4.
package main

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/jeremygprawira/wlog"
	wlogecho "github.com/jeremygprawira/wlog/middleware/echo"
)

func main() {
	logger := wlog.New()
	e := echo.New()
	e.Use(wlogecho.Middleware(logger))
	e.GET("/orders/:id", func(c echo.Context) error {
		wlog.Set(c.Request().Context(), "order_id", c.Param("id"))
		return c.NoContent(http.StatusOK)
	})
	e.POST("/pay/:id", func(c echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
	e.Start(":8080")
}
