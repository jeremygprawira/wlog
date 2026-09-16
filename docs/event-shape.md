# Event shape

One unit of work produces one event. The event is a JSON object with a fixed core and
any fields the caller adds.

## Example

```json
{
  "timestamp": "2026-09-16T12:00:00.123456789Z",
  "level": "error",
  "operation": "POST /orders/:id",
  "duration_ms": 42,
  "outcome": "error",
  "service": {"name": "orders", "version": "1.4.0", "env": "prod"},
  "trace": {"request_id": "b2f...", "trace_id": "4bf9...", "span_id": "00f0..."},
  "http": {"method": "POST", "route": "/orders/:id", "status": 500},
  "order_id": "4821",
  "error": {"code": "PAYMENT_DECLINED", "message": "declined"},
  "logs": [{"level": "info", "msg": "charge retried", "attrs": {"attempt": "2"}}],
  "wlog.dropped_fields": 0
}
```

## Reserved keys

| Key | Set by | Meaning |
|---|---|---|
| `timestamp` | core | RFC3339Nano, at emit |
| `level` | core | `debug`, `info`, `warn`, or `error` |
| `operation` | `Start` | the name passed to `Start`, or the route for HTTP |
| `duration_ms` | core | time from `Start` to emit |
| `outcome` | core | `success`, or `error` when the level is `error` |
| `service.name` `service.version` `service.env` | `WithService` | service metadata |
| `trace.request_id` | http-std | one id per request, also sent as `X-Request-ID` |
| `trace.trace_id` `trace.span_id` | http-std or `trace/otel` | W3C trace context |
| `trace.parent_operation` | `Detach` | the operation that started the parent |
| `http.*` | http-std | capture: method, route, path, status, duration, bytes, client ip, user agent, headers, query, params, cookies, request and response bodies |
| `error` | `Error` | one `ErrorInfo` that decided the outcome |
| `errors` | `Error` | earlier `ErrorInfo` values, capped at 10 |
| `logs` | `log/slog` input | folded log lines, capped at 50 |
| `audit` | `audit.Do` | one `audit.Record` |
| `redact.fingerprint` | core | short hash of the active redactor |
| `wlog.dropped_fields` | core | writes rejected by a cap |
| `wlog.dropped_logs` | core | log lines rejected by the 50 line cap |
| `wlog.late_writes` | core | writes after the event sealed |
| `wlog.unknown_keys` | core | `StrictKeys` names seen but not registered |

User keys stay at the top level, outside these namespaces. A caller that sets a
reserved key overwrites the default value. The `wlog map` CLI flags that as a likely
mistake.

## Stage order

Every event runs the same five stages, in this order:

```
1. keep/sample   a Keeper decides; audit events always pass
2. enrich        every Enricher, in order
3. redact        the active redactor
4. rename        the active field-name preset
5. sinks/drains  stdout, then every Drain
```

A dropped event skips stages 2 to 5. Enrichers and Keepers see the context the event
was started from. Drains see that context with cancellation removed.

## Caps

| Field | Cap | Counter |
|---|---|---|
| top-level keys | 200 | `wlog.dropped_fields` |
| fields in one group | 50 | `wlog.dropped_fields` |
| elements in one array | 200 | `wlog.dropped_fields` |
| `logs[]` | 50 | `wlog.dropped_logs` |
| `errors[]` | 10 | none |
