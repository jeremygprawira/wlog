package wlogstd_test

import (
	"net/http"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
)

// wlogstdAdapter implements conformance.Adapter for the net/http middleware — the
// reference implementation the conformance suite is built from.
type wlogstdAdapter struct{}

func (wlogstdAdapter) Build(log *wlog.Logger, routes conformance.Routes) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ok", routes.OK)
	mux.HandleFunc("GET /panic", routes.Panic)
	mux.HandleFunc("POST /echo", routes.Echo)
	mux.HandleFunc("GET /skip", routes.OK)
	return wlogstd.Middleware(log, wlogstd.SkipPaths("/skip"))(mux)
}

func TestConformance(t *testing.T) {
	conformance.Run(t, wlogstdAdapter{})
}
