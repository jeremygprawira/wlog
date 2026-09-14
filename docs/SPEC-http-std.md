# Spec: http-std

> Module id `http-std` · package `github.com/jeremygprawira/wlog/middleware/nethttp` (`wlogstd`) ·
> root module · depends on: `core`. Project-wide rules in [SPEC.md](SPEC.md) apply. This spec
> also defines the **conformance contract** every later HTTP adapter (Echo, Gin, ...) must pass.

## Objective

One `net/http` middleware (and, via a route-name hook, gorilla/mux) that wraps a request in a
`wlog.Start`/`end`, captures everything by default, and is the reference implementation the
shared conformance suite (`internal/conformance`) is built from.

## Behaviour

```go
func Middleware(log *wlog.Logger, opts ...Option) func(http.Handler) http.Handler

func WithRouteFunc(fn func(*http.Request) string) Option // default: r.Pattern (Go 1.22+ ServeMux)
func WithUserFunc(fn func(*http.Request) string) Option  // default: none (http.user_id unset)
func SkipPaths(paths ...string) Option                   // exact match; no event at all for these
func CaptureHeaders(on bool) Option    // default true
func CaptureQuery(on bool) Option      // default true
func CaptureCookies(on bool) Option    // default true
func CaptureBody(on bool) Option       // default true, both directions
func MaxBodyCapture(n int) Option      // default 10*1024 bytes, per direction
func BodyContentTypes(types ...string) Option // default: application/json, text/*; others skipped
```

### Fields set (canonical, namespaced — see SPEC-core.md)

```
http.method http.route http.path http.status
http.duration_ms http.bytes_in http.bytes_out
http.client_ip http.user_agent
http.request_headers http.request_query http.request_params http.request_cookies http.request_body
http.response_headers http.response_body
trace.request_id (X-Request-ID: reused if present, else generated, always echoed on the response)
trace.trace_id trace.span_id (from a valid W3C traceparent header; unset if absent/invalid)
user.id (via WithUserFunc, if set)
```

`http.route` uses `r.Pattern` (populated by Go's `net/http.ServeMux` since 1.22) when non-empty,
else falls back to `r.URL.Path`; `WithRouteFunc` overrides for gorilla/mux
(`mux.CurrentRoute(r).GetPathTemplate()`) or any other router.

### Body capture

Both directions captured by default, capped at `MaxBodyCapture` bytes, only for content types
matching `BodyContentTypes` (default JSON + text); other types are skipped (not read, not
buffered) to avoid corrupting binary uploads/downloads. The request body is restored
(`io.NopCloser` over the buffered bytes) so the real handler still reads it normally. The
response writer wrapper preserves `http.Flusher`, `http.Hijacker`, and `io.ReaderFrom` when the
underlying writer supports them, and stops buffering past the cap without altering what is
written to the real client (gate G4).

### Panics

A panic in the wrapped handler is recovered, turned into a 500 response (if nothing was written
yet) with the panic captured via `wlog.Error` (using `runtime/debug.Stack()` as `ErrorInfo.Stack`
through a default extractor that recognizes a recovered panic value), and the event still emits.
The process keeps serving.

### Plugin hooks

Every plugin from `log.Plugins()` implementing `wlog.RequestStarter` runs (in registration
order) right after `wlog.Start`, before the handler; every one implementing
`wlog.RequestFinisher` runs right before `end()`, given `ctx` only — the final event map is
core-internal at this point, so a finisher observes side effects via `ctx` or its own state
(e.g. a metrics counter), not the rendered event. Use an `Enricher` to add or read fields on
the event itself.

## Success Criteria (also the conformance suite's contract)

1. `internal/conformance.Run(t, adapter)` exercises: 200 response with route/status/duration
   captured; a panic recovered into 500 with the event still emitted; request id reuse/echo;
   valid and invalid `traceparent`; `wlog.Set` from inside the handler visible on the event;
   `SkipPaths` producing no event; body capture on/off and content-type filtering; header/query/
   cookie capture including redaction of `Authorization`/`Cookie` via the default redactor (no
   special-casing in this module — proof that core's defaults already cover it).
2. `http-std` passes its own conformance suite.
3. A gorilla/mux example (in `examples/`) passes the same suite using `WithRouteFunc`.
4. Middleware + core overhead (excluding body size and drain network time) is ≤ 50µs p50 for a
   1KB JSON body, on an M-series Mac — SPEC.md's budget, measured at Checkpoint 2A.
5. Zero imports outside the standard library plus `core` (`net/http` is stdlib).

## Testing

`httptest.NewRecorder`/`httptest.NewServer`. Package `nethttp_test` for unit tests;
`internal/conformance` is importable by every adapter module's own tests.

## Boundaries

- **Always:** run every new HTTP adapter against `internal/conformance` before merging it.
- **Ask first:** changing default capture behavior (currently: everything on).
- **Never:** read a request body whose content type isn't in `BodyContentTypes`.

## Open Questions

None.
