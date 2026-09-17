# Spec: logger bridges, error libraries, and flags (track C)

> Phase 12 · depends on: `core-shape`, `core-problems`, the `log` conformance suite. Root module
> ids: `log-slog` and `log-std`. Own-module ids: `log-logr`, `log-zap`, `log-zerolog`,
> `log-logrus`, and `log-hclog`. Also `errors-validator`, `errors-oops`, `errors-cockroach`, and
> `flag-openfeature`. Project-wide rules in [SPEC.md](SPEC.md) apply. Facts come from the API research of
> 2026-09-16, checked against each library's source.

## Objective

An app keeps its logger and its error library. Log lines written inside a unit of work fold
into that unit's event, and finished events can go out through the app's logger. Errors from
any library fill the same `ErrorInfo` fields, with no secret copied by accident.

## How a bridge finds the event

Only four libraries pass a context on each call: slog (`*Context` methods), zerolog (`.Ctx`),
logrus (`WithContext`), and OpenFeature hooks. logr, zap, hclog, and stdlib `log` pass none. A
bridge for those binds a per-unit logger to the context through a core `Starter` plugin. The
library's own lookup then returns the bound logger: `logr.FromContext`, `klog.FromContext`,
`hclog.FromContext`, or `zerolog.Ctx`.

| Library | Input path | Fields kept | Library floor | Module Go floor |
|---|---|---|---|---|
| slog | `Handler(next)` reads the call's context | all | Go 1.21 | 1.21 (root) |
| logr, klog v2, controller-runtime | `Plugin()` binds a sink with `logr.NewContext` | all | logr v1.4.0 | 1.21 |
| zap | `Core(next)` reads a `context.Context` field. `Plugin(base, store)` binds a core | all | zap v1.22.0 | 1.21 |
| zerolog | `Hook()` reads `Event.GetCtx`. `Plugin(base)` binds a writer with `WithContext` | hook: level and message. Bound writer: all | zerolog v1.30.0 | 1.21 |
| logrus | `Install(logger)` adds a hook for `Entry.Context` and wraps the formatter | all | logrus v1.4.0 | 1.21 |
| hclog | `Plugin(base)` binds a logger with `hclog.WithContext` | all | hclog v0.10.0 | 1.21 |
| stdlib `log` | `wlogstdlog.Logger(ctx, prefix, flags)` returns a per-unit `*log.Logger` | message | Go 1.21 | 1.21 (root) |

A global call with no context never folds: `slog.Info` without `Context`, global `log.Printf`,
`klog.InfoS`, and `hclog.L()`. Each bridge's package doc states this, and `wlog doctor` warns
about a global call inside a handler.

## Shared rules for every bridge

1. A folded record is not written to the wrapped logger. `AlsoWrite()` writes it to both.
2. A bridge folds a record only at a level the wrapped logger writes, and skips other levels.
   `FoldLevel(level)` overrides that. (HTTP-18)
3. Levels map by number, never by level text: debug, info, warn, error. zap `DPanic`, `Panic`,
   and `Fatal`, zerolog `Fatal` and `Panic`, and logrus `Panic` and `Fatal` all map to `error`.
   zerolog `NoLevel` and hclog `NoLevel` map to `info`.
4. A value that is an `error` becomes `err.Error()`, and its `LogValue`, `MarshalJSON`, and
   `String` methods are not called. samber/oops `LogValue` holds a raw HTTP request dump.
5. A `context.Context` value in fields is removed before any write, because its `String` method
   prints every value in the context chain.
6. Every value goes through the core value copy and the redactor.
7. Output drains never let an event key overwrite the logger's own keys. A clashing key moves to
   `wlog.fields.<key>`, the same rule as the output presets. zap reserves `level`, `ts`, `msg`, `caller`, `stacktrace`, and `logger`.
   zerolog reserves `level`, `message`, `time`, `error`, `caller`, and `stack`. logrus reserves
   `msg`, `level`, `time`, `logrus_error`, `func`, and `file`. slog reserves `time`, `level`,
   `msg`, and `source`. The zap output adds no stack trace of its own, so
   `zap.AddStacktrace` never attaches the drain goroutine's stack. (HTTP-20)
8. Each bridge passes the `log` conformance suite.

## Logger modules

### log-slog (root, package `wlogslog`)

<!-- snippet:sketch -->
```go
func Handler(next slog.Handler, opts ...Option) slog.Handler // input
func Drain(h slog.Handler, opts ...DrainOption) wlog.Drain   // output
func AlsoWrite() Option
func FoldLevel(l slog.Level) Option
```

- The handler stores an ordered list of groups and attrs from `WithGroup` and `WithAttrs`, so
  `Handle` rebuilds the right nesting. It passes `testing/slogtest`.
- It drops `slog.Attr{}`, drops an empty group, inlines a group with key `""`, and resolves every
  value inside groups. `WithGroup("")` returns the same handler.
- `Drain` checks `h.Enabled` before `Handle`, and it maps the event level to a slog level.

### log-logr (package `wloglogr`)

