# wlog and zap

`zap` is Uber's structured logger. It writes one entry per call, with typed fields, and a
`zapcore.Core` is the contract a backend implements. The wlog bridge lives in `log/zap`,
which is its own module.

## The two directions

| Direction | Type | What it does |
|---|---|---|
| Output | `wlogzap.Drain` | Writes each wlog event to a zap logger |
| Input | the core in `log/zap/input.go` | Folds an entry that carries a context into the open event |

The output direction keeps the log destination of a team that already runs zap. The input
direction keeps a zap line that a library wrote during a request inside that request's
event.

## What the tests prove

`log/zap/conformance_test.go` runs the shared log suite in `internal/conformance/log`
against the bridge in both directions. A field that reaches zap under the wrong key, or a
line that misses the open event, fails that suite.

## The shape difference

zap writes one entry per call. wlog writes one event per unit of work, and it enriches
that event while the work runs. The two fit together in one program: zap keeps the lines
that belong to no unit of work, and the bridge moves the lines that belong to one into its
event.

## Not measured

No page here holds a throughput or an allocation comparison between wlog and zap. The
repository measures wlog alone, with `make bench` and the budget in `bench/baseline.txt`.
