# Spec: HTTP routers and RPC (track A)

> Phase 12 · depends on: `http-core`, `work`, `core-calls`, `propagate`, the `http` and `work`
> conformance suites. Own-module ids: `http-chi`, `http-fiber`, `http-fiber3`, `http-fasthttp`,
> `http-httprouter`, `http-gozero`, `http-hertz`, `http-kratos`, and `http-huma`. Also
> `rpc-grpc`, `rpc-connect`, `rpc-gqlgen`, and `rpc-twirp`. Project-wide rules in
> [SPEC.md](SPEC.md) apply. Facts come from the API research of 2026-09-16, checked against source
> and runtime probes.

## Objective

Every popular Go router and RPC layer emits the same event as net/http. Route, status, level,
and error are right, including for 404s, framework errors, and panics. Each entry-point adapter
has a one-line setup. An adapter that adds to an open event reads the Logger from the context.

## Shared rules

1. Each adapter wraps at the outermost point that sees every request. Where the framework has a
   point that also sees redirects and rejections, `Setup` uses it: gin `Handler(engine)`, Hertz
   `server.WithTracer`, go-zero `rest.WithRouter`, Kratos `khttp.Filter`.
2. Each adapter reads every framework value before its handler returns, and copies it.
3. A framework whose status inside middleware is wrong until its error handler runs resolves the
   error the way that framework's own logger does. That covers Echo, Fiber, and Kratos.
4. A framework that crashes the process on a panic (Fiber, fasthttp, gRPC, Connect) still does
   under `PanicPolicy(Repanic)`. Under the default `Recover500` for HTTP, the adapter writes 500
   and stops the chain. RPC adapters always emit, then panic again, so each RPC library keeps its
   own panic behavior.
5. An RPC adapter inside an open request event adds to that event, and starts no new one. It sets
   `kind` to `rpc`, fills the `rpc` group, and replaces `operation`. The `http` group stays.
6. Library floors are the lowest API version that builds on the module's Go floor.
   `tools vuln` runs govulncheck on an upgraded copy of the build list, and the package doc names
   the lowest version with no known vulnerability. A `require` line is a minimum, so users are
   never pinned to it.

## HTTP routers

| Module | Wrap point and `Setup` | Route | Status, errors, panics | Library floor, module Go floor |
|---|---|---|---|---|
| `http-chi` | `r.Use(Middleware(log))`, before any route. `Setup(r chi.Router)` | `chi.RouteContext(ctx).RoutePattern()` after `next`, read before return. A `/*` pattern on 404 or 405 becomes `""` | `NetHTTP` writer | chi v5.0.0, Go 1.21 |
| `http-fasthttp` | `Middleware(log)(next fasthttp.RequestHandler)`. Exports `RequestView` and `ResponseView` over `*fasthttp.RequestCtx` for the Fiber modules | `RouteFunc(func(*fasthttp.RequestCtx) string)`, none by default | Final status and body after `next`. Unknown size for a body stream. Emits before return | fasthttp v1.63.0 (iterators), Go 1.23 |
| `http-fiber` | `app.Use(Middleware(log))` first. `Setup(app *fiber.App)` | Before `Next`, keeps its own route pointer. After `Next`, a different `c.Route()` pointer means an endpoint matched. `.Path` gives the template | A `Next` error goes to `app.ErrorHandler(c, err)`, then the middleware returns nil, like Fiber's logger. `c.SetUserContext` carries the context | fiber v2.20.0 with fasthttp v1.63.0, Go 1.23 |
| `http-fiber3` | same as `http-fiber`. With `SkipUnmatchedRoutes` set, 404s skip middleware, so `Setup` reports a problem | `c.FullPath()` with `c.Matched()` after `Next` | same, with `c.SetContext` | fiber v3.5.0, Go 1.25 |
| `http-httprouter` | `wloghttprouter.New(log, *httprouter.Router)` returns a wrapper whose `Handle`, `Handler`, and method helpers record the template. Its `ServeHTTP` covers redirects, 404, 405, and OPTIONS | the registered path | `NetHTTP` writer | httprouter v1.3.0, Go 1.21 |
| `http-gozero` | `rest.WithRouter(RouterOption(log, inner))`, passed before `rest.WithNotFoundHandler`. It records each template in `Handle` and wraps `ServeHTTP` | the path given to `Handle` | Sees Recover 500, Timeout 503, 404, 405, and native rejections. A handler still running after a timeout adds nothing, because the event already emitted | go-zero v1.3.0, Go 1.21 |
| `http-hertz` | `server.WithTracer(Tracer(log))` for every request, including redirects and 400s. `Setup` also adds `Middleware(log)` for panics and `c.Errors` | `ctx.FullPath()` in `Finish` | `ctx.Response.StatusCode()` in `Finish`, after Hertz writes default bodies. Context passes through `ctx.Next(newCtx)` | hertz v0.4.0, Go 1.21 |
| `http-kratos` | `khttp.Filter(Filter(log))` for every request. `khttp.Middleware(Middleware())` adds `operation`, `http.route`, and the Kratos `Reason` and `Metadata` | `tr.(khttp.Transporter).PathTemplate()` in middleware | Filter writer for the final status. `kerrors.FromError(err)` for code and reason. The event uses its own context, never Kratos's 1 second timeout context | kratos v2.8.4, Go 1.21 |
| `http-huma` | `api.UseMiddleware(Middleware())`, next to a router adapter from this track. It never starts an event | `ctx.Operation().Path`, and `OperationID` into `http.operation_id` | `ctx.Status()` after next. huma writes handler errors itself | huma v2.13.0, Go 1.21 |

