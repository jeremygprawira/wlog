// Command ignore_app is a fixture for the wlog map ignore tests.
package main

import (
	"fmt"
	"net/http"

	"github.com/jeremygprawira/wlog"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
)

//wlog:ignore logging.no_print -- the operator asked for this line in the log
func handleIgnored(w http.ResponseWriter, r *http.Request) {
	fmt.Println("debug")
	wlog.Set(r.Context(), "order_id", "1")
	w.WriteHeader(http.StatusOK)
}

//wlog:ignore logging.no_print
func handleBadIgnore(w http.ResponseWriter, r *http.Request) {
	fmt.Println("debug")
	wlog.Set(r.Context(), "order_id", "1")
	w.WriteHeader(http.StatusOK)
}

func main() {
	logger := wlog.New()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ignored", handleIgnored)
	mux.HandleFunc("GET /bad-ignore", handleBadIgnore)
	http.ListenAndServe(":8080", wlogstd.Middleware(logger)(mux))
}
