# Research: logger bridges, error extractors, feature-flag hook

Date: 2026-09-16. Toolchain: `go1.26.1 darwin/arm64`. No file in the wlog repo was changed.

Method: every claim below was checked against source in the module cache
(`~/go/pkg/mod/...`) at the version named. Claims with a "proof" link also ran as a Go test in
the throwaway module `scratchpad/research/logs-work` (`GOWORK=off`, `replace
github.com/jeremygprawira/wlog => /Users/jeremygeraldprawira/Documents/wlog`). Anything not
checked this way says **UNVERIFIED**.

Rerun all proofs:

```bash
cd /private/tmp/claude-502/-Users-jeremygeraldprawira-Documents-wlog/36271c09-50a7-4c73-993b-8468cbbc789b/scratchpad/research/logs-work
GOWORK=off go test -v ./...
```

All proof tests pass, except `slogcheck/TestSlogtestAgainstWlogInput`. That test fails on
purpose. It shows 5 slogtest failures in wlog's current slog input handler (section 2.1).

---

## 0. Headline findings

1. **The current `log/slog` input handler fails `testing/slogtest`** in 5 cases: empty-attr,
   inline-group, multi-With, empty-group-record, nested-empty-group-record. A corrected
   handler that uses the slog handler guide's "groups or attrs" list passes all cases, also
   under `-race` (proof: `slogcheck/fixed_test.go`). See 2.1.
2. **Only 4 of the 8 loggers give ctx on each log call.** These are slog (`*Context` methods
   only), zerolog (`Event.Ctx` / `Context.Ctx`, v1.30.0+), logrus (`Entry.Context`, v1.4.0+),
   and OpenFeature hooks. logr, zap, hclog, and stdlib `log` pass no ctx on a log call. For
   them, the input side must use a **ctx-bound logger stored in ctx**. The ecosystem already
   has a lookup convention for each: `logr.FromContext`, `klog.FromContext`,
   controller-runtime `log.FromContext`, `hclog.FromContext`, `zerolog.Ctx`, charm
   `log.FromContext`, grpc-middleware `ctxzap.Extract`. zap has one more choice: a
   `context.Context` value passed as a field, which follows the OTel `otelzap` convention.
3. **A zerolog hook can read ctx but cannot read fields.** Fields live in a private `[]byte`
   buffer. A hook that folds a line gets only level and message. To keep fields, bind a
   ctx-aware `LevelWriter` and decode the JSON. That breaks under the `binary_log` build tag.
4. **zap's `zap.Any("ctx", ctx)` leaks ctx values.** `*context.valueCtx` is a `fmt.Stringer`.
   Its `String()` prints every string value in the ctx chain. Proof output:
   `"ctx":"context.Background.WithValue(zapcheck.reqKey, Bearer secret-token)"`. A zap input
   core must remove ctx fields before it forwards to the next core.
5. **At `Write` time, the zap input core must run `next.Check` again.** A direct
   `next.Write` call skips sampling. Proof: 5 lines through a sampler instead of 1.
6. **wlog's `defaultExtractor` misses three shapes.** (a) samber/oops v1.20.0+ declares
   `Code() any`, not `Code() string`, so its code falls back to `INTERNAL`. (b)
   `errors.Unwrap` returns nil for `errors.Join` and for multi-`%w` errors, so `Cause` stays
   empty. (c) It uses plain type assertions, not `errors.As`. So a wrapped coded error loses
   its code. SPEC.md says the default extractor uses `errors.As` and `Unwrap() []error`.
7. **Secrets in error libraries.** validator `FieldError.Value()` returns the raw rejected
   input, such as a password. oops `ToMap()`, `MarshalJSON()`, and `LogValue()` include
   `httputil.DumpRequestOut`, which holds the `Authorization` header and the body. A value
   scan is needed to catch these, because a key-name denylist does not. cockroachdb
   `Error()`, hints, and details are unredacted. Only `redact.Sprint(err).Redact()` and
   `GetAllSafeDetails` are PII-free.
8. **An error value in `logs[].attrs` becomes `{}`.** `normalize` JSON-encodes values, and
   error types have no exported fields. `time.Duration` becomes an integer count of
   nanoseconds. Also, `normalize` runs `json.Marshal` while `e.mu` is held. A user
   `MarshalJSON` that logs through wlog on the same event can deadlock. That deadlock is
   **UNVERIFIED** (no test run). The lock order is verified from `logs.go:20-45`.
9. **Go floor.** The existing `log/logrus`, `log/zap`, and `log/zerolog` `go.mod` files say
   `go 1.26.1`, but root says `go 1.23`. The latest versions of validator (1.25.0),
   cockroachdb/errors (1.25.0), open-feature (1.25.0), and charm.land/log/v2 (1.25.8) need
   newer Go than root. Section 1 lists the newest version of each library that still builds
   on Go 1.23.

---

## 1. Version table

"Floor (API)" is the oldest version with every API this report uses. "Newest on go 1.23" is
the newest version whose `go` directive is 1.23 or lower. Both were checked from each
version's `go.mod` and source.

