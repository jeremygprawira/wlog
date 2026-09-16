// Command errguidance is a fixture for the wlog map rule tests. One handler reports an
// error with no guidance, one reports a catalog error, and one reports nothing.
package main

import (
	"errors"
	"net/http"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/catalog"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
)

var registry = catalog.New("app", catalog.Entry{
	Code: "not_found", Why: "no row has that id", Fix: "check the id and retry",
})

func handleBad(w http.ResponseWriter, r *http.Request) {
	err := errors.New("no row")
	wlog.Error(r.Context(), err)
}

func handleGood(w http.ResponseWriter, r *http.Request) {
	err := registry.Err("not_found")
	wlog.Error(r.Context(), err)
}

func handleNone(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func main() {
	logger := wlog.New(wlog.WithErrorExtractor(catalog.Extractor(wlog.DefaultExtractor(), registry)))
	mux := http.NewServeMux()
	mux.HandleFunc("/bad", handleBad)
	mux.HandleFunc("/good", handleGood)
	mux.HandleFunc("/none", handleNone)
	http.ListenAndServe(":8080", wlogstd.Middleware(logger)(mux))
}
