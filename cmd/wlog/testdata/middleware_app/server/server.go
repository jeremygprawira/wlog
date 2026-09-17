// Package server builds the router, in its own package, the way a real service does.
package server

import (
	"net/http"

	"github.com/jeremygprawira/wlog"
)

// Router returns the app's routes.
func Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /orders/{id}", handleOrder)
	return mux
}

func handleOrder(w http.ResponseWriter, r *http.Request) {
	wlog.Set(r.Context(), "order_id", r.PathValue("id"))
	w.WriteHeader(http.StatusOK)
}
