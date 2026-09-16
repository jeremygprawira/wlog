# HTTP router integration research for `http-core`

Date: 2026-09-16. Toolchain: go1.26.1 darwin/arm64. wlog root module is `go 1.23`.

## Method and evidence

- Versions: `go list -m -versions` and `go mod download -json <mod>@latest`. Release time comes from `go list -m -json <mod>@<ver>`.
- Source: read in the module cache at `~/go/pkg/mod/<module>@<version>`.
- Behavior proofs: tests in `scratchpad/research/http-work/latest/<lib>/*_test.go` (module `scratch/latest`, all libs at latest). Every "test:" note below is output from those tests.
- Floor proofs: `scratchpad/research/http-work/floor/check.sh` builds `floor/src/<lib>/main.go` (the API surface an adapter needs) against old versions with `GOTOOLCHAIN=local`. Results are in `floor/run/`.
- Anything not proven from source or a test is marked **UNVERIFIED**.

## 1. Version table

| Library | Module path | Latest | Released (UTC) | License | `go` line (latest) | Lowest version that builds the adapter surface (its `go` line) | Nearest older version that fails |
|---|---|---|---|---|---|---|---|
| chi | `github.com/go-chi/chi/v5` | v5.3.2 | 2026-08-20 | MIT | 1.23 | v5.0.0 (1.16) | none, v5.0.0 is the first v5 |
| gin | `github.com/gin-gonic/gin` | v1.12.0 | 2026-02-28 | MIT | 1.25.0 | v1.5.0 (1.12) | not tested below |
| echo v4 | `github.com/labstack/echo/v4` | v4.15.4 | 2026-06-15 | MIT | 1.25.0 | v4.9.0 (1.17), lowest tested | not tested below |
| echo v5 | `github.com/labstack/echo/v5` | v5.3.1 | 2026-07-21 | MIT | 1.25.0 | v5.0.0 (1.25.0) without `echo.StatusCode`. `echo.StatusCode(err)` first builds at v5.0.4. **Recommend v5.1.0** (see note) | v5.0.3 lacks `StatusCode` |
| fiber v2 | `github.com/gofiber/fiber/v2` | v2.52.15 | 2026-08-12 | MIT | 1.20 (needs fasthttp v1.51.0) | v2.20.0 (1.16) | v2.19.0 lacks `App.ErrorHandler`. v2.10.0 lacks `UserContext`, which v2.15.0 has |
| fiber v3 | `github.com/gofiber/fiber/v3` | v3.5.0 | 2026-08-12 | MIT | 1.25.0 (needs fasthttp v1.73.0) | v3.0.0 (1.25.0) | none, first stable |
| httprouter | `github.com/julienschmidt/httprouter` | v1.3.0 | 2019-09-29 | BSD-3-Clause | 1.7 | v1.2.0 (no go.mod) | n/a |
| go-zero | `github.com/zeromicro/go-zero` (package `rest`) | v1.10.3 | 2026-07-31 | MIT | 1.24.0 | v1.3.0 (1.15) | not tested below. `rest.NewServerless` (test helper only) first builds at v1.9.0 |
| hertz | `github.com/cloudwego/hertz` | v0.10.6 | 2026-08-06 | Apache-2.0 | 1.20 | v0.4.0 (1.16) | not tested below. Pre-1.0, so breaking changes are allowed |
| kratos | `github.com/go-kratos/kratos/v2` | v2.9.2 | 2025-12-05 | MIT | 1.22 | v2.1.0 (1.16) | v2.0.0 lacks `khttp.Transporter` |
| huma | `github.com/danielgtaylor/huma/v2` | v2.39.1 | 2026-07-29 | MIT (LICENSE.md) | 1.25.0 | **v2.13.0** (1.20) | v2.12.0 lacks `huma.Context.Status()`. v2.7.0 lacks `huma.WithContext`, which v2.8.0 has |
| fasthttp | `github.com/valyala/fasthttp` | v1.74.0 | 2026-09-07 | MIT | 1.25.0 | v1.51.0 (1.20) with `VisitAll*`. Iterators `Header.All()`, `Args.All()`, `Header.Cookies()` first build at **v1.63.0** (1.23.0) | v1.62.0 lacks `All()` |

