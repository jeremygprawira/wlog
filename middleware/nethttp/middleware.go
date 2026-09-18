// Package wlogstd is wlog's net/http (and, via WithRouteFunc, gorilla/mux) middleware:
// one Start/end per request, with method/route/path/status/duration/bytes and a
// request id captured by default.
//
// Read top to bottom: Middleware wraps a handler; config.go holds its Options;
// writer.go is the response-writer wrapper that observes status and byte count;
// recover.go and trace.go hold the panic and W3C traceparent handling.
package wlogstd

import (
	"context"
	"net/http"

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

			for _, p := range log.Plugins() {
				if rs, ok := p.(wlog.RequestStarter); ok {
					ctx = rs.OnRequestStart(ctx)
				}
			}

			requestID := r.Header.Get("X-Request-ID")
			if requestID == "" {
				requestID = generateRequestID()
			}
			w.Header().Set("X-Request-ID", requestID)
			wlog.SetGroup(ctx, "trace", "request_id", requestID)
			if traceID, spanID, ok := parseTraceparent(r.Header.Get("traceparent")); ok {
				wlog.SetGroup(ctx, "trace", "trace_id", traceID, "span_id", spanID)
			}

			if cfg.userFunc != nil {
				if uid := cfg.userFunc(r); uid != "" {
					wlog.SetGroup(ctx, "user", "id", uid)
				}
			}

			if cfg.captureHeaders {
				wlog.SetGroup(ctx, "http", "request_headers", captureHeaders(r.Header))
			}
			if cfg.captureQuery {
				wlog.SetGroup(ctx, "http", "request_query", captureQuery(r))
			}
			if cfg.captureCookies {
				wlog.SetGroup(ctx, "http", "request_cookies", captureCookies(r))
			}
			if cfg.captureBody {
				if body := captureRequestBody(r, cfg.maxBodyCapture, cfg.bodyContentTypes); body != nil {
					wlog.SetGroup(ctx, "http", "request_body", body)
				}
			}

			sw := &statusWriter{
				ResponseWriter: w, status: http.StatusOK,
				captureBody: cfg.captureBody, allowedTypes: cfg.bodyContentTypes, maxCapture: cfg.maxBodyCapture,
			}
			// req, not r: net/http.ServeMux sets Pattern on the *Request it actually
			// dispatches to, which WithContext made a shallow copy of — reading
			// route/path off the original r would always see the zero value.
			req := r.WithContext(ctx)

			// Registered in this order so they unwind in the opposite one:
			// recoverPanic runs first (turns a panic into a 500 + logged error, so
			// sw.status/route/etc below are still correct for a panicking request),
			// then the http.* fields, then the RequestFinisher plugins, then end()
			// emits the event last.
			defer end()
			defer runRequestFinishers(ctx, log)
			defer func() {
				fields := []any{
					"method", r.Method,
					"route", cfg.route(req),
					"path", req.URL.Path,
					"status", sw.status,
					"bytes_out", sw.bytes,
					"client_ip", clientIP(r),
					"user_agent", r.UserAgent(),
				}
				if r.ContentLength >= 0 {
					fields = append(fields, "bytes_in", r.ContentLength)
				}
				wlog.SetGroup(ctx, "http", fields...)
				if body := sw.body(); body != nil {
					wlog.SetGroup(ctx, "http", "response_body", body)
				}
			}()
			defer recoverPanic(ctx, sw)

			next.ServeHTTP(sw, req)
		})
	}
}

func runRequestFinishers(ctx context.Context, log *wlog.Logger) {
	for _, p := range log.Plugins() {
		if rf, ok := p.(wlog.RequestFinisher); ok {
			rf.OnRequestFinish(ctx)
		}
	}
}
