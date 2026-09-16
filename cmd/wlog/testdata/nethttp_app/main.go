// Command nethttp_app is a fixture for the wlog map tests. It holds one named handler
// and one function literal.
package main

import (
	"net/http"

	"github.com/jeremygprawira/wlog"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
)

func handleOrders(w http.ResponseWriter, r *http.Request) {
	wlog.Set(r.Context(), "order_id", r.PathValue("id"))
	w.WriteHeader(http.StatusOK)
}

func main() {
	logger := wlog.New()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /orders/{id}", handleOrders)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	http.ListenAndServe(":8080", wlogstd.Middleware(logger)(mux))
}
