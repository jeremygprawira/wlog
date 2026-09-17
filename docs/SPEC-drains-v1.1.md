# Spec: drains-v1.1

> Module ids `drain-sentry`, `drain-clickhouse`, `drain-datadog`.
> Packages `github.com/jeremygprawira/wlog/drain/sentry`, `.../clickhouse`, `.../datadog`.
> All live in the root module, so every drain uses the standard library only. Depends on
> `core`, `pipeline`, and `pipeline/httpdrain`. Project-wide rules in [SPEC.md](SPEC.md)
> apply. [SPEC-drains-v1.md](SPEC-drains-v1.md) still governs the shared common shape:
> `New` returns an error, `MustNew` panics, an option wins over env, and every HTTP drain
> sends the wlog identity headers.

## Objective

Ship the three v1.1 drains. Each one turns a batch of already-redacted wide events into
the backend's own wire format, so a team reaches Sentry, ClickHouse, or Datadog without a
vendor SDK.

## drain-sentry

- DSN: `SENTRY_DSN`, or `WithDSN(dsn)`. The DSN shape is
  `https://<public_key>@<host>/<project_id>`. A DSN without a scheme is treated as HTTPS.
- Endpoint: `{scheme}://{host}/api/{project_id}/envelope/`.
- Auth: `X-Sentry-Auth: Sentry sentry_version=7, sentry_key={public_key}, sentry_client=wlog/{version}`.
- Wire format: one Sentry envelope per batch, content type `application/x-sentry-envelope`,
  three newline-delimited lines per item:
  ```
  {"event_id":"<32 hex>","sent_at":"<RFC3339Nano>"}
  {"type":"event","length":<n>}
  {"event_id":"<32 hex>","timestamp":"<RFC3339Nano>","platform":"go","level":"error",...}
  ```
  All items share one envelope header line and one `sent_at`.
- Error events only by default. An event is an error event when its `level` is `error` or
  it carries the reserved `error` key.
- An error event maps to one `event` item:
  - `event_id`: 32 random hex characters, one per item.
  - `fingerprint`: `[error.code]`, falling back to `[error.kind]`, then `["INTERNAL"]`, so
    Sentry groups every event with the same code into one issue. The plan calls this
    "grouping by error code (fallback type)".
  - `message.formatted`: `error.message`, then `operation`.
  - `tags`: `service`, `env`, `level`, `operation`, when each is present.
  - `contexts.wlog`: the whole redacted event, so the wide event is attached.
- `WithAllEvents(true)`, or `SENTRY_ALL_EVENTS=1`, also sends every non-error event as a
  `log` item in the same envelope, for Sentry Logs:
  ```
  {"type":"log","item_count":<n>,"content_type":"application/vnd.sentry.items.log+json"}
  {"items":[{"timestamp":<unix seconds>,"level":"info","body":"<operation>","attributes":{...},"trace_id":"..."}]}
  ```
  `attributes` carries the whole event under `wlog`. Error events still get their `event`
  item, so an error is never only a log line.
- A batch with no error event and no `AllEvents` sends nothing and returns nil.
- Options: `WithDSN`, `WithAllEvents`.

## drain-clickhouse

- URL: `CLICKHOUSE_URL`, default `http://localhost:8123`. `WithURL`.
- Insert: `POST {url}/?query=<urlencoded query>` where the query is
  `INSERT INTO {database}.{table} FORMAT JSONEachRow`, content type
  `application/x-ndjson` (one JSON object per line).
- Auth: `CLICKHOUSE_USER` and `CLICKHOUSE_PASSWORD`, or `WithBasicAuth`. Basic auth is
  sent only when the user is non-empty.
- Env: `CLICKHOUSE_DATABASE` (default `default`), `CLICKHOUSE_TABLE` (default
  `wlog_events`). Options `WithDatabase`, `WithTable`.
