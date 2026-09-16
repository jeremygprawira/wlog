// Command app is an init fixture with an Echo v5 router.
package main

import (
	echo "github.com/labstack/echo/v5"
)

func main() {
	e := echo.New()
	e.GET("/", func(c *echo.Context) error { return c.NoContent(200) })
	e.Start(":8080")
}
