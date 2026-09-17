# Spec: http-core

> Phase 11 · root module · depends on: `core-shape`, `work`, `propagate`. Module ids: `http-core`
> in package `wlog/middleware/httpcore`, and the rebuilt `http-std` (`wlogstd`), `http-echo`,
> `http-echo5`, and `http-gin`. Project-wide rules in [SPEC.md](SPEC.md) apply. It replaces
> [SPEC-http-std.md](SPEC-http-std.md) and [SPEC-http-adapters.md](SPEC-http-adapters.md). Facts
> about each framework come from the API research of 2026-09-16, checked against source.

## Objective

Every HTTP stack emits the same request event: net/http, Echo, Gin, and every router in track A.
One package owns capture, redaction input, levels, operation names, trace context, and panics. An
adapter only passes what its framework knows, at the moment it knows it.

## Framework-neutral view

<!-- snippet:sketch -->
```go
type Request interface {
	Method() string
	Path() string
	Proto() string
	Header(name string) string                 // first value, any letter case
	EachHeader(fn func(name, value string))
	EachQuery(fn func(key, value string))
	EachCookie(fn func(name, value string))
	RemoteAddr() string                        // the connection's ip:port
	ContentLength() int64                      // -1 when unknown
}

type Response interface {
	Status() int
	BytesWritten() int64 // -1 when unknown
	EachHeader(fn func(name, value string))
}
```

A view must copy every string and byte slice it returns. A pooled framework context (Gin, Echo,
Fiber, fasthttp, Hertz) reuses its buffers after the handler returns.

## Exchange API

<!-- snippet:sketch -->
```go
func New(log *wlog.Logger, opts ...Option) *Core // built once per middleware
func (c *Core) Skip(r Request) bool

func (c *Core) Start(ctx context.Context, r Request) (context.Context, *Exchange)
func (x *Exchange) Route(template string, matched bool)   // any time before End
func (x *Exchange) OperationID(id string)                 // huma, kratos
func (x *Exchange) RequestBody(body []byte, truncated bool, decoded bool)
func (x *Exchange) ResponseBody(body []byte, truncated bool)
func (x *Exchange) Panic(value any, stack []byte)
func (x *Exchange) Hijacked()                             // records status 101
func (x *Exchange) End(resp Response, err error)          // err is the framework error, may be nil

func NetHTTP(log *wlog.Logger, opts ...Option) func(http.Handler) http.Handler
```

- `Start` extracts trace context and the request id through `propagate`, starts a `request` event,
  runs `Starter` plugins, and captures the request fields the policy allows.
- A context that already holds an open request event from the same Logger starts no new event.
  That covers huma autopatch sub-requests and a router mounted inside another wrapped router.
  The inner handler's fields land on the outer event.
- `End` records the framework error through `wlog.Error` with its status in `ErrorInfo.Status`.
  It picks the level by the SPEC.md rule, sets `operation` and `http.route`, runs `Finisher`
  plugins, and emits.
- `NetHTTP` is the adapter for net/http and every router built on it. It reads the route after
  `next` returns, through `RouteFunc`, `r.Pattern`, or nothing.

## Fields

| Field | Default capture | `CaptureAll()` |
|---|---|---|
| `http.method`, `http.route`, `http.path`, `http.status`, `http.protocol`, `http.scheme`, `http.host` | yes | yes |
| `http.bytes_in`, `http.bytes_out`, `http.client_ip`, `http.user_agent` | yes | yes |
| `http.request_headers` | allow-list: `accept`, `accept-language`, `content-type`, `content-length`, `origin`, `referer`, `idempotency-key`, `x-forwarded-proto`, `x-forwarded-host` | every header |
| `http.response_headers` | allow-list: `content-type`, `content-length`, `cache-control`, `location`, `retry-after` | every header |
| `http.request_query_keys`, `http.request_cookie_names` | sorted names, at most 50 | names |
| `http.request_query` | no | values |
| `http.request_cookies` | no | values, each masked unless `CookieValues(names...)` lists it |
| `http.request_params` | no | path parameter values |
| `http.request_body`, `http.response_body` | no | see "Bodies" |
| `http.operation_id` | when the framework has one | same |

- Header names are lowercase. Every value passes through the redactor like any other field.
- `operation` is `{METHOD} {route}` for a matched route, and `{METHOD} unmatched` otherwise.
- The route is `""` in two cases. The framework reports no match, or the status is 404 or 405
  and the template ends in `/*`. Chi, Echo, and Fiber report such a wildcard template for an
  unmatched path under a group.
- `http.path` keeps the raw path. Built-in value patterns mask a known token shape inside it.
- `http.scheme` is `https` for a TLS connection and `http` otherwise. `http.host` is the `Host`
  header. From a trusted proxy, `X-Forwarded-Proto` and `X-Forwarded-Host` replace them. The
  OTel metric needs `url.scheme`, and the GCP and Datadog presets build a full URL from both.

