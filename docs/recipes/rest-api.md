# Recipe: REST API

A small chi API where every request is one wide event. The runnable example lives in
[examples/rest-api](../../examples/rest-api), and its test compares the event with the
golden in `testdata/event.json`.

## 1. Setup

```go
package main

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/jeremygprawira/wlog"
	wlogchi "github.com/jeremygprawira/wlog/middleware/chi"
)

func main() {
	logger := wlog.New(wlog.WithService("rest-api", "0.0.1", "local"))
	router := chi.NewRouter()
	router.Use(wlogchi.Middleware(logger))
	router.Get("/orders/{id}", func(w http.ResponseWriter, req *http.Request) {
		wlog.Set(req.Context(), "order_id", chi.URLParam(req, "id"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": chi.URLParam(req, "id")})
	})
	_ = http.ListenAndServe(":8080", router)
}
```

## 2. The event

One `GET /orders/42` with `X-Request-ID: recipe-1` gives this event. The duration and the
id change between runs, so the example test normalizes them.

```json
{"duration_ms":8.25,"event_id":"0191f0b9-1c2f-7a3d-8e4f-0a1b2c3d4e5f","http":{"bytes_in":0,"bytes_out":47,"client_ip":"192.0.2.1","host":"example.com","method":"GET","path":"/orders/42","protocol":"HTTP/1.1","response_headers":{"content-type":"application/json"},"route":"/orders/{id}","scheme":"http","status":200,"user_agent":""},"kind":"request","level":"info","operation":"GET /orders/{id}","order_id":"42","outcome":"success","summary":"GET /orders/{id} 200 in {d} (order_id=42)","timestamp":"2026-01-01T00:00:00Z","trace":{"request_id":"recipe-1"},"wlog":{"schema_version":2}}
```

## 3. Five questions

| Question | wlog query | jq | Backend |
|---|---|---|---|
| Which operations fail most | `wlog query --level error --group-by operation --count ./logs` | `jq -r 'select(.level=="error") \| .operation' logs.ndjson \| sort \| uniq -c \| sort -rn` | Grafana: count by operation where level is error |
| Which calls are slowest | `wlog query --stats duration_ms ./logs` | `jq -s 'map(.duration_ms) \| sort' logs.ndjson` | Loki: `quantile_over_time(0.95, {service="rest-api"} \| json \| unwrap duration_ms [5m])` |
| What one user did | `wlog query --where user.id=u-1 --format summary ./logs` | `jq -r 'select(.user.id=="u-1") \| .operation' logs.ndjson` | Elasticsearch: `user.id: "u-1"` |
| One trace across services | `wlog query --trace 3f2a91c4d0e5 ./logs` | `jq -r 'select(.trace.trace_id=="3f2a91c4d0e5")' logs.ndjson` | Tempo: search by trace id |
| Cost per operation | `wlog query --group-by llm.operation --stats llm.cost_micros ./logs` | `jq -r '[.llm.operation, .llm.cost_micros] \| @tsv' logs.ndjson` | ClickHouse: `SELECT llm.operation, sum(llm.cost_micros) FROM events GROUP BY 1` |

## 4. Explain ids

- `wlog explain http.status` for the status field and its levels.
- `wlog explain http.route` for the route template and the operation.
- `wlog explain error.code` for the code a failure carries.
- When a drain cannot send an event, `wlog explain WLOG_DRAIN_FAILED`.
- When an event passes the size cap, `wlog explain WLOG_EVENT_TOO_LARGE`.
