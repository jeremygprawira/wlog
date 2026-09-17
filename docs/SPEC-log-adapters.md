# Spec: log-adapters

> Module ids `log-slog`, `log-zap`, `log-zerolog`, `log-logrus`.
> Package `github.com/jeremygprawira/wlog/log/slog` (`wlogslog`) lives in the root module,
> since `log/slog` is standard library. `log/zap`, `log/zerolog`, `log/logrus` each get their
> own `go.mod`. Depends on `core`. Project-wide rules in [SPEC.md](SPEC.md) apply.

## Objective

Let a wlog event reach a team's existing logger. Let an existing logger's calls join a wlog
event too. A team can then adopt wlog without ripping out its current logging setup on day one.

## Pinned versions

| Logger | Module | Version |
|---|---|---|
| zap | `go.uber.org/zap` | v1.28.0 |
| zerolog | `github.com/rs/zerolog` | v1.35.1 |
| logrus | `github.com/sirupsen/logrus` | v1.10.2 |

`log/slog` uses only the standard library `log/slog` package.

## Behaviour

### Output: wlog event to X (slog, zap, zerolog, logrus)

<!-- snippet:sketch -->
```go
func Drain(handler slog.Handler) wlog.Drain     // log/slog
func Drain(logger *zap.Logger) wlog.Drain       // log/zap
func Drain(logger zerolog.Logger) wlog.Drain    // log/zerolog
func Drain(logger *logrus.Logger) wlog.Drain    // log/logrus
```

Each `Drain` turns one wlog event into one record in the target logger. The level maps from
SPEC-core.md's `Level` to each library's own level type. Every nested group stays a nested value
in the target format: a slog group, a `zap.Object` or `zap.Any`, or a zerolog dict. logrus has no
native nesting, so its adapter flattens each nested group to dotted keys, for example
`http.status`, inside one flat `logrus.Fields` map.

### Input: X calls into a wlog event (slog only, for v1)

<!-- snippet:sketch -->
```go
func Handler(next slog.Handler) slog.Handler
```

Wraps an existing `slog.Handler`. Take a record made with a `ctx` that carries a wlog event
from `wlog.Start`. This handler appends that record to the event's `logs[]` array, instead of
sending it to `next`. The array is capped like any other array field (SPEC-core.md's
`maxArrayLen`). A record with no such `ctx` still passes through to `next` unchanged. This
makes `slog.SetDefault(slog.New(wlogslog.Handler(...)))` safe to set globally, even before every
call site has an event in its context.

zap, zerolog, and logrus each already support writing to a `io.Writer` or a custom core/hook, so
routing them *into* wlog is deferred: a user who wants that today can point their existing logger
at a small `io.Writer` that calls `wlog.Info`, without needing a dedicated adapter package.

## Success Criteria

1. `wlogslog.Drain`, `wlogzap.Drain`, `wlogzerolog.Drain`, and `wloglogrus.Drain` each render one
   wlog event as one record, with nested groups intact for the three that support nesting.
2. `wlogslog.Handler` appends slog records made inside an event to that event's `logs[]`, and
   passes through everything else, proven against a third-party library that logs via
   `slog.Default()`.
3. `log/slog` has zero non-stdlib imports. `log/zap`, `log/zerolog`, and `log/logrus` each have
   exactly one non-stdlib, non-`wlog` import: their own logging library.

## Testing

Package `<name>_test`, black-box. `log/zap` and `log/zerolog` use their own `zaptest`/in-memory
writer helpers. `log/logrus` uses `logrus.Logger.Out` set to a `bytes.Buffer`.

## Boundaries

- **Always:** map levels the same way across all four adapters (see SPEC-core.md's `Level`).
- **Ask first:** adding a second input adapter (zap/zerolog/logrus to wlog) beyond slog.
- **Never:** let an output adapter drop a nested group silently. If the target format has no
  nesting, flatten the group instead.

## Open Questions

None.
