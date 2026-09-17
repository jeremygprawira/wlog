// Command config_app is a fixture for the wlog map config tests: it has one handler per rule
// outcome, so its score sits below 100 and a gate can bite.
package main

import (
	"net/http"

	"github.com/jeremygprawira/wlog"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
)

func handlePlain(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func main() {
	logger := wlog.New()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /orders/{id}", handlePlain)
	http.ListenAndServe(":8080", wlogstd.Middleware(logger)(mux))
}
