// Command nocatalog is the quiet twin of suggestions: the same literal code, but no
// registry holds it.
package main

import (
	"errors"
	"net/http"

	"github.com/jeremygprawira/wlog"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
)

func handleLiteral(w http.ResponseWriter, r *http.Request) {
	err := errors.New("APP_NOT_FOUND")
	wlog.Error(r.Context(), err)
	w.WriteHeader(http.StatusOK)
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/orders", handleLiteral)
	http.ListenAndServe(":8080", wlogstd.Middleware(wlog.New())(mux))
}
