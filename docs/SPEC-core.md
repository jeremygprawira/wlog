# Spec: core

> Module id `core` · package `github.com/jeremygprawira/wlog` (import path is the module root) ·
> root module · depends on: `redact`. Project-wide rules in [SPEC.md](SPEC.md) apply. Every
> value below already exists as a decision in SPEC.md's Decisions table. This file gives it an
> exact name, type, and test.
> v1.2 additions to this module: [SPEC-v1.2-additions.md](SPEC-v1.2-additions.md).

## Objective

Give every other module one stable surface: a `Logger`, a `context.Context` API to build one
event, and the interfaces (`Drain`, `ErrorExtractor`, `Enricher`, `Keeper`, plugin hooks).
Those interfaces let redaction, sampling, HTTP capture, error libraries, and backends plug in
without core knowing about any of them.

## Reserved keys (default namespaced layout)

Every key below is reserved. A user `Set` of a reserved top-level key is allowed, because it
overwrites the default value. A linter rule in `cli-map` (MP4) flags it as likely a mistake.

```
timestamp            RFC3339Nano, set at Start
level                debug | info | warn | error
operation             the name passed to Start, or the route for HTTP
duration_ms          set at emit
outcome               success | error
service.name service.version service.env
trace.request_id trace.trace_id trace.span_id trace.parent_operation
http.method http.route http.path http.status http.duration_ms
http.bytes_in http.bytes_out http.client_ip http.user_agent
http.request_headers http.request_query http.request_params http.request_cookies http.request_body
http.response_headers http.response_body
error                 one ErrorInfo (the error that decided the outcome)
errors                []ErrorInfo, earlier errors, capped at 10
logs                  []LogLine folded in by a log-*-in module, capped at 50
audit                 up to 20 audit.Records, added by audit.Do through wlog.Append
redact.fingerprint     short hash of the active redactor (omitted if disabled)
wlog.dropped_fields wlog.late_writes wlog.dropped_logs   overflow counters (SPEC.md G4)
```

User keys set with `Set`/`SetGroup`/`Append` stay at the top level, outside these namespaces.

## Behaviour

### Event lifecycle

```go
type Logger struct{ /* unexported */ }

func New(opts ...Option) *Logger
func (l *Logger) Close(ctx context.Context) error   // flushes drains, respects ctx deadline

func Start(ctx context.Context, operation string) (context.Context, func())
func Detach(ctx context.Context, operation string) (context.Context, func())
```

`Start` reads the `*Logger` from `ctx`, which middleware or `l.Context(ctx)` put there for
non-HTTP code. It creates an event and returns a child `ctx` plus an `end` func. Calling `end`
runs the fixed stage order (SPEC.md: keep/sample → enrich → redact → rename → sinks) once, then
seals the event. `Detach` is the same, except the new event carries `trace.request_id`,
`trace.trace_id`, and `trace.parent_operation` copied from the parent, and is independent: it
has its own sampling decision and its own `end`.

A sealed event, one that already ran `end`, ignores every later write. The write increments
`wlog.late_writes` on the **parent's next** still-open event. With no open ancestor, it is
dropped silently. A write never blocks and never panics (G3).

### Enrichment API

```go
func Set(ctx context.Context, key string, value any)
func SetGroup(ctx context.Context, group string, kv ...any)   // pairs, or a single map[string]any
func Append(ctx context.Context, key string, value any)       // builds/extends a []any
func SetLevel(ctx context.Context, level Level)                // overrides inferred level
func Error(ctx context.Context, err error)                     // uses the active ErrorExtractor
func Info(ctx context.Context, msg string, kv ...any)          // plain one-off line (C12)
func Warn(ctx context.Context, msg string, kv ...any)
func AppendLog(ctx context.Context, line LogLine)     // folds one log record into logs[]
```

All are no-ops on a `ctx` with no event (never panics). `Set`/`SetGroup`/`Append` respect the
caps from SPEC.md G4: `MaxKeys` (default 200), `MaxGroupFields` (default 50), `MaxArrayLen`
(default 200). An entry beyond a cap is dropped and counted in `wlog.dropped_fields`.

