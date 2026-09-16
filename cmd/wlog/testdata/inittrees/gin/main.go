// Command app is an init fixture with a Gin router.
package main

import (
	"github.com/gin-gonic/gin"
)

func main() {
	r := gin.New()
	r.GET("/", func(c *gin.Context) { c.Status(200) })
	r.Run(":8080")
}