| Module | Latest (date) | License | `go` at latest | Floor (API) | Newest on go ≤1.23 |
|---|---|---|---|---|---|
| `log/slog`, `testing/slogtest` | Go 1.21+ | BSD-3 | n/a | Go 1.21 (`slogtest.Run` 1.22, `DiscardHandler` 1.24, `GroupAttrs`/`Record.Source` 1.25, `NewMultiHandler` 1.26) | n/a |
| `github.com/go-logr/logr` | v1.4.4 (2026-07-20) | Apache-2.0 | 1.18 | v1.0.0 for `LogSink`/`CallDepthLogSink`. v1.2.0 for `CallStackHelperLogSink`. v1.4.0 for `FromSlogHandler`/`ToSlogHandler`/`NewContextWithSlogLogger` (slog parts need `go1.21` build tag) | v1.4.4 |
| `k8s.io/klog/v2` | v2.140.0 | Apache-2.0 | 1.21 | `FromContext` present at v2.140.0 (floor not searched) | v2.140.0 |
| `sigs.k8s.io/controller-runtime` | v0.25.1 | Apache-2.0 **UNVERIFIED** (license file not read) | not read | `log.FromContext` = `logr.FromContext` + global fallback (read at v0.25.1) | not checked |
| `go.uber.org/zap` | v1.28.0 (2026-04-28) | MIT | 1.19 | v1.0.0 for `zapcore.Core`, `MapObjectEncoder`. v1.22.0 for `Logger.Log` (used by the current Drain). v1.28.0 for `CheckPreWriteHook` | v1.28.0 |
| `go.uber.org/zap/exp` (zapslog) | v0.3.0 (2024-10-22) | MIT | 1.19 | needs zap v1.26.0 | v0.3.0 |
| `go.opentelemetry.io/contrib/bridges/otelzap` | v0.20.1 | Apache-2.0 **UNVERIFIED** | not read | used only as a convention reference | n/a |
| `github.com/rs/zerolog` | v1.35.1 (2026-04-20) | MIT | 1.23 | v1.30.0 for `Event.Ctx`, `Event.GetCtx`, `Context.Ctx` (checked: absent in v1.29.1, present in v1.30.0) | v1.35.1 |
| `github.com/sirupsen/logrus` | v1.10.2 (2026-08-25) | MIT | 1.23 | v1.4.0 for `Entry.Context` (absent in v1.3.0) | v1.10.2 (v1.9.4 is `go 1.17`) |
| `github.com/hashicorp/go-hclog` | v1.6.3 (2024-04-01) | MIT | 1.13 | v0.9.2 or older for `WithContext`/`FromContext`. v0.10.0 for `InterceptLogger`/`SinkAdapter` | v1.6.3 |
| `charm.land/log/v2` | v2.0.1 (2026-09-03) | MIT | **1.25.8** | slog.Handler methods since `github.com/charmbracelet/log` v0.3.0 (absent in v0.2.0) | use `github.com/charmbracelet/log` v1.0.0 (`go 1.21`, 2026-03-09) |
| stdlib `log` | Go 1.0+ | BSD-3 | n/a | n/a | n/a |
| `github.com/go-playground/validator/v10` | v10.30.4 (2026-09-03) | MIT | **1.25.0** | v10 API unchanged for what we use (`FieldError` methods). Old versions not searched | v10.27.0 (`go 1.20`). v10.28.0 moves to `go 1.24.0` |
| `github.com/samber/oops` | v1.23.2 (2026-09-14) | MIT | 1.21 | v1.20.0 changed `Code() string` to `Code() any`. Support both by type switch | v1.23.2 |
| `github.com/pkg/errors` | v0.9.1 (2020-01-14) | BSD-2 | no `go.mod` | v0.8.0+ for `StackTrace()` **UNVERIFIED** (only v0.9.1 read). Repo archived **UNVERIFIED** | v0.9.1 |
| `github.com/cockroachdb/errors` | v1.14.0 (2026-06-18) | Apache-2.0 | **1.25.0** | API used exists at v1.14.0. Older not searched | v1.12.0 (`go 1.23.0`, `toolchain go1.23.8`). v1.11.3 is `go 1.19` |
| `github.com/cockroachdb/redact` | v1.1.8 (resolved by tidy) | Apache-2.0 **UNVERIFIED** | not read | `RedactableString.Redact()` / `StripMarkers()` used | n/a |
| `github.com/open-feature/go-sdk` | v1.18.0 (2026-08-13) | Apache-2.0 | **1.25.0** | **v1.15.0**. `Finally` gained the `InterfaceEvaluationDetails` parameter there. It is a breaking change for hook implementers (v1.14.1 has the old shape) | v1.15.x (`go 1.23.0`). v1.16.0 moves to `go 1.24.0` |

Dependency weight: cockroachdb/errors v1.14.0 directly requires `google.golang.org/grpc`,
`github.com/getsentry/sentry-go`, `github.com/gogo/protobuf`, `github.com/gogo/status`,
`github.com/pkg/errors`, and `github.com/cockroachdb/redact`. samber/oops requires
`github.com/samber/lo`, `github.com/oklog/ulid/v2`, and `go.opentelemetry.io/otel/trace`.
charm.land/log/v2 pulls in several `github.com/charmbracelet/*` terminal packages.

---

## 2. Findings about current wlog code

### 2.1 `log/slog/input.go` fails slogtest

Proof: `slogcheck/slogtest_test.go`. It wraps the handler so every call gets a ctx with an
event, then maps `logs[0]` back to the flat shape slogtest wants. `LogLine` has no time, so
the results function adds a fake `time` key, except in the zero-time case, as slogtest's docs
allow.

```
--- FAIL: empty-attr        unexpected key ""          (Any("", nil) kept)
--- FAIL: inline-group      missing key "c"            (Group("", ...) kept under key "")
--- FAIL: multi-With        missing key "a", "c"       (all With attrs pushed inside the groups)
--- FAIL: empty-group-record        missing "a","c"; unexpected "H"
--- FAIL: nested-empty-group-record missing "a","c"; unexpected "H"
```

Root cause: `WithAttrs` joins attrs into one flat slice, and `WithGroup` joins group names
into another. So `Handle` cannot tell which attrs came before which group. `attrsToMap` then
wraps every attr in every group. It also wraps a record that has no attrs.

A fix that passes all 17 cases, also under `-race`, is in `slogcheck/fixed_test.go`. It keeps
an ordered `[]goa{group string; attrs []slog.Attr}`. In `Handle`, it builds the map from the
record attrs. It then walks `goas` backwards. A group wraps a map that is not
empty, and skips an empty one. An attrs entry merges under the map, so record attrs win on a duplicate key. The
per-attr rules are:

- skip `a.Equal(slog.Attr{})`
- `v := a.Value.Resolve()`, then check `KindGroup` after the resolve, because a LogValuer can
  return a group
- skip an empty group
- inline a group whose key is `""`
- convert `KindAny` errors to `err.Error()`

`Resolve` does not recurse into a group, so each group member needs its own resolve
(`slog/value.go:500`).

Other verified behaviour of the current handler (proof: `slogcheck/values_test.go`):

- `Enabled` returns true whenever an event exists. So `DebugContext` lines are folded even
  when the next handler is set to Info, or is `DiscardHandler`. The spec must decide on a
  bridge minimum level.
- `slog.Info(...)` without `Context` passes `context.Background()`
  (`slog/logger.go:208-210`), so the line goes to the next handler and never reaches the
  event. Only `*Context` methods and `Log`/`LogAttrs` with a real ctx fold.
- Values: `errors.New("boom")` becomes `{}`. `1500*time.Millisecond` becomes `1500000000`.
  `time.Time` becomes an RFC 3339 string. A struct becomes a JSON object. Key-based redaction
  does run on folded attrs (`password` and a nested `Token` came out as `[REDACTED]`).
- Key collisions: attrs named `msg` or `level` are safe, because they sit under `attrs`, not
  beside the line's own `msg` and `level`.

### 2.2 `errors.go` default extractor

Proof: `errcheck/err_test.go`, `TestOops`, `TestStdlibMulti`.

- `any(oopsErr).(interface{ Code() string })` is `false` on v1.23.2, so the result is
  `Code: INTERNAL`.
- `errors.Unwrap(errors.Join(a, b)) == nil` and `errors.Unwrap(fmt.Errorf("%w %w", a, b)) ==
  nil`. Both implement `Unwrap() []error`. The extractor's `Cause` is `""` for both.
- validator's `ValidationErrors.Error()` has one line per field, joined by `\n`, and includes
  the Go struct name (`Key: 'Signup.email' Error:Field validation for 'email' failed on the
  'email' tag`). Today that whole text lands in `Message` and again in `Cause`.

### 2.3 Output drains: duplicate JSON keys

Proof: `zapcheck/dup_test.go`. Neither zap nor zerolog removes duplicate keys:

```
zap:     {"level":"info","ts":...,"msg":"real","msg":"dup","level":"dup","ts":"dup"}
zerolog: {"level":"info","message":"dup","level":"dup","message":"real"}
```

zap writes its own keys first, so a later user `msg` wins in a last-wins JSON parser.
zerolog writes `message` last, so the real message wins, but a user `level` still shows up
twice. logrus renames a clash: `prefixFieldClashes` moves `time`, `msg`, `level`, and
`logrus_error` to `fields.<key>` (`formatter.go:55-78`). The current drains skip only
`level`, `operation`, `message`, and `timestamp` (slog). A top-level event field named `msg`,
`ts`, `time`, or `caller` clashes.

