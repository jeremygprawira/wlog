# Recipe: Lambda function

A Lambda function where every invocation is one wide event. The runnable example lives in
[examples/lambda](../../examples/lambda), and its test invokes one API Gateway request and
compares the event with the golden in `testdata/event.json`.

## 1. Setup

```go
package main

import (
	"context"

	"github.com/aws/aws-lambda-go/lambda"

	"github.com/jeremygprawira/wlog"
	wloglambda "github.com/jeremygprawira/wlog/faas/lambda"
)

func main() {
	logger := wlog.New(wlog.WithService("lambda-orders", "0.0.1", "prod"))
	lambda.StartWithOptions(
		wloglambda.Wrap(logger, handle),
		wloglambda.SIGTERMFlush(logger),
	)
}

func handle(ctx context.Context, in map[string]any) (map[string]any, error) {
	wlog.Set(ctx, "order_id", in["order_id"])
	return map[string]any{"ok": true}, nil
}
```

## 2. The event

One `GET /orders/42` through API Gateway gives this event. The duration, the event id, and
the trace ids change between runs, so the example test normalizes them.

```json
{"timestamp":"2026-01-01T00:00:00Z","level":"info","summary":"function lambda-orders http success in {d} cold start (order_id=42)","operation":"function lambda-orders","kind":"function","outcome":"success","duration_ms":8.25,"event_id":"0191f0b9-1c2f-7a3d-8e4f-0a1b2c3d4e5f","trace":{"trace_id":"4bf92f3577b34da6a3ce929d0e0e4736","span_id":"00f067aa0ba902b7"},"http":{"method":"GET","route":"/orders/{id}","path":"/orders/42","status":200,"protocol":"HTTP/1.1"},"faas":{"system":"aws_lambda","name":"lambda-orders","trigger":"http","invocation_id":"recipe-1","cold_start":true,"remaining_ms":4999},"order_id":"42","wlog":{"schema_version":2}}
```

## 3. Five questions

| Question | wlog query | jq | Backend |
|---|---|---|---|
| Which functions fail most | `wlog query --level error --group-by operation --count ./logs` | `jq -r 'select(.level=="error") \| .operation' logs.ndjson \| sort \| uniq -c \| sort -rn` | Grafana: count by operation where level is error |
| Which invocations are slowest | `wlog query --stats duration_ms ./logs` | `jq -s 'map(.duration_ms) \| sort' logs.ndjson` | Loki: `quantile_over_time(0.95, {service="lambda-orders"} \| json \| unwrap duration_ms [5m])` |
| Which trigger started the work | `wlog query --group-by faas.trigger --count ./logs` | `jq -r '.faas.trigger' logs.ndjson \| sort \| uniq -c` | Elasticsearch: `faas.trigger: "sqs"` |
| Which runs were cold | `wlog query --where faas.cold_start=true --count ./logs` | `jq -r 'select(.faas.cold_start==true) \| .operation' logs.ndjson` | ClickHouse: `SELECT operation, count() FROM events WHERE faas.cold_start GROUP BY 1` |
| One trace across services | `wlog query --trace 4bf92f3577b34da6a3ce929d0e0e4736 ./logs` | `jq -r 'select(.trace.trace_id=="4bf92f3577b34da6a3ce929d0e0e4736")' logs.ndjson` | Tempo: search by trace id |

## 4. Explain ids

- `wlog explain faas` for the function fields, the trigger, and the cold start flag.
- `wlog explain error` for the error block of a failed invocation.
- When a drain cannot send an event, `wlog explain WLOG_DRAIN_FAILED`.
- When an event passes the size cap, `wlog explain WLOG_EVENT_TOO_LARGE`.
