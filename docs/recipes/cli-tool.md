# Recipe: CLI tool

A small command line tool where one run is one wide event. The runnable example lives in
[examples/cli-tool](../../examples/cli-tool), and its test compares the event with the
golden in `testdata/event.json`. `command-cobra` in phase 13 wraps the same unit of work
around a cobra command.

## 1. Setup

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/work"
)

func run(ctx context.Context, logger *wlog.Logger, args []string, stdout io.Writer) int {
	flags := flag.NewFlagSet("orders", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	limit := flags.Int("limit", 10, "how many orders to read")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	ctx, handle := work.Start(ctx, logger, work.Unit{
		Kind: work.KindCommand,
		Fields: map[string]any{
			"path":  "orders list",
			"flags": []string{"limit"},
		},
	})
	wlog.Set(ctx, "orders_read", *limit)
	_, _ = fmt.Fprintf(stdout, "read %d orders\n", *limit)
	handle.Status("0", work.ClassOf(work.KindCommand, "0"))
	handle.End(nil)
	return 0
}

func main() {
	logger := wlog.New(wlog.WithService("cli-tool", "0.0.1", "local"))
	os.Exit(run(context.Background(), logger, os.Args[1:], os.Stdout))
}
```

## 2. The event

One `orders list --limit 5` run gives this event. The duration and the id change between
runs, so the example test normalizes them.

```json
{"cli":{"exit_code":"0","flags":["limit"],"path":"orders list"},"duration_ms":8.25,"event_id":"0191f0b9-1c2f-7a3d-8e4f-0a1b2c3d4e5f","kind":"command","level":"info","operation":"orders list","orders_read":5,"outcome":"success","summary":"orders list exit 0 in {d}","timestamp":"2026-01-01T00:00:00Z","trace":{},"wlog":{"schema_version":2}}
```

## 3. Five questions

| Question | wlog query | jq | Backend |
|---|---|---|---|
| Which runs failed | `wlog query --where cli.exit_code? --level error --group-by cli.path --count ./logs` | `jq -r 'select(.cli.exit_code != "0") \| .cli.path' logs.ndjson \| sort \| uniq -c` | Grafana: count by cli.path where cli.exit_code is not 0 |
| How long does a run take | `wlog query --group-by cli.path --stats duration_ms ./logs` | `jq -s 'group_by(.cli.path) \| map({path: .[0].cli.path, count: length})' logs.ndjson` | Loki: `quantile_over_time(0.95, {service="cli-tool"} \| json \| unwrap duration_ms [1d])` |
| Which flags are used | `wlog query --where cli.flags? --group-by cli.path --count ./logs` | `jq -r '.cli.flags[]?' logs.ndjson \| sort \| uniq -c` | Elasticsearch: `cli.flags: limit` |
| How much did a run read | `wlog query --stats orders_read ./logs` | `jq -s 'map(.orders_read // 0) \| add' logs.ndjson` | ClickHouse: `SELECT sum(orders_read) FROM events WHERE kind = 'command'` |
| One run from a cron host | `wlog query --where host.name? --text orders --format summary ./logs` | `jq -r 'select(.cli.path == "orders list") \| [.timestamp, .cli.exit_code] \| @tsv' logs.ndjson` | Datadog: `service:cli-tool cli.path:"orders list"` |

## 4. Explain ids

- `wlog explain cli.exit_code` for the exit code field and its levels.
- `wlog explain work` for the unit-of-work kinds and their groups.
- `wlog explain WLOG_NO_EVENT` for a write that lands outside a unit of work.
- `wlog explain WLOG_LATE_WRITE` for a write that lands after the event emitted.