---

## 3. Loggers

### Level mapping (all verified from source)

| Library | Native levels | To wlog `LogLine.Level` |
|---|---|---|
| slog | `Level` int: Debug -4, Info 0, Warn 4, Error 8. Custom values give `"INFO+2"` | `<= -4`: debug. `< 4`: info. `< 8`: warn. `>= 8`: error. Do not use `strings.ToLower(String())`, because it gives `"info+2"`. |
| logr | `Info(level int)` V-levels 0..n. `Error(err, ...)` has no level. | V0: info. V≥1: debug. `Error`: error. slog→logr maps Debug to V4 and **Warn to V0 (clamped)**, so warn is lost (`sloghandler.go:levelFromSlog`). |
| zap | Debug -1, Info 0, Warn 1, Error 2, DPanic 3, Panic 4, Fatal 5. `String()`: `debug`, `info`, `warn`, `error`, `dpanic`, `panic`, `fatal` | Debug: debug. Info: info. Warn: warn. Error and above: error. |
| zerolog | Trace -1, Debug 0, Info 1, Warn 2, Error 3, Fatal 4, Panic 5, NoLevel 6, Disabled 7. `String()` reads the **mutable globals** `LevelTraceValue` etc. | Switch on the constants, not on `String()`. Trace and Debug: debug. NoLevel: info. |
| logrus | Panic 0, Fatal 1, Error 2, Warn 3, Info 4, Debug 5, Trace 6. `String()` gives **`"warning"`** | Map Warn to `warn`, Trace to `debug`, Panic and Fatal to `error`. |
| hclog | NoLevel 0, Trace 1, Debug 2, Info 3, Warn 4, Error 5, Off 6. `String()`: `none`, `off` | Trace to debug. NoLevel to info. |
| charm log | Same ints as slog, plus Fatal 12 | Same as slog. |
| stdlib log | none | info. `slog.SetDefault` uses `SetLogLoggerLevel`, default Info. |

### 3.1 log/slog

- **Interface:** `Enabled(context.Context, Level) bool`, `Handle(context.Context, Record)
  error`, `WithAttrs([]Attr) Handler`, `WithGroup(string) Handler`.
- **Input ctx:** yes, from `*Context` methods, `Log`, and `LogAttrs`. Non-ctx methods and
  `NewLogLogger` / the `SetDefault` writer pass `context.Background()`
  (`logger.go:87,105,175`).
- **Handler contract** (`handler.go:30-95`, `slogtest.go`):
  - ignore zero `Record.Time`
  - ignore zero `PC`
  - resolve every value, including inside groups and in `WithAttrs` attrs
  - drop `Attr{}`
  - inline an empty-key group
  - drop an empty group
  - `WithGroup("")` returns the receiver
  - do not print a `WithGroup` that ends up with no attrs, at any depth
  - `Handle` must not keep the `Record` after it returns (use `Clone` to keep it)
  - cancelling the ctx must not affect the record
- **Collisions:** none inside `logs[]`. For output, `slog.TimeKey`, `LevelKey`, `MessageKey`,
  and `SourceKey` are `time`, `level`, `msg`, `source`.
- **Gotchas:**
  - Go 1.26 `slog.NewMultiHandler` fans out, so a wlog handler inside one folds once per
    wrap. **UNVERIFIED**, not run.
  - `testing/slogtest.Run` needs Go 1.22. It is fine in a test file, because root is Go 1.23.
  - `DiscardHandler` needs 1.24, so do not use it in non-test root code while root is
    `go 1.23`.

### 3.2 go-logr/logr (with klog, controller-runtime)

Exact interfaces at v1.4.4:

```go
type LogSink interface {
    Init(info RuntimeInfo)
    Enabled(level int) bool
    Info(level int, msg string, keysAndValues ...any)
    Error(err error, msg string, keysAndValues ...any)
    WithValues(keysAndValues ...any) LogSink
    WithName(name string) LogSink
}
type CallDepthLogSink interface{ WithCallDepth(depth int) LogSink }
type CallStackHelperLogSink interface{ GetCallStackHelper() func() }
type Marshaler interface{ MarshalLog() any }
type SlogSink interface {            // go1.21 build tag
    LogSink
    Handle(ctx context.Context, record slog.Record) error
    WithAttrs(attrs []slog.Attr) SlogSink
    WithGroup(name string) SlogSink
}
func FromSlogHandler(slog.Handler) Logger
func ToSlogHandler(Logger) slog.Handler
func NewContext(ctx, Logger) context.Context
func FromContext(ctx) (Logger, error)   // also accepts a *slog.Logger stored by NewContextWithSlogLogger
func FromContextOrDiscard(ctx) Logger
func NewContextWithSlogLogger(ctx, *slog.Logger) context.Context
func FromContextAsSlogLogger(ctx) *slog.Logger
```

- **Input ctx:** **no.** `LogSink` methods take no ctx. `FromSlogHandler(h)` builds a
  `slogSink` that calls `h.Handle(context.Background(), record)` (`slogsink.go:94`). So
  wrapping wlog's slog handler in logr never folds. Proof:
  `logrcheck/TestFromSlogHandlerLosesCtx`. In the other direction, `ToSlogHandler(logger)`
  passes ctx only to a sink that also implements `SlogSink`. Proof:
  `TestToSlogHandlerPassesCtxOnlyForSlogSink`.
- **Working pattern:** at `wlog.Start` (or in middleware), build
  `logr.New(&eventSink{ctx: ctx, next: logr.FromContextOrDiscard(ctx).GetSink()})` and store
  it with `logr.NewContext`. Code that calls `logr.FromContext`, `klog.FromContext`, or
  controller-runtime `log.FromContext(ctx)` gets it. Proof:
  `logrcheck/TestCtxBoundSinkViaKlogFromContext`, where a klog `FromContext` →
  `WithValues` → `V(2).Info` call and an `Error` call both fold.
  - `klog.FromContext` reads ctx only while `EnableContextualLogging` is on. Its doc says the
    default is on (`contextual.go:141-157`). Kubernetes components can turn it off with a
    feature gate. **UNVERIFIED** for the gate default.
  - Global `klog.InfoS` and `klog.V(n).InfoS` never see ctx.
  - controller-runtime `log.FromContext(ctx, kv...)` returns `logr.FromContext(ctx)` or the
    global `Log`, then `WithValues(kv...)` (`pkg/log/log.go:91-99`).
- **Field conversion:** `keysAndValues` are alternating. Keys must be strings. A non-string
  key or an odd count needs a fallback. funcr uses `"<non-string-key: ...>"` and
  `"<no-value>"` **UNVERIFIED** (funcr not read). Apply `logr.Marshaler` (`MarshalLog()`) to
  values. `WithName` names join with `/` in `slogSink`, and funcr joins with `/` too
  **UNVERIFIED** for funcr. The logr→slog path writes the name under key `logger` and the
  error under key `err` (`slogsink.go:45-49`).
- **Collisions:** sink attrs live under `attrs`, so none in `logs[]`. For output, logr has no
  reserved keys. The concrete sink decides.
