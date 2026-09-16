# RPC integration research: gRPC, Connect, Twirp, gqlgen, Kratos

Date: 2026-09-16. Toolchain: go1.26.1 darwin/arm64. Purpose: facts for the `work` kit (one event per RPC, message, job, or command) and `core-calls` (outbound calls as timed sub-operations with W3C traceparent).

## How this was checked

- Throwaway module: `scratchpad/research/rpc-work/` (module `example.com/rpcwork`).
- Versions came from `go list -m -versions` and `go mod download -json <module>@latest`. Source was read in `~/go/pkg/mod`.
- Go directive history came from the `.mod` file of every stable version on proxy.golang.org (`rpc-work/gofloor.sh`).
- Runtime probes, all run and passing. Their output backs every "runtime verified" claim below:
  - `rpc-work/grpcprobe/main.go`: bufconn server and client, interceptors, stats handler, status details, unknown method, bidi stream.
  - `rpc-work/connectprobe/main.go`: httptest server and client, interceptors, codes, details, malformed body, panic.
  - `rpc-work/connectctx/main.go`: which context changes a Connect client interceptor can make.
  - `rpc-work/twirpprobe/main.go`: uses the generated `github.com/twitchtv/twirp/example` Haberdasher service. Covers hook order, codes, bad route, panic, and a canceled client.
  - `rpc-work/gqlprobe/main.go`: uses `graphql/handler/testserver`. Covers hook order, operation data, parse, validation, bad JSON, complexity, resolver errors, and a panic.
  - `rpc-work/kratosprobe/main.go`: real TCP loopback. Covers transport context, header propagation, error round trip, and stream middleware scope.
- Floor checks: `rpc-work/floors/check.sh` compiles an adapter-shaped sketch (`floors/sketches/*.go.txt`) against each candidate version. The module starts at `go 1.20`. `go mod tidy` raises the line only for a dependency that needs a higher one. `floors/vuln.sh` runs `govulncheck -scan module`. Symbol mode was also run for some candidates.
- Anything not verified from source or a probe is marked **UNVERIFIED**.

---

## 0. Summary table

| Library | Module path | Latest stable | License | `go` line at latest | API floor (sketch compiles) | govulncheck-clean floor |
|---|---|---|---|---|---|---|
| gRPC-Go | `google.golang.org/grpc` | v1.83.2 | Apache-2.0 | 1.25.0 | v1.57.0 (Go line 1.20 after tidy) | v1.83.2 only (Go 1.25.0) |
| rpc error details | `google.golang.org/genproto/googleapis/rpc` (pkg `errdetails`) | pseudo only: v0.0.0-20260911204522-f61a6ca850bd | Apache-2.0 | 1.25.0 | use the version gRPC requires | n/a |
| Connect | `connectrpc.com/connect` | v1.21.0 | Apache-2.0 | 1.25.0 | v1.9.1 (Go 1.20) | v1.16.2 (Go 1.20) |
| Twirp | `github.com/twitchtv/twirp` | v8.1.3+incompatible (2022-10-24) | Apache-2.0 | no go.mod | v7.1.0 for interceptors. v8.1.0 for current generated code | v8.1.0 (no findings) |
| gqlgen | `github.com/99designs/gqlgen` | v0.17.95 | MIT | 1.26 | v0.17.49 (Go 1.20). v0.17.20 and v0.17.36 fail | v0.17.49 plus `github.com/gorilla/websocket v1.5.3` (Go 1.20) |
| Kratos | `github.com/go-kratos/kratos/v2` | v2.9.2 (2025-12-05) | MIT | 1.22 | v2.5.0 verified. Older versions not tested | none. GO-2026-5471 has no fix (`transport/http` only) |

gqlparser (`github.com/vektah/gqlparser/v2` v2.5.37) is a gqlgen dependency. Its license is **UNVERIFIED**.

### Go directive history (proxy `.mod` files, shows where the line changes)

- grpc: v1.24.0 1.11, v1.41.0 1.14, v1.49.0 1.17, v1.58.0 1.19, v1.65.0 1.21, v1.68.0 1.22.7, v1.72.0 1.23, v1.76.0 1.24.0, v1.81.0 1.25.0.
- connect: v1.5.2 1.19, v1.15.0 1.20, v1.17.0 1.21, v1.19.0 1.24.0, v1.20.0 1.25.0. v1.9.0 and v1.10.0 are retracted ("module cache poisoned").
- gqlgen: v0.17.25 1.18, v0.17.44 1.20, v0.17.50 1.22.5, v0.17.65 1.22.12, v0.17.67 1.23.0, v0.17.71 1.23.8, v0.17.73 1.23.0, v0.17.79 1.24.0, v0.17.87 1.25, v0.17.95 1.26. v0.17.95 needs Go 1.26 because it calls `errors.AsType` (seen in `graphql/error.go`).
- kratos: v2.6.3 1.19, v2.8.0 1.20, v2.8.4 1.21, v2.9.2 1.22. v2.9.2 requires grpc v1.61.1.
- twirp: no go.mod, so it has no Go line. The root package imports only stdlib and `internal/contextkeys`. It uses `interface{}`.

### Candidate floors per wlog root Go floor

The adapter module requires the wlog root too, so its `go` line is the higher of the root floor and the library floor.

| Library | Root at Go 1.21 (planned) | Root at Go 1.23 (current) | Lowest API floor |
|---|---|---|---|
| grpc | v1.67.3 (tidy line 1.21) | v1.75.1 (tidy line 1.23.0) | v1.57.0 |
| connect | v1.18.1 (1.21, clean) | v1.18.1 (v1.19.0 needs 1.24) | v1.16.2 is the lowest clean one |
| twirp | v8.1.0 | v8.1.0 | v8.1.0 |
| gqlgen | v0.17.49 (+ websocket v1.5.3) | v0.17.78 (+ websocket v1.5.3) | v0.17.49 |
| kratos | v2.8.4 | v2.9.2 | v2.5.0 verified |

### govulncheck findings (module mode, stdlib findings from the go1.26.1 host left out)

- **grpc** at v1.57.0, v1.58.3, v1.64.1, v1.67.3, and v1.75.1:
  - GO-2023-2153: HTTP/2 Rapid Reset. Affects versions below 1.56.3, 1.57.0, and 1.58.0 to 1.58.2.
  - GO-2026-4762 (CVE-2026-33186): authorization bypass through a `:path` with no leading slash. Fixed in 1.79.3.
  - GO-2026-6061: xDS RBAC and HTTP/2 transport server. Fixed in 1.82.1.
  - GO-2026-6348 (CVE-2026-84304): out-of-memory through HTTP/2 DATA frame fragmentation. Fixed in 1.83.1.
  - GO-2026-6441 (CVE-2026-84303): xDS RBAC header matching bypass. Fixed in 1.83.1.
  - GO-2026-6443 (CVE-2026-84445): server panic on a request with no authority or Host header. Fixed in 1.82.2 and 1.83.2. Versions 1.83.0 and 1.83.1 are affected.
  - Old x/net, x/text, and protobuf versions also appear at the low floors.
  - **Symbol mode** on the tiny interceptor sketch at v1.75.1 still reaches GO-2026-6061 and GO-2026-6348. At v1.83.2 it is clean. **A govulncheck CI gate fails for any grpc require below v1.83.2.**