- gin: `Handler(engine)` wraps `engine.ServeHTTP`, so redirects produce events too. The `http-core`
  spec covers the rest of gin.
- fasthttp family: `Immutable` off means every string is a view over a reused buffer. The views
  call `strings.Clone` on each value.
- huma autopatch sub-requests find the open event and add to it. They start no new event.
- `SkipUnmatchedRoutes` arrives in fiber v3.5.0, so `http-fiber3` has that library floor.
- A Kratos gRPC server takes the `rpc-grpc` interceptors through the Kratos `transport/grpc`
  options `UnaryInterceptor` and `StreamInterceptor`. They run inside the Kratos interceptor, so
  they see `transport.FromServerContext`.

## RPC

### rpc-grpc (package `wloggrpc`, google.golang.org/grpc)

<!-- snippet:sketch -->
```go
func ServerOptions(log *wlog.Logger, opts ...Option) []grpc.ServerOption // chained unary and stream interceptors
func DialOptions(opts ...Option) []grpc.DialOption                        // client interceptors
func UnknownServiceHandler(log *wlog.Logger) grpc.ServerOption            // unknown methods get an event and Unimplemented
func PropagateTrace(on bool) Option                                        // default true
```

- Server: one event per RPC. `rpc.system` `grpc`, and `rpc.service` and `rpc.method` split
  `FullMethod` at the last `/`. `rpc.peer` comes from `peer.FromContext`, and headers from
  incoming metadata.
- The status code comes from `status.FromError(err)`. For a non-status error, it comes from
  `status.FromContextError(err)`, as the gRPC server does. The level follows the `work` table.
- A stream interceptor wraps `grpc.ServerStream` with atomic `messages_sent` and
  `messages_received` counters, and overrides `Context()` to carry the event.
- A panic is recorded with its stack, the event emits, and the panic continues, because gRPC core
  has no recovery.
- Client: one `calls` record per logical call. Interceptors see the whole call, while stats
  handlers run once per attempt. `ctx.Err()` after the call tells a local cancel from a remote one.
- Propagation appends `traceparent` to outgoing metadata. With an OTel stats handler present,
  `PropagateTrace(false)` leaves injection to OTel, and `trace-otel` supplies the span ids.
- Unknown service or method calls never reach interceptors. `UnknownServiceHandler` gives them an
  event. The package doc says so.
- Error details: `ErrorInfo.Reason` gives `code`, `LocalizedMessage` gives `message`, and
  `status.Message()` gives `why`. `Help.Links[0].Url` gives `link`. `RetryInfo` and
  `BadRequest` give `fix`. `data` holds domain, metadata, field violations, and retry delay.
  `DebugInfo` is never read.
- `Extractor()` returns the `wlog.ErrorExtractor` that reads a status and its details into
  the event error. Configure it with `wlog.WithErrorExtractor`.
- Floor: grpc v1.67.3, the newest on Go 1.21. The package doc says v1.83.2 is the first version
  with no known vulnerability.

### rpc-connect (package `wlogconnect`, connectrpc.com/connect)

<!-- snippet:sketch -->
```go
func Interceptor(log *wlog.Logger, opts ...Option) connect.Interceptor // server and client
```

