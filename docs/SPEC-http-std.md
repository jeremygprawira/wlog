# Spec: http-std

> Module id `http-std` · package `github.com/jeremygprawira/wlog/middleware/nethttp` (`wlogstd`) ·
> root module · depends on: `core`. Project-wide rules in [SPEC.md](SPEC.md) apply. This spec
> also defines the **conformance contract** every later HTTP adapter (Echo, Gin, ...) must pass.

## Objective

One `net/http` middleware that wraps a request in a `wlog.Start`/`end`. It also covers
gorilla/mux through a route-name hook. It captures safe fields by default, and `CaptureAll()`
adds bodies and values. The shared conformance suite (`internal/conformance`) is built from it.

## Behaviour

<!-- snippet:sketch -->
```go
func Middleware(log *wlog.Logger, opts ...Option) func(http.Handler) http.Handler

func WithRouteFunc(fn func(*http.Request) string) Option // default: r.Pattern (Go 1.22+ ServeMux)
func WithUserFunc(fn func(*http.Request) string) Option  // default: none (http.user_id unset)
func SkipPaths(paths ...string) Option                   // exact match. No event at all for these
func CaptureHeaders(on bool) Option    // default true
func CaptureQuery(on bool) Option      // default true
func CaptureCookies(on bool) Option    // default true
func CaptureBody(on bool) Option       // default true, both directions
func MaxBodyCapture(n int) Option      // default 10*1024 bytes, per direction
func BodyContentTypes(types ...string) Option // default: application/json, text/*. Others skipped
```

### Fields set (canonical, namespaced, see SPEC-core.md)

```
http.method http.route http.path http.status
http.duration_ms http.bytes_in http.bytes_out
http.client_ip http.user_agent
http.request_headers http.request_query http.request_params http.request_cookies http.request_body
http.response_headers http.response_body
trace.request_id (X-Request-ID: reused if present, else generated, always echoed on the response)
trace.trace_id trace.span_id (from a valid W3C traceparent header. Unset if absent/invalid)
user.id (via WithUserFunc, if set)
```

When `r.Pattern` holds a value, `http.route` uses it. Go 1.22 and later fill that field in
`net/http.ServeMux`. Otherwise the route falls back to `r.URL.Path`. `WithRouteFunc` overrides both for gorilla/mux
(`mux.CurrentRoute(r).GetPathTemplate()`) or any other router.

### Body capture

Both directions captured by default, capped at `MaxBodyCapture` bytes, only for content types
matching `BodyContentTypes` (default JSON + text). Other types are skipped (not read, not
buffered) to avoid corrupting binary uploads/downloads. The request body is restored
(`io.NopCloser` over the buffered bytes) so the real handler still reads it normally. The
response writer wrapper keeps `http.Flusher`, `http.Hijacker`, and `io.ReaderFrom` for a writer
that supports them. It stops buffering past the cap, and it never alters what the real client
receives (gate G4).

### Panics

A panic in the wrapped handler is recovered. The panic is captured through `wlog.Error`, and the
default extractor recognizes a recovered panic value. It uses `runtime/debug.Stack()` as
`ErrorInfo.Stack`. The response is a 500, unless the handler already wrote a response. The event
still emits, and the process keeps serving.

### Plugin hooks

Core calls the `Starter` hook of every plugin at `wlog.Start`, and its `Finisher` hook
after finalize. http-std calls neither one. A `Finisher` reads the read-only event, so it
sees the summary, the outcome, and the redacted fields. A `Starter` returns the context
the request continues with, so a logger bridge binds a per-request logger.

## Success Criteria (also the conformance suite's contract)

1. `internal/conformance.Run(t, adapter)` exercises: 200 response with route/status/duration
   captured. A panic recovered into 500 with the event still emitted. Request id reuse/echo. A valid and an invalid `traceparent`. `wlog.Set` from inside the handler is visible on the
   event.
   `SkipPaths` producing no event. Body capture on/off and content-type filtering. Header/query/
   cookie capture including redaction of `Authorization`/`Cookie` via the default redactor (no
   special-casing in this module, proof that core's defaults already cover it).
2. `http-std` passes its own conformance suite.
3. A gorilla/mux example (in `examples/`) passes the same suite using `WithRouteFunc`.
4. Middleware + core overhead (excluding body size and drain network time) is ≤ 55µs p50 for a
   1KB JSON body, on an M-series Mac, SPEC.md's budget, measured at Checkpoint 2A.
5. Zero imports outside the standard library plus `core` (`net/http` is stdlib).

## Testing

`httptest.NewRecorder` and `httptest.NewServer`. Unit tests live in package `nethttp_test`.
`internal/conformance` is importable by every adapter module's own tests.

## Boundaries

- **Always:** run every new HTTP adapter against `internal/conformance` before merging it.
- **Ask first:** changing default capture behavior (currently: everything on).
- **Never:** read a request body whose content type is not in `BodyContentTypes`.

## Open Questions

None.