- **Gotchas:**
  - A ctx-bound sink captures ctx, so a logger kept past `end()` writes to a sealed event.
    `AppendLog` counts that as a late write. It does not panic.
  - With no event, the bound sink must forward to `next`. It must also forward `Init`.
  - `Error` must still work with a nil `err`. The logr→slog path passes `Error(nil, ...)`
    for `>= LevelError` records.
  - `Enabled(level)` has no ctx, but a bound sink knows its ctx, so it can answer true when
    the event exists.

### 3.3 go.uber.org/zap

Exact interface (`zapcore/core.go:25-45`):

```go
type Core interface {
    LevelEnabler                         // Enabled(Level) bool
    With([]Field) Core
    Check(Entry, *CheckedEntry) *CheckedEntry
    Write(Entry, []Field) error
    Sync() error
}
type Entry struct { Level Level; Time time.Time; LoggerName string; Message string; Caller EntryCaller; Stack string }
type Field struct { Key string; Type FieldType; Integer int64; String string; Interface interface{} }
type CheckPreWriteHook func(Entry, []Field) (Entry, []Field) // v1.28.0, ce.Before(...)
```

- **Input ctx:** **no** ctx in `Core`, `Entry`, or `Logger`. zap has no built-in ctx field. Two
  patterns work:
  1. **ctx as a field** (the otelzap convention, `bridges/otelzap/core.go:19,268-287`). Any
     field whose `Interface` is a `context.Context` is used as the emit ctx, from `With` or
     from the call site. `zap.Any("ctx", ctx)` gives a `StringerType` field (type 25).
     `zap.Reflect` gives `ReflectType`. Both hold ctx in `Interface`. Proof:
     `zapcheck/TestZapCtxField`, where `Debug(..., zap.Any("ctx", ctx))` and
     `With(zap.Reflect("ctx", ctx)).Info(...)` both fold.
  2. **ctx-bound core** for a per-request logger:
     `logger.WithOptions(zap.WrapCore(func(c) zapcore.Core { return boundCore{ctx, c} }))`.
     It is found through an app's ctx convention, such as grpc-middleware v1 `ctxzap.ToContext`
     / `ctxzap.Extract` (read at v1.4.0). This works in `Check` too, because ctx is known.
- **Design rules** (verified):
  - With pattern 1, `Check` cannot see fields. It must `ce.AddCore(e, self)`, and `Enabled`
    must return true for levels you want to fold. Otherwise `Logger.check` drops the entry
    before `Check` (`logger.go:331`).
  - In `Write` with no event, call `next.Check(e, nil)` and then `nce.Write(fields...)`. Do
    **not** call `next.Write` directly. Sampling lives in `sampler.Check`
    (`zapcore/sampler.go:214`). Proof: `zapcheck/TestSamplerBypass`, direct write gives 5
    lines and re-check gives 1 line. Caller info survives, because `Entry.Caller` is set on
    the entry passed to `Write`.
  - Remove ctx fields before forwarding. Otherwise the stringer leak in finding 4 happens.
    Proof: `TestZapAnyCtxLeaksStringValues`.
  - Embedding `zapcore.Core` in a wrapper struct is a trap. A promoted `Check` adds the
    *embedded* value's receiver to the entry, so the wrapper's `Write` never runs. This was
    hit while writing the proof. Declare `Check` on the outer type.
- **Field conversion:** `f.AddTo(zapcore.NewMapObjectEncoder())` gives `enc.Fields
  map[string]any`. Proof output:
  - `zap.Error(err)` becomes `"error":"boom"`
  - `zap.Duration` becomes a `time.Duration` value (JSON integer nanoseconds)
  - `zap.Namespace("ns")` nests later fields under `ns`
  - for an error whose `%+v` output differs from `Error()`, such as a pkg/errors stack,
    `ErrorType` adds `<key>Verbose` (`zapcore/error.go:64-76`)
  - `errorGroup` (`Errors() []error`) adds `<key>Causes`
  - `SkipType` fields do nothing
- **Collisions:** production encoder keys are `level`, `ts`, `msg`, `caller`, `stacktrace`,
  `logger`, with no de-duplication (2.3).
- **zapslog** (`go.uber.org/zap/exp/zapslog` v0.3.0): a `slog.Handler` backed by a
  `zapcore.Core`. `Handle(ctx, record)` **ignores ctx** (`handler.go:138-180`). So
  `slog.New(wlogslog.Handler(zapslog.NewHandler(core)))` is the zero-new-code path for apps
  already on slog-over-zap. zapslog adds a namespace only for a record with at least one
  field. That matches slogtest's empty-group rule.

### 3.4 rs/zerolog

```go
type Hook interface { Run(e *Event, level Level, message string) }
func (e *Event) Ctx(ctx context.Context) *Event      // v1.30.0
func (e *Event) GetCtx() context.Context             // v1.30.0, returns Background when unset
func (c Context) Ctx(ctx context.Context) Context    // v1.30.0, logger-level ctx
func (e *Event) Discard() *Event                     // sets level Disabled, write() skips
func (l Logger) WithContext(ctx) context.Context; func Ctx(ctx) *Logger
type LevelWriter interface { io.Writer; WriteLevel(Level, []byte) (int, error) }
```

- **Input ctx:** yes, for a caller that used `.Ctx(ctx)` on the event, or `With().Ctx(ctx)` on
  the logger. Proof: `zerologcheck/TestZerologHookCtx`. Both forms fold. `e.Discard()` in the
  hook stops the normal write, so the no-ctx line still reaches the writer.
- **Fields:** a hook **cannot** read them. `Event` has only `buf []byte` and no exported
  accessor (`event.go:23-32`). Exported no-arg methods are `Enabled`, `Discard`, `Send`,
  `CreateDict`, `CreateArray`, `Stack`, `GetCtx`, `Timestamp`. Hooks run before the message
  is appended (`event.go:150-156`).
- **Full-fidelity pattern:** a ctx-bound `LevelWriter` on a per-request logger, stored with
  `logger.Output(w).WithContext(ctx)` and found with `zerolog.Ctx(ctx)`. It receives the
  full JSON line. `json.Unmarshal` it, then remove `MessageFieldName` (`"message"`),
  `LevelFieldName`, and `TimestampFieldName` (`"time"`). Proof:
  `TestZerologWriterBoundToCtx` folds `{error: boom, n: 3, svc: a}`.
  - Cost: one JSON decode per line.
  - Breaks with `-tags binary_log` (CBOR encoder).
  - A user `Str("message", ...)` makes a duplicate key, and `json.Unmarshal` keeps the last
    one, so that user field is lost.
- **Level filter runs before hooks** (`log.go:488-496`, `should`). A Debug event on an Info
  logger is `nil` and hooks never run, so debug cannot fold unless the logger level is
  lowered.
- **Collisions:** `level`, `message` (not `msg`), `time`, `error`, `caller`, `stack`. These
  are all mutable package globals.
- **Gotchas:**
  - `DefaultContextLogger` is a global.
  - `Logger.WithContext` does not store a disabled logger (`ctx.go:34`).
  - A hook sees `NoLevel` for `Log()` events.

### 3.5 sirupsen/logrus

```go
type Hook interface { Levels() []Level; Fire(*Entry) error }
type Entry struct {
    Logger *Logger; Data Fields; Time time.Time; Level Level; Caller *runtime.Frame
    Message string; Buffer *bytes.Buffer; Context context.Context; /* err string */
}
func (entry *Entry) WithContext(ctx context.Context) *Entry  // also Logger.WithContext
var ErrorKey = "error"                                       // WithError key, mutable global
```