## Options

<!-- snippet:sketch -->
```go
func CaptureAll() Option
func CaptureHeaders(names ...string) Option          // adds names to the request allow-list
func CaptureResponseHeaders(names ...string) Option
func CookieValues(names ...string) Option            // cookie values kept unmasked under CaptureAll
func CaptureBody(on bool) Option                     // bodies without the rest of CaptureAll
func MaxBody(bytes int) Option                       // default 16 KiB per direction, clamped to 0..1 MiB
func BodyTypes(types ...string) Option               // default application/json, application/*+json, text/*, application/x-www-form-urlencoded
func SkipPaths(patterns ...string) Option            // exact or glob, ** crosses /
func Skip(fn func(Request) bool) Option
func ForRoute(pattern string, opts ...Option) Option // per-route rules, matched on "METHOD template"
func RouteFunc(fn func(*http.Request) string) Option // NetHTTP only
func TrustedProxies(cidrs ...string) Option
func TrustRequestID(on bool) Option                  // default true, with the SPEC.md length and charset rule
func EchoRequestID(on bool) Option                   // default true
func PanicPolicy(p Policy) Option                    // Recover500 (default) or Repanic
func User(fn func(Request) string) Option            // sets user.id
```

- Env `local`, `dev`, or `development` turns on `CaptureAll()`. An explicit option wins.
- `SkipPaths` defaults to none. A skipped request starts no event and runs no plugin.
- `TrustedProxies` empty means `http.client_ip` is the connection address. With CIDRs set, core
  walks `X-Forwarded-For` from the right and takes the first untrusted address.
- `ForRoute("POST /login", CaptureBody(false))` applies to a matched route that equals the
  pattern, or matches it as a glob. Other routes ignore it.

### Deliberate response changes

wlog changes a response in two documented cases only. It sets the `X-Request-ID` response header,
which `EchoRequestID(false)` turns off. A panic before any write becomes a 500 response, which
`PanicPolicy(Repanic)` turns off. Every other response byte and status stays exactly as the app
wrote it.

## Bodies

- A body is captured only under `CaptureAll()` or `CaptureBody(true)`, only for `BodyTypes`, and
  never for HEAD requests or 204 and 304 responses.
- A body with a `Content-Encoding` other than `identity` is skipped, unless the adapter passes
  `decoded` true. Fiber's `c.Body()` decodes, for example.
- A JSON body of any shape (object, array, or scalar) is parsed into the event tree, so key rules
  redact inside it.
- A JSON body cut at `MaxBody`, or one that fails to parse, becomes
  `{"truncated": true, "bytes": N}`. Its text is never kept.
- A form body is parsed into key and value pairs, so key rules redact a `password` field.
- A text body is kept as a string cut at `MaxBody`, and value patterns scan it.
- The request body reaches the handler in full. Capture reads at most `MaxBody` bytes from a
  shared pool, then chains the rest.

## Problem responses

<!-- snippet:sketch -->
```go
func WriteProblem(w http.ResponseWriter, r *http.Request, err error) // RFC 9457
func ParseProblem(body []byte) (wlog.ErrorInfo, bool)
```

- `WriteProblem` runs the Logger's extractor on `err` and writes `application/problem+json`. The
  body holds `type`, `title`, `status`, `detail`, and `instance`. It adds the extensions `code`,
  `fix`, `link`, `request_id`, and `data`.
- `type` is `ErrorInfo.Link`, else `about:blank`. `title` is the HTTP status text, and `detail`
  is `ErrorInfo.Message`. `instance` is the request path.
- It never writes `internal`, `stack`, `cause`, `causes`, `caller`, or `attrs`.
- It also records the error on the event, so the response and the event agree.
- Echo, Gin, Fiber, and the other adapters give `WriteProblem(c, err)` with the same body.
- `ParseProblem` reads such a body back into an `ErrorInfo`, for a client or a test. (PAR-8, BET-6)

## Panics

- With `Recover500`, `httpcore` recovers a panic in the handler chain. It records the error with a
  stack. If nothing was written yet, it writes 500. Then it emits. A later middleware in the chain does not
  run. The adapter calls its framework's abort.
- `http.ErrAbortHandler` always panics again after the event emits.
- With `Repanic`, the event emits and the panic continues.

## net/http writer

The `NetHTTP` writer wrapper implements `Unwrap`, `Flush`, `Hijack`, and `ReadFrom`. So
`http.ResponseController` reaches the real writer for deadlines and full duplex. `ReadFrom` copies
at most `MaxBody` bytes into the capture buffer. `Flush` before `WriteHeader` counts as status
200. A successful `Hijack` records status 101. The wrapper never forwards a second `WriteHeader`,
the same as net/http.

## Rebuilt adapters