- **connect**: v1.9.1 and v1.14.0 pull protobuf below 1.33 (GO-2024-2611). v1.16.2, v1.18.1, v1.19.2, and v1.21.0 are clean. Connect itself has no findings.
- **twirp** v8.1.0 and v8.1.3: no findings.
- **gqlgen**: v0.17.49 and v0.17.78 pull gorilla/websocket v1.5.0 (GO-2026-6278, fixed in v1.5.3). v0.17.64 also pulls go-viper/mapstructure (GO-2025-3787, GO-2025-3900). Requiring websocket v1.5.3 makes v0.17.49 and v0.17.78 clean. gqlgen and gqlparser have no findings of their own.
- **kratos** v2.7.3, v2.8.4, and v2.9.2:
  - GO-2026-5471 (CVE-2026-6993): "Confused Deputy". It lists only the package `github.com/go-kratos/kratos/v2/transport/http` and has no fixed version.
  - The grpc findings above also appear through kratos's own grpc require.
  - Symbol mode on the sketch at v2.9.2 reports only GO-2026-6061 and GO-2026-6348. It does not report 5471, because a gRPC-only adapter never imports `transport/http`.

**Decision for the lead (a dependency choice, so "ask first")**
- CAPABILITIES says each `go` line is the lowest the code needs, and CI runs govulncheck. For gRPC those two rules conflict.
- Option A: require grpc v1.83.2. The Go floor becomes 1.25.0 for the grpc and kratos adapters.
- Option B: require the API floor that matches the root Go floor, for example v1.67.3 at Go 1.21. Run govulncheck in CI on an upgraded copy of the build list (`go get -u ./... && govulncheck`), and tell users to run a current grpc.
- Recommendation: B. A require line is only a minimum. It never pins users to a vulnerable version, and B keeps "old Go keeps working."

---

## 1. gRPC-Go (`google.golang.org/grpc` v1.83.2)

### 1.1 Module facts
- Apache-2.0. `go 1.25.0`. Requires `google.golang.org/genproto/googleapis/rpc` (pseudo version) and `google.golang.org/protobuf v1.36.11`.
- API floor facts (checked with grep on downloaded versions):
  - `status.FromError` unwraps with `errors.As` from **v1.55.0**. v1.54.0 does not.
  - `grpc.OnFinish` CallOption from **v1.54.0**. v1.53.0 does not have it.
  - `stats.InPayload/OutPayload.CompressedLength` from **v1.54.0**.
  - `metadata.ValueFromIncomingContext` is present in v1.50.0 and absent in v1.45.0.
  - Several `grpc.StatsHandler` options add up (`statsHandlers = append`) in v1.50.0 but not in v1.45.0.
  - `grpc.Method(ctx)`, `StreamServerInfo.IsClientStream`, and `InHeader.RemoteAddr` are present in v1.45.0.
  - **v1.57.0** is the first version that requires the split module `google.golang.org/genproto/googleapis/rpc`. v1.55 and v1.56 require the old monolithic `google.golang.org/genproto`. The "ambiguous import" risk with both genproto forms in one build is **UNVERIFIED** here, but it is a known problem. That is why the recommended API floor is v1.57.0 and not v1.55.0.

### 1.2 Hook signatures (from `interceptor.go`, `stats/handlers.go`)
```go
type UnaryServerInterceptor func(ctx context.Context, req any, info *UnaryServerInfo, handler UnaryHandler) (resp any, err error)
type StreamServerInterceptor func(srv any, ss ServerStream, info *StreamServerInfo, handler StreamHandler) error
type UnaryClientInterceptor func(ctx context.Context, method string, req, reply any, cc *ClientConn, invoker UnaryInvoker, opts ...CallOption) error
type StreamClientInterceptor func(ctx context.Context, desc *StreamDesc, cc *ClientConn, method string, streamer Streamer, opts ...CallOption) (ClientStream, error)

type UnaryServerInfo struct { Server any; FullMethod string }   // "/package.service/method"
type StreamServerInfo struct { FullMethod string; IsClientStream bool; IsServerStream bool }

// stats package ("All APIs are experimental" per package doc)
type Handler interface {
    TagRPC(context.Context, *RPCTagInfo) context.Context
    HandleRPC(context.Context, RPCStats)
    TagConn(context.Context, *ConnTagInfo) context.Context
    HandleConn(context.Context, ConnStats)
}
```
- Server options: `grpc.ChainUnaryInterceptor(...)`, `grpc.ChainStreamInterceptor(...)`, `grpc.StatsHandler(h)`. StatsHandler appends and ignores nil.
- Dial options: `grpc.WithChainUnaryInterceptor(...)`, `grpc.WithChainStreamInterceptor(...)`, `grpc.WithStatsHandler(h)`. The doc says the first interceptor is the outermost. `WithUnaryInterceptor` is always put in front of the chain.
- RPCStats types:
  - `Begin`: `IsClientStream`, `IsServerStream`, `IsTransparentRetryAttempt`, `FailFast`.
  - `InHeader` (server side): `FullMethod`, `RemoteAddr`, `LocalAddr`, `Header`, `Compression`, `WireLength`.
  - `OutHeader` (client side): `FullMethod`, `RemoteAddr`.
  - `InPayload` and `OutPayload`: `Length`, `CompressedLength`, `WireLength`, `Payload`.
  - `InTrailer`: `WireLength`, client `Trailer`.
  - `OutTrailer`: `WireLength` is deprecated and never set.
  - `End`: `BeginTime`, `EndTime`, `Error`, deprecated `Trailer`.
  - `DelayedPickComplete`.

### 1.3 Getting the data
- **Full method**: `info.FullMethod` in server interceptors, `grpc.Method(ctx)` in any server context, and the `method` argument in client interceptors. In stats: `RPCTagInfo.FullMethodName`, `InHeader.FullMethod` (server), and `OutHeader.FullMethod` (client). Runtime verified: `/probe.Probe/Echo`.
- **Service and method**: split at the last `/`. The server does `strings.CutPrefix(stream.Method(), "/")` and then `LastIndex("/")`. Parse loosely. Before v1.79.3 a path without a leading slash reached handlers (GO-2026-4762).
- **Peer (server)**: `peer.FromContext(ctx)` gives `Peer{Addr, LocalAddr, AuthInfo}`. The server calls `peer.NewContext` for every connection. Runtime verified inside interceptors. Stats alternative: `InHeader.RemoteAddr`.
- **Peer (client)**: add `grpc.Peer(&p)` to `opts` before `invoker`. `p.Addr` is filled after the call (runtime verified). `cc.Target()` and `cc.CanonicalTarget()` give the dial target. Stats alternative: `OutHeader.RemoteAddr`.
- **Read request metadata (server)**:
  - `metadata.FromIncomingContext(ctx)` returns a copy with lowercase keys.
  - `metadata.ValueFromIncomingContext(ctx, key)` matches keys without case and returns a copy.
  - Runtime verified: the `traceparent` sent by the client was read with key `"TraceParent"`.
