// Command echo is a runnable example of wlog with Echo v4.
package main

import (
	"log"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/jeremygprawira/wlog"
	wlogecho "github.com/jeremygprawira/wlog/middleware/echo"
)

func main() {
	wlogger := wlog.New(wlog.WithService("echo-example", "0.0.1", "local"))
	e := echo.New()
	e.Use(wlogecho.Middleware(wlogger))
	e.GET("/orders/:id", func(c echo.Context) error {
		wlog.Set(c.Request().Context(), "order_id", c.Param("id"))
		return c.NoContent(http.StatusOK)
	})
	log.Fatal(e.Start(":8080"))
}
