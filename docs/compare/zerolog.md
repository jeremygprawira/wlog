# wlog and zerolog

`zerolog` is a structured logger that builds one JSON object per call. The wlog bridge
lives in `log/zerolog`, which is its own module.

## The two directions

| Direction | Type | What it does |
|---|---|---|
| Output | `wlogzerolog.Drain` | Writes each wlog event to a zerolog logger |
| Input | the hook and the bound writer in `log/zerolog/input.go` | Folds zerolog lines into the open event |

The output direction keeps the log destination of a team that already runs zerolog. The
input direction keeps a zerolog line that a library wrote during a request inside that
request's event.

## What the tests prove

`log/zerolog/conformance_test.go` runs the shared log suite in `internal/conformance/log`
against the bridge in both directions. A line that reaches zerolog with the wrong field
name, or one that misses the open event, fails that suite.

## The shape difference

zerolog writes one object per call. wlog writes one event per unit of work, and it
enriches that event while the work runs. A program that runs both sends its event to the
existing zerolog output. It folds a zerolog call made during a request into the event.

## Not measured

No page here holds a throughput or an allocation comparison between wlog and zerolog. The
repository measures wlog alone, with `make bench` and the budget in `bench/baseline.txt`.
