// Command rules_app is a fixture for the wlog map rule tests. It holds one handler per
// rule outcome.
package main

import (
	"fmt"
	"net/http"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/audit"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
)

func handleGood(w http.ResponseWriter, r *http.Request) {
	wlog.Set(r.Context(), "order_id", "1")
	w.WriteHeader(http.StatusOK)
}

func handleNoContext(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func handlePrint(w http.ResponseWriter, r *http.Request) {
	fmt.Println("debug")
	wlog.Set(r.Context(), "order_id", "1")
	w.WriteHeader(http.StatusOK)
}

func handleDeniedKey(w http.ResponseWriter, r *http.Request) {
	wlog.Set(r.Context(), "password", "hunter2")
	w.WriteHeader(http.StatusOK)
}

func handleSensitive(w http.ResponseWriter, r *http.Request) {
	wlog.Set(r.Context(), "refund_id", "r1")
	w.WriteHeader(http.StatusOK)
}

func handleSensitiveAudited(w http.ResponseWriter, r *http.Request) {
	wlog.Set(r.Context(), "refund_id", "r1")
	audit.Do(r.Context(), audit.Record{Action: "refund.create", Outcome: "success"})
	w.WriteHeader(http.StatusOK)
}

func main() {
	logger := wlog.New()
	mux := http.NewServeMux()
	mux.HandleFunc("/good", handleGood)
	mux.HandleFunc("/nocontext", handleNoContext)
	mux.HandleFunc("/print", handlePrint)
	mux.HandleFunc("/keys", handleDeniedKey)
	mux.HandleFunc("/refund/{id}", handleSensitive)
	mux.HandleFunc("/refund/audit", handleSensitiveAudited)
	http.ListenAndServe(":8080", wlogstd.Middleware(logger)(mux))
}