- **Input ctx:** yes, `Entry.Context`, for a caller that used `WithContext(ctx)`. `Entry.Data`
  holds every field, with raw values (an error stays an `error`). Proof:
  `logruscheck/TestLogrusHookCtx` folds `{error: boom, msg: dup}`, level `warning`.
- **Suppressing passthrough:** a hook cannot stop the write. `fireHooks` then `write()`
  (`entry.go:337-347`), and a `Fire` error only stops later hooks. The working method wraps
  `Logger.Formatter`. If `Entry.Context` holds an event, the wrapper returns `nil, nil`, and
  `Out.Write(nil)` writes nothing. Proof: same test, where only the no-ctx line reached the
  buffer.
- **Level filter runs before hooks** (`Entry.Log` checks `IsLevelEnabled`). Debug does not
  fold on an Info logger.
- **Conversion:** copy `Data`, turn an `error` into `.Error()`, and map `Level` as in the
  table. `Fields` is `map[string]any`, so its order is random. That is fine for a map.
- **Collisions:** `msg`, `level`, `time`, `logrus_error`, `func`, `file` (`formatter.go:21-27`).
  The formatter renames clashes to `fields.X`.
- **Gotchas:**
  - v1.10.x needs Go 1.23 (`maps.Clone`).
  - `Hooks` are per `*Logger` and shared by every goroutine, so a hook must be stateless and
    read ctx from the entry.
  - A hook that logs through the same logger again reads `logger.mu` in `hooksForLevel`. In
    v1.10.2 the lock is released before firing (`logger.go:422-430`). Older versions:
    **UNVERIFIED**.

### 3.6 hashicorp/go-hclog

```go
type Logger interface {
    Log(level Level, msg string, args ...interface{})
    Trace/Debug/Info/Warn/Error(msg string, args ...interface{})
    IsTrace/IsDebug/IsInfo/IsWarn/IsError() bool
    ImpliedArgs() []interface{}
    With(args ...interface{}) Logger
    Name() string; Named(name string) Logger; ResetNamed(name string) Logger
    SetLevel(level Level); GetLevel() Level
    StandardLogger(opts *StandardLoggerOptions) *log.Logger
    StandardWriter(opts *StandardLoggerOptions) io.Writer
}
type InterceptLogger interface { Logger; RegisterSink(SinkAdapter); DeregisterSink(SinkAdapter); NamedIntercept(string) InterceptLogger; ResetNamedIntercept(string) InterceptLogger; StandardLoggerIntercept(*StandardLoggerOptions) *log.Logger; StandardWriterIntercept(*StandardLoggerOptions) io.Writer }
type SinkAdapter interface { Accept(name string, level Level, msg string, args ...interface{}) }
func WithContext(ctx context.Context, logger Logger, args ...interface{}) context.Context
func FromContext(ctx context.Context) Logger   // falls back to global L()
```

- **Input ctx:** **no.** No method takes ctx, and `SinkAdapter.Accept` has none. Only the
  ctx-bound-logger pattern works: embed `hclog.Logger`, override `Log`, the five level
  methods, `With`, and `Named`, then store it with `hclog.WithContext`. Proof:
  `hclogcheck/TestHclogFromContext` folds `{a: 1, req: 1, rows: 3}` from
  `FromContext(ctx).Named("db").With("a",1).Debug(...)`.
- **Do not** use `RegisterSink` for per-request folding. Sinks sit on the shared intercept
  logger, and `interceptLogger.log` sends every goroutine's lines to every sink under one
  mutex (`interceptlogger.go:49-60`). Folding there mixes lines across requests.
- **Conversion:** args alternate. An odd count gets `EXTRA_VALUE_AT_END` (`MissingKey`).
  Include `ImpliedArgs()` from `With`. The name comes from `Name()`.
- **Collisions:** the JSON format uses `@message`, `@level`, `@timestamp`, `@module`,
  `@caller` (`intlogger.go:689-711`). Plain keys do not clash.
- **Gotchas:**
  - Methods not overridden (`ResetNamed`, `StandardLogger`, `StandardWriter`) return the
    unbound inner logger and escape the binding.
  - `IsDebug()` and similar report the inner level, so callers that guard with
    `if l.IsDebug()` skip folding debug lines.

### 3.7 charmbracelet/log

- **Module path:** `charm.land/log/v2` (v2.0.1, `go 1.25.8`). `github.com/charmbracelet/log`
  v1.0.0 is `go 1.21`. `go list` also lists `github.com/charmbracelet/log/v2` versions, but
  the v2 `go.mod` declares `charm.land/log/v2`, so a fetch by the GitHub path fails
  **UNVERIFIED** (not fetched).
- **slog:** `*log.Logger` implements `slog.Handler` (`logger_121.go`, `var _ slog.Handler =
  (*Logger)(nil)`), since v0.3.0.
  - `Handle(ctx, ...)` ignores ctx, and `Enabled` ignores ctx.
  - `WithGroup(name)` becomes a prefix (`"prefix":"G"`), not a nested group. Proof output:
    `{"level":"info","prefix":"G","msg":"grouped","x":1}`. It does not meet slogtest.
- **Options:** `TimeFunction`, `TimeFormat`, `Level`, `Prefix`, `ReportTimestamp`,
  `ReportCaller`, `CallerFormatter`, `CallerOffset`, `Fields []any`, `Formatter`
  (Text/JSON/Logfmt).
- **Keys** (mutable globals): `time`, `msg`, `level`, `caller`, `prefix`.
- **ctx helpers:** `log.WithContext(ctx, *Logger)` and `log.FromContext(ctx)` (falls back to
  `Default()`).
- **Input:** no hooks and no ctx on `Info(msg any, kv ...any)`. The practical paths are:
  - `slog.New(wlogslog.Handler(charmLogger))`, where charm is the `next` handler. Proof:
    `charmcheck/TestCharmAsSlogHandler` folds with ctx and passes through without.
  - a ctx-bound `*log.Logger` is not possible, because `Logger` is a struct, not an
    interface.
- **Output:** `wlogslog.Drain(charmLogger)` works as is.

### 3.8 stdlib log

- `log.Logger` exposes `SetOutput(io.Writer)`, `Writer()`, `SetFlags`, `SetPrefix`, and
  `Output`. **No ctx anywhere.**
- `slog.SetDefault(l)` redirects the global `log` package into `l.Handler()` through
  `handlerWriter`, with `context.Background()`, level `SetLogLoggerLevel` (default Info), and
  flags reset to 0 (`slog/logger.go:61-105`). So `log.Printf` inside a request never folds,
  even with wlog's slog handler installed. Proof: `stdlogcheck/TestStdlog`.
- **Only pattern:** a per-request `log.New(eventWriter{ctx}, "", 0)` handed to code that
  accepts a `*log.Logger`. Proof: same test. Global `log.Print*` and
  `http.Server.ErrorLog` (server-wide) cannot be tied to a request.

---

## 4. Error libraries

