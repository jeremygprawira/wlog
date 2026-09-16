// Command echo_err_app is a fixture for the wlog map rule tests. One handler reports
// its error through wlog and one does not.
package main

import (
	"errors"

	"github.com/labstack/echo/v4"

	"github.com/jeremygprawira/wlog"
	wlogecho "github.com/jeremygprawira/wlog/middleware/echo"
)

func handleOK(c echo.Context) error {
	if err := run(); err != nil {
		wlog.Error(c.Request().Context(), err)
		return err
	}
	return nil
}

func handleFail(c echo.Context) error {
	return errors.New("boom")
}

func run() error { return nil }

func main() {
	e := echo.New()
	e.Use(wlogecho.Middleware(wlog.New()))
	e.GET("/ok", handleOK)
	e.GET("/fail", handleFail)
	e.Start(":8080")
}
