package main

import (
	"net/http"
	"testing"

	"github.com/gorilla/mux"
	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
)

// muxAdapter implements conformance.Adapter for gorilla/mux, proving http-std is
// framework-agnostic: only WithRouteFunc changes, nothing else about the middleware.
type muxAdapter struct{}

func (muxAdapter) Build(log *wlog.Logger, routes conformance.Routes) http.Handler {
	r := mux.NewRouter()
	r.HandleFunc("/ok", routes.OK).Methods(http.MethodGet)
	r.HandleFunc("/panic", routes.Panic).Methods(http.MethodGet)
	r.HandleFunc("/echo", routes.Echo).Methods(http.MethodPost)
	r.HandleFunc("/skip", routes.OK).Methods(http.MethodGet)
	return wlogstd.Middleware(log, wlogstd.WithRouteFunc(routeFunc), wlogstd.SkipPaths("/skip"))(r)
}

func TestConformance(t *testing.T) {
	conformance.Run(t, muxAdapter{})
}