Target: `wlog.ErrorInfo{Code, Message, Kind, Status, Cause, Stack, Why, Fix, Link, Attrs,
Data, Internal}`. Per SPEC-v1.2-additions, `Data` is safe to send to a client and `Internal`
is log-only. SPEC.md also plans `type`, `causes`, and `caller`. Precedent (SPEC-errors-herr):
`Message` is internal by default, and a public message needs an opt-in.

### 4.1 stdlib `errors.Join` and multi-`%w`

- `errors.Join(a, b)` and `fmt.Errorf("%w %w", a, b)` both implement `Unwrap() []error`.
  `errors.Unwrap` returns nil for both. `errors.Is` and `errors.As` walk the tree. `Join`'s
  `Error()` joins with `\n`. Proof: `errcheck/TestStdlibMulti`.
- Mapping: `Cause` is the first child's `Error()`. The planned `causes` field holds each
  child's `Error()`, capped for G4. Walk the tree depth-first with a cap. A cycle is not
  possible through the stdlib types, but a user `Unwrap` can loop, so cap the depth.
- A stdlib-only walker that handles both shapes is in `errcheck/duck_test.go` (`walk`).

### 4.2 go-playground/validator/v10

```go
type ValidationErrors []FieldError                 // errors.As(err, &ve) works (verified)
type InvalidValidationError struct{ Type reflect.Type } // returned as *InvalidValidationError
type FieldError interface {
    Tag() string; ActualTag() string; Namespace() string; StructNamespace() string
    Field() string; StructField() string; Value() interface{}; Param() string
    Kind() reflect.Kind; Type() reflect.Type; Translate(ut ut.Translator) string; Error() string
}
func (ve ValidationErrors) Translate(ut ut.Translator) ValidationErrorsTranslations // map[string]string keyed by Namespace
```

Proof output (`errcheck/TestValidator`, JSON tag names registered):

```
ns="Signup.email"    structNs="Signup.Email"    field="email"    tag="email" param=""           value="bob@"
ns="Signup.password" structNs="Signup.Password" field="password" tag="min"   param="12"         value="hunter2"
ns="Signup.role"     structNs="Signup.Role"     field="role"     tag="oneof" param="admin user" value="root"
invalid input: errors.As(*InvalidValidationError)=true, Error()="validator: (nil int)"
```

| ErrorInfo | Source | Notes |
|---|---|---|
| Code | none. A constant such as `VALIDATION_FAILED`, set by option | validator has no code |
| Message | a summary, such as `"validation failed on 3 fields"` | Not `ve.Error()`, which is multi-line dev text with Go type names |
| Kind | constant `validation` | |
| Status | option, default 400 (or 422) | none native |
| Cause | empty | validator errors do not wrap |
| Stack | empty | |
| Why / Fix / Link | empty. Optional per-tag `Fix` text from a user map | |
| **Data** | `fields: [{field, tag, param}]` | `field` is `Namespace()` without its first segment (the root struct name). If the app called `RegisterTagNameFunc`, it uses JSON names. Otherwise it uses Go names. `param` is static struct-tag text, safe to show. |
| Internal | `struct_namespace`, `actual_tag`, `kind`, `type` per field | Go type and field names are internal detail |
| Attrs | nil | |
| `*InvalidValidationError` | Code `INTERNAL`, Kind `internal`, Status 500, Message `Error()` | a programmer error (non-struct passed) |

Secrets and PII:

- **`Value()` holds the raw rejected input** (password, email, card number). Never read it.
- `Error()` is safe: its format is `Key: '%s' Error:Field validation for '%s' failed on the
  '%s' tag` (`errors.go:13`).
- The built-in `translations/en` never calls `Value()` (grep count 0). A custom
  `TranslationFunc` can, so `Translate` output is only as safe as the app's translators.
- `Param()` of cross-field tags names other fields (`eqfield=Password`). That is not a
  secret, but it is internal structure.
- G4: a `dive` over a large slice returns one `FieldError` per bad element. Cap `fields` and
  count what is dropped.
- Stdlib-only detection is awkward, because `ValidationErrors` is a named slice. Use the
  typed import in its own `go.mod` (`errors/validator`, as CAPABILITIES.md plans).

### 4.3 samber/oops

Detection: `oops.AsOops(err)` does a fast type assertion and then `errors.As` into the value
type `oops.OopsError`. Plain `errors.As(err, &oops.OopsError{})` also works (proof). Every
accessor except `Span()` returns the **deepest** value in the oops chain, and maps merge
across layers.

Accessors at v1.23.2 (`error.go`):

- `Code() any` (was `string` before v1.20.0)
- `Time()`, `Duration()`, `Domain()`, `Tags()`, `HasTag()`, `Context() map[string]any`
- `Trace()`: an explicit trace wins, else an auto-generated ULID
- `Span()`: the current layer, not the deepest
- `Hint()`, `Public()`, `Owner()`
- `User() (string, map[string]any)`, `Tenant() (string, map[string]any)`
- `Request() *http.Request`, `Response() *http.Response`
- `Stacktrace() string`, `StackFrames() []runtime.Frame`, `Sources() string`
- `Layers() []*OopsErrorLayer`, `LogValue() slog.Value`, `ToMap()`, `MarshalJSON()`,
  `Format()` (with `%+v` verbose)
- `Unwrap() error`, `Is(error) bool`

Proof output (`TestOops`):

- `Code="user_not_found"`. This is the deepest code. The outer layer set `"outer"`.
- `Domain="auth"`, `Tags=[db]`, `Context=map[email:bob@example.com]`
- `Hint="check the users table"`, `Public="User not found."`
- `Trace="01M2MSQ..."` (auto), `User="u-1" map[email:bob@example.com]`
- `Stacktrace` starts with
  `"Oops: lookup bob@example.com failed\n  --- at /abs/path/err_test.go:49 TestOops()"`

| ErrorInfo | Source | Notes |
|---|---|---|
| Code | `Code()`: `string` as is, other types through `fmt.Sprint`, nil gives `INTERNAL` | type switch supports both old and new oops |
| Message | `Error()` by default. `Public()` only with a `WithPublicMessage` option (herr precedent) | `Error()` includes `Errorf` args, which can be PII |
| Kind | `Domain()` | |
| Status | none native | leave 0, or map a code through the catalog |
| Cause | `Unwrap().Error()`, skipped for a nil cause | |
| Stack | `Stacktrace()` | absolute build paths. Depth is set by the global `oops.StackTraceMaxDepth` (default 10) |
| Why | empty | no oops field |
| Fix | `Hint()` | "debugging hint for developers" |
| Link | empty | |
| Data | `public: Public()` | the only field oops calls user-safe |
| Internal | `tags`, `context`, `owner`, `span`, `user_id`, `tenant_id`, `duration`, `time`. Store `Trace()` as `oops_trace`. | Do not overwrite wlog's trace id |
| Attrs | nil, or `Context()` for old consumers | |

Secrets and PII:

- **`Request()` / `Response()` and every rendering that uses them.** `ToMap()`,
  `MarshalJSON()`, and `LogValue()` add `httputil.DumpRequestOut(req, withBody)`, which holds
  the `Authorization` header, cookies, and the body. Proof: `ToMap request contains
  Authorization: true, body password: true`. `LogValue` adds the same `request` string
  (`error.go:585-697`, line 84 of that range).
  - So logging an oops error as a slog attr folds a raw request dump into `logs[].attrs`. A
    key-name denylist cannot catch it.
  - The extractor must never call `ToMap`, `MarshalJSON`, `LogValue`, `Request`, or
    `Response`.
