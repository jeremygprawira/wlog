// Command nomiddleware_app is a fixture for the wlog map rule tests. It never installs
// wlog middleware, so the coverage rule fails.
package main

import (
	"net/http"

	"github.com/jeremygprawira/wlog"
)

func handle(w http.ResponseWriter, r *http.Request) {
	wlog.Set(r.Context(), "order_id", "1")
	w.WriteHeader(http.StatusOK)
}

func main() {
	http.HandleFunc("/orders", handle)
	http.ListenAndServe(":8080", nil)
}
