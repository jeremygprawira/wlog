// Command echo5 is a runnable example of wlog with Echo v5.
package main

import (
	"log"
	"net/http"

	echo "github.com/labstack/echo/v5"

	"github.com/jeremygprawira/wlog"
	wlogecho5 "github.com/jeremygprawira/wlog/middleware/echo5"
)

func main() {
	wlogger := wlog.New(wlog.WithService("echo5-example", "0.0.1", "local"))
	e := echo.New()
	e.Use(wlogecho5.Middleware(wlogger))
	e.GET("/orders/:id", func(c *echo.Context) error {
		wlog.Set(c.Request().Context(), "order_id", c.Param("id"))
		return c.NoContent(http.StatusOK)
	})
	log.Fatal(e.Start(":8080"))
}