- Row mapping, one column per reserved field, plus the whole event in the `event` column:

  | Column | Source | Type |
  |---|---|---|
  | `timestamp` | `timestamp` | `DateTime64(9)` |
  | `level` | `level` | `LowCardinality(String)` |
  | `operation` | `operation` | `String` |
  | `duration_ms` | `duration_ms` | `UInt64` |
  | `outcome` | `outcome` | `LowCardinality(String)` |
  | `service_name` | `service.name` | `LowCardinality(String)` |
  | `service_version` | `service.version` | `String` |
  | `service_env` | `service.env` | `LowCardinality(String)` |
  | `trace_id` | `trace.trace_id` | `String` |
  | `span_id` | `trace.span_id` | `String` |
  | `request_id` | `trace.request_id` | `String` |
  | `http_method` | `http.method` | `LowCardinality(String)` |
  | `http_route` | `http.route` | `String` |
  | `http_status` | `http.status` | `UInt16` |
  | `error_code` | `error.code` | `LowCardinality(String)` |
  | `error_message` | `error.message` | `String` |
  | `event` | the whole event | `JSON` |

  A missing value is the column's zero value: an empty string or 0. A wrong type is an
  empty value, never a dropped row.
- `DDL(database, table string) string` returns the `CREATE TABLE IF NOT EXISTS` statement for
  the recommended schema, with a `String` column for the whole event, which every ClickHouse
  version accepts. `DDLJSON(database, table)` is the same schema with a `JSON` column, for a
  server on 25.3 or newer. The drain never creates or alters a table: a missing table is a
  ClickHouse error that `pipeline` reports.
- The timestamp column takes `2006-01-02 15:04:05.000000000` in UTC, the `DateTime64(9)`
  text form, not RFC 3339. A database or table name must match `^[A-Za-z_][A-Za-z0-9_]*$`,
  so a config value can never inject SQL. `CLICKHOUSE_URL` may carry its own query, such as
  `secure=true`, which the insert statement joins.
- Options: `WithURL`, `WithBasicAuth`, `WithDatabase`, `WithTable`, `WithPipeline`.
- Constructors: `New` returns the async drain, `NewSender` the raw sender, and `MustNew`
  panics on a configuration error.

## drain-datadog

- Intake: `https://http-intake.logs.{DD_SITE}/api/v2/logs`, where `DD_SITE` defaults to
  `datadoghq.com`. `WithSite` wins over env, and `WithURL` wins over both so a test can
  point at a fake.
- Auth: `DD-API-KEY: {DD_API_KEY}`. Options `WithAPIKey`, `WithSite`, `WithURL`.
- Wire format: one JSON array, content type `application/json`. Each element is one event:
  ```json
  {"ddsource":"wlog","service":"orders","ddtags":"env:prod,version:1.4.0",
   "level":"error","message":"{\"level\":\"error\",...}"}
  ```
  - `ddsource`: `wlog`, or `WithSource`.
  - `service`: `service.name`, else the `DD_SERVICE` env var.
  - `ddtags`: comma-joined tags, sorted, from `env:<service.env>`, `version:<service.version>`,
    and `service:<service.name>`. An absent value adds no tag. `DD_ENV` supplies `env:` when
    the event has none.
  - `message`: the whole event, encoded as one JSON string.
  - `level`: the event's `level` unchanged.
- A 413 (payload too large) splits the batch in half and sends each half again. A single
  event that still gets a 413 returns the error, so `pipeline` reports it. Every other
  non-2xx status is returned unchanged.
- Options: `WithAPIKey`, `WithSite`, `WithURL`, `WithSource`, `WithClient`.

## Success Criteria

1. Each drain sends the wire format above, verified against `internal/httpfake` or a small
   local server.
2. Each drain works from env vars alone.
3. Sentry groups by error code, attaches the whole event, and sends nothing for a clean
   batch unless `AllEvents` is on.
4. ClickHouse builds `JSONEachRow` rows from the reserved fields and returns the same DDL
   every time.
5. Datadog builds `ddsource`, `service`, and `ddtags`, and splits a 413 batch until each
   part fits.
6. Gate G1: no drain ever receives or sends a value the redactor denies.
7. Zero imports outside the standard library.

## Testing

- Package `<name>_test`, black box. HTTP drains use `internal/httpfake`, except the
  Datadog 413 test which needs a server whose status depends on the body size.
- Each package has one G1 test through a real `wlog.Logger` and `pipeline.Wrap`.
- `drain/clickhouse` also has a `//go:build integration` test against a docker ClickHouse,
  wired into `make integration`.

## Boundaries

- **Always:** read env when an option is absent; keep the whole redacted event in the
  backend payload.
- **Ask first:** adding a vendor SDK, changing the recommended ClickHouse schema, adding a
  fourth v1.1 drain.
- **Never:** create or alter a ClickHouse table, call the network in a default `go test`,
  or block in `SendBatch`.

## Open Questions

None.