- One interceptor serves both sides, told apart by `Spec().IsClient`. The procedure splits at the
  last `/`. `Peer.Protocol` goes to `rpc.protocol`.
- The code comes from `errors.As` into `*connect.Error`, then `context.Canceled` and
  `context.DeadlineExceeded`, then `Unknown`. `connect.CodeOf` is never used, because it reports
  `Unknown` for a canceled context.
- Early HTTP errors (405, 415, bad JSON) never reach interceptors. `Setup` docs wrap the Connect
  handler with `wlogstd.Middleware` too, and the interceptor then adds to that request event.
- A client interceptor adds context values only, and never calls `connect.NewClientContext`.
- Stream wrappers use atomic counters and `sync.Once` to finish.
- Floor: connect v1.18.1, Go 1.21.

### rpc-gqlgen (package `wloggqlgen`, 99designs/gqlgen)

<!-- snippet:sketch -->
```go
func Extension(opts ...Option) graphql.HandlerExtension // OperationInterceptor and ResponseInterceptor
func Recover(next graphql.RecoverFunc) graphql.RecoverFunc
```

- The extension adds to the open request event. `rpc.system` `graphql`. `rpc.method` is the
  operation name from `Operation.Name`, then `OperationName`, then `anonymous`. `rpc.graphql.type` is
  `query`, `mutation`, or `subscription`.
- It records `rpc.graphql.errors_count`, `rpc.graphql.partial`, and complexity from
  `extension.GetComplexityStats`, after `graphql.HasOperationContext` is true.
- Parse, validation, complexity, and bad JSON errors give level `warn`. A resolver panic gives
  `error`. Other resolver errors go through the error extractor, and the highest level wins.
- It never records `Variables`, field arguments, or `RawQuery`. It records a SHA-256 of the query.
  A bad JSON body error holds the whole request body, so that case records a fixed message only.
- `Recover` wraps the user's recover function, and never replaces it.
- Floor: gqlgen v0.17.49 with gorilla/websocket v1.5.3, Go 1.21.

### rpc-twirp (package `wlogtwirp`, twitchtv/twirp)

<!-- snippet:sketch -->
```go
func ServerHooks(opts ...Option) *twirp.ServerHooks
func ClientHooks(opts ...Option) *twirp.ClientHooks
```

- Server hooks add to the request event from `wlogstd.Middleware` around the Twirp handler, which
  supplies peer and headers. `RequestRouted` sets package, service, and method. `ResponseSent`
  finishes, because it fires on success, error, bad route, and panic.
- The code maps by Twirp code name to the `work` table, never by Twirp's HTTP status. A
  `context.Canceled` or `DeadlineExceeded` behind an `internal` error maps to its real code.
- The client accepts an `Error` hook with no `RequestPrepared` before it.
- Floor: twirp v8.1.0, Go 1.21.

## Success criteria

1. Every HTTP router module passes the `http` suite. The huma module passes it together with the
   chi adapter.
2. `rpc-grpc`, `rpc-connect`, and `rpc-twirp` run the `work` and `calls` scenarios with their own
   fixtures, because those suites take a generic unit that an RPC library cannot build. Each
   module proves the server fields and the client call record, with unary and streaming fixtures
   where the library has them.
3. A gRPC call with `ErrorInfo`, `Help`, `LocalizedMessage`, and `BadRequest` details gives an
   event with code, message, why, link, fix, and data per this spec.
4. A Connect request with a malformed body produces one event with status 400 through the HTTP
   middleware.
5. A GraphQL request with a password in `variables` and in an inline literal never shows the
   password. A bad JSON body shows only the fixed message.
6. A Fiber 404 and a Fiber handler error log their final statuses, and the response bytes match a
   run without wlog.
7. Hertz and gin redirects each produce one event with status 301.
8. A go-zero route that times out logs 503.
9. Each module passes `tools floor` at its library floor.

## Testing

Each module has black-box tests with the suites, plus runtime fixtures for each gotcha named
above. gRPC tests use `bufconn`. Connect, Twirp, and gqlgen use `httptest`.

## Boundaries

- **Always:** keep each library's own panic behavior for RPC.
- **Ask first:** reading any RPC field this spec marks as never read.
- **Never:** record GraphQL variables, field arguments, raw queries, or RPC request bodies by
  default.

## Open questions

None.
