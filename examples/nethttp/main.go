// Command nethttp is a runnable example of wlog with net/http's own ServeMux.
package main

import (
	"log"
	"net/http"

	"github.com/jeremygprawira/wlog"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
)

func main() {
	wlogger := wlog.New(wlog.WithService("nethttp-example", "0.0.1", "local"))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		wlog.Set(r.Context(), "order_id", r.PathValue("id"))
		w.WriteHeader(http.StatusOK)
	})
	log.Fatal(http.ListenAndServe(":8080", wlogstd.Middleware(wlogger)(mux)))
}