| Adapter | Wrap point and `Setup` | Route | Status and errors | Floor |
|---|---|---|---|---|
| `http-std` (root) | `wlogstd.Middleware(log, opts...)` is `httpcore.NetHTTP`. `Setup(mux *http.ServeMux) http.Handler` | `r.Pattern` after `next`, from a `//go:build go1.23` file. gorilla/mux docs show `router.Use(...)` with `mux.CurrentRoute` | writer wrapper | Go 1.21 |
| `http-echo` | `e.Use(Middleware(log))` first, so it is the outermost. `Setup(e)` | `c.Path()`, blanked on `echo.ErrNotFound` and on a `/*` template with 404 or 405 | A returned error goes to `c.Error(err)`, then the middleware reads `c.Response().Status` and returns nil, like Echo's own `RequestLogger`. `PassErrors()` instead returns the error and takes the status from `*echo.HTTPError` | echo v4.9.0, Go 1.21 |
| `http-echo5` | same as `http-echo` | `c.Path()` and `c.RouteInfo().Name`, blanked for `NotFoundRouteName` and `MethodNotAllowedRouteName` | same, with `echo.UnwrapResponse` and `echo.StatusCode(err)` | echo v5.1.0, Go 1.25 |
| `http-gin` | `engine.Use(Middleware(log))`, never group middleware. `Setup(engine)`. `Handler(engine) http.Handler` also covers redirects | `c.FullPath()`, `""` on 404 and 405 | `c.Writer.Status()` after `c.Next()`. `c.Writer.Size()` counts only above -1. A panic calls `c.AbortWithStatus(500)`. `c.Errors` go to `errors` in a deferred step | gin v1.9.0 (first `Unwrap`), Go 1.21 |

Gin and Echo keep their own writers. The adapter reads status and size from them, and wraps a
writer only under `CaptureAll()` to capture a response body.

## Success criteria

1. `http-std`, `http-echo`, `http-echo5`, `http-gin`, and the gorilla/mux example pass every
   scenario of the `http` conformance suite. (HTTP-22)
2. A JSON array body, a JSON body cut at the cap, and a form body with `password` leak nothing
   under `CaptureAll()`. The handler still reads the whole body. (HTTP-1)
3. A panicking Gin auth middleware stops the chain, and the handler never runs. (HTTP-2)
4. Gin: a status change before the first write reaches the wire and the event, and a 404 or 405
   logs its real status. (HTTP-3, HTTP-4)
5. Echo v5: an error after a committed response writes no second body.
   `http.ResponseController` deadlines work under `NetHTTP`. (HTTP-5, HTTP-6 part)
6. `http.ErrAbortHandler` reaches net/http after the event emits, and a proxied stream that
   aborts is cut on the wire. (HTTP-6)
7. A 64 MB text download through `ReadFrom` allocates under 2 MB more than without wlog. (HTTP-7)
8. A panicking `Starter` or `Finisher` plugin leaves the response unchanged, and each is reported.
   (HTTP-8)
9. gorilla/mux with `router.Use` gives the route template. (HTTP-9)
10. `BenchmarkMiddleware_Realistic` (13 headers, a cookie, a query, a 1 KB JSON body, safe
    defaults, JSON to stdout through the async writer) is 50µs p50 or less on an M-series Mac.
    (HTTP-10)
11. Echo: a 400 `HTTPError` gives level `warn`, and a 404 from a scanner gives level `warn` on
    every adapter. (HTTP-11)
12. Gin `c.Errors` before a panic appear in `errors`. An Echo panic updates `echo.Response`.
    (HTTP-12)
13. A spoofed `X-Forwarded-For` with no trusted proxy, and an 8 KB request id, never reach the
    event. (HTTP-13)
14. With safe defaults, `sid`, `PHPSESSID`, `code`, `key`, and `sig` values never appear. (HTTP-14)
15. An unmatched path gives route `""` on every adapter. (HTTP-15)
16. `MaxBody(-1)` clamps to 0, and a 7 byte body allocates from the pool, not 1 MiB. (HTTP-16)
17. `http.request_params`, `http.response_headers`, the header allow-list, globs in `SkipPaths`,
    and `ForRoute` each have a test. (HTTP-17)
18. HEAD, 204, 304, a gzip body, `application/problem+json`, a sniffed content type, a future
    `traceparent` version, and a missing `traceparent` behave as this spec says. (HTTP-19)
19. The `wlogstd` package doc lists the capture defaults, the proxy rule, both deliberate response
    changes, and the emit point. (HTTP-23)

## Testing

Black-box tests per package, plus the `http` suite from `internal/conformance/http`. Benchmarks
live in `middleware/httpcore/bench_test.go` with a realistic request fixture, and `tools bench`
guards them.

## Boundaries

- **Always:** read every framework value before the handler returns, and copy it.
- **Ask first:** a new deliberate response change, or a change to the default allow-lists.
- **Never:** keep raw text of a JSON body that failed to parse or was cut.

## Open questions

None.
