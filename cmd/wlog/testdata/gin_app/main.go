// Command gin_app is a fixture for the wlog map tests. It uses Gin.
package main

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/jeremygprawira/wlog"
	wloggin "github.com/jeremygprawira/wlog/middleware/gin"
)

func main() {
	logger := wlog.New()
	r := gin.New()
	r.Use(wloggin.Middleware(logger))
	r.GET("/orders/:id", func(c *gin.Context) {
		wlog.Set(c.Request.Context(), "order_id", c.Param("id"))
		c.Status(http.StatusOK)
	})
	r.POST("/pay/:id", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	r.Run(":8080")
}
