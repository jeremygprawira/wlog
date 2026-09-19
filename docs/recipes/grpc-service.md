# Recipe: gRPC service

A gRPC server where every call is one wide event. The runnable example lives in
[examples/grpc-service](../../examples/grpc-service), and its test calls one RPC over
bufconn and compares the event with the golden in `testdata/event.json`.

## 1. Setup

```go
package main

import (
	"log"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/jeremygprawira/wlog"
	wloggrpc "github.com/jeremygprawira/wlog/rpc/grpc"
)

func main() {
	logger := wlog.New(wlog.WithService("grpc-service", "0.0.1", "local"))
	server := grpc.NewServer(wloggrpc.ServerOptions(logger)...)
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(server, healthServer)

	listener, err := net.Listen("tcp", ":9090")
	if err != nil {
		log.Fatal(err)
	}
	log.Fatal(server.Serve(listener))
}
```

## 2. The event

One `grpc.health.v1.Health/Check` over bufconn gives this event. The duration, the trace
id, and the span id change between runs, so the example test normalizes them.

```json
{"duration_ms":8.25,"event_id":"0191f0b9-1c2f-7a3d-8e4f-0a1b2c3d4e5f","kind":"rpc","level":"info","operation":"grpc.health.v1.Health/Check","outcome":"success","rpc":{"method":"Check","peer":"bufconn","service":"grpc.health.v1.Health","status_code":"OK","system":"grpc"},"summary":"grpc grpc.health.v1.Health/Check OK in {d}","timestamp":"2026-01-01T00:00:00Z","trace":{},"wlog":{"schema_version":2}}
```

## 3. Five questions

| Question | wlog query | jq | Backend |
|---|---|---|---|
| Which methods fail most | `wlog query --level error --group-by rpc.method --count ./logs` | `jq -r 'select(.level=="error") \| .rpc.method' logs.ndjson \| sort \| uniq -c \| sort -rn` | Grafana: count by rpc.method where level is error |
| Which calls are slowest | `wlog query --where rpc.system? --stats duration_ms ./logs` | `jq -s 'map(select(.rpc)) \| map(.duration_ms) \| sort' logs.ndjson` | Loki: `quantile_over_time(0.95, {service="grpc-service"} \| json \| unwrap duration_ms [5m])` |
| Which service one user called | `wlog query --where user.id=u-1 --group-by rpc.service --count ./logs` | `jq -r 'select(.user.id=="u-1") \| .rpc.service' logs.ndjson` | Elasticsearch: `user.id: "u-1" AND rpc.system: grpc` |
| One trace across services | `wlog query --trace 4bf92f3577b34da6a3ce929d0e0e4736 ./logs` | `jq -r 'select(.trace.trace_id=="4bf92f3577b34da6a3ce929d0e0e4736")' logs.ndjson` | Tempo: search by trace id |
| Which status codes appear | `wlog query --group-by rpc.status_code --count ./logs` | `jq -r '.rpc.status_code' logs.ndjson \| sort \| uniq -c` | ClickHouse: `SELECT rpc.status_code, count() FROM events GROUP BY 1 ORDER BY 2 DESC` |

## 4. Explain ids

- `wlog explain rpc.status_code` for the gRPC status table and its levels.
- `wlog explain work` for the unit-of-work kinds and their groups.
- `wlog explain error.code` for the code a failed call carries.
- When a call lands after `Close`, `wlog explain WLOG_LOGGER_CLOSED`.
