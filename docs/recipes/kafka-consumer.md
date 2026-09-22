# Recipe: Kafka consumer

A Kafka consumer where every message is one wide event. The runnable example lives in
[examples/kafka-consumer](../../examples/kafka-consumer). Its test consumes one message
through a fake reader. Then it compares the event with the golden in `testdata/event.json`.

## 1. Setup

```go
package main

import (
	"context"
	"log"

	"github.com/segmentio/kafka-go"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/queue/kafkago"
)

func main() {
	logger := wlog.New(wlog.WithService("kafka-consumer", "0.0.1", "prod"))
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{"localhost:9092"},
		GroupID: "orders",
		Topic:   "orders",
	})
	for {
		if err := wlogkafkago.Consume(context.Background(), logger, reader, handle); err != nil {
			log.Fatal(err)
		}
	}
}

func handle(ctx context.Context, msg kafka.Message) error {
	wlog.Set(ctx, "order_id", string(msg.Key))
	return nil
}
```

`Consume` fetches with `FetchMessage` and commits with `CommitMessages` after the handler
returns nil. A failed handler leaves the message uncommitted, so the group delivers it again.
The module also gives `Drain`, which ships finished events to a topic.

## 2. The event

One message from topic `orders` gives this event. The duration, the event id, and the times
change between runs, so the example test normalizes them.

```json
{"timestamp":"2026-01-01T00:00:00Z","level":"info","summary":"process orders success in {d} (order_id=ord-1)","operation":"process orders","kind":"message","outcome":"success","duration_ms":8.25,"event_id":"0191f0b9-1c2f-7a3d-8e4f-0a1b2c3d4e5f","trace":{"trace_id":"4bf92f3577b34da6a3ce929d0e0e4736","span_id":"00f067aa0ba902b7","parent_span_id":"00f067aa0ba902b7","request_id":"4bf92f3577b34da6a3ce929d0e0e4736"},"messaging":{"system":"kafka","operation":"process","destination":"orders","consumer_group":"orders","partition":2,"offset":41,"lag_ms":2000,"kafka":{"offset_lag":1}},"order_id":"ord-1","wlog":{"schema_version":2}}
```

## 3. Five questions

| Question | wlog query | jq | Backend |
|---|---|---|---|
| Which topics fail most | `wlog query --level error --group-by messaging.destination --count ./logs` | `jq -r 'select(.level=="error") \| .messaging.destination' logs.ndjson \| sort \| uniq -c \| sort -rn` | Grafana: count by messaging.destination where level is error |
| Which handlers are slowest | `wlog query --stats duration_ms ./logs` | `jq -s 'map(.duration_ms) \| sort' logs.ndjson` | Loki: `quantile_over_time(0.95, {service="kafka-consumer"} \| json \| unwrap duration_ms [5m])` |
| How far behind the group is | `wlog query --stats messaging.kafka.offset_lag ./logs` | `jq -r '.messaging.kafka.offset_lag' logs.ndjson \| sort -n \| tail -1` | ClickHouse: `SELECT max(messaging.kafka.offset_lag) FROM events` |
| One trace across services | `wlog query --trace 4bf92f3577b34da6a3ce929d0e0e4736 ./logs` | `jq -r 'select(.trace.trace_id=="4bf92f3577b34da6a3ce929d0e0e4736")' logs.ndjson` | Tempo: search by trace id |
| Which consumer groups run | `wlog query --group-by messaging.consumer_group --count ./logs` | `jq -r '.messaging.consumer_group' logs.ndjson \| sort \| uniq -c` | Elasticsearch: `messaging.consumer_group: "orders"` |

## 4. Explain ids

- `wlog explain messaging` for the message fields, the offset lag, and the delivery count.
- `wlog explain trace` for the trace group of a consumed message.
- When a drain cannot send an event, `wlog explain WLOG_DRAIN_FAILED`.
- When an event passes the size cap, `wlog explain WLOG_EVENT_TOO_LARGE`.
