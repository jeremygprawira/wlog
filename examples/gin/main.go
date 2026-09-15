// Command gin is a runnable example of wlog with Gin.
package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/jeremygprawira/wlog"
	wloggin "github.com/jeremygprawira/wlog/middleware/gin"
)

func main() {
	wlogger := wlog.New(wlog.WithService("gin-example", "0.0.1", "local"))
	r := gin.New()
	r.Use(wloggin.Middleware(wlogger))
	r.GET("/orders/:id", func(c *gin.Context) {
		wlog.Set(c.Request.Context(), "order_id", c.Param("id"))
		c.Status(http.StatusOK)
	})
	log.Fatal(r.Run(":8080"))
}
