// Command doctorbad is a fixture for the wlog doctor tests. It has no middleware, builds
// the Axiom drain with no credentials, and disables the redactor.
package main

import (
	"context"
	"net/http"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/drain/axiom"
	"github.com/jeremygprawira/wlog/pipeline"
	"github.com/jeremygprawira/wlog/redact"
)

func handleOrders(w http.ResponseWriter, r *http.Request) {
	wlog.Set(r.Context(), "order_id", "1")
	w.WriteHeader(http.StatusOK)
}

func main() {
	drain := pipeline.Wrap(axiom.MustNew())
	log := wlog.New(wlog.WithRedactor(redact.Disabled()), wlog.WithDrains(drain))
	mux := http.NewServeMux()
	mux.HandleFunc("/orders", handleOrders)
	http.ListenAndServe(":8080", mux)
	_ = log.Close(context.Background())
}