<!-- snippet:sketch -->
```go
func Plugin(opts ...Option) wlog.Plugin                  // Starter: wraps logr.FromContextOrDiscard(ctx)
func Sink(ctx context.Context, next logr.LogSink) logr.LogSink
func Drain(l logr.Logger) wlog.Drain
```

- The bound sink folds `Info(v, ...)` at `V(0)` as `info` and `V(1)` or higher as `debug`. It
  folds `Error` as `error`, and a nil error is allowed. `WithName` names join with `/` under
  `logger`.
- With no event, or after the event emits, the sink forwards to `next`, including `Init`.
- It implements `logr.CallDepthLogSink` and `logr.SlogSink`, so `logr.ToSlogHandler` keeps the
  context.
- klog's `FromContext` reads the bound logger while contextual logging is on. The package doc
  says so.

### log-zap (package `wlogzap`)

<!-- snippet:sketch -->
```go
func Core(next zapcore.Core, opts ...Option) zapcore.Core          // folds entries that carry a ctx field
func Plugin(base *zap.Logger, store func(context.Context, *zap.Logger) context.Context) wlog.Plugin
func Bind(ctx context.Context, l *zap.Logger) *zap.Logger
func Drain(l *zap.Logger, opts ...DrainOption) wlog.Drain
```

- A field whose value is a `context.Context` marks the entry, as in the otelzap convention. `Core`
  removes that field before any write.
- `Check` is declared on the wrapper type, never promoted from an embedded core. It adds the
  wrapper to the entry.
- For an entry with no event, `Write` calls `next.Check` again, then writes, so sampling still
  applies.
- `store` lets the app keep its own convention, such as `ctxzap.ToContext`.
- Fields convert through `zapcore.NewMapObjectEncoder`. `zap.Error` becomes its message.

### log-zerolog (package `wlogzerolog`)

<!-- snippet:sketch -->
```go
func Hook(opts ...Option) zerolog.Hook                     // level and message, for events with .Ctx(ctx)
func Plugin(base zerolog.Logger) wlog.Plugin               // binds a writer, found through zerolog.Ctx
func Bind(ctx context.Context, l zerolog.Logger) zerolog.Logger
func Drain(l zerolog.Logger) wlog.Drain
```

- A hook cannot read zerolog fields, so `Hook` folds level and message only, and calls
  `e.Discard()` for a folded record.
- The bound writer receives each JSON line, decodes it, and removes the logger's own `level`,
  `message`, and `time` keys. The package doc says the bound writer does not work with the
  `binary_log` build tag.
- zerolog runs its level filter before hooks and writers. The doc says debug lines need a debug
  logger to fold.
- Levels map from constants, never from `Level.String()`, because the level names are globals.

### log-logrus (package `wloglogrus`)

<!-- snippet:sketch -->
```go
func Install(l *logrus.Logger, opts ...Option) // adds the hook and wraps the formatter
func Drain(l *logrus.Logger) wlog.Drain
```

- The hook folds an entry whose `Context` holds an event, with every `Data` field.
- A hook cannot stop logrus from writing, so the wrapped formatter returns no bytes for a folded
  entry.
- `warning` maps to `warn`, `trace` to `debug`, and `panic` and `fatal` to `error`.

### log-hclog (package `wloghclog`)

<!-- snippet:sketch -->
```go
func Plugin(base hclog.Logger) wlog.Plugin // binds a logger with hclog.WithContext
func Bind(ctx context.Context, l hclog.Logger) hclog.Logger
func Drain(l hclog.Logger) wlog.Drain
```

- The bound logger overrides `Log`, `Trace`, `Debug`, `Info`, `Warn`, `Error`, `With`, `Named`,
  and `ResetNamed`. Its `IsDebug` and other level checks return true while an event is open.
- It never uses `RegisterSink`, because a sink mixes lines from all goroutines.

### log-std (root, package `wlogstdlog`)

<!-- snippet:sketch -->
```go
func Logger(ctx context.Context, prefix string, flags int) *log.Logger // per-unit, folds each line
func ErrorLog(l *wlog.Logger) *log.Logger                              // for http.Server.ErrorLog, plain events
```

The package doc says a global `log.Printf` never folds, even after `slog.SetDefault`.

The charm log recipe shows `slog.New(wlogslog.Handler(charmLogger))` for input and
`wlogslog.Drain(charmLogger)` for output. It notes that charm turns `WithGroup` into a prefix.

## Error modules

Each module returns a decorator: `Extractor(next wlog.ErrorExtractor, opts...) wlog.ErrorExtractor`.
It fills only the fields its library knows, and leaves the rest to `next`. Each runs under the
core extractor recover.

### errors-validator (package `wlogvalidator`, go-playground/validator v10)

| ErrorInfo | Value |
|---|---|
| `Code` | `VALIDATION_FAILED`, or `WithCode(code)` |
| `Kind` | `validation` |
| `Status` | 400, or `WithStatus(422)` |
| `Message` | `validation failed on N fields` |
| `Data.fields` | `[{field, tag, param}]`, where `field` is `Namespace()` without the root struct name. At most 50, with `Data.fields_dropped` |
| `Internal.fields` | `[{struct_namespace, actual_tag, kind, type}]` |
| `Fix` | per tag, from `WithFixes(map[tag]string)`. Empty without that option |

