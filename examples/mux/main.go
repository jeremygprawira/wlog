// Command mux is a runnable example showing wlogstd.WithRouteFunc wired to
// gorilla/mux, proving http-std is framework-agnostic beyond net/http.ServeMux.
package main

import (
	"log"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/jeremygprawira/wlog"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
)

func routeFunc(r *http.Request) string {
	if route := mux.CurrentRoute(r); route != nil {
		if tmpl, err := route.GetPathTemplate(); err == nil {
			return tmpl
		}
	}
	return r.URL.Path
}

func main() {
	wlogger := wlog.New(wlog.WithService("mux-example", "0.0.1", "local"))

	r := mux.NewRouter()
	// Use registers the middleware inside the router, so the request the middleware
	// reads already carries the route that gorilla/mux matched.
	r.Use(wlogstd.Middleware(wlogger, wlogstd.WithRouteFunc(routeFunc)))
	r.HandleFunc("/orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		wlog.Set(r.Context(), "order_id", mux.Vars(r)["id"])
		w.WriteHeader(http.StatusOK)
	}).Methods(http.MethodGet)

	log.Fatal(http.ListenAndServe(":8080", r))
}
