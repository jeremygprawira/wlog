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

			// The response body is captured only when the policy asks for it, and only
			// up to the cap, so a large download is never held.
			var capture []byte
			if core.cfg.captureBody && r.Method != http.MethodHead {
				capture = core.pool.get()[:0]
				defer core.ReturnBody(capture)
			}
			sw := newStatusWriter(w, capture, core.cfg.maxBody)

			// The request body is read before the handler, and the reader hands every
			// byte back, so the handler still reads the whole body.
			if x.owned && core.CapturesBody(view) {
				body, rest, truncated := core.ReadBody(r.Body)
				if rest != nil {
					r.Body = io.NopCloser(rest)
				}
				x.RequestBody(body, truncated, false)
				defer core.ReturnBody(body)
			}

			// req, not r: net/http.ServeMux sets Pattern on the request it actually
			// dispatches, which WithContext made a shallow copy of, so reading the
			// pattern off the original r would always see the zero value.
			req := r.WithContext(ctx)
			again := core.runHandler(x, sw, req, next)

			template, matched := core.cfg.route(req)
			x.Route(template, matched)
			if body, truncated := sw.captured(); body != nil {
				x.ResponseBody(body, truncated)
			}
			x.End(netHTTPResponse{sw}, nil)

			// The event is out, so a panic that net/http reads continues now.
			if again != nil {
				panic(again)
			}
		})
	}
}
