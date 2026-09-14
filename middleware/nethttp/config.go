package wlogstd

import "net/http"

// Option configures Middleware.
type Option func(*config)

type config struct {
	routeFunc func(*http.Request) string
}

func newConfig(opts []Option) *config {
	c := &config{routeFunc: defaultRoute}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *config) route(r *http.Request) string {
	return c.routeFunc(r)
}

// defaultRoute uses r.Pattern (set by net/http.ServeMux since Go 1.22) when a request
// was matched against a registered pattern, else falls back to the raw path.
func defaultRoute(r *http.Request) string {
	if r.Pattern != "" {
		return r.Pattern
	}
	return r.URL.Path
}

// WithRouteFunc overrides how the route name is derived — needed for any router
// other than net/http.ServeMux, e.g. gorilla/mux:
//
//	wlogstd.WithRouteFunc(func(r *http.Request) string {
//		if route := mux.CurrentRoute(r); route != nil {
//			if tmpl, err := route.GetPathTemplate(); err == nil {
//				return tmpl
//			}
//		}
//		return r.URL.Path
//	})
func WithRouteFunc(fn func(*http.Request) string) Option {
	return func(c *config) { c.routeFunc = fn }
}
