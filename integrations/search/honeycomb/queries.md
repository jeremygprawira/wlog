# Honeycomb queries

Saved queries for wlog on Honeycomb, one per question.

## Errors by operation

- Dataset: `wlog`
- Calculation: `COUNT`
- Group by: `operation`, `error.code`
- Filter: `level = error`
- Order: count, descending

## p95 duration by operation

- Calculation: `P95(duration_ms)`
- Group by: `operation`
- Order: p95, descending

## Slowest calls

- Calculation: `MAX(duration_ms)`
- Group by: `operation`, `http.route`
- Filter: `duration_ms > 1000`
- Order: max, descending

## One trace across services

- Filter: `trace.trace_id = 4bf92f3577b34da6a3ce929d0e0e4736`
- Columns: `timestamp`, `service.name`, `operation`, `summary`
- Order: timestamp, ascending
