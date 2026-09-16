// Command mux_app is a fixture for the wlog map tests. It uses gorilla/mux and chains
// .Methods onto the registration.
package main

import (
	"net/http"

	"github.com/gorilla/mux"

	"github.com/jeremygprawira/wlog"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
)

func main() {
	logger := wlog.New()
	router := mux.NewRouter()
	router.HandleFunc("/orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		wlog.Set(r.Context(), "order_id", mux.Vars(r)["id"])
		w.WriteHeader(http.StatusOK)
	}).Methods("GET")
	http.ListenAndServe(":8080", wlogstd.Middleware(logger)(router))
}
