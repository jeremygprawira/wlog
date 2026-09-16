// Command echo5_app is a fixture for the wlog map tests. It uses Echo v5.
package main

import (
	"net/http"

	echo "github.com/labstack/echo/v5"

	"github.com/jeremygprawira/wlog"
	wlogecho5 "github.com/jeremygprawira/wlog/middleware/echo5"
)

func main() {
	logger := wlog.New()
	e := echo.New()
	e.Use(wlogecho5.Middleware(logger))
	e.GET("/orders/:id", func(c *echo.Context) error {
		wlog.Set(c.Request().Context(), "order_id", c.Param("id"))
		return c.NoContent(http.StatusOK)
	})
	e.POST("/pay/:id", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
	e.Start(":8080")
}
