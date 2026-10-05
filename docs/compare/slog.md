# wlog and slog

`slog` is the structured logger of the Go standard library. It writes one record per call,
and its `Handler` interface is the contract a backend implements. The wlog bridge lives in
`log/slog`, in the root module, because `log/slog` is standard library.

## The two directions

| Direction | Type | What it does |
|---|---|---|
| Output | `wlogslog.Drain` | Writes each wlog event as one slog record |
| Input | `wlogslog.Handler` | Appends a record made inside an event to that event's `logs[]` |

The output direction puts an event into an application that already writes slog records.
The input direction keeps a library call that uses slog, such as an HTTP client's debug
line, inside the event that is open.

## What the tests prove

`log/slog/conformance_test.go` runs the bridge against `testing/slogtest` for folded
records, and against the shared log suite in `internal/conformance/log` in both
directions. A handler that drops a record, or one that writes an attribute under the wrong
key, fails that suite.

## The shape difference

slog writes one record per call. wlog writes one event per unit of work, and it enriches
that event while the work runs. A program that wants both reads a wlog event as a slog
record through the output drain. It folds a slog call into the event through the input
handler.

## Not measured

No page here holds a throughput or an allocation comparison between wlog and slog. The
repository measures wlog alone, with `make bench` and the budget in `bench/baseline.txt`.