- `User()` / `Tenant()` data maps: PII by design. Put only IDs in `Internal`. Drop the maps,
  or let an opt-in allow them.
- `Context()`: arbitrary app values. It goes through the redactor, but values are unknown.
- `Sources()`: source code fragments. The global `SourceFragmentsHidden` defaults to true.
- `Error()` / `Stacktrace()` first line: formatted args (in the proof, the email).
- `DereferencePointers` defaults to true, so pointer values are dereferenced into maps.

### 4.4 pkg/errors

- There is no exported interface. The documented stable contract is
  `interface{ StackTrace() errors.StackTrace }` (`errors.go:68-90`).
- `type StackTrace []Frame`, and `type Frame uintptr`. The stored value is the program
  counter plus 1. `Frame.pc()` subtracts 1.
- Types with a stack: `*fundamental` (`New`, `Errorf`) and `*withStack` (`Wrap`, `Wrapf`,
  `WithStack`). `*withMessage` has none.
- All types implement `Cause() error`, and `Unwrap() error` from v0.9.0 (`go113.go`).
- Rendering **without importing pkg/errors**:
  1. **Reflection** (proof: `errcheck/TestPkgErrors`, `pkgStack`):
     - Walk `errors.Unwrap` and keep the deepest error whose method `StackTrace` returns a
       slice of a `uintptr`-kind element.
     - Copy those values to `[]uintptr` and pass them to `runtime.CallersFrames`.
     - Proof: 3 frames, top frame = the `pkgerrors.New` call site.
     - The same reflection finds cockroachdb stacks (their `StackTrace` type is an alias of
       pkg/errors'). Proof: 3 frames in `TestCockroach`.
     - About `CallersFrames` and the +1: this passes the stored values unchanged, and the
       reported line matched. **UNVERIFIED** whether inlined frames are always right.
  2. **`fmt.Sprintf("%+v", err)`** prints the message, then `function\n\tfile:line` for each
     frame, for **every** layer with a stack. That is long and repeats frames, and it is
     verified from the first lines of output. It needs no import and no reflection.
- Mapping:
  - Code: `INTERNAL`.
  - Message: `Error()`.
  - Cause: the deepest error from the `Cause()` loop or the `Unwrap` loop, as `Error()`.
    `pkgerrors.Cause` stops at the first error without `Cause()`. Proof: through a stdlib
    `fmt.Errorf` wrapper, `Cause` returned the outer error unchanged.
  - Stack: the deepest stack, as frames.
  - Everything else is empty.
- Secrets: messages only. Stacks show build paths. zap's `ErrorType` adds `errorVerbose` with
  the full `%+v` stack to zap output.

### 4.5 cockroachdb/errors

Top-level API used (v1.14.0):

```go
func GetAllHints(err error) []string;  func FlattenHints(err error) string        // de-duplicated
func GetAllDetails(err error) []string; func FlattenDetails(err error) string
func GetAllSafeDetails(err error) []SafeDetailPayload // outer→inner; {OriginalTypeName, ErrorTypeMark, SafeDetails []string}
func GetSafeDetails(err error) SafeDetailPayload      // this layer only
func GetReportableStackTrace(err error) *ReportableStackTrace // = *sentry.Stacktrace, THIS LAYER ONLY
func GetTelemetryKeys(err error) []string             // map-built: order is random
func GetAllIssueLinks(err error) []IssueLink          // {IssueURL, Detail}
func GetDomain(err error) Domain                      // NoDomain prints "error domain: <none>"
func UnwrapOnce(err error) error; func UnwrapAll(err error) error
exthttp.GetHTTPCode(err error, defaultCode int) int   // read from source, not run
extgrpc.GetGrpcCode(err error) codes.Code             // read from source, not run
// duck-typable per layer, no import needed (proof: errcheck/duck_test.go):
interface{ ErrorHint() string }; interface{ ErrorDetail() string }; interface{ SafeDetails() []string }
```

Proof output (`TestCockroach`):

```
Error()="user bob@example.com not found"
hints=["See: https://example.com/1" "sign up first"]   // issue links add a hint
details=["email=bob@example.com"]  telemetry=["auth.missing" "a.b"]
safe: *safedetails.withSafeDetails ["attempt 3 for ×"]     // unsafe arg replaced by ×
safe: *withstack.withStack ["\nexample.com/...TestCockroach\n\t/abs/path:105\n..."]  // whole stack as one "safe" string
safe: *errutil.leafError ["user × not found"]
GetReportableStackTrace(outer)=<nil>; walking UnwrapOnce finds 3 frames at *withstack.withStack
redactable="user ‹bob@example.com› not found" redacted="user ‹×› not found" stripped="user bob@example.com not found"
```

There is no native error code. `GetTelemetryKeys` is the nearest match. `pgcode` belongs to
CockroachDB itself, not to this library **UNVERIFIED**.

| ErrorInfo | Source | Notes |
|---|---|---|
| Code | option: first of **sorted** `GetTelemetryKeys`, else `INTERNAL` | sort to keep G6 (the map order is random) |
| Message | `string(redact.Sprint(err).Redact())` by default. Raw `Error()` only by opt-in | redaction markers are `‹` U+2039 and `›` U+203A, redacted text is `×` |
| Kind | `GetDomain(err)`, skipped for `errors.NoDomain` | |
| Status | `exthttp.GetHTTPCode(err, 0)` | pulls in the `exthttp` package |
| Cause | redacted `UnwrapAll(err)` | |
| Stack | walk `UnwrapOnce` until `GetReportableStackTrace(e) != nil`, then render frames. Or use the reflection walk from 4.4 | the outer layer returns nil |
| Why | empty, or `FlattenDetails` by opt-in | the doc says details can contain PII |
| Fix | `FlattenHints` | the doc says hints can contain PII and are for end users |
| Link | first `GetAllIssueLinks()[i].IssueURL` | |
| Data | empty by default | the library calls nothing client-safe, and hints can hold PII |
| Internal | `telemetry_keys` (sorted), `safe_details` (flattened, **without** the `withStack` entry, which is the full stack), `details` by opt-in | |

Secrets and PII:

- `Error()`, `UnwrapAll().Error()`, hints, and details are all unredacted.
- `%+v` output includes hints, details, and stacks.
- Only `GetAllSafeDetails` and `redact.Sprint(...).Redact()` are PII-free. The `withStack`
  "safe" detail still shows absolute build paths.
- Weight: the typed import pulls grpc, sentry-go, and gogo protobuf. A stdlib-only duck-typed
  extractor gets hints, details, safe details, and stacks (reflection) without them. It
  cannot redact `Error()`, which needs `redact.Sprint`, or read the domain, telemetry keys,
  or HTTP code, which are private wrapper types.

---

## 5. OpenFeature go-sdk

Exact interface (v1.18.0, `openfeature/hooks.go`). This shape exists from v1.15.0 on:

```go
type Hook interface {
    Before(ctx context.Context, hookContext HookContext, hookHints HookHints) (*EvaluationContext, error)
    After(ctx context.Context, hookContext HookContext, flagEvaluationDetails InterfaceEvaluationDetails, hookHints HookHints) error
    Error(ctx context.Context, hookContext HookContext, err error, hookHints HookHints)
    Finally(ctx context.Context, hookContext HookContext, flagEvaluationDetails InterfaceEvaluationDetails, hookHints HookHints)
}
type UnimplementedHook struct{} // embed to implement only Finally/Error
// HookContext accessors:
FlagKey() string; FlagType() Type /* Boolean|String|Float|Int|Object, String(): "bool","string",... */
DefaultValue() any; ClientMetadata() ClientMetadata /* Domain() */; ProviderMetadata() Metadata /* .Name */
EvaluationContext() EvaluationContext
type InterfaceEvaluationDetails = GenericEvaluationDetails[any] // {Value any; EvaluationDetails{FlagKey, FlagType, ResolutionDetail}}
type ResolutionDetail struct { Variant string; Reason Reason; ErrorCode ErrorCode; ErrorMessage string; FlagMetadata FlagMetadata }
// Reason: DEFAULT TARGETING_MATCH SPLIT DISABLED STATIC CACHED UNKNOWN ERROR
// ErrorCode: PROVIDER_NOT_READY PROVIDER_FATAL FLAG_NOT_FOUND PARSE_ERROR TYPE_MISMATCH TARGETING_KEY_MISSING INVALID_CONTEXT GENERAL
```

- **ctx:** yes. Every hook method gets the invocation ctx passed to
  `client.BooleanValue(ctx, ...)` and the other evaluation calls. Proof:
  `ofcheck/TestOpenFeatureHookCtx`. A `Finally` hook appended to `flags` on the wlog event:
  - `{key:new-ui, type:bool, provider:InMemoryProvider, variant:on, reason:STATIC}`
  - `{key:missing, type:string, variant:"", reason:ERROR, error_code:FLAG_NOT_FOUND}`
- **Registration levels:** API with `openfeature.AddHooks(...)` (global, or
  `(*EvaluationAPI).AddHooks` on an isolated `isolated.NewAPI()`, new in v1.18.0). Client with
  `client.AddHooks(...)`. Invocation with the `openfeature.WithHooks(...)` option. The
  provider adds its own through `Provider.Hooks()`.
- **Order** (`client.go:672-819`): `Before` runs API → client → invocation → provider.
  `After`, `Error`, and `Finally` run in reverse. `Finally` is deferred, so it runs on every
  path after the UTF-8 key check.
- **Gotchas** (from source):
  - An invalid UTF-8 flag key returns **before** any hook runs.
  - On `PROVIDER_NOT_READY` / `PROVIDER_FATAL`, only `Error` hooks get the error. `Finally`
    gets details with an empty `Reason` and `ErrorCode`. Record from both `Error` and
    `Finally`.
  - Hook calls have **no `recover`**, so a panicking hook crashes the evaluation call. The
    wlog hook must never panic (G3).
  - An `After` hook that returns an error aborts later `After` hooks and turns the
    evaluation into an error.
  - Global `AddHooks` is package-level state, so prefer client-level registration in
    examples and tests.
- **Field naming:** the SDK's `telemetry` package uses OTel keys `feature_flag.key`,
  `feature_flag.result.variant`, `feature_flag.result.reason`, `feature_flag.provider.name`,
  `feature_flag.context.id`, `feature_flag.result.value`, `feature_flag.set.id`,
  `feature_flag.version`, and event name `feature_flag.evaluation`. Use them under
  `FieldsOTel()`.
- **Secrets and PII:**
  - `EvaluationContext()` holds the targeting key and attributes, usually a user id or email.
    Never copy it whole.
  - `Value` of `Object` / `String` flags can hold config or secrets. Log only the variant by
    default.
  - `FlagMetadata` is provider-defined.
  - `ErrorMessage` is provider text.
  - `DefaultValue()` is app code.

---

## 6. UNVERIFIED items (collected)

- controller-runtime, otelzap, and cockroachdb/redact license files were not read.
- A wlog slog handler inside `slog.NewMultiHandler` folds once per wrapping handler. Not run.
- `normalize` calls `json.Marshal` under `e.mu`, so a user `MarshalJSON` that logs through
  wlog on the same event can deadlock. The lock order is verified. No deadlock test was run.
- klog `ContextualLogging` feature-gate default in Kubernetes components.
- logr `funcr` key formatting for bad keys and odd counts. Name joining in funcr.
- logrus hook locking in versions before v1.10.2.
- `github.com/charmbracelet/log/v2` GitHub path fetch failure (the canonical path is
  `charm.land/log/v2`, verified from `go.mod`).
- pkg/errors: repository archived status. Lowest version with `StackTrace()`.
- Inlined-frame accuracy for pkg/errors `Frame` values passed to `runtime.CallersFrames`.
- `pgcode` lives outside cockroachdb/errors.
- validator, cockroachdb/errors, and klog floors older than the versions read were not
  searched.
- `exthttp.GetHTTPCode` / `extgrpc.GetGrpcCode` were read from source only, not run.

## 7. Proof files

All under
`/private/tmp/claude-502/-Users-jeremygeraldprawira-Documents-wlog/36271c09-50a7-4c73-993b-8468cbbc789b/scratchpad/research/logs-work/`:

| File | Proves |
|---|---|
| `slogcheck/slogtest_test.go` | current wlog slog input fails 5 slogtest cases (intended FAIL) |
| `slogcheck/fixed_test.go` | goas-list handler passes slogtest (`-race`) |
| `slogcheck/values_test.go` | value kinds, debug capture, non-ctx `Info` not folded, redaction of folded attrs |
| `logrcheck/logr_test.go` | `FromSlogHandler` drops ctx. ctx-bound sink through `klog.FromContext`. `ToSlogHandler` ctx only for `SlogSink` |
| `zapcheck/zap_test.go` | ctx-field core folds. `zap.Any(ctx)` leaks ctx string values |
| `zapcheck/sampler_test.go` | direct `next.Write` skips sampling. Re-check keeps it and keeps caller |
| `zapcheck/dup_test.go` | duplicate keys in zap and zerolog |
| `zerologcheck/zerolog_test.go` | hook reads `GetCtx`, event and logger ctx, `Discard`. Writer-bound JSON decode keeps fields |
| `logruscheck/logrus_test.go` | hook reads `Entry.Context` and `Data`. Formatter wrapper stops passthrough |
| `hclogcheck/hclog_test.go` | embedded ctx-bound `hclog.Logger` through `hclog.FromContext` |
| `charmcheck/charm_test.go` | charm as slog `next`. `WithGroup` becomes a prefix |
| `stdlogcheck/stdlog_test.go` | `log.Printf` through `slog.SetDefault` never folds. Per-request `log.New` writer folds |
| `errcheck/err_test.go` | validator accessors and `Value()` exposure. oops accessors, request-dump leak, `Code() any` mismatch. pkg/errors reflection stack. cockroach hints, details, safe details, redaction, layer-only stack. stdlib multi-unwrap |
| `errcheck/duck_test.go` | stdlib-only duck-typed walk for cockroach (`ErrorHint`, `ErrorDetail`, `SafeDetails`) and oops |
| `ofcheck/of_test.go` | OpenFeature `Finally` hook gets ctx and records flag results on the event |