Notes:
- The echo v5 CHANGELOG (v5.0.0, 2026-01-18) allows breaking v5 API changes for critical issues until 2026-03-31, even against semantic versioning. v5.1.0 is dated 2026-03-31. The same entry says v4 gets security and bug fixes until 2026-12-31.
- In fasthttp v1.74.0, `RequestHeader.VisitAll`, `Args.VisitAll`, and `VisitAllCookie` are marked `Deprecated: Use All instead` / `Use Cookies instead` (header.go:1292, args.go:91). staticcheck SA1019 will flag them in `make lint`.
- The huma root module requires many routers: gin, chi, fiber v2, fiber v3, echo v4, echo v5, gorilla/mux, httprouter, and bunrouter. All adapters are packages in one module.
- gin `responseWriter.Unwrap()` arrived in v1.9.0 (CHANGELOG line 268, #3489).
- A `go` line in a dependency is a minimum toolchain. A wlog adapter module that requires echo v5, fiber v3, or huma latest must itself be `go 1.25` or higher. This is standard Go behavior and was not tested here.

## 2. Cross-framework summary

| Library | Wrap point | Route template API | Template known before `next`? | 404/405 run through the wrap? | Status visible inside the wrap after `next` | Written after the wrap returns? | Recovers panics by default? | Replace request ctx |
|---|---|---|---|---|---|---|---|---|
| chi | `r.Use(func(http.Handler) http.Handler)` | `chi.RouteContext(ctx).RoutePattern()` | No (""). Only after next | Yes, 404/405. Not when the mux has zero routes | Own writer wrapper | No | No | `r.WithContext` |
| httprouter | Wrap `Router.ServeHTTP` plus each `Handle` | None in v1.3.0 | Only by wrapping at registration | Only the outer ServeHTTP wrap sees them | Own writer wrapper | No | Opt-in via `PanicHandler` | `r.WithContext` |
| gin | `engine.Use(gin.HandlerFunc)` | `c.FullPath()` | Yes | Yes, via engine.Use only. Trailing-slash and fixed-path redirects bypass it | `c.Writer.Status()` is preset to 404/405. `Size()` = -1 | Yes: default 404/405 body, and `WriteHeaderNow` for empty handlers | No (`gin.Default()` adds Recovery) | `c.Request = c.Request.WithContext(ctx)` |
| echo v4 | `e.Use` (after routing) / `e.Pre` | `c.Path()` | Yes for `Use` | Yes (handler returns `ErrNotFound` or `ErrMethodNotAllowed`) | No. If err != nil, it is 200 and uncommitted | Yes: `HTTPErrorHandler` runs after the chain | No | `c.SetRequest(r.WithContext(ctx))` |
| echo v5 | same | `c.Path()`, `c.RouteInfo()`, `r.Pattern` | Yes for `Use` | Yes | No (same as v4). Use `echo.StatusCode(err)` | Yes | No | `c.SetRequest` |
| fiber v2 | `app.Use(func(*fiber.Ctx) error)` | `c.Route().Path` | No. Before next it is the middleware's own route | Yes | No. If err != nil, it is 200 | Yes: `ErrorHandler` runs after the chain | No, and fasthttp crashes the process | `c.SetUserContext(ctx)` |
| fiber v3 | `app.Use(func(fiber.Ctx) error)` | `c.FullPath()` + `c.Matched()` | No | Yes, unless `Config.SkipUnmatchedRoutes=true` | No. If err != nil, it is 200 | Yes | No, and fasthttp crashes the process | `c.SetContext(ctx)` |
| fasthttp | `func(fasthttp.RequestHandler) fasthttp.RequestHandler` | None | n/a | n/a (no router) | `ctx.Response.StatusCode()` is final | No (response is written after the handler returns) | No, crash | `ctx.SetUserValue` only |
| hertz | `h.Use(app.HandlerFunc)` or `server.WithTracer` | `ctx.FullPath()` | Yes | Yes, via engine.Use. Redirects bypass. The Tracer sees all | `ctx.Response.StatusCode()` is preset to 404/405 | Yes: default 404/405 body | No (`server.Default()` adds recovery). The netpoll pool logs and swallows | `ctx.Next(newCtx)` |
| go-zero | `server.Use(rest.Middleware)` or `rest.WithRouter(wrapper)` | None in middleware. The router wrapper's `Handle(method, path, h)` gets it | Only via the router wrapper | No for `Use`. Yes for a router wrapper | Own writer wrapper, which sees a `*handler.timeoutWriter` | Yes: Recover 500 and Timeout 503 happen outside `Use` | Yes (native `RecoverHandler`, outside `Use`) | `r.WithContext` |
| kratos | `khttp.Filter(FilterFunc)` + `middleware.Middleware` | `tr.(khttp.Transporter).PathTemplate()`, `tr.Operation()` | Middleware: yes. Global Filter: never | Global Filter: yes (and 405 becomes 404). Middleware: no | Filter: own wrapper. Middleware: only `err` | Yes: `ErrorEncoder` runs after the middleware | No (`recovery.Recovery()` is middleware scope only) | Filter: `r.WithContext`. Middleware: `h(newCtx, req)` |
| huma | `api.UseMiddleware(func(huma.Context, func(huma.Context)))` | `ctx.Operation().Path`, `.OperationID` | Yes | No (router handles them) | `ctx.Status()` (v2.13.0 and later) | No (errors are written inside huma) | No | `huma.WithContext` / `huma.WithValue` |

## 3. Per-library detail

### 3.1 chi v5 (v5.3.2)

**Wrap API**
```go
func (mx *Mux) Use(middlewares ...func(http.Handler) http.Handler) // panics if called after any route: "chi: all middlewares must be defined before routes on a mux"
type Middlewares []func(http.Handler) http.Handler
func (mx *Mux) NotFound(h http.HandlerFunc)
func (mx *Mux) MethodNotAllowed(h http.HandlerFunc)
```

**Route template**
```go
func RouteContext(ctx context.Context) *chi.Context
func (x *Context) RoutePattern() string // strings.Join(RoutePatterns,"") + wildcard cleanup
```
- The root mux creates `rctx` from a `sync.Pool` and stores it in ctx before middleware runs. `RoutePatterns` fill during `routeHTTP`, so read the pattern after `next` (context.go:109-121 says so).
- `mx.pool.Put(rctx)` runs right after `mx.handler.ServeHTTP` returns (mux.go ServeHTTP). Read the pattern before the middleware returns. Never read it from a goroutine.
- v5.3.2 `routeHTTP` also sets `r.Pattern = rctx.RoutePattern()` and `r.SetPathValue(...)` on the matched request.
- Test results:
  - `/users/7` gives before="" and after="/users/{id}".
  - Nested `Route` gives "/api/items/{id}". Mounted gives "/mnt/x/{y}".
  - `/nope` gives "" with status 404. `POST /users/7` gives "" with status 405.
  - **A 404 inside `Route`/`Mount` gives the pattern "/api/*" or "/mnt/*".**
  - A mux with zero routes skips middleware entirely (`mx.handler == nil`, so `NotFoundHandler` is called directly). Test: middleware not called, 404.

**Status and bytes**
- chi core does not wrap the writer. Nothing is written after the middleware returns.
- The same module has `chi/middleware.NewWrapResponseWriter(w http.ResponseWriter, protoMajor int) WrapResponseWriter`. Methods: `Status() int`, `BytesWritten() int`, `Tee(io.Writer)`, `Unwrap() http.ResponseWriter`, `Discard()`. It has Flush/Hijack/Push/ReadFrom variants.

**Request access:** plain net/http (`r.Header`, `r.URL.Query()`, `r.Cookies()`, `r.RemoteAddr`, `r.Body`, restorable by replacing `r.Body`).

**Panics:** chi core has no recover. `middleware.Recoverer` writes 500 and re-panics `http.ErrAbortHandler`.

**Operation id:** none. Pattern only.

**Gotchas**
1. Reading `RoutePattern()` before `next` gives "".
2. A 404 under a subrouter reports the pattern "/prefix/*". Drop the route on 404/405.
3. `Use` after routes panics.
4. A mux with no routes bypasses middleware.
5. Inline `Group`/`With` middleware only runs for matched routes.
6. `NotFound` set on an inline group is chained with that group's middlewares (mux.go:203-216).

### 3.2 julienschmidt/httprouter (v1.3.0)

**API**
```go
type Handle func(http.ResponseWriter, *http.Request, Params)
func (r *Router) Handle(method, path string, handle Handle)
func (r *Router) Handler(method, path string, handler http.Handler)   // puts Params in ctx under ParamsKey only if len(p) > 0
func (r *Router) Lookup(method, path string) (Handle, Params, bool)    // no template
func ParamsFromContext(ctx context.Context) Params
type Router struct {
    RedirectTrailingSlash, RedirectFixedPath, HandleMethodNotAllowed, HandleOPTIONS bool // all default true in New()
    GlobalOPTIONS, NotFound, MethodNotAllowed http.Handler
    PanicHandler func(http.ResponseWriter, *http.Request, interface{})
}
```

- No middleware concept, and no matched-template API in any tag. `SaveMatchedRoutePath` and `Params.MatchedRoutePath()` exist only on master (pseudo-version `v1.3.1-0.20240130105656-484018016424`), never tagged. `r.Pattern` is not set (test: "").
- The adapter needs two layers. An outer wrap of `Router.ServeHTTP` handles lifecycle, 404/405, redirects, and OPTIONS. A per-route wrap at registration time records the template on the event.
- 404: `NotFound` handler, else `http.NotFound`.
- 405: `MethodNotAllowed`, else `http.Error(405)` with an `Allow` header.
- A trailing-slash or case mismatch gives 301 (GET) or 307 (other methods) without calling any handler (test: 301).
- OPTIONS is answered automatically with `Allow`.
- Panics: if `PanicHandler != nil`, the router recovers them. Otherwise they propagate (test).

**Gotchas**
1. For `Handler`/`HandlerFunc` routes that have params, `Params` are in ctx. Other routes put nothing in ctx.
2. The last release is from 2019 and huma pins v1.3.0.

### 3.3 gin (v1.12.0)

**Wrap API**
```go
type HandlerFunc func(*gin.Context)
func (engine *Engine) Use(middleware ...HandlerFunc) IRoutes  // also rebuilds allNoRoute/allNoMethod
func (group *RouterGroup) Use(...)                             // does NOT apply to 404/405
func (engine *Engine) NoRoute(handlers ...HandlerFunc)
func (engine *Engine) NoMethod(handlers ...HandlerFunc)
func (c *Context) Next(); Abort(); AbortWithStatus(int); AbortWithError(int, error) *Error; IsAborted() bool; Error(err) *Error
```

**Route template**
- `c.FullPath() string`. `handleHTTPRequest` sets `c.fullPath` before `c.Next()`, so it is available before next.
- "" for 404/405 (test).

**404/405**
- `serveError` sets `c.writermem.status = code`. Then it runs `engine.allNoRoute`/`allNoMethod` (engine.Use middleware plus NoRoute handlers). If nothing was written, it then writes the default body.
- Test inside middleware for 404: `Status()=404`, `Size()=-1`. Final body is 18 bytes.
- `HandleMethodNotAllowed` defaults to false, so 405 cases become 404.
- **Trailing-slash and fixed-path redirects call no handlers** (test: 301, middleware not called).

**Status and bytes**
```go
type ResponseWriter interface { http.ResponseWriter; http.Hijacker; http.Flusher; http.CloseNotifier
    Status() int; Size() int; WriteString(string) (int, error); Written() bool; WriteHeaderNow(); Pusher() http.Pusher }
// concrete *responseWriter also has Unwrap() http.ResponseWriter
```
- `WriteHeader` only records the code. The header is sent on the first Write or `WriteHeaderNow`.
- For matched routes, `handleHTTPRequest` calls `c.writermem.WriteHeaderNow()` after `c.Next()`. A handler that writes nothing gets its 200 header sent after the middleware returns. `Status()` already reports 200 inside the middleware.
- `Size()` is -1 until anything is written.
- `Hijack` fails once `size > 0`.
- Prefer reading `c.Writer.Status()`/`Size()` over replacing `c.Writer`. A replacement must implement the whole interface.

**Request access**
- `c.Request.Header`, `c.Request.URL.Query()`, `c.Request.Cookies()`, `c.ClientIP()`, `c.RemoteIP()`.
- Body: `c.Request.Body` is a stream. `c.GetRawData()` reads and consumes it.
- The Context is pooled (`engine.pool.Put(c)` after handling). Use `c.Copy()` for goroutines.

**Panics**
- The engine has no recover (test: the panic escaped `ServeHTTP`).
- `gin.Recovery()` / `CustomRecoveryWithWriter` call `handle(c, rec)`, which by default does `AbortWithStatus(500)`. A broken pipe gives `c.Error(err)` + `Abort()`.
- If our middleware sits inside Recovery, it sees the panic unwind. If it sits outside, it sees status 500.

**Context**
- `c.Request = c.Request.WithContext(ctx)`. Test: the handler saw the value.
- `*gin.Context` is itself a `context.Context`. `Value` checks `ContextRequestKey`, `ContextKey`, then string keys in `c.Keys`. If `engine.ContextWithFallback` is true, it falls back to `Request.Context()`.

**Operation id:** none (`c.HandlerName()` only).

**Gotchas**
1. Redirects produce no event unless the adapter also wraps `engine.ServeHTTP`.
2. Group middleware misses 404/405.
3. The final 404 body size is only known after the engine returns.
4. `c.Errors` does not change the status.

### 3.4 echo v4 (v4.15.4)

**Wrap API**
```go
type HandlerFunc func(c echo.Context) error
type MiddlewareFunc func(next HandlerFunc) HandlerFunc
type HTTPErrorHandler func(err error, c echo.Context)
func (e *Echo) Use(middleware ...MiddlewareFunc) // runs after routing
func (e *Echo) Pre(middleware ...MiddlewareFunc) // runs before routing
func (e *Echo) GET(path string, h HandlerFunc, m ...MiddlewareFunc) *Route // Route has Name
```

**Route template**
- `c.Path()`. Without Pre middleware, `ServeHTTP` runs `Router.Find` before the `Use` chain, so the path is known before next. With Pre middleware, routing runs inside the chain.
- Test values:
  - Match: "/users/:id".
  - 405: "/users/:id" (not empty).
  - 404 with no match: "".
  - 404 under a group that has `g.Use(...)`: "/api/*". `Group.Use` auto-registers `RouteNotFound("")` and `RouteNotFound("/*")`.

**404/405:** they go through `Use` as handlers returning `echo.ErrNotFound` / `ErrMethodNotAllowed`.

**Status**
```go
type Response struct { Writer http.ResponseWriter; Status int; Size int64; Committed bool } // + Before(fn), After(fn), Flush(), Hijack(), Unwrap()
```
- `ServeHTTP` does `if err := h(c); err != nil { e.HTTPErrorHandler(err, c) }` after the whole chain.
- Test: inside the middleware with a returned error, `Status=200` and `Committed=false`. Final status is 418/404/405/500.
- The adapter must derive the status from err. `DefaultHTTPErrorHandler` uses a type assertion `err.(*HTTPError)` (plus `Internal *HTTPError`), otherwise 500.
- Alternative: call `c.Error(err)` itself, as `middleware.RequestLogger` does with `HandleError: true`. If `Committed`, `DefaultHTTPErrorHandler` returns early. Custom handlers can write twice.
- If the writer does not support flushing, `Response.Flush` panics.

**Panics**
- None recovered in `ServeHTTP` (test).
- `middleware.Recover` calls `c.Error(err)` itself and returns **nil** (unless `DisableErrorHandler`). It re-panics `http.ErrAbortHandler`.
- Test: an outer middleware sees `err=nil`, `Status=500`, `Committed=true`.

**Request and context**
- `c.Request()`, `c.QueryParams()` (cached), `c.Cookies()`, `c.RealIP()` (`IPExtractor`, else legacy XFF).
- Replace ctx with `c.SetRequest(c.Request().WithContext(ctx))`.
- The context is pooled (`e.pool.Put(c)`).

**Operation id:** `*Route.Name` at registration. There is no per-request route info in v4.

**Gotchas**
1. A returned handler error leaves the middleware with a wrong status.
2. Recover hides the error value.
3. A 404 under a group reports the template "/api/*" (use `err`/status to blank it).
4. Pre middleware sees `Path()` only after next.

### 3.5 echo v5 (v5.3.1)

**Wrap API**
```go
type HandlerFunc func(c *echo.Context) error
type MiddlewareFunc func(next HandlerFunc) HandlerFunc
type HTTPErrorHandler func(c *echo.Context, err error)
func (e *Echo) Use(middleware ...MiddlewareFunc); func (e *Echo) Pre(middleware ...MiddlewareFunc) // chains prebuilt by buildRouterChains
func UnwrapResponse(rw http.ResponseWriter) (*echo.Response, error)
type Response struct { http.ResponseWriter; Status int; Size int64; Committed bool }
type HTTPStatusCoder interface{ StatusCode() int }
func StatusCode(err error) int // 0 if no coder; added v5.0.4
type RouteInfo struct { Name, Method, Path string; Parameters []string }
const NotFoundRouteName = "echo_route_not_found_name"; MethodNotAllowedRouteName = "echo_route_method_not_allowed_name"; RouteNotFound = "echo_route_not_found"
```

**Route template**
- `c.Path()` and `c.RouteInfo()`. The router also sets `c.Request().Pattern = rPath` (router.go, after `InitializeRoute`).
- Test values:

| Request | `c.Path()` | `RouteInfo` |
|---|---|---|
| Match | "/users/:id" | Method "GET", Path "/users/:id", Name "" |
| 404 | "" | Name `NotFoundRouteName` |
| 405 | "/users/:id" | Name `MethodNotAllowedRouteName`, empty Path |
| 404 under group with `g.Use` | "/api/*" | Method `echo.RouteNotFound`, Path "/api/*" |

**Status**
- `c.Response()` returns `http.ResponseWriter`. Get the counters with `echo.UnwrapResponse`.
- The error handler runs after the chain, as in v4. Test: 200 and uncommitted inside the middleware, with `StatusCode(err)` = 418/404/405 and 0 for plain errors (final 500).
- `c.JSON` wraps the writer in a `delayedStatusWriter` that holds the status until the first Write.

**Panics**
- `middleware.Recover` **returns** the error (a `*middleware.PanicStackError` wrapping the panic value) and does not call the error handler. `StatusCode` gives 0, so the final status is 500. It re-panics `http.ErrAbortHandler`.
- `serveHTTP` has no recover.

**Context:** `c.SetRequest(...)`. The context is pooled (`defer e.contextPool.Put(c)`).

**Operation id:** `RouteInfo.Name` (empty unless the user sets it).

**Gotchas:** same as v4, plus:
1. Recover behaves differently from v4.
2. v5.0.x allowed breaking changes until 2026-03-31.

### 3.6 fiber v2 (v2.52.15)

**Wrap API**
```go
type Handler = func(*fiber.Ctx) error
type ErrorHandler = func(*fiber.Ctx, error) error
func (app *App) Use(args ...interface{}) Router
func (c *Ctx) Next() error
func (c *Ctx) Route() *Route   // Route{Method, Name, Path string; Params []string; Handlers []Handler; unexported use/mount/...}
func (app *App) ErrorHandler(ctx *Ctx, err error) error // exported since v2.20.0
```

**Route template**
- Read `c.Route().Path` after `c.Next()`. Before Next, inside `app.Use`, it is the middleware's own route: Path "/" and Method = the request method, **not "USE"**, because `addRoute` overwrites `Method`.
- After Next it is the last matched route. Endpoint routes carry the group prefix (test: "/v1/items/:id").
- For 404/405 it stays the middleware route "/" (test).
- No exported flag says "endpoint". Workaround: `own := c.Route()` before Next, then compare the pointer after. Limits:
  - `addRoute` merges consecutive registrations with the same Path and use-flag into one Route (router.go:456). A later `app.Use` middleware that aborts keeps the same pointer (correctly reported as "no endpoint").
  - `app.Use("/api", mw)` creates a distinct route, so an abort there looks like an endpoint "/api".
- `.Name("users.create")` is readable via `Route().Name` (test).

**404/405**
- `app.next` returns `fiber.NewError(404, "Cannot GET /nope")` or `ErrMethodNotAllowed` through the middleware's `c.Next()`.
- Test: `err` is set, `Response().StatusCode()` is 200 inside the middleware, and final is 404/405.

**Status and bytes**
- `c.Response().StatusCode()`, `len(c.Response().Body())`. The response is buffered and sent after the handler returns.
- `app.handler` calls `app.ErrorHandler(c, err)` after the chain.
- The fiber logger pattern: call `c.App().ErrorHandler(c, chainErr)`. If that errors, call `SendStatus(500)`. Then **return nil** so the app does not handle it twice (logger.go:139-145, returns nil).
- `SendStream` / `SetBodyStreamWriter` leave the body size unknown (`IsBodyStream()`).

**Request access**
- Headers: `c.Get(key)`, `c.GetReqHeaders() map[string][]string`, `c.Request().Header.VisitAll`.
- Query: `c.Context().QueryArgs()`.
- Cookies: `c.Cookies(key)`, `c.Request().Header.VisitAllCookie`.
- Remote: `c.IP()`, `c.Context().RemoteAddr()`.
- Body:
  - `c.Body()` decodes Content-Encoding (gzip/br/deflate) and restores the raw body.
  - `c.BodyRaw()` returns the raw bytes.
  - The body is buffered in memory (`BodyLimit` default 4 MB) unless `Config.StreamRequestBody`.

**Lifetime**
- `*fiber.Ctx` is pooled (`defer app.ReleaseCtx(c)`).
- `c.Method()`, `c.Path()`, `c.OriginalURL()`, and `c.Get()` return zero-copy strings over fasthttp buffers (`app.getString`) unless `Config.Immutable`. Copy them (`strings.Clone`) before storing in an event.

**Panics**
- Neither fiber nor fasthttp recovers, so a panic crashes the process. fasthttp `server.go` has no `recover()`.
- `middleware/recover` turns a panic into a returned error for `ErrorHandler`.

**Context:** `c.UserContext() context.Context` (default `context.Background()`, stored as a fasthttp user value) and `c.SetUserContext(ctx)`. Test: the handler saw the value. `c.Context()` is `*fasthttp.RequestCtx`.

**Gotchas**
1. The route before Next is wrong, and there is no endpoint flag.
2. If the chain returns an error, the status is wrong.
3. Unsafe strings.
4. Panics kill the process.
5. `UserContext` is not cancelled on client disconnect.

### 3.7 fiber v3 (v3.5.0)

**Wrap API**
```go
type Handler = func(fiber.Ctx) error        // Ctx is an interface; CustomCtx via fiber.NewWithCustomCtx(func(*App) CustomCtx, ...Config)
type ErrorHandler = func(fiber.Ctx, error) error
func (app *App) Use(args ...any) Router      // also accepts adapted net/http and fasthttp handlers (adapter.go toFiberHandler)
Ctx: Next() error; Route() *Route; FullPath() string; Matched() bool; IsMiddleware() bool
     Context() context.Context; SetContext(context.Context); RequestCtx() *fasthttp.RequestCtx
     Abandon(); IsAbandoned() bool   // timeout/SSE middlewares keep the ctx out of the pool
```

**Route template**
- After Next: `c.FullPath()` (= `Route().Path`) and `c.Matched()`.
- Test: match gives "/users/:id" with `Matched=true`. 404/405 give "/" with `Matched=false`. Before Next, `IsMiddleware()=true`.

**404/405**
- `Config.SkipUnmatchedRoutes` (default false, router_skip.go): when true and middleware exists, 404/405 are answered by `ErrorHandler` **before the chain**. Test: middleware not called. CORS preflight is exempt.
- The version that introduced it is UNVERIFIED (listed in `docs/whats_new.md`, not in the v3.0.0 floor build).

**Status:** same as v2. Test: 200 inside the middleware with err set. `defaultRequestHandler` calls `ErrorHandler` after the chain. The v3 recover middleware sets `err = cfg.PanicHandler(c, r)` and returns it.

**Request access**
- Headers: `c.Get`, `c.GetHeaders()`/`GetReqHeaders()`, `c.Request().Header.All()` (iter).
- Query: `c.RequestCtx().QueryArgs()`.
- Other: `c.Cookies(key)`, `c.IP()`, `c.Body()`/`BodyRaw()`.
- The `Immutable` and zero-copy rules are the same as v2 (interface docs say "Returned value is only valid within the handler").

**Context**
- `c.Context()` returns the user context (default `context.Background()`), and `c.SetContext(ctx)` replaces it. Test: the handler saw the value via `c.Context()`.
- `fiber.Ctx` itself implements `context.Context`, but `Value` reads fasthttp user values (Locals) and `Done()` is always nil. Test: `c.Value(key)` = nil after `SetContext`.

**Gotchas:** same as v2, plus:
1. `SkipUnmatchedRoutes` hides 404/405 from middleware.
2. After `Abandon`, the ctx can outlive the handler.

### 3.8 fasthttp (v1.74.0)

**API**
```go
type RequestHandler func(ctx *fasthttp.RequestCtx)
// middleware: func(next fasthttp.RequestHandler) fasthttp.RequestHandler
ctx.Response.StatusCode() int; ctx.Response.Body() []byte; ctx.Response.IsBodyStream() bool; ctx.Response.Header.All()/VisitAll
ctx.Request.Header.Peek(key) []byte; .All() iter.Seq2[[]byte,[]byte] (v1.63.0+); .Cookies() iter (v1.63.0+); .VisitAll/.VisitAllCookie (deprecated in v1.74.0)
ctx.QueryArgs() *Args (.All() v1.63.0+); ctx.RemoteAddr() net.Addr; ctx.RemoteIP() net.IP; ctx.PostBody() []byte; ctx.RequestBodyStream() io.Reader (http.go:638)
ctx.SetUserValue(key, v any); ctx.UserValue(key any) any
```

- No router and no route template. `fasthttp/router` is a separate module (not researched, UNVERIFIED).
- The response is written after the top handler returns, so status and body after `next` are final. Exceptions: `Hijack`, `TimeoutError`, and body-stream writers, whose size is unknown. Test: status 201, body length 5.
- Lifetime (server.go:612-623): the handler must not keep references to `RequestCtx` or its members after it returns. If that is unavoidable, the handler must call `ctx.TimeoutError()` before it returns. User values are removed (and `io.Closer` values closed) after the top handler returns. Every `[]byte` must be copied.
- Panics: no `recover()` anywhere in the server path. The only one is in `Response.writeBodyStream`. A panic crashes the process.
- Context: `RequestCtx` implements `context.Context`. `Deadline` always returns false. `Done` is `ctx.s.done`, closed only on server shutdown. `Value` = `UserValue`. There is no per-request cancellation.

### 3.9 hertz (v0.10.6)

**Wrap API**
```go
type HandlerFunc func(c context.Context, ctx *app.RequestContext)
func (engine *route.Engine) Use(middleware ...app.HandlerFunc) IRoutes
func (ctx *RequestContext) Next(c context.Context)   // loops remaining handlers with the c passed in
ctx.Abort(); ctx.AbortWithStatus(int); ctx.IsAborted(); ctx.Error(err) *errors.Error; ctx.Copy() *RequestContext; ctx.FullPath() string
server.New(opts...) // no recovery;  server.Default(opts...) = New + h.Use(recovery.Recovery())
server.WithTracer(t tracer.Tracer) // type Tracer interface { Start(ctx context.Context, c *app.RequestContext) context.Context; Finish(ctx context.Context, c *app.RequestContext) }
server.WithHandleMethodNotAllowed(bool) // default false
server.WithStreamBody(bool)             // default false
```

**Route template**
- `ctx.FullPath()`. `SetFullPath` runs before `ctx.Next(c)`, so it is known before next.
- "" for 404/405 (test).

**404/405**
- `serveError` sets the status. Then it runs `engine.allNoRoute`/`allNoMethod` (global `Use` + `NoRoute`). It sets the default body ("Not Found", "Method Not Allowed") **after** the chain.
- Test inside middleware: status 404/405, body length 0.
- A missing Host on HTTP/1.1, or a path without a leading "/", gives 400 through `serveError` with `engine.Handlers`.
- **`RedirectTrailingSlash` (default true) and `RedirectFixedPath` bypass middleware** (test: 301, not called).

**Tracer**
- `http1/server.go` calls `DoStart` before reading the request. The ctx it returns is passed to `Core.ServeHTTP`.
- `DoFinish` runs after the response is written. It covers every request, including redirects and 400s. This is the only hook that sees everything.

**Status and bytes**
- `ctx.Response.StatusCode()`, `len(ctx.Response.Body())`, `ctx.Response.IsBodyStream()`.
- The response is buffered and written after `ServeHTTP` returns, except with `ctx.Flush()`, hijack, or streaming.

**Request access**
- Headers: `ctx.Request.Header.Peek/VisitAll`, `ctx.VisitAllHeaders(f)`.
- Query: `ctx.QueryArgs().VisitAll`, `ctx.VisitAllQueryArgs(f)`.
- Cookies: `ctx.Cookie(key) []byte`, `ctx.Request.Header.VisitAllCookie`.
- Remote: `ctx.RemoteAddr() net.Addr`, `ctx.ClientIP()`.
- Body: `ctx.Request.Body()` / `ctx.GetRawData()` (buffered). If `WithStreamBody(true)` is set, use `ctx.RequestBodyStream()`.
- Types are `github.com/cloudwego/hertz/pkg/protocol`, **not fasthttp**. hertz does not import fasthttp.
- `RequestContext` is pooled and reset. The docs say to call `Copy()` before passing it to a goroutine.

**Panics**
- `engine.PanicHandler` recovers panics. It is off unless the user sets it.
- The recovery middleware default handler logs and calls `AbortWithStatus(500)`.
- Without recovery, the result depends on the transport:
  - The default transport is netpoll on (amd64||arm64) && (linux||darwin), unless `HERTZ_NO_NETPOLL`. netpoll runs `onRequest` through `bytedance/gopkg` gopool, which recovers and logs `GOPOOL: panic in pool` (worker.go:60). The connection is closed and no response is sent.
  - The standard transport uses `go func` with no recover, so the process crashes.
  - Both outcomes are source-only. End-to-end behavior is UNVERIFIED.

**Context**
- The ctx is an explicit argument. The middleware calls `ctx.Next(newCtx)`, and downstream handlers receive it. Test: the handler's `c.Value` = "ev".
- `ctx.Value(key)` does not see it (test: nil).

**Operation id:** none (`ctx.HandlerName()`).

**Gotchas**
1. Redirects and 400s bypass middleware (use the Tracer).
2. The default 404 body is set after the middleware.
3. Not fasthttp types.
4. Panics are swallowed or crash depending on the transport.
5. Pre-1.0 API.

### 3.10 go-zero rest (v1.10.3)

**Wrap API**
```go
type Middleware func(next http.HandlerFunc) http.HandlerFunc
func (s *Server) Use(middleware Middleware)
func WithMiddleware(middleware Middleware, rs ...Route) []Route
func ToMiddleware(handler func(next http.Handler) http.Handler) Middleware
func WithRouter(router httpx.Router) RunOption
type httpx.Router interface { http.Handler; Handle(method, path string, handler http.Handler) error; SetNotFoundHandler(http.Handler); SetNotAllowedHandler(http.Handler) }
func WithNotFoundHandler(h http.Handler) RunOption; func WithNotAllowedHandler(h http.Handler) RunOption
func pathvar.Vars(r *http.Request) map[string]string
```

**Per-route chain order** (engine.go `bindRoute` + `buildChainWithNativeMiddlewares`, all `Middlewares.*` default true):
1. Trace
2. Log
3. Prometheus
4. MaxConns
5. Breaker
6. Shedding
7. Timeout (default 3000 ms)
8. Recover
9. Metrics
10. MaxBytes (default 1048576, reads only `ContentLength`)
11. Gunzip
12. JWT/signature (`appendAuthHandler`)
13. **user `Use` middlewares**
14. handler

**Route template**
- Not passed to `Use` middleware, and `r.Pattern` is not set.
- The final path (with the `WithPrefix` group joined) is passed to `router.Handle(route.Method, route.Path, chain)` at bind time.
- A `rest.WithRouter` wrapper can record it by wrapping the handler per route, and can wrap `ServeHTTP` for the full lifecycle. Test: `Handle(GET, "/v1/users/:id")`, and the wrapper saw 404/405/500/503.

**What `Use` middleware does not see**
- 404/405 (test: not called).
- MaxConns, Breaker, and Shedding rejections.
- 413 from MaxBytes and 401 from JWT (source).

**Timeout**
- `timeoutHandler` runs the rest of the chain (including `Use`) in a goroutine with a buffered `*handler.timeoutWriter` (Flush/Hijack/Push/Header/Write/WriteHeader, **no Unwrap**).
- On timeout it writes 503 (or 499 on client cancel) while the user middleware keeps running.
- Test: final 503, and the middleware finished later with its own 202.
- Websocket upgrades and `Accept: text/event-stream` skip the timeout.

**Panics**
- `RecoverHandler` sits outside `Use`. The middleware sees the panic unwind, then `RecoverHandler` writes 500 (test).
- Under Timeout, the goroutine recovers the panic and re-panics on the serving goroutine.

**404 default and router option order**
- `NewServer` prepends `WithNotFoundHandler(nil)`. That wraps `http.NotFoundHandler` with a Trace+Log chain and a `HeaderOnceResponseWriter` that writes 404.
- **`WithRouter` replaces the router after that default was applied**. Pass `WithNotFoundHandler` after `WithRouter` to keep trace/log on 404s.
- `patRouter` 405: the `notAllowed` handler, or `WriteHeader(405)` with `Allow`.

**Request access**
- net/http. When `Use` runs, `r.Body` can already be a gunzip reader.
- `pathvar.Vars(r)` gives params. If the route has no params, it is not set.

**Operation id:** the route path (native Trace and Prometheus handlers use `route.Path`).

**Gotchas**
1. There is no template in `Use`.
2. `Use` misses 404/405 and every native rejection.
3. The timeout goroutine lets the middleware outlive the response. The event status can disagree with the wire status.
4. Router option order matters.
5. `rest.NewServerless` (for tests) exists only from v1.9.0.

### 3.11 kratos v2 (v2.9.2, gorilla/mux v1.8.1)

**Three layers**
```go
type FilterFunc func(http.Handler) http.Handler
func Filter(filters ...FilterFunc) ServerOption                // wraps the whole router: Handler = FilterChain(srv.filters...)(srv.router)
func (s *Server) Route(prefix string, filters ...FilterFunc) *Router
func (r *Router) Handle(method, relativePath string, h HandlerFunc, filters ...FilterFunc) // type HandlerFunc func(khttp.Context) error
type middleware.Handler func(ctx context.Context, req any) (any, error)
type middleware.Middleware func(middleware.Handler) middleware.Handler
func Middleware(m ...middleware.Middleware) ServerOption; func (s *Server) Use(selector string, m ...middleware.Middleware)
transport.FromServerContext(ctx) (transport.Transporter, bool) // Operation() string
type khttp.Transporter interface { transport.Transporter; Request() *http.Request; PathTemplate() string }
func SetOperation(ctx context.Context, op string)
```

**Where the Transport comes from**
- `srv.filter()` is a gorilla-mux middleware, so it runs only for matched routes.
- It builds `*khttp.Transport{operation: pathTemplate, pathTemplate: mux.CurrentRoute(req).GetPathTemplate()}`.
- It also wraps the ctx in `context.WithTimeout(req.Context(), s.timeout)` (**default 1 s**) and passes the new request down.
- The global `Filter` runs before that. Test: transport absent in the Filter.
- Route-level filters (`Route(...)`/`Handle(..., filters...)`) run inside the mux route, so the transport is present. This is source-only. It is UNVERIFIED by test.

**Middleware scope**
- Handler code must call `ctx.Middleware(h)`. Without that call, no middleware runs.
- Generated code (`protoc-gen-go-http` template) does: Bind / BindQuery / BindVars (**errors return before middleware**), then `http.SetOperation(ctx, "/pkg.Service/Method")`, then `h := ctx.Middleware(...)`, then `ctx.Result(200, reply)`.
- Plain `srv.HandleFunc` routes get no middleware. Test: /plain, `mw=false`.

**Values seen in middleware (test)**
- `tr.Operation()` = "/api.v1.Users/Get".
- `PathTemplate()` = "/users/{id}".
- Only `err` is visible, not the status. The status comes from `errors.FromError(err).Code`/`.Reason`, as the kratos logging middleware does.

**Status**
- `Router.Handle` calls `r.srv.ene(res, req, err)` (`DefaultErrorEncoder` writes `se.Code`) after the handler, so after the middleware returns.
- The global Filter with its own writer sees the final status (test: 404).

**404/405**
- `NewServer` sets `router.NotFoundHandler = http.DefaultServeMux` **and** `router.MethodNotAllowedHandler = http.DefaultServeMux`.
- Test: `POST` to a GET route gives **404**. The default mux also serves anything registered globally, such as pprof.
- Override with `khttp.NotFoundHandler(h)` / `khttp.MethodNotAllowedHandler(h)`. Only the global Filter sees these requests.

**Panics:** no recover in the kratos server (net/http recovers and aborts the connection). `recovery.Recovery()` converts a panic to `ErrUnknownRequest` (500), but only inside `ctx.Middleware` scope.

**Context**
- In a Filter: `r.WithContext(ctx)`. The values survive `srv.filter()`, which derives from `req.Context()`.
- In middleware: call `h(newCtx, req)`. The new ctx reaches the service method, not the encoder.
- `khttp.Context` implements `context.Context` by delegating to `c.req.Context()`.

**Operation id:** `tr.Operation()` (proto full method for generated routes, the path template otherwise). `Server.Use` selectors match on it.

**Gotchas**
1. The global Filter never sees the template or operation.
2. Middleware never sees 404/405, bind errors, or non-generated routes.
3. 405 becomes 404 through `DefaultServeMux`.
4. The default 1 s timeout ctx can cancel emit work bound to the request ctx.

### 3.12 huma v2 (v2.39.1)

**Wrap API**
```go
API.UseMiddleware(middlewares ...func(ctx huma.Context, next func(huma.Context)))
type Middlewares []func(ctx Context, next func(Context)); Operation.Middlewares Middlewares
// Register: a.Handle(&op, api.Middlewares().Handler(op.Middlewares.Handler(func(ctx Context){...}))) (huma.go:881)
huma.Context: Operation() *Operation; Context() context.Context; Method(); Host(); RemoteAddr(); URL() url.URL; Param(); Query(name); Header(name);
              EachHeader(func(name, value string)); BodyReader() io.Reader; SetStatus(int); Status() int (v2.13.0+); SetHeader; AppendHeader; BodyWriter() io.Writer
func WithContext(ctx Context, override context.Context) Context  // v2.8.0+
func WithValue(ctx Context, key, value any) Context
Operation{OperationID, Method, Path ("/users/{id}"), Tags []string, Metadata map[string]any, DefaultStatus int, ...}
type StatusError interface{ GetStatus() int; Error() string }
var NewError, NewErrorWithContext // package-level overridable funcs
```

**Template and operation**
- `ctx.Operation()` is available before next. Test: OperationID "get-user", Path "/users/{id}".
- Huma middleware runs only for registered operations. Test: `/nope` had no huma middleware and chi middleware saw 404.

**Status**
- `ctx.Status()` after next. Test: 404 from `huma.Error404NotFound`.
- Handler errors are written inside huma (huma.go:1230-1245): a `StatusError` uses `GetStatus()`, anything else becomes `NewErrorWithContext(ctx, 500, "unexpected error occurred", err)`.
- The **error value is never passed to middleware**.

**Panics:** no recover in the core request path (`recover()` appears only in `transforms.go`, `validate.go`, and `humabunrouter`). Panics reach the router.

**Context**
- `huma.WithValue`/`WithContext` propagate to the underlying request: humachi `r.WithContext`, humagin `c.Request`, humaecho `SetRequest`, humafiber v3 `c.SetContext`, v2 `SetUserContext`. Test: the handler saw the value.
- `humafiber` builds `ctx` from `c.Context()`, so a fiber middleware's `SetContext` value is visible.

**Unwrap helpers**
- `humachi.Unwrap(ctx) (*http.Request, http.ResponseWriter)`
- `humagin.Unwrap(ctx) *gin.Context`
- `humaecho.Unwrap(ctx) *echo.Context` (v5) and `humaecho.UnwrapV4(ctx) echo.Context`
- `humafiber.Unwrap(ctx) fiber.Ctx` (v3) and `humafiber.UnwrapV2(ctx) *fiber.Ctx`
- `humahttprouter.Unwrap(ctx) (*http.Request, http.ResponseWriter, httprouter.Params)`

**Gotchas**
1. The huma layer alone misses 404/405 and panics. Pair it with the router adapter and use the huma middleware only to enrich (OperationID, template, tags).
2. **autopatch** (opt-in) issues internal GET/PUT sub-requests through `adapter.ServeHTTP`, with a ctx derived from the parent request ctx (autopatch.go:289, 415). Router middleware runs again and finds the parent event already in ctx.
3. `humafiber.StreamBody` uses `SetBodyStreamWriter`, so the size is unknown.
4. `humafiber` `ServeHTTP` goes through `app.Test` (test only).

## 4. Can one "fasthttp view" serve fasthttp, fiber v2, fiber v3 and hertz?

**fasthttp, fiber v2, fiber v3: yes. hertz: no, it needs its own view.**

- **Shared type.** `*fasthttp.RequestCtx` is reachable from all three: fasthttp directly, fiber v2 `c.Context()`, fiber v3 `c.RequestCtx()`. A shared view can cover:
  - request headers, query keys, cookie names, and remote addr
  - the buffered body (`PostBody` / `Body`)
  - `Response.StatusCode()`, `len(Response.Body())` / `IsBodyStream()`, and response headers
- **Per-framework parts stay thin.**
  - Route template: none / v2 pointer compare / v3 `FullPath` + `Matched`.
  - ctx get/set: `SetUserValue` / `SetUserContext` / `SetContext`.
  - Error-to-status: fiber must resolve the returned error through `ErrorHandler`, because the in-middleware status is 200. Use the logger pattern: call `ErrorHandler`, then return nil.
  - fiber v3 `SkipUnmatchedRoutes`.
  - fiber `Immutable` string copying.
- **API version constraint.** fiber v2.52.15 requires fasthttp v1.51.0. The iterator APIs need v1.63.0 (`go 1.23.0`, which matches the wlog root). `VisitAll*` works from v1.51.0 but is deprecated in v1.74.0 and trips SA1019.
  - Option: require fasthttp ≥ v1.63.0 in the view module. MVS then lifts fiber v2 users to it. The latest test module built fiber v2.52.15 against fasthttp v1.74.0 and its tests passed, so the newer fasthttp works with fiber v2.
  - Behavior of fiber v2 on fasthttp v1.63.x specifically is UNVERIFIED.
- **Why hertz differs.** Its request and response types come from `github.com/cloudwego/hertz/pkg/protocol`, forked from fasthttp but distinct. hertz does not depend on fasthttp. The method names are similar: `Header.VisitAll`, `Peek`, `StatusCode`, `Body`. A hertz view can copy the code shape of the fasthttp view. It cannot share the type. It also differs in status timing (404 status preset in middleware) and ctx propagation (`Next(ctx)`).
- **Lifetime rule for all four.** Pooled contexts and zero-copy bytes. The view must copy every string or `[]byte` into the event before the handler returns, and must emit (or hand off copied data) before return.

## 5. Adapter-breaking gotchas, condensed

1. **Status after an error is not final inside middleware:** echo v4/v5, fiber v2/v3, and kratos middleware. Resolve it from the error, or invoke the framework error handler and swallow the error.
2. **Default body or header written after middleware:** gin (404/405 body, `WriteHeaderNow`), hertz (404/405 body), go-zero (Recover 500, Timeout 503).
3. **Requests that skip middleware:**
   - gin and hertz redirects
   - httprouter redirects and auto OPTIONS
   - chi mux with no routes
   - fiber v3 `SkipUnmatchedRoutes`
   - go-zero 404/405 and native rejections
   - kratos middleware for 404/405, bind errors, and non-generated routes
   - huma middleware for 404/405
   - For full coverage, wrap at the outermost `http.Handler` / engine level (hertz: `server.WithTracer`).
4. **Wrong template on 404:** chi subrouter "/api/*", echo group "/api/*", fiber "/" (the middleware route), echo 405 keeps the template. If the status is 404/405, or the framework flags no match (`Matched()`, `RouteInfo.Name`), blank the template.
5. **Panics that kill the process:** fiber, fasthttp, hertz on the standard transport. Swallowed without a response: hertz on netpoll. Always observe with `defer`/`recover` and re-panic, and never block.
6. **Pooled contexts:** chi rctx, gin, echo, fiber, fasthttp, hertz. Read everything before the wrap returns and copy fasthttp-family bytes.
7. **Recover middleware differences:** echo v4 Recover calls the error handler and returns nil. echo v5 Recover returns an error. gin Recovery aborts with 500. go-zero Recover sits outside user middleware.
8. **Context replacement:**
   - net/http family: `WithContext`.
   - hertz: explicit `Next(ctx)`. fiber: `Set(User)Context`.
   - These are not seen by `fiber.Ctx.Value`, `hertz ctx.Value`, or `gin.Context.Value` (the last only with `ContextWithFallback`).
   - kratos adds a default 1 s timeout ctx. go-zero adds the route timeout ctx.
9. **Double events:** huma autopatch sub-requests, and nested mounts that re-enter a wrapped router. Detect an existing event in ctx.
10. **Mutable globals in frameworks:** huma `NewError` and `NewErrorWithContext` are package vars. kratos 404 goes to `http.DefaultServeMux`. The adapter must not rely on or change them.