### Folded log lines

```go
type LogLine struct {
    Level string         `json:"level"`
    Msg   string         `json:"msg"`
    Attrs map[string]any `json:"attrs,omitempty"`
}
```

A log-*-in adapter (`log-slog`'s `Handler`) calls `AppendLog` to fold one record made inside an
event into that event's `logs[]`. The array is capped at 50 lines. A line beyond the cap is
dropped and counted in `wlog.dropped_logs`, the same way `wlog.dropped_fields` counts other
overflow. `AppendLog` is a no-op on a sealed event or a `ctx` with no event, and increments
`wlog.late_writes` on a sealed one, like every other write.

### Typed keys (C11)

```go
type Key[T any] struct{ name string }

func NewKey[T any](name string) Key[T]
func (k Key[T]) Set(ctx context.Context, v T)
func (k Key[T]) Name() string

func StrictKeys(keys ...interface{ Name() string }) Option // any Key[T] satisfies this
```

`Key[T].Set` compiles only for the declared `T`. It calls the same `Set` under the hood.
`StrictKeys` records the registered names. In `local`/`dev` env, an untyped `Set` call with a
name not in that list adds its name to `wlog.unknown_keys` on the event. In other envs it is a
no-op check (never rejects the write).

### Errors (C4)

```go
type ErrorInfo struct {
    Code, Message, Kind string
    Status               int
    Cause                string   // err.Error() of the wrapped cause, if any
    Stack                string
    Why, Fix, Link       string
    Attrs                map[string]any
}

type ErrorExtractor interface {
    Extract(err error) ErrorInfo
}
```

Default extractor, used with no `WithErrorExtractor` option: `Code = "INTERNAL"`,
`Message = err.Error()`, walks `errors.Unwrap` for `Cause`. An error that implements
`interface{ Stack() string }` populates `Stack`, as http-std's recovered panics do. Every other
error leaves it empty.
`wlog.Error` appends the
previous `error` value (if any) to `errors[]` (cap 10, overflow counted in
`wlog.dropped_fields`) before replacing it.

### Drains, plugins, enrichers, keepers (C6, C7, C10)

```go
type Drain interface{ Send(ctx context.Context, event map[string]any) }
type DrainFunc func(ctx context.Context, event map[string]any)
func (f DrainFunc) Send(ctx context.Context, event map[string]any) { f(ctx, event) }
// optional: Close(ctx context.Context) error

type Enricher interface{ Enrich(ctx context.Context, event map[string]any) }
type Keeper interface{ Keep(ctx context.Context, event map[string]any) bool }  // true = force-keep

type Plugin interface{ Name() string }
// optional, detected by type assertion:
type RequestStarter interface{ OnRequestStart(ctx context.Context) context.Context }
type RequestFinisher interface{ OnRequestFinish(ctx context.Context) }
type Setup interface{ Setup(l *Logger) error }
```

A panic in any `Enricher`, `Keeper`, `Drain`, or plugin hook is recovered. The panic is
reported to `OnError(err error, source string)`. It never affects the event, the response, or
other drains and plugins (G3). `WithDrains` fans out the same redacted snapshot to every drain
concurrently. `WithPlugins` and `WithEnrichers` run in the order given.

Enrichers and Keepers run with the context the event was started from, so they can read
request-scoped values (an active trace span, a tenant id). Drains receive that same context
with cancellation removed (`context.WithoutCancel`), so a canceled request never aborts a
drain's network call.

### Stage order (C7, locking SPEC.md's decision)

```
1. keep/sample    WithSampler(Keeper). No Keeper configured = always keep (SPEC.md default)
2. enrich         WithEnrichers(...), in order
3. redact         the active *redact.Redactor (C8)
4. rename         the active field-name preset (C9)
5. sinks/drains   WithSinks(stdout json|pretty), WithDrains(...)
```

A dropped event skips steps 2 to 5 entirely. A `Keeper` that returns false drops the event at
step 1. An event with a non-nil `audit` field always skips step 1 (SPEC.md: audit bypasses
sampling).

### Redactor (C8)

```go
func WithRedactor(r *redact.Redactor) Option
func (l *Logger) SetRedactor(r *redact.Redactor)   // atomic.Pointer swap
```

No `WithRedactor` given: `redact.Default()`. Every event's step 3 uses whichever `*Redactor` is
current at that moment. `redact.fingerprint` (from `r.Fingerprint()`) is set on the event unless
`WithRedactFingerprint(false)`.

### Field-name presets (C9)

```go
func FieldsNamespaced() FieldNames   // default, per Reserved keys above
func FieldsFlat() FieldNames         // method, path, status_code, request_id, ... (boilerplate-compatible)
func FieldsOTel() FieldNames         // http.request.method, http.response.status_code, service.name, ...
func WithFieldNames(names FieldNames) Option
func (n FieldNames) Rename(canonical, output string) FieldNames
```

Renaming runs after redaction (step 4), so denylist entries always match canonical names
(`http.request_headers.cookie`), regardless of the output preset.

### Sinks (C1, C13)

```go
func WithSinks(s ...Sink) Option   // default: one auto-format stdout sink
type Sink interface{ Write(event map[string]any) }
```

The stdout sink picks JSON or the pretty console from `WLOG_FORMAT` or `WithFormat`. Pretty
wins for a `local` or `dev` env with `NO_COLOR` unset. JSON wins otherwise. A sink failure
calls `OnError` and does not block or retry (that is `pipeline`'s job for drains, not a sink's).

### Config (C14)

```go
func WithService(name, version, env string) Option
func WithLevel(min Level) Option
func WithFormat(f Format) Option   // JSON | Pretty | Auto (default)
func OnError(fn func(err error, source string)) Option
```

Env fallbacks are read once in `New`. A matching `Option` overrides each one:
`WLOG_SERVICE`, `WLOG_VERSION`, `WLOG_ENV`, `WLOG_LEVEL`, `WLOG_FORMAT`. An invalid env value
calls `OnError` and falls back to the built-in default. It never panics.

## Success Criteria

1. `wlog.New()` with zero options plus one `Start`/`Set`/`end()` produces a valid JSON line on
   stdout with every reserved top-level key present and correctly typed.
2. A `Set(ctx, "password", "x")` call is `"[REDACTED]"` in the output (proves step 3 runs by
   default, using `redact.Default()`).
3. `Start` → `Detach` → both `end()` calls produce two events sharing `trace.request_id`, the
   child carrying `trace.parent_operation`.
4. A write after `end()` does not panic, does not appear in the emitted event, and increments
   `wlog.late_writes` (G3, G4).
5. A `Keeper` returning `false` with no other `Keeper` returning `true` means no `Enricher`,
   redactor, or `Drain` is invoked for that event (proves stage order).
6. An event with `audit` set is kept, whatever every configured `Keeper` returns.
7. `SetRedactor` under 1000 concurrent emits passes `-race`, and no event mixes fields from two
   different redactor configs (G2).
8. `FieldsOTel()` and `FieldsFlat()` each match a golden JSON file for the same input event.
9. A panicking `Enricher` is reported to `OnError` and the event still emits with every other
   field intact (G3).
10. Benchmark: `Start` → 10 `Set` calls → `end()` with one no-op drain is ≤ 20µs p50 on an
    M-series Mac (half of SPEC.md's 50µs request budget, leaving room for HTTP capture).
11. Root module (`go list -deps .`) imports nothing outside the standard library and `redact`.
12. `make compat` (Go 1.23) passes for the whole root module.

## Testing

Per SPEC.md, tests are black-box in `package wlog_test` and table-driven. Every
concurrency-sensitive test runs under `-race` (C2, C5, C8). Golden files under `testdata/`
cover C9 and C13. One `Example*` test covers each exported function, and `bench_test.go`
covers criterion 10.

## Boundaries (module-specific)

- **Always:** keep the stage order fixed. Every new hook point (drain, enricher, plugin) gets
  panic isolation before it ships.
- **Ask first:** adding or renaming a reserved key. Changing a default cap.
- **Never:** import `redact`'s internals directly. Use its public API only.

## Open Questions

None.
