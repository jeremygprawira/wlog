# Recipe: cron job

A scheduled job where every run is one wide event. The runnable example lives in
[examples/cron-job](../../examples/cron-job), and its test runs one job and compares the
event with the golden in `testdata/event.json`.

## 1. Setup

```go
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/robfig/cron/v3"

	"github.com/jeremygprawira/wlog"
	wlogcron "github.com/jeremygprawira/wlog/job/cron"
)

const spec = "@every 5m"

func main() {
	logger := wlog.New(wlog.WithService("cron-job", "0.0.1", "prod"))
	scheduler := cron.New(cron.WithChain(
		cron.SkipIfStillRunning(cron.DefaultLogger),
		wlogcron.Wrap(logger, "reindex", spec),
	))
	_, _ = scheduler.AddJob(spec, wlogcron.Job(logger, "reindex", spec, reindex))
	scheduler.Start()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	<-signals
	<-scheduler.Stop().Done()
	_ = logger.Flush(context.Background())
}

func reindex(ctx context.Context) error {
	wlog.Set(ctx, "rows", 128)
	return nil
}
```

`Wrap` sits inside `SkipIfStillRunning`, so a skipped run records nothing. `Stop` returns a
context. The context is done after the running jobs finish. Flush the Logger there in a
short-lived process. `work.Ticker` covers a plain `time.Ticker` loop.

## 2. The event

One run of `reindex` gives this event. The duration, the event id, and the trace ids change
between runs, so the example test normalizes them.

```json
{"timestamp":"2026-01-01T00:00:00Z","level":"info","summary":"job reindex success in {d}","operation":"job reindex","kind":"job","outcome":"success","duration_ms":8.25,"event_id":"0191f0b9-1c2f-7a3d-8e4f-0a1b2c3d4e5f","trace":{"trace_id":"4bf92f3577b34da6a3ce929d0e0e4736","span_id":"00f067aa0ba902b7"},"job":{"system":"cron","name":"reindex","schedule":"@every 5m"},"rows":128,"wlog":{"schema_version":2}}
```

## 3. Five questions

| Question | wlog query | jq | Backend |
|---|---|---|---|
| Which jobs fail most | `wlog query --level error --group-by job.name --count ./logs` | `jq -r 'select(.level=="error") \| .job.name' logs.ndjson \| sort \| uniq -c \| sort -rn` | Grafana: count by job.name where level is error |
| Which runs are slowest | `wlog query --stats duration_ms ./logs` | `jq -s 'map(.duration_ms) \| sort' logs.ndjson` | Loki: `quantile_over_time(0.95, {service="cron-job"} \| json \| unwrap duration_ms [5m])` |
| Which schedules fire | `wlog query --group-by job.schedule --count ./logs` | `jq -r '.job.schedule' logs.ndjson \| sort \| uniq -c` | Elasticsearch: `job.schedule: "@every 5m"` |
| One trace across services | `wlog query --trace 4bf92f3577b34da6a3ce929d0e0e4736 ./logs` | `jq -r 'select(.trace.trace_id=="4bf92f3577b34da6a3ce929d0e0e4736")' logs.ndjson` | Tempo: search by trace id |
| How much one run moved | `wlog query --where 'rows?' --format summary ./logs` | `jq -r '[.job.name, .rows] \| @tsv' logs.ndjson` | ClickHouse: `SELECT job.name, sum(rows) FROM events GROUP BY 1` |

## 4. Explain ids

- `wlog explain job` for the job fields, the schedule, and the attempt.
- `wlog explain trace` for the trace group of one run.
- When a drain cannot send an event, `wlog explain WLOG_DRAIN_FAILED`.
- When an event passes the size cap, `wlog explain WLOG_EVENT_TOO_LARGE`.
