// This file holds the net/http adapter. It covers net/http and every router built on it,
// and it reads the route template after the handler returns, when the router has set it.
package httpcore

import (
	"io"
	"net/http"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/propagate"
)

// NetHTTP wraps next so every request becomes one wlog request event. Build the returned
// middleware once, at startup.
func NetHTTP(log *wlog.Logger, opts ...Option) func(http.Handler) http.Handler {
	core := New(log, opts...)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			view := netHTTPRequest{r}
			if core.Skip(view) {
				next.ServeHTTP(w, r)
				return
			}
			ctx, x := core.Start(r.Context(), view)
			if core.cfg.echoRequestID {
				if trace, ok := propagate.FromContext(ctx); ok && trace.RequestID != "" {
					w.Header().Set("X-Request-ID", trace.RequestID)
				}
			}
			// The body is read before the handler, and the reader hands every byte back,
			// so the handler still reads the whole body.
			if x.owned && core.CapturesBody(view) {
				body, rest, truncated := core.ReadBody(r.Body)
				if rest != nil {
					r.Body = io.NopCloser(rest)
				}
				x.RequestBody(body, truncated, false)
				defer core.ReturnBody(body)
			}

			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			// req, not r: net/http.ServeMux sets Pattern on the request it actually
			// dispatches, which WithContext made a shallow copy of, so reading the
			// pattern off the original r would always see the zero value.
			req := r.WithContext(ctx)
			next.ServeHTTP(sw, req)

			template, matched := core.cfg.route(req)
			x.Route(template, matched)
			x.End(netHTTPResponse{sw}, nil)
		})
	}
}

// statusWriter observes the status and the body size of one response, and forwards every
// call to the writer the app gave.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
	wrote  bool
}

// WriteHeader records the first status the handler writes, and forwards it. A second
// call is dropped, the same as net/http.
func (w *statusWriter) WriteHeader(code int) {
	if w.wrote {
		return
	}
	w.wrote = true
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Write records the size of the body, and forwards the bytes.
func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}
