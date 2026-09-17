// Command crosspkg is a fixture for the wlog map handler-discovery tests. Every handler it
// registers lives in another package, or arrives through a conversion, a factory, or a type
// with ServeHTTP, and it registers through net/http, gorilla/mux, Echo, and Gin.
package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/mux"
	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/cmd/wlog/testdata/crosspkg/handler"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
	"github.com/labstack/echo/v4"
)

func main() {
	logger := wlog.New()

	// net/http: a named handler in another package, and a conversion of one.
	std := http.NewServeMux()
	std.HandleFunc("GET /orders/{id}", handler.Orders)
	std.Handle("GET /audit", http.HandlerFunc(handler.Orders))

	// gorilla/mux: the route lives in the chain.
	router := mux.NewRouter()
	router.Methods("POST").Path("/refunds/{id}").HandlerFunc(handler.Service{}.Refund())
	router.Handle("/audit-mux", handler.AuditHandler{})

	// Echo: Match takes a list of methods, Add takes the method first. Both need the handler
	// wrapped, which is the conversion shape the finder must see through.
	e := echo.New()
	e.Match([]string{http.MethodGet, http.MethodHead}, "/echo/orders/{id}", echo.WrapHandler(http.HandlerFunc(handler.Orders)))
	e.Add(http.MethodPost, "/echo/refunds", echo.WrapHandler(handler.Service{}.Refund()))

	// Gin: Match takes a list of methods too.
	g := gin.New()
	g.Match([]string{http.MethodPost}, "/gin/orders", gin.WrapH(http.HandlerFunc(handler.Orders)))

	http.ListenAndServe(":8080", wlogstd.Middleware(logger)(std))
	_ = e
	_ = g
	_ = router
}
