# Compare wlog with other loggers

These pages compare wlog with one library each. Every claim comes from the wlog code, its
tests, or the library's own documentation. A claim that nobody measured says so.

## The rule

A comparison page states a tested fact or names the test. The repository holds one
benchmark budget, `bench/baseline.txt`, and that budget covers wlog alone. No page claims a
throughput or an allocation figure against another library, because nobody measured one.

## The pages

| Page | The library | What wlog ships for it |
|---|---|---|
| [slog](slog.md) | `log/slog`, the standard library | `log/slog` in the root module, both directions |
| [zap](zap.md) | `go.uber.org/zap` | `log/zap`, the output direction |
| [zerolog](zerolog.md) | `github.com/rs/zerolog` | `log/zerolog`, the output direction |
| [otel](otel.md) | the OpenTelemetry Logs Bridge | `trace/otellog` |
| [evlog](evlog.md) | evlog, the TypeScript logger | the module map in `docs/evlog-parity.md` |

## What the pages do not cover

wlog sends one event per unit of work. A comparison of a line-oriented logger with a wide
event is a comparison of two shapes, not of two speeds. The pages name the shape and the
adapter, and they leave the speed out.
