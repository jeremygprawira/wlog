// Command strictkeys_app is a fixture for the keys.strict rule. One handler spells a field
// two ways, and one uses the declared key and an unrelated one.
package main

import (
	"net/http"

	"github.com/jeremygprawira/wlog"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
)

// OrderID is the declared spelling of the field.
var OrderID = wlog.NewKey[string]("order_id")

func nearMiss(w http.ResponseWriter, r *http.Request) {
	wlog.Set(r.Context(), "orderID", r.PathValue("id"))
	w.WriteHeader(http.StatusOK)
}

func exact(w http.ResponseWriter, r *http.Request) {
	wlog.Set(r.Context(), "order_id", r.PathValue("id"))
	wlog.Set(r.Context(), "tenant", "acme")
	w.WriteHeader(http.StatusOK)
}

func main() {
	logger := wlog.New()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders/{id}", nearMiss)
	mux.HandleFunc("GET /orders/{id}", exact)
	http.ListenAndServe(":8080", wlogstd.Middleware(logger)(mux))
}