- **Write propagation headers (client)**:
  - `metadata.AppendToOutgoingContext(ctx, "traceparent", v)` lowercases keys and appends. If the key is already present, the metadata ends up with two values.
  - `metadata.NewOutgoingContext` overwrites everything added before.
- **Response headers and trailers**:
  - Server: `grpc.SetHeader(ctx, md)`, `grpc.SendHeader`, `grpc.SetTrailer`, or `ServerStream.SetHeader/SetTrailer`.
  - Client: the `grpc.Header(&md)` and `grpc.Trailer(&md)` CallOptions, or `ClientStream.Header()` and `ClientStream.Trailer()`.
- **Status code**: `status.FromError(err)` returns `(*Status, ok)`. A non-status error gives `Unknown` with `ok=false`. When the status error is wrapped, the returned message is the **whole `err.Error()` text**.
  - What the server does (`server.go` processUnaryRPC and processStreamingRPC): if `!ok`, it calls `status.FromContextError(err)`. That maps `context.Canceled` to Canceled, `DeadlineExceeded` to DeadlineExceeded, and anything else to Unknown. **The adapter must copy this.**
  - Runtime verified: a handler that returned `context.Canceled` showed `ok=false` to the interceptor. The mapped and wire code was Canceled.
- **Error details**: `st.Details() []any`. Each item is a proto message. For an Any type that is not registered, the item is an `error` instead. Importing `google.golang.org/genproto/googleapis/rpc/errdetails` registers the types. Runtime verified: ErrorInfo, Help, LocalizedMessage, and BadRequest round-trip to the client.
- **Message sizes**: only through `stats.Handler` (`InPayload`/`OutPayload` `Length`, `CompressedLength`, `WireLength`, and `InHeader.WireLength`). Interceptors do not see sizes. Runtime verified: `len=4 wire=9`.
- **Stream message counts**:
  - Server: wrap `grpc.ServerStream` and count `SendMsg` and `RecvMsg` (runtime verified: recv=3, sent=3). Or count `InPayload` and `OutPayload` in a stats handler.
  - Client: wrap `grpc.ClientStream`.
  - Stream kind: `StreamServerInfo.IsClientStream/IsServerStream` on the server, `StreamDesc.ClientStreams/ServerStreams` on the client.

### 1.4 Codes
From `codes/codes.go`:
- OK 0, Canceled 1, Unknown 2, InvalidArgument 3, DeadlineExceeded 4, NotFound 5.
- AlreadyExists 6, PermissionDenied 7, ResourceExhausted 8, FailedPrecondition 9, Aborted 10, OutOfRange 11.
- Unimplemented 12, Internal 13, Unavailable 14, DataLoss 15, Unauthenticated 16.

See section 6 for levels.

### 1.5 Panics
- gRPC core has **no recover** around handlers. `rg "recover\(\)"` over non-test code finds only `internal/xds/clients/xdsclient/channel.go`.
- A handler panic crashes the process. So the event is lost unless the interceptor defers a recover.
- Recovery middleware exists only outside core (go-grpc-middleware). Its details are **UNVERIFIED**.

