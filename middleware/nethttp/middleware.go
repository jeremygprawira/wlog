// Package wlogstd is wlog's net/http (and, via WithRouteFunc, gorilla/mux) middleware:
// one Start/end per request, with method/route/path/status/duration/bytes and a
// request id captured by default.
//
// Read top to bottom: Middleware wraps a handler; config.go holds its Options;
// writer.go is the response-writer wrapper that observes status and byte count.
package wlogstd

import (
	"net/http"
	"time"

	"github.com/jeremygprawira/wlog"
)

// Middleware wraps next so every request becomes one wlog event under "http.request".
func Middleware(log *wlog.Logger, opts ...Option) func(http.Handler) http.Handler {
	cfg := newConfig(opts)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.skipPaths[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}

			ctx := log.WithContext(r.Context())
			ctx, end := wlog.Start(ctx, "http.request")
			defer end()

			requestID := r.Header.Get("X-Request-ID")
			if requestID == "" {
				requestID = generateRequestID()
			}
			w.Header().Set("X-Request-ID", requestID)
			wlog.SetGroup(ctx, "trace", "request_id", requestID)

			if cfg.captureHeaders {
				wlog.SetGroup(ctx, "http", "request_headers", captureHeaders(r.Header))
			}
			if cfg.captureQuery {
				wlog.SetGroup(ctx, "http", "request_query", captureQuery(r))
			}
			if cfg.captureCookies {
				wlog.SetGroup(ctx, "http", "request_cookies", captureCookies(r))
			}

			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()
			// req, not r: net/http.ServeMux sets Pattern on the *Request it actually
			// dispatches to, which WithContext made a shallow copy of — reading
			// route/path off the original r would always see the zero value.
			req := r.WithContext(ctx)
			next.ServeHTTP(sw, req)

			wlog.SetGroup(ctx, "http",
				"method", r.Method,
				"route", cfg.route(req),
				"path", req.URL.Path,
				"status", sw.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"bytes_out", sw.bytes,
			)
		})
	}
}
