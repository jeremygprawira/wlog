// Command na_app is a fixture for the wlog map n/a test: its handler has no error path at all,
// so the three error rules do not apply to it.
package main

import (
	"net/http"

	"github.com/jeremygprawira/wlog"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
)

func handlePlain(w http.ResponseWriter, r *http.Request) {
	wlog.Set(r.Context(), "order_id", r.PathValue("id"))
	w.WriteHeader(http.StatusOK)
}

func main() {
	logger := wlog.New()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /orders/{id}", handlePlain)
	http.ListenAndServe(":8080", wlogstd.Middleware(logger)(mux))
}