### 1.6 Gotchas
1. **Unknown service or method never reaches interceptors.** Runtime verified: interceptor call delta 0 for `/probe.Probe/Nope`. The stats handler does get `TagRPC` and `InHeader`, but **no `End`** (runtime verified: no End printed). The exception is `grpc.UnknownServiceHandler`, which routes the call through `processStreamingRPC` and the stream interceptors. Result: without an HTTP-level wrapper, Unimplemented calls from unknown methods produce no wide event.
2. **Server order**: `TagRPC` and `InHeader` run in `handleStream` before method lookup and before interceptors (source and runtime). When a wlog interceptor runs, the span that `otelgrpc.NewServerHandler` extracts is already in `ctx`.
3. **Client stats run once per attempt**: `newAttemptLocked` calls `TagRPC` and `Begin`, and `csAttempt.finish` calls `End`. A transparent retry gives several Begin and End pairs for one call (`Begin.IsTransparentRetryAttempt`). Client interceptors wrap the whole logical call. Record `core-calls` from interceptors.
4. **otelgrpc v0.71.0 has no interceptors anymore.** It offers only `NewServerHandler` and `NewClientHandler` (stats handlers). Its client `TagRPC` runs `inject`, which is `FromOutgoingContext`, then `md.Set(key, v)`, then `metadata.NewOutgoingContext`. It runs per attempt and **after** client interceptors, so it **replaces** a `traceparent` that wlog appended. The span id on the wire is then the otel attempt span, not the one wlog recorded. Suggestion: when a global OTel propagator or otelgrpc is present, let it own injection and have wlog read the span from ctx.
5. **Wrapped status errors leak wrapper text to clients**. Runtime verified: the client got `msg="wrapped: rpc error: code = InvalidArgument desc = bad email"`.
6. **ServerStream concurrency**: the docs say `SendMsg` and `RecvMsg` can run at the same time on different goroutines. Counters on a wrapper must be atomic (gate G2).
7. **To put the event into a stream's ctx**, wrap `ServerStream` and override `Context()`. Kratos's `wrappedStream` does this.
8. **Client stream end is hard to detect**: the call is done at the first non-nil `RecvMsg` result (`io.EOF` means success). Or use `grpc.OnFinish(func(err error))`, which is **experimental**, runs exactly once, is "mainly used by streaming interceptors", and exists from v1.54. If the caller drops a stream without canceling ctx, it never finishes (gRPC's own contract).
9. `ClientStream.Context()` turns off client-side retries. The docs say to call it only after `Header` or `RecvMsg` returns. Do not call it early in a wrapper.
10. Remote `Canceled` and local cancel look the same by code. Runtime verified: a server that returned `context.Canceled` gave the client `Canceled`. Check `ctx.Err()` after `invoker` to tell our own cancel from a remote one.
11. The `stats` package is marked experimental. Interceptor APIs are not.

---

## 2. Connect (`connectrpc.com/connect` v1.21.0)

### 2.1 Module facts
- Apache-2.0. `go 1.25.0`. Requires only `google.golang.org/protobuf` and `go-cmp`.
- Version facts (grep):
  - `Spec.Schema` from v1.13.0.
  - `CallInfo`, `NewClientContext`, `CallInfoForHandlerContext`, and the client context sentinel check from **v1.19.0**.
  - `WithRequestGate` from v1.21.0.
  - `WithRecover`, `Peer.Query`, `IsWireError`, and `Spec.IdempotencyLevel` are present in v1.9.1.
- Connect does not depend on genproto. Typed `errdetails` needs our own require on `google.golang.org/genproto/googleapis/rpc`. The floor check pinned `v0.0.0-20230525234030-28d5490b6b19`, the version grpc v1.57.0 uses.

### 2.2 Hook signatures (`interceptor.go`, `connect.go`)
```go
type UnaryFunc func(context.Context, AnyRequest) (AnyResponse, error)
type StreamingClientFunc func(context.Context, Spec) StreamingClientConn
type StreamingHandlerFunc func(context.Context, StreamingHandlerConn) error
type Interceptor interface {
    WrapUnary(UnaryFunc) UnaryFunc
    WrapStreamingClient(StreamingClientFunc) StreamingClientFunc
    WrapStreamingHandler(StreamingHandlerFunc) StreamingHandlerFunc
}
type UnaryInterceptorFunc func(UnaryFunc) UnaryFunc // streaming methods are no-ops

type AnyRequest interface { Any() any; Spec() Spec; Peer() Peer; Header() http.Header; HTTPMethod() string; internalOnly(); setRequestMethod(string) }
type AnyResponse interface { Any() any; Header() http.Header; Trailer() http.Header; internalOnly() }
type Spec struct { StreamType StreamType; Schema any; Procedure string; IsClient bool; IdempotencyLevel IdempotencyLevel }
type Peer struct { Addr string; Protocol string; Query url.Values /* server-only */ }
```
- Register with `connect.WithInterceptors(...)`. It works for handlers and clients. The first interceptor acts first (`newChain` reverses the slice).
- One `WrapUnary` serves both sides. Tell them apart with `req.Spec().IsClient`.
- The unexported methods mean tests cannot fake `AnyRequest`. Use `connect.NewRequest`.
- `StreamingHandlerConn`: `Spec`, `Peer`, `Receive(any)`, `RequestHeader`, `Send(any)`, `ResponseHeader`, `ResponseTrailer`. The read side can run at the same time as the write side.
- `StreamingClientConn`: `Spec`, `Peer`, `Send`, `RequestHeader`, `CloseRequest`, `Receive`, `ResponseHeader`, `ResponseTrailer`, `CloseResponse`.

### 2.3 Getting the data (runtime verified unless noted)
- **Procedure**: `Spec().Procedure` = `/probe.v1.ProbeService/Echo`. `StreamType.String()` returns `unary`, `client`, `server`, or `bidi`.
- **Service and method**: split `Procedure` at the last `/`. For protobuf, `Spec.Schema` is a `protoreflect.MethodDescriptor` (source comment).
- **Peer**: `Peer.Addr` on the server is the client `IP:port`. On the client it is the host or host:port from the URL. `Peer.Protocol` is `connect`, `grpc`, or `grpcweb`.
- **Request headers**: read with `req.Header()` on the server. Write on the client with `req.Header().Set("Traceparent", ...)` before `next`, and the server received it. Streaming client: `conn.RequestHeader()` before the first `Send`.
- **Response headers**: `res.Header()` and `res.Trailer()`. On the handler side `CallInfoForHandlerContext(ctx)` (v1.19+) also works.
- `HTTPMethod()`: POST or GET on the server. On the client it stays empty until after `next`.
- **Code**: see gotcha 2 about `connect.CodeOf`. Codes 1 to 16 use the same numbers and names as gRPC. `Code.String()` gives `invalid_argument` and so on.
- **Error**: `*connect.Error` has `Code()`, `Message()` (the underlying error text), `Details() []*ErrorDetail`, `Meta() http.Header`, and `connect.IsWireError(err)`.
  - `ErrorDetail.Type()` returns names like `google.rpc.ErrorInfo`. `ErrorDetail.Value()` returns `(proto.Message, error)`.
  - Runtime verified: ErrorInfo and Help details arrived as `*errdetails.ErrorInfo` and `*errdetails.Help`.
  - On the client, `Meta()` holds the response headers (Content-Type, Date, and so on).
- **HTTP mapping** (`protocol_connect.go connectCodeToHTTP`): the same as the google.rpc table in section 6. Canceled is 499. Unknown code values map to 500.
- **Message sizes**: not exposed. `AnyRequest` and `AnyResponse` have no size accessor. Wrapping `http.Handler` or `http.RoundTripper` for byte counts is **UNVERIFIED**.
- **Stream counts**:
  - Server: wrap `StreamingHandlerConn` `Receive` and `Send` with atomic counters. At the end of the client stream, `Receive` returns an error that wraps `io.EOF` (doc).
  - Client: wrap `StreamingClientConn`. The call ends at the first of `Receive` returning an error (`io.EOF` means success) or `CloseResponse`. Guard with `sync.Once`.

### 2.4 Panics
- The docs say that handlers do not recover from panics by default.
- Runtime: `net/http` recovered and logged `http: panic serving`. The code after `next` in the interceptor never ran. The client got `unavailable: unexpected EOF` with `IsWireError=false`.
- Opt-in: `connect.WithRecover(func(ctx, Spec, http.Header, any) error)`. It does not handle `http.ErrAbortHandler`.

### 2.5 Gotchas
1. **Unary handler interceptors run only after the request is read and decoded.** `NewUnaryHandler` calls `receiveUnaryRequest` and then the chain. Before that, `Handler.ServeHTTP` can return early with no interceptor call:
   - 505 for bidi on HTTP/1.
   - 405 for a bad method.
   - 415 for an unsupported content type.
   - Timeout header parse errors.
   - Request gates (v1.21).
   - Decode errors.
   Runtime verified: malformed JSON gave HTTP 400 and interceptor call delta 0. Result: wrap the Connect handler with the wlog HTTP middleware too, or those requests have no event. Also, `conn.Send(response)` runs after the interceptor returns, so write and marshal errors are not seen.
2. **`connect.CodeOf` is not safe to use as is.** It returns `CodeUnknown` for `nil`. It also returns `CodeUnknown` for a raw `context.Canceled` that the wire later reports as `canceled`. Runtime verified: `CodeOf=unknown`, wire code `canceled`. Adapter mapping:
   - `nil` is OK.
   - Next try `errors.As(*connect.Error)`.
   - Next `errors.Is(context.Canceled)` or `errors.Is(context.DeadlineExceeded)`. `wrapIfContextError` also treats `os.ErrDeadlineExceeded` as DeadlineExceeded.
   - Anything else is Unknown.
3. **Client interceptors must not create a new client context.** Calling `connect.NewClientContext` inside an interceptor fails the call with `creating a new context in an interceptor is prohibited` (v1.19+, runtime verified). `context.WithValue` is fine (runtime verified).
4. On the client, `IsWireError` tells a remote error from a local one. A transport EOF after a server panic is a local error.
5. Server-streaming clients call `CloseResponse` only from the user's `Close()` call (source: `ServerStreamForClient.Close`). Do not wait for it.
6. otelconnect (`connectrpc.com/otelconnect`) also injects `traceparent` through an interceptor. How it interacts with wlog is **UNVERIFIED**. Expect a clash like the one with otelgrpc.

---

## 3. Twirp (`github.com/twitchtv/twirp` v8.1.3+incompatible)

### 3.1 Module facts
- Apache-2.0. No go.mod, so there is no Go line and the version has the `+incompatible` suffix.
- Last tag 2022-10-24. `gh api repos/twitchtv/twirp`: `archived:false`, `pushed_at:2024-08-05`.
- `interceptors.go` first appears in **v7.1.0**. v8.1.0 adds `TwirpPackageMinVersion_8_1_0`, which code from protoc-gen-twirp v8.1 checks at compile time. Recommended floor: v8.1.0.
- Twirp has no streaming, so there are no message counts.

### 3.2 Hook signatures
```go
type ServerHooks struct {
    RequestReceived  func(context.Context) (context.Context, error)
    RequestRouted    func(context.Context) (context.Context, error)
    ResponsePrepared func(context.Context) context.Context
    ResponseSent     func(context.Context)
    Error            func(context.Context, Error) context.Context
}
type ClientHooks struct {
    RequestPrepared  func(context.Context, *http.Request) (context.Context, error)
    ResponseReceived func(context.Context)
    Error            func(context.Context, Error)
}
type Interceptor func(Method) Method
type Method func(ctx context.Context, request interface{}) (interface{}, error)
```
Options: `twirp.WithServerHooks`, `twirp.ChainHooks`, `twirp.WithServerInterceptors`, `twirp.WithClientHooks`, `twirp.ChainClientHooks`, `twirp.WithClientInterceptors`.

### 3.3 Hook order (runtime verified with the generated example service)

| Case | Server sequence | Client sequence |
|---|---|---|
| success | RequestReceived → RequestRouted → interceptor → ResponsePrepared → ResponseSent (StatusCode "200") | RequestPrepared → ResponseReceived |
| handler returns twirp error | RequestReceived → RequestRouted → interceptor → Error (StatusCode "400") → ResponseSent | RequestPrepared → Error |
| bad route | RequestReceived → Error(`bad_route`, 404, no method name) → ResponseSent | n/a |
| panic | RequestReceived → RequestRouted → Error(`internal`, "Internal service panic") → ResponseSent → **re-panic** (net/http logs it). The interceptor's code after `next` does not run | RequestPrepared → Error(internal) |
| client ctx already canceled | n/a | **Error only**, with no RequestPrepared: `internal`, "aborted because context was done: context canceled" |

Generated code (protoc-gen-twirp `generator.go`):
- Client hooks run inside client interceptors. The interceptor wraps `callX`, and `callX` calls the hooks.
- If writing a success body fails, the Error hook gets `Unknown` and ResponseSent still runs.
- If writing an error body fails, the failure is ignored on purpose.

### 3.4 Getting the data
- **Names**: `twirp.PackageName(ctx)` (for example `twitch.twirp.example`), `twirp.ServiceName(ctx)` (`Haberdasher`), and `twirp.MethodName(ctx)` (`MakeHat`). The method name exists only from RequestRouted on. Full route: `/twirp/<pkg>.<Service>/<Method>`, with a configurable prefix (`WithServerPathPrefix`).
- **Status**: `twirp.StatusCode(ctx)` gives a string such as `"200"` or `"400"`. It is set before the Error hook and before ResponseSent. It is never set in client contexts (runtime verified: empty).
- **Peer**: server hooks receive only ctx, and ctx holds the response writer but no `*http.Request`. So there is no peer address and no incoming headers. Wrap the Twirp `http.Handler` with HTTP middleware to read `RemoteAddr` and `traceparent`. Client: `RequestPrepared` gets `*http.Request`, so use `r.URL.Host`.
- **Headers**:
  - Server response: `twirp.SetHTTPResponseHeader` or `AddHTTPResponseHeader(ctx, k, v)` (both reject Content-Type).
  - Client: set `r.Header` in `RequestPrepared` (runtime verified), or use `twirp.WithHTTPRequestHeaders(ctx, h)` (rejects Accept, Content-Type, Twirp-Version).
  - `HTTPRequestHeaders(ctx)` reads only the headers stored by `WithHTTPRequestHeaders`, not incoming server headers (source).
- **Error**: `twirp.Error` has `Code() ErrorCode`, `Msg()`, `Meta(k)`, `MetaMap()`, and `WithMeta`.
  - Non-twirp errors become `Internal` through `InternalErrorWith`, which adds meta `cause` = type name. Runtime: `cause:*errors.errorString`.
  - `InvalidArgumentError` adds meta `argument`.
  - `wrappedErr` and the generated `wrappedError` both have `Unwrap`, so `errors.Is(twerr, context.Canceled)` works (source verified, not run).
- **Codes** (strings): canceled, unknown, invalid_argument, malformed, deadline_exceeded, not_found, bad_route, already_exists, permission_denied, unauthenticated, resource_exhausted, failed_precondition, aborted, out_of_range, unimplemented, internal, unavailable, data_loss, and NoError `""`.
- **Twirp HTTP mapping** (`ServerHTTPStatusFromErrorCode`) differs from google.rpc:
  - canceled 408 (not 499)
  - deadline_exceeded 408 (not 504)
  - failed_precondition 412
  - malformed 400
  - bad_route 404
  - everything else matches
- Sizes: no API.

### 3.5 Panics
Generated `ensurePanicResponses` recovers, writes a 500 `internal` error, calls the Error hook and ResponseSent, flushes, and then **re-panics**. The Error hook's error wraps the panic value (`errFromPanic`), and its meta is empty.

### 3.6 Gotchas
1. `context.Canceled` returned by a handler becomes `internal` / 500 (runtime verified). A canceled client ctx also becomes `internal` (runtime verified). Check `errors.Is(err, context.Canceled/DeadlineExceeded)` before trusting `Code()`. Map by name to the gRPC-style codes (section 6), not by Twirp's HTTP status.
2. The client Error hook can fire without RequestPrepared. The adapter must accept a finish with no start.
3. The adapter finishes the event in `ResponseSent`. It always fires: on success, error, bad route, and panic. The interceptor misses bad routes and panics.
4. The project is quiet. The last release was in 2022.

---

## 4. gqlgen (`github.com/99designs/gqlgen` v0.17.95)

### 4.1 Module facts
- MIT. `go 1.26`. Uses gqlparser/v2 v2.5.37.
- The sketch (all interfaces below, `HasOperationContext`, `GetComplexityStats`, `gqlerror.Error.Err`, `SetRecoverFunc`, `SetErrorPresenter`) **compiles at v0.17.49** (Go 1.20), v0.17.64, v0.17.78, and v0.17.95.
- It **fails at v0.17.20 and v0.17.36**: `gqlerror.Error` has no exported `Err` in the gqlparser they pick. Use `errors.Unwrap(e)` to support older versions.
- **UNVERIFIED**: users' generated code is tied to their gqlgen version. A high require can force them to upgrade and regenerate. Keep the floor low.

### 4.2 Hook signatures (`graphql/handler.go`)
```go
type OperationHandler func(ctx context.Context) ResponseHandler
type ResponseHandler  func(ctx context.Context) *Response   // iterator: nil = end of stream
type Resolver         func(ctx context.Context) (res any, err error)

type HandlerExtension interface { ExtensionName() string; Validate(schema ExecutableSchema) error }
type OperationParameterMutator interface { MutateOperationParameters(ctx context.Context, request *RawParams) *gqlerror.Error }
type OperationContextMutator interface { MutateOperationContext(ctx context.Context, opCtx *OperationContext) *gqlerror.Error }
type OperationInterceptor interface { InterceptOperation(ctx context.Context, next OperationHandler) ResponseHandler }
type ResponseInterceptor interface { InterceptResponse(ctx context.Context, next ResponseHandler) *Response }
type RootFieldInterceptor interface { InterceptRootField(ctx context.Context, next RootResolver) Marshaler }
type FieldInterceptor interface { InterceptField(ctx context.Context, next Resolver) (res any, err error) }
```
- Register with `srv.Use(ext)`, or the func forms `AroundOperations`, `AroundResponses`, `AroundFields`, `AroundRootFields`.
- Build the server with `handler.New(es)` and add transports. `handler.NewDefaultServer` is **Deprecated** ("not suitable for production use").

### 4.3 Operation data
- `graphql.GetOperationContext(ctx)` **panics** without an operation context. Guard with `graphql.HasOperationContext(ctx)`.
- `OperationContext` fields:
  - Request data: `RawQuery`, `Variables`, `OperationName`, `Extensions`, `Headers`.
  - Parsed data: `Doc`, `Operation *ast.OperationDefinition`.
  - Timing: `Stats` (`Read`, `Parsing`, `Validation`, `OperationStart`, and extension values).
- **Operation type**: `oc.Operation.Operation` is `ast.Query`, `ast.Mutation`, or `ast.Subscription` (`"query"`, `"mutation"`, `"subscription"`).
- **Operation name**:
  - `oc.OperationName` is what the client sent and can be `""`.
  - `oc.Operation.Name` comes from the document.
  - Runtime verified: `mutation M {…}` sent without `operationName` gave `OperationName=""` and `Operation.Name="M"`. An anonymous `{ name }` gives both `""`.
  - Recommendation: use `Operation.Name`, then `OperationName`, then `"anonymous"`.
- **Field data**: `graphql.GetFieldContext(ctx)` gives `Object`, `Field.Name`, `Path()`, `IsResolver`, `IsMethod`, and `Args`. Args can hold secrets.

### 4.4 Flow and errors (runtime verified)

| Request | InterceptOperation | InterceptResponse | HasOperationContext | HTTP |
|---|---|---|---|---|
| valid query | yes | yes | true | 200 |
| mutation where the executor returns `ErrorResponse` | yes | yes, error with `Err=nil` | true | 200 |
| parse error | **no** | yes, `extensions.code=GRAPHQL_PARSE_FAILED`, `Operation=nil` | true | 422 |
| variable validation error | **no** | yes, `GRAPHQL_VALIDATION_FAILED`, path `variable.id` | true | 422 |
| bad JSON body | **no** | yes | **false** | 400 |
| complexity over limit | **no** | yes, `COMPLEXITY_LIMIT_EXCEEDED`, stats `5/2` | true | **200** |
| resolver error (`graphql.AddError`) with `data: null` | yes | yes, `Err=*errors.errorString` | true | 200 |
| panic in an AroundOperations extension | no | no | n/a | 500, RecoverFunc called, panic swallowed |

- Errors in `CreateOperationContext` (parse, validation, `OperationContextMutator`s such as complexity) go to `DispatchError`. That runs **only the response middleware**. Source: `executor.go`, `transport/http_post.go`.
- GET transport errors before the executor never reach any extension. These are bad `variables` or `extensions` JSON and mutations over GET, handled by `writeJsonError` (source). An unsupported transport returns 400 from `Server.ServeHTTP` with no hooks.
- **HTTP status**:
  - `statusFor(errs)`: `KindProtocol` codes (only `GRAPHQL_VALIDATION_FAILED` and `GRAPHQL_PARSE_FAILED` are registered) give 422. With the response type `application/graphql-response+json` they give 400 instead. Everything else is 200.
  - `COMPLEXITY_LIMIT_EXCEEDED` is not registered, so it returns 200 even though `ComplexityLimit`'s doc says "422".
  - `errcode.RegisterErrorType` writes to a package-level map, which is mutable global state. Do not call it from wlog.
- **Partial success**: `Response.Data` is not null and `Response.Errors` is not empty, with HTTP 200. The error fields are `Message`, `Path`, `Locations`, `Extensions`, `Rule`, and `Err` (the wrapped original error, `json:"-"`). Errors pass through the error presenter (`SetErrorPresenter`, one slot). The default presenter keeps `*gqlerror.Error` or wraps the error with the path.
- **Subscriptions**: InterceptOperation runs once. InterceptResponse runs per event. The handler returns nil at the end of the stream or after ctx is done (source).
- **Complexity**:
  - Install `extension.FixedComplexityLimit(n)` or `&extension.ComplexityLimit{Func: …}`.
  - Read it with `extension.GetComplexityStats(ctx)`, which returns `*ComplexityStats{Complexity, ComplexityLimit}`. It returns nil without the extension.
  - It is set in `MutateOperationContext`, so it can be read in InterceptResponse even for rejected operations.
  - It calls `GetOperationContext` and **panics without an operation context** (the bad JSON case). Guard with `HasOperationContext`.

### 4.5 Keeping secrets out
- Never log `oc.Variables`, `FieldContext.Args`, or `RawParams.Variables`.
- `RawQuery` can hold inline literal secrets, such as `login(password:"…")`. Log the operation name, the type, and at most a hash of the query or the persisted query hash from `Extensions`.
- **Bad JSON body errors embed the whole request body in the error message.** Runtime verified: `json request body could not be decoded: unexpected EOF body:{"query":"{ name }","variables":{"password":"hunter2"}`. The redactor cannot catch this by key name.
  - Recommendation: when `HasOperationContext` is false, record only a fixed message or error code, never `Message`.
- Validation messages seen here did not echo the value ("cannot use string as Int"). Whether every gqlparser coercion message avoids echoing values is **UNVERIFIED**. Treat all messages as possibly sensitive.

### 4.6 Panics
- Generated field code (`codegen/field.gotpl`, `object.gotpl`, `graphql/resolve_field.go`) recovers resolver panics. It passes them through `RecoverFunc`, then the error presenter, then adds the error to the response. The request keeps going.
- `graphql.DefaultRecover` prints the panic and `debug.PrintStack()` to **stderr** and returns "internal system error".
- `Server.ServeHTTP` also recovers panics outside resolvers, for example in extensions or transports. It returns HTTP 500 and the panic does not propagate (runtime verified). An outer HTTP middleware never sees these panics.
- The websocket transport has its own recover (`transport/websocket.go`).
- `SetRecoverFunc` has one slot. An adapter that sets it replaces the user's function, so offer a wrapping helper instead.

---

## 5. Kratos v2, gRPC transport (`github.com/go-kratos/kratos/v2` v2.9.2)

### 5.1 Hook signatures
```go
// middleware
type Handler func(ctx context.Context, req any) (any, error)
type Middleware func(Handler) Handler
func Chain(m ...Middleware) Middleware
// transport
type Transporter interface { Kind() Kind; Endpoint() string; Operation() string; RequestHeader() Header; ReplyHeader() Header }
type Header interface { Get(string) string; Set(string, string); Add(string, string); Keys() []string; Values(string) []string }
func FromServerContext(ctx context.Context) (tr Transporter, ok bool)
func FromClientContext(ctx context.Context) (tr Transporter, ok bool)
```
- Server options (`transport/grpc`): `Middleware(m...)`, `StreamMiddleware(m...)`, `UnaryInterceptor(...grpc.UnaryServerInterceptor)`, `StreamInterceptor(...)`, `Options(...grpc.ServerOption)`, `Timeout(d)`, `Address`, `Listener`.
- Client options: `WithEndpoint`, `WithMiddleware`, `WithStreamMiddleware`, `WithTimeout`, and `DialInsecure`/`Dial`.
- `NewServer` builds `grpc.ChainUnaryInterceptor(kratosInterceptor, userInterceptors...)`. User gRPC interceptors run inside Kratos's interceptor, so they can see `transport.FromServerContext`.

### 5.2 Getting the data (runtime verified)
- `tr.Kind()` is `grpc`. `tr.Operation()` is `/probe.Probe/Echo`, the same as `FullMethod`.
- `tr.Endpoint()` on the server is the server's advertised `grpc://<host-ip>:<port>`, **not the peer**. On the client it is `cc.Target()`.
- **Peer**: not on Transporter. `google.golang.org/grpc/peer.FromContext(ctx)` works inside Kratos middleware (runtime verified). Getting the client peer through `selector.Peer` is **UNVERIFIED**.
- **Headers**:
  - Server `tr.RequestHeader()` wraps incoming metadata.
  - Client `tr.RequestHeader().Set("traceparent", v)` inside middleware is copied into outgoing metadata by `AppendToOutgoingContext` after the chain. Runtime verified: the server read it.
  - `tr.ReplyHeader()` exists only on the server and is sent with `grpc.SetHeader` after the handler.
- **Errors**: `kerrors.Error` embeds `Status{Code int32 /*HTTP*/, Reason, Message, Metadata map[string]string}` plus a cause.
  - `kerrors.FromError(err)`: if `errors.As` finds a `*Error`, that is used. Otherwise it takes the gRPC status, maps the code with `FromGRPCCode`, and reads `ErrorInfo.Reason` and `Metadata` from the details. Non-status errors become 500.
  - `(*Error).GRPCStatus()` uses `ToGRPCCode(HTTP)` and adds `ErrorInfo{Reason, Metadata}` with no Domain.
  - Runtime: `NotFound("USER_NOT_FOUND")` with meta `id=7` reached the client as code 404 with reason and meta intact.
- **Code maps** (`transport/http/status`):
  - `FromGRPCCode` equals the google.rpc HTTP mapping exactly. Canceled is 499 (`ClientClosed`).
  - `ToGRPCCode`: 400 InvalidArgument, 401 Unauthenticated, 403 PermissionDenied, 404 NotFound, **409 Aborted**, 429 ResourceExhausted, 500 Internal, 501 Unimplemented, 503 Unavailable, 504 DeadlineExceeded, 499 Canceled, **anything else Unknown**.

### 5.3 Panics
- Kratos adds no recovery by default. `NewServer` installs only its own interceptor.
- `middleware/recovery.Recovery()` is opt-in. It returns `ErrUnknownRequest = InternalServer("UNKNOWN", "unknown request error")`.

### 5.4 Gotchas
1. **Stream middleware does not wrap the stream.** `streamServerInterceptor` calls `middleware.Chain(next...)(h)` and **throws the result away**. The handler then runs directly with a `wrappedStream` that runs StreamMiddleware on **every `SendMsg` and `RecvMsg`**. Runtime verified: 9 middleware calls for a 3-message bidi stream. Per-stream events must use `kgrpc.StreamInterceptor(grpcInterceptor)`.
2. The HTTP-to-gRPC round trip loses codes. `kerrors.New(422, …)` (or any unmapped HTTP code) becomes `codes.Unknown` on the wire and `500` on the client. `409` always becomes `Aborted`, never `AlreadyExists` (source).
3. The server merges ctx with the app base ctx (`ic.Merge`) and applies `Timeout` for any value above 0. Kratos's default timeout value is **UNVERIFIED**.
4. `kratos/v2` pulls `grpc v1.61.1` into the build list (the go.mod requirement), which carries grpc's vulnerabilities (section 0).
5. Recommendation: the Kratos adapter is a thin middleware that adds `Operation` and kratos `Reason`/`Metadata` to the event from the plain gRPC adapter. Or it just registers the gRPC interceptors through `kgrpc.UnaryInterceptor` and `kgrpc.StreamInterceptor`.

---

## 6. Status code to level (recommended table)

Rule: map each code to its HTTP status using the **google.rpc.Code "HTTP Mapping" comments** (`googleapis/rpc/code/code.pb.go`). Then apply the HTTP rule wlog already uses (4xx warn, 5xx error). This table is identical to Connect's `connectCodeToHTTP` and Kratos's `FromGRPCCode`, all source verified.

It also matches OpenTelemetry. otelgrpc `serverStatus` marks Error on the server for exactly Unknown, DeadlineExceeded, Unimplemented, Internal, Unavailable, and DataLoss (source, `otelgrpc/interceptor.go`). Those are the 5xx rows.

| gRPC code | Connect | Twirp code(s) | HTTP (google.rpc) | Server event level | Outbound call outcome |
|---|---|---|---|---|---|
| OK | (nil error) | "" / success | 200 | info | ok |
| Canceled | canceled | canceled | 499 | **warn** (client went away) | `canceled`. If our own `ctx.Err()` is set, it is not a dependency failure |
| InvalidArgument | invalid_argument | invalid_argument, **malformed** | 400 | warn | client_fault (our request was wrong) |
| FailedPrecondition | failed_precondition | failed_precondition (Twirp sends 412) | 400 | warn | client_fault |
| OutOfRange | out_of_range | out_of_range | 400 | warn | client_fault |
| Unauthenticated | unauthenticated | unauthenticated | 401 | warn | client_fault |
| PermissionDenied | permission_denied | permission_denied | 403 | warn | client_fault |
| NotFound | not_found | not_found, **bad_route** | 404 | warn | client_fault |
| AlreadyExists | already_exists | already_exists | 409 | warn | client_fault |
| Aborted | aborted | aborted | 409 | warn | client_fault (a concurrency conflict you can retry) |
| ResourceExhausted | resource_exhausted | resource_exhausted | 429 | warn | throttled |
| Unimplemented | unimplemented | unimplemented | 501 | error | remote_fault |
| Unknown | unknown | unknown | 500 | error | remote_fault |
| Internal | internal | internal | 500 | error | remote_fault |
| DataLoss | data_loss | data_loss | 500 | error | remote_fault |
| Unavailable | unavailable | unavailable | 503 | error | remote_fault (retryable) |
| DeadlineExceeded | deadline_exceeded | deadline_exceeded (Twirp sends 408) | 504 | error | `timeout`. If our own deadline passed, the cause is local |

Why:
- **Canceled is warn.** The caller left, so the server is not at fault (499). It is still worth keeping: a spike means clients time out on us.
- **DeadlineExceeded is error.** It means we did not finish within the caller's budget, which is usually our latency. Google maps it to 504 and OTel marks it as an error.
- **Unimplemented is error**, per 501 and OTel. Unknown-method Unimplemented never reaches interceptors anyway (gRPC gotcha 1).
- **ResourceExhausted is warn** because 429 is the caller's quota. It can also be our own limit. gRPC returns ResourceExhausted for oversized messages (**UNVERIFIED**).
- **Twirp**: map by code **name** to the gRPC row. Do not use `ServerHTTPStatusFromErrorCode`, which sends canceled and deadline_exceeded to 408 (warn) and failed_precondition to 412. First check `errors.Is(err, context.Canceled/DeadlineExceeded)`, because Twirp reports those as `internal`.
- **Kratos**: use `kerrors.FromError(err).Code` (already HTTP) with the same 4xx and 5xx rule.
- **gqlgen** (proposal, **UNVERIFIED** as a policy):
  - Protocol errors (`GRAPHQL_PARSE_FAILED`, `GRAPHQL_VALIDATION_FAILED`, `COMPLEXITY_LIMIT_EXCEEDED`, bad JSON) are warn.
  - A panic recovered through RecoverFunc is error.
  - For resolver errors, classify each `gqlerror.Error.Err` with the wlog error model: a client-fault kind is warn, anything else is error.
  - Partial success takes the highest level found among its errors. Record `graphql.errors_count` and `graphql.partial=true`.
- **Outbound calls**: gRPC cannot tell local and remote Canceled/DeadlineExceeded apart by code (runtime verified). Check `ctx.Err()` after the call. On Connect use `IsWireError`.

---

## 7. Filling ErrorInfo {code, message, why, fix, link, data}

The fields below are a proposal. Everything they read from is source verified. All values must go through the redactor: ErrorInfo metadata and field violation descriptions often echo user input.

### gRPC and Connect (google.rpc details)
Detail types in `errdetails` and their fields (source):
- `ErrorInfo{Reason, Domain, Metadata map[string]string}`
- `RetryInfo{RetryDelay *durationpb.Duration}`
- `DebugInfo{StackEntries []string, Detail}`
- `QuotaFailure{Violations[]{Subject, Description, ApiService, QuotaMetric, QuotaId, QuotaDimensions, QuotaValue, FutureQuotaValue}}`
- `PreconditionFailure{Violations[]{Type, Subject, Description}}`
- `BadRequest{FieldViolations[]{Field, Description, Reason, LocalizedMessage}}`
- `RequestInfo{RequestId, ServingData}`
- `ResourceInfo{ResourceType, ResourceName, Owner, Description}`
- `Help{Links[]{Description, Url}}`
- `LocalizedMessage{Locale, Message}`

| wlog field | Source |
|---|---|
| `code` | `ErrorInfo.Reason` (UPPER_SNAKE, stable). Fallback: the status code name. Always keep the RPC code in its own field (`rpc.code`) |
| `message` | `LocalizedMessage.Message` (user-facing). Fallback: `status.Message()` / `connect.Error.Message()` |
| `why` | `status.Message()`, for the case where `LocalizedMessage` filled `message`. The status message is developer-facing. Otherwise the first `PreconditionFailure` or `QuotaFailure` violation `Description` |
| `fix` | From `RetryInfo`: "retry after <delay>". From `BadRequest`: "fix field <Field>: <Description>" |
| `link` | `Help.Links[0].Url`. Put all links in `data.help_links` |
| `data` | `ErrorInfo.Domain`, `ErrorInfo.Metadata`, `BadRequest.FieldViolations` (field, description, reason), `RetryInfo.RetryDelay` in ms, `QuotaFailure` and `PreconditionFailure` violations, `ResourceInfo`, `RequestInfo.RequestId`. Leave out `DebugInfo.StackEntries` by default |

- gRPC: `status.FromError(err)` then `st.Details()`. Skip items that are an `error` (type not registered).
- Connect: `ce.Details()[i].Value()`, with `Type()` as a fallback key.
- If the wrapped error already carries wlog `ErrorInfo` (for example from `errors/herr`), prefer that over the details.

### Twirp
- `code` ← `Error.Code()`, `message` ← `Msg()`, `data` ← `MetaMap()`.
- Leave out or rename the Twirp-added `cause` (a type name) and keep `argument`.
- No link or fix exists natively. A convention can read the meta keys `why`, `fix`, and `link`. The probe's `WithMeta("why", …)` round-tripped to the client meta (runtime verified).

### Kratos
- `code` ← `Reason`, `message` ← `Message`, `data` ← `Metadata`. The HTTP code comes from `Code`.

### gqlgen
- Per `gqlerror.Error`:
  - `message` ← `Message`. Mind the bad JSON leak in 4.5.
  - `code` ← `Extensions["code"]`, for a string value.
  - `data` ← `Path.String()`, `Locations`, and other `Extensions`.
- Underlying error: `Err` in newer gqlparser, `errors.Unwrap` otherwise. Use it for classification. It can also carry a wlog ErrorInfo to use directly.

---

## 8. Panic behavior summary

| Library | Recovers by default? | What happens | Hook still fires? |
|---|---|---|---|
| gRPC-Go | No | The process crashes (no `recover` in core) | No. The interceptor must `defer` recover |
| Connect | No (opt-in `WithRecover`) | net/http recovers the goroutine and the client sees `unavailable: unexpected EOF` (runtime) | Code after `next` does not run. `defer` it |
| Twirp | Partly | Generated `ensurePanicResponses` writes a 500, calls Error and ResponseSent, then re-panics (runtime) | Yes: Error and ResponseSent. The interceptor does not |
| gqlgen | Yes | Resolver panics become a GraphQL error through RecoverFunc. Panics outside resolvers become HTTP 500 in `Server.ServeHTTP`, swallowed (runtime) | InterceptResponse runs for resolver panics (source). Panics outside resolvers skip hooks and only call RecoverFunc |
| Kratos gRPC | No (opt-in `middleware/recovery`) | Same as gRPC | Same as gRPC |

For every adapter, `defer` a recover that finishes the event with a panic error and then re-panics. This keeps each library's behavior and meets gate G3 "never blocks".

---

## 9. Other gotchas across libraries

1. **Duplicate trace propagation**: otelgrpc (stats handler) and otelconnect (interceptor, **UNVERIFIED**) inject `traceparent` themselves. otelgrpc overwrites earlier values. Detect them, or document one owner.
2. **The event is attached through ctx**: gRPC unary and Connect pass `ctx` directly. gRPC server streams need a `ServerStream` wrapper that overrides `Context()`. Twirp hooks return ctx and the generated code carries it forward.
3. **Races**: stream wrappers see sends and receives on different goroutines in gRPC and Connect. Use `atomic` counters and `sync.Once` finishes.
4. **Requests before routing** get no RPC hook: gRPC unknown methods, Connect's early HTTP errors and decode failures, Twirp's GET/405 path (Twirp does run RequestReceived and Error), gqlgen GET errors and unsupported transport. Mounting the wlog HTTP middleware outside the RPC handler covers them for Connect, Twirp, and gqlgen. gRPC over its own HTTP/2 server has no such layer, only `stats.Handler`, which also emits no `End` for unknown methods.
5. **Wrapped error text reaches clients**: gRPC wrapped status errors (runtime verified). Twirp internal errors send `Msg` = the raw `err.Error()` (runtime: `msg="plain"`). This is a leak risk in services, not in wlog, but worth a doc note.
6. **Go floors**: gqlgen latest needs Go 1.26, and grpc and connect latest need Go 1.25. Pick require versions from the table in section 0.
