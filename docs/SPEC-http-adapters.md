# Spec: http-adapters

> Module ids `http-echo`, `http-echo5`, `http-gin`.
> Packages `github.com/jeremygprawira/wlog/middleware/echo` (`wlogecho`), `.../middleware/echo5` (`wlogecho5`), `.../middleware/gin` (`wloggin`).
> Each has its own `go.mod`, since each pulls in a third-party framework.
> Depends on `core` and `http-std`. Project-wide rules in [SPEC.md](SPEC.md) apply.
> Each adapter must pass the `internal/conformance` suite defined in [SPEC-http-std.md](SPEC-http-std.md).

## Objective

Three thin middlewares so an Echo v4, Echo v5, or Gin app gets the same wide event as a plain
`net/http` app, in one call. Each adapter reuses `http-std`'s behavior (capture, panics, plugin
hooks) and only translates the framework's own request/response objects into what `http-std`
needs.

## Pinned versions

| Framework | Module | Version |
|---|---|---|
| Echo v4 | `github.com/labstack/echo/v4` | v4.15.4 |
| Echo v5 | `github.com/labstack/echo/v5` | v5.3.1 |
| Gin | `github.com/gin-gonic/gin` | v1.12.0 |

## Behaviour

<!-- snippet:sketch -->
```go
// middleware/echo and middleware/echo5 (same shape, different import path)
func Middleware(log *wlog.Logger, opts ...Option) echo.MiddlewareFunc

// middleware/gin
func Middleware(log *wlog.Logger, opts ...Option) gin.HandlerFunc
```

Each package exposes the same `Option` set as `http-std`: `WithUserFunc`, `SkipPaths`,
`CaptureHeaders`, `CaptureQuery`, `CaptureCookies`, `CaptureBody`, `MaxBodyCapture`, and
`BodyContentTypes`. Each adapter wraps `http-std`'s `Middleware` around the framework's own
`*http.Request` and `http.ResponseWriter`. This way, no adapter duplicates the capture logic.

### Echo (v4 and v5)

- Route: `c.Path()` (Echo's registered path template, for example `/users/:id`).
- Errors: a handler that returns an `error` still reaches `wlog.Error` before the event closes,
  and then flows to Echo's own `HTTPErrorHandler` unchanged, so Echo's error response behavior
  does not change.
- Registered as `e.Use(wlogecho.Middleware(log))`.

### Gin

- Route: `c.FullPath()` (empty for an unmatched route, same as Echo's behavior for a 404).
- Errors: everything in `c.Errors` after the handler runs is reported via `wlog.Error`, one call
  per error, in the order Gin recorded them.
- Body capture reads through Gin's own `c.Request.Body`, and the response capture wraps Gin's
  `ResponseWriter` (which already implements `http.Flusher`) the same way `http-std` wraps a
  plain one.
- Registered as `r.Use(wloggin.Middleware(log))`.

## Success Criteria

1. Each adapter passes `internal/conformance.Run(t, adapter)` unchanged.
2. A handler-returned error (Echo) or `c.Error(err)` (Gin) reaches `wlog.Error` and the framework's
   own error handling still runs after.
3. Each adapter's own `go.mod` has exactly one non-stdlib, non-`wlog` dependency: its framework.
4. `examples/echo`, `examples/echo5`, and `examples/gin` each set up wlog in 5 lines or fewer
   (SPEC.md criterion 2), verified in EX1.

## Testing

Package `<name>_test`, black-box, using each framework's own test helpers
(`echo.New()`/`httptest`, Gin's `gin.CreateTestContext`) plus `internal/conformance`.

## Boundaries

- **Always:** run `internal/conformance` before merging any adapter.
- **Ask first:** bumping a pinned framework version to a new major.
- **Never:** duplicate `http-std`'s capture logic inside an adapter. Wrap it instead.

## Open Questions

None.
