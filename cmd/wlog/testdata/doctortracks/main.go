// Command doctortracks is a fixture for the wlog doctor track checks: it installs the gin
// adapter and calls a package-level logger inside a handler.
package main

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/jeremygprawira/wlog"
	wloggin "github.com/jeremygprawira/wlog/middleware/gin"
)

// handleOrders is a handler that calls a package-level logger, which lands outside the
// event.
func handleOrders(w http.ResponseWriter, r *http.Request) {
	slog.Info("an order arrived")
	w.WriteHeader(http.StatusOK)
}

func main() {
	engine := gin.New()
	engine.Use(wloggin.Middleware(wlog.Default()))
	http.HandleFunc("/orders", handleOrders)
	_ = http.ListenAndServe(":8080", nil)
}