- `FieldError.Value()` holds the raw rejected input, such as a password. The module never calls
  it, and a test proves no rejected value reaches output.
- `*validator.InvalidValidationError` maps to `INTERNAL`, kind `internal`, status 500.
- Library floor v10.27.0, the newest release on Go 1.20. Module Go floor 1.21.

### errors-oops (package `wlogoops`, samber/oops)

| ErrorInfo | Value |
|---|---|
| `Code` | `Code()` through a type switch, because it is `string` before v1.20.0 and `any` after |
| `Kind` | `Domain()` |
| `Message` | `Error()`. `WithPublicMessage()` uses `Public()` instead |
| `Fix` | `Hint()` |
| `Stack` | `Stacktrace()` |
| `Data.public` | `Public()` |
| `Internal` | `tags`, `context`, `owner`, `span`, `user_id`, `tenant_id`, `oops_trace` |

- The module never calls `ToMap`, `MarshalJSON`, `LogValue`, `Request`, or `Response`, because
  they hold a raw HTTP request dump with auth headers and bodies. A test proves no request dump
  reaches output.
- `User()` and `Tenant()` maps are dropped. Only their ids are kept.
- Library floor v1.20.0, the release that changed `Code()`. The floor test proves each accessor
  exists there. Module Go floor 1.21.

### errors-cockroach (package `wlogcockroach`, cockroachdb/errors)

| ErrorInfo | Value |
|---|---|
| `Code` | the first of the sorted `GetTelemetryKeys`, or `next`'s code |
| `Kind` | `GetDomain`, skipped for `NoDomain` |
| `Status` | `exthttp.GetHTTPCode(err, 0)` |
| `Message` | `redact.Sprint(err).Redact()`. `WithUnredactedMessage()` uses `Error()` |
| `Stack` | the first layer with a reportable stack, from walking `UnwrapOnce` |
| `Link` | the first issue link URL |
| `Fix` | `FlattenHints`, only with `WithHints()`, because hints can hold personal data |
| `Internal` | `telemetry_keys` sorted, `safe_details` without the stack entry. `details` only with `WithDetails()` |

- The library imports gRPC, sentry-go, and gogo protobuf, so this module is its own go.mod and
  P3.
- Library floor v1.12.0, the newest release on Go 1.23. The floor test proves each call exists
  there. Module Go floor 1.23.

## flag-openfeature (package `wlogopenfeature`)

<!-- snippet:sketch -->
```go
func Hook(opts ...Option) openfeature.Hook // Error and Finally
func WithValues() Option                   // also record bool and number values
```

- Each evaluation appends one entry to the `feature_flags` array on the event:
  `{key, type, provider, variant, reason, error_code}`. The array holds at most 50 entries.
- The hook records from `Error` too, because a provider that is not ready calls `Error` with a
  cause and `Finally` with an empty reason.
- It never records `EvaluationContext`, a string or object value, `FlagMetadata`, or the default
  value. With `WithValues()`, it records bool, int, and float values only.
- The SDK does not recover hook panics, so every hook method recovers its own.
- The `otel` preset keeps the array at `attributes.feature_flags`, because OTel defines
  `feature_flag.*` attributes for one evaluation, not a list.
- Library floor v1.15.0, where the `Finally` signature changed. Module Go floor 1.23, which is
  that release's floor.

## Success criteria

1. `log-slog` passes `testing/slogtest` for folded records, under `-race`.
2. Each logger module passes the `log` conformance suite: fold with an event, pass through
   without one, respect the level, cap at 50, keep logger keys, and leak no secret.
3. klog `FromContext(ctx).V(2).Info(...)`, `zerolog.Ctx(ctx).Info()`, and
   `hclog.FromContext(ctx).Named("db").Debug(...)` inside a handler each fold with the plugin
   installed and no other app change.
4. A zap entry with `zap.Any("ctx", ctx)` folds, and no context string reaches any output.
5. A zap sampler that allows one line per second still writes one line for five entries with no
   event.
6. A logrus entry with `WithContext(ctx)` folds, and the wrapped logger writes nothing for it.
7. An oops error with a request attached, recorded through `wlog.Error` or logged as a slog attr,
   leaks no `Authorization` header or body.
8. A validator error for a password field records `field`, `tag`, and `param`, and never the
   password.
9. An OpenFeature evaluation of a missing flag records `reason` `ERROR` and `error_code`
   `FLAG_NOT_FOUND`. A panicking inner step never reaches the SDK.
10. Each module builds and passes its tests at its library floor and its Go floor, and at the
    newest release of each.

## Testing

Each module has black-box tests plus the `log` suite or an extractor table. Floor tests run
through `tools floor`, with a `go.mod` pinned to the library floor in `testdata/floor/`.

## Boundaries

- **Always:** keep the no-fold path byte-identical to the wrapped logger's own output.
- **Ask first:** reading any field this spec marks as never read.
- **Never:** call a library method that renders a request, a response, a context, or a rejected
  input value.

## Open questions

None.
