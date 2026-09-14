package wlogstd

import "net/http"

// Option configures Middleware.
type Option func(*config)

type config struct {
	routeFunc        func(*http.Request) string
	captureHeaders   bool
	captureQuery     bool
	captureCookies   bool
	captureBody      bool
	maxBodyCapture   int
	bodyContentTypes []string
	skipPaths        map[string]bool
}

func newConfig(opts []Option) *config {
	c := &config{
		routeFunc:        defaultRoute,
		captureHeaders:   true,
		captureQuery:     true,
		captureCookies:   true,
		captureBody:      true,
		maxBodyCapture:   defaultMaxBodyCapture,
		bodyContentTypes: defaultBodyContentTypes,
		skipPaths:        map[string]bool{},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// CaptureBody toggles capturing both the request and response body. Default true.
func CaptureBody(on bool) Option { return func(c *config) { c.captureBody = on } }

// MaxBodyCapture caps how many bytes of each body direction are logged. The handler
// still receives the complete, untruncated request body regardless of this cap.
// Default 10KB.
func MaxBodyCapture(n int) Option { return func(c *config) { c.maxBodyCapture = n } }

// BodyContentTypes sets which Content-Types are captured; anything else is skipped
// entirely (never read or buffered), so a binary upload/download is never corrupted
// or needlessly held in memory. An entry ending in "/" matches a whole top-level type
// (e.g. "text/"). Default: "application/json", "text/".
func BodyContentTypes(types ...string) Option {
	return func(c *config) { c.bodyContentTypes = types }
}

// CaptureHeaders toggles capturing request headers under http.request_headers.
// Default true.
func CaptureHeaders(on bool) Option { return func(c *config) { c.captureHeaders = on } }

// CaptureQuery toggles capturing the query string under http.request_query.
// Default true.
func CaptureQuery(on bool) Option { return func(c *config) { c.captureQuery = on } }

// CaptureCookies toggles capturing cookies under http.request_cookies. Default true.
func CaptureCookies(on bool) Option { return func(c *config) { c.captureCookies = on } }

// SkipPaths excludes exact paths from logging entirely — no event is emitted at all
// (the handler still runs). Typical use: health checks.
func SkipPaths(paths ...string) Option {
	return func(c *config) {
		for _, p := range paths {
			c.skipPaths[p] = true
		}
	}
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
