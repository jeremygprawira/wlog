// Command suggestions is a fixture for the wlog map suggestion rules. It declares a
// catalog and holds one handler with a literal catalog code and one write route with no
// audit record.
package main

import (
	"errors"
	"net/http"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/catalog"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
)

var registry = catalog.New("app",
	catalog.Entry{Code: "not_found"},
	catalog.Entry{Code: "conflict"},
)

func handleLiteral(w http.ResponseWriter, r *http.Request) {
	err := errors.New("APP_NOT_FOUND")
	wlog.Error(r.Context(), err)
	w.WriteHeader(http.StatusOK)
}

func handleOtherLiteral(w http.ResponseWriter, r *http.Request) {
	err := errors.New("SOMETHING_ELSE")
	wlog.Error(r.Context(), err)
	w.WriteHeader(http.StatusOK)
}

func handleWriteNoAudit(w http.ResponseWriter, r *http.Request) {
	wlog.Set(r.Context(), "order_id", "1")
	w.WriteHeader(http.StatusCreated)
}

func handleReadNoAudit(w http.ResponseWriter, r *http.Request) {
	wlog.Set(r.Context(), "order_id", "1")
	w.WriteHeader(http.StatusOK)
}

func main() {
	logger := wlog.New()
	mux := http.NewServeMux()
	mux.HandleFunc("/orders", handleLiteral)
	mux.HandleFunc("/orders/other", handleOtherLiteral)
	mux.HandleFunc("POST /orders/create", handleWriteNoAudit)
	mux.HandleFunc("GET /orders/read", handleReadNoAudit)
	http.ListenAndServe(":8080", wlogstd.Middleware(logger)(mux))
}
