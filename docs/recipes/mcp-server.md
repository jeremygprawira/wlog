# Recipe: MCP server

An MCP server with one tool, where every request is one wide event. The runnable example
lives in [examples/mcp-server](../../examples/mcp-server). Its test calls the tool over an
in-memory transport. Then it compares the event with the golden in `testdata/event.json`.

## 1. Setup

```go
package main

import (
	"context"
	"log"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jeremygprawira/wlog"
	wlogmcp "github.com/jeremygprawira/wlog/ai/mcpsdk"
)

type weatherInput struct {
	City string `json:"city" jsonschema:"the city to report"`
}

type weatherOutput struct {
	Weather string `json:"weather"`
}

func newServer(logger *wlog.Logger) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "weather-mcp", Version: "0.0.1"}, nil)
	server.AddReceivingMiddleware(wlogmcp.Middleware(logger, wlogmcp.WithService("weather-mcp")))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_weather",
		Description: "Reports the weather of one city.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, in weatherInput) (*mcp.CallToolResult, weatherOutput, error) {
		return nil, weatherOutput{Weather: "sunny"}, nil
	})
	return server
}

func main() {
	logger := wlog.New(wlog.WithService("weather-mcp", "0.0.1", "local"))
	server := newServer(logger)
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
	_ = logger.Flush(context.Background())
}
```

`Middleware` wraps the server's receiving method handler, so every request, the initialize
handshake included, gives one event of kind `rpc` with `rpc.system` `mcp`. A notification
gets no event, because the MCP spec gives it no result to report. `WithService` names the
server in `rpc.service` and in the operation, because a server carries no exported way to
read its own name back.

## 2. The event

One `tools/call` for `get_weather` gives this event. The duration, the event id, and the
trace ids change between runs, so the example test normalizes them.

```json
{"duration_ms":8.25,"event_id":"0191f0b9-1c2f-7a3d-8e4f-0a1b2c3d4e5f","kind":"rpc","level":"info","operation":"weather-mcp/tools/call","outcome":"success","rpc":{"mcp":{"client":"weather-cli/2.0.0","protocol_version":"2026-07-28","result":"ok","tool":"get_weather"},"method":"tools/call","service":"weather-mcp","system":"mcp"},"summary":"mcp weather-mcp/tools/call in {d}","timestamp":"2026-01-01T00:00:00Z","trace":{},"wlog":{"schema_version":2}}
```

## 3. Five questions

| Question | wlog query | jq | Backend |
|---|---|---|---|
| Which tools run most | `wlog query --group-by rpc.mcp.tool --count ./logs` | `jq -r '.rpc.mcp.tool' logs.ndjson \| sort \| uniq -c` | Grafana: count by rpc.mcp.tool |
| Which tools fail most | `wlog query --level warn --group-by rpc.mcp.tool --count ./logs` | `jq -r 'select(.level=="warn") \| .rpc.mcp.tool' logs.ndjson \| sort \| uniq -c` | ClickHouse: `SELECT rpc.mcp.tool, count() FROM events WHERE level='warn' GROUP BY 1 ORDER BY 2 DESC` |
| Which calls are slowest | `wlog query --stats duration_ms ./logs` | `jq -s 'map(.duration_ms) \| sort' logs.ndjson` | Loki: `quantile_over_time(0.95, {service="mcp-server"} \| json \| unwrap duration_ms [5m])` |
| Which clients call the server | `wlog query --group-by rpc.mcp.client --count ./logs` | `jq -r '.rpc.mcp.client' logs.ndjson \| sort \| uniq -c` | Elasticsearch: `rpc.mcp.client: "weather-cli/2.0.0"` |
| How many calls ask for more input | `wlog query --where 'rpc.mcp.result=input_required' --count ./logs` | `jq -r 'select(.rpc.mcp.result=="input_required")' logs.ndjson \| wc -l` | ClickHouse: `SELECT count() FROM events WHERE rpc.mcp.result='input_required'` |

## 4. Explain ids

- `wlog explain rpc` for the rpc fields every kind-rpc event carries, `mcp` included.
- `wlog explain kind` for what a kind-rpc event's fields mean.
- When a drain cannot send an event, `wlog explain WLOG_DRAIN_FAILED`.
- When an event passes the size cap, `wlog explain WLOG_EVENT_TOO_LARGE`.
