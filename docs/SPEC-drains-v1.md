# Spec: drains-v1

> Module ids `drain-axiom`, `drain-loki`, `drain-file`, `drain-webhook`, `drain-otlp`.
> Packages `github.com/jeremygprawira/wlog/drain/axiom`, `.../drain/loki`,
> `.../drain/file`, `.../drain/webhook`, `.../drain/otlp`. All live in the root module, so
> every drain uses only the standard library. Depends on `core`, `pipeline`, and
> `internal/httpdrain`. Project-wide rules in [SPEC.md](SPEC.md) apply.
> v1.2 additions to the file and memory modules: [SPEC-v1.2-additions.md](SPEC-v1.2-additions.md).

## Objective

Ship the five v1 backend drains, so a team turns one on with env vars and nothing else.
Each drain implements `pipeline.Sender` (`SendBatch(ctx, events) error`). A user wraps it with
`pipeline.Wrap` to get batching, retry, and a bounded buffer.

## Common shape

```go
d, err := axiom.New()                    // reads AXIOM_* env; explicit options win over env
log := wlog.New(wlog.WithDrains(pipeline.Wrap(d, pipeline.BatchSize(100))))
```

- `func New(opts ...Option) (*Drain, error)` returns an error for missing or invalid
  configuration. The error message names the drain and the missing value, for example
  `axiom: AXIOM_TOKEN is required`.
- `func MustNew(opts ...Option) *Drain` panics on that error, for `main`.
- Explicit options win over env vars, the same rule core uses (SPEC.md: code wins over env).
- Every HTTP drain sends `User-Agent: wlog/<version>` and `X-Wlog-Source: <name>` through
  `internal/httpdrain` (`axiom`, `loki`, `webhook`, `otlp`). `file` sends nothing.
- A drain never panics and never blocks: `pipeline.Wrap` owns retry, buffering, and `Close`.
- Each `SendBatch` sends one request per batch. The drain returns a `*httpdrain.StatusError`
  unchanged, so `pipeline` can read `Retryable()` and `RetryAfter()`.

## drain-axiom

- URL: `{AXIOM_URL}/v1/datasets/{AXIOM_DATASET}/ingest`, default `AXIOM_URL` is
  `https://api.axiom.co`. `AXIOM_TOKEN` and `AXIOM_DATASET` are required.
- Wire format: `application/x-ndjson`, one JSON event per line, trailing newline. No wrapper
  envelope.
- Auth: `Authorization: Bearer {AXIOM_TOKEN}`.
- Options: `WithToken`, `WithDataset`, `WithURL`, `WithGzip` (default off).
- 401 and 403 are non-retryable, because the token or dataset is wrong. `httpdrain` already
  marks only 429 and 5xx retryable, so `SendBatch` returns its error unchanged.

## drain-loki

- URL: `{LOKI_URL}/loki/api/v1/push`, default `LOKI_URL` is `http://localhost:3100`.
- Wire format: the Push API JSON body.
  ```json
  {"streams":[{"stream":{"service":"api","env":"prod","level":"info"},
               "values":[["1758000000000000000","{\"level\":\"info\",...}"]]}]}
  ```
  The line is the redacted event, encoded as one JSON object. `values[0]` is the event's
  `timestamp` in Unix nanoseconds as a decimal string.
- Streams are grouped by label set: events with the same labels share one stream, and one
  batch can produce several streams.
- Default labels are `service` (from `service.name`), `env` (from `service.env`), and `level`.
  `WithLabels(keys...)` replaces the default set. A missing label value is the empty string.
- High-cardinality labels are rejected at construction with an error. The denylist is
  `trace.request_id`, `trace.trace_id`, `trace.span_id`, `http.path`, `http.client_ip`,
  `http.user_agent`, `error.message`, and `user.id`. A label later added to the event is
  ignored, so a bad key fails fast at startup instead of overloading Loki.
- Env: `LOKI_URL`, `LOKI_USERNAME`, `LOKI_PASSWORD`, `LOKI_TENANT_ID`.
- Auth: when both username and password are set, basic auth is sent. `LOKI_TENANT_ID` sets
  the `X-Scope-OrgID` header.
- Options: `WithURL`, `WithBasicAuth`, `WithTenantID`, `WithLabels`, `WithGzip` (default off).

## drain-file

- Wire format: NDJSON, one JSON event per line, appended to one file.
- Env: `WLOG_FILE_PATH` (required). Options: `WithPath`, `WithMaxSize`, `WithMaxAge`,
  `WithMaxBackups`.
- Defaults: `maxSize` 100 MiB, `maxAge` 24h, `maxBackups` 3.
- Mode: every file is created with mode 0600.
- Rotation by size: rotate before a write on any file with content whose size plus the new
  blob exceeds `maxSize`. An empty file never rotates for size, so a first blob larger than
  `maxSize` still lands in the main file.
- Rotation by age: rotate before a write on any file whose modification time is older
  than `maxAge`.
- One rotation: close the current file, shift `path.N` to `path.N+1` for N from
  `maxBackups-1` down to 1, delete `path.maxBackups`, rename `path` to `path.1`, then open a
  new `path`. `maxBackups` 0 means delete the rotated file instead of keeping it.
- Concurrency: one mutex guards the write and the rotation, so concurrent `SendBatch` calls
  never interleave a line or race a rename.
- `SendBatch` appends one blob per batch. A failed write returns the error, and `pipeline`
  retries it.

## drain-webhook

- URL: `WLOG_WEBHOOK_URL` (required). Options: `WithURL`, `WithHeaders`, `WithNDJSON`,
  `WithSecret`.
- Wire format: JSON array of events, `application/json`. `WithNDJSON(true)` sends one JSON
  event per line with `application/x-ndjson` instead.
- Custom headers are set on every request.
- Signature: when a secret is set, the drain adds
  `X-Wlog-Signature: sha256=<hex HMAC-SHA256 of the request body>`. No secret means no header.
- Any 2xx is success. The drain returns the `httpdrain` status error unchanged, so 401 and 403
  are non-retryable.

## drain-otlp

- URL: `{OTEL_EXPORTER_OTLP_ENDPOINT}/v1/logs`, default endpoint `http://localhost:4318`.
  The scheme selects HTTP or HTTPS.
- Wire format: OTLP/HTTP JSON `ExportLogsServiceRequest`, `application/json`.
- Env: `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_HEADERS`. Options: `WithEndpoint`,
  `WithHeaders`. Headers are comma-separated `key=value` pairs. An explicit option value wins.
- Resource attributes come from the event's `service` group:
  `service.name` stays `service.name`, `service.version` stays `service.version`, and
  `service.env` becomes `deployment.environment`.
- Scope: `{"name":"wlog","version":"<internal/version>"}`.
- `body` is the event's `operation`, or its `message` for a plain line, as `stringValue`.
- `severityNumber` maps `debug` 5, `info` 9, `warn` 13, `error` 17. `severityText` is the
  upper-case level. An unknown level maps to 9 and `INFO`.
- Attributes are every remaining top-level field, in this mapping:
  - `string` to `stringValue`, `bool` to `boolValue`
  - integer to `intValue` as a decimal string, float to `doubleValue`
  - a slice to `arrayValue` with each element mapped the same way
  - a nested map to dotted keys, for example `http.status`
  - `timestamp` and `level` are not attributes, because the record already carries them
- A present `trace.trace_id` and `trace.span_id` set `traceId` and `spanId`. Those two keys
  are not attributes, since the record already carries them.
- `timeUnixNano` and `observedTimeUnixNano` are the event's `timestamp` in Unix nanoseconds.
- One `SendBatch` sends one request with one `scopeLogs` and one `logRecords` entry per event.
- `drain/otlp/testdata/export.golden.json` pins the exact JSON for one fixed event.

## Success Criteria

1. Each drain sends the wire format above, verified against `internal/httpfake` or a temp file.
2. Each drain works from env vars alone, with no option set.
3. A 401 or 403 response is non-retryable. A 429 or 5xx response is retryable.
4. Loki groups by label set, uses nanosecond timestamps, and rejects a high-cardinality label
   at construction.
5. File rotation by size and age keeps at most `maxBackups` files, mode 0600, under concurrent
   batches.
6. The OTLP payload matches the golden file byte for byte.
7. Gate G1: no drain ever receives or sends a value that the redactor denies.
8. Zero imports outside the standard library in the root module.

## Testing

- Package `<name>_test`, black box. HTTP drains use `internal/httpfake`, never a real network.
- `file` uses `t.TempDir()`.
- Each package has one G1 test. The test builds a real `wlog.Logger` and wraps the drain
  with `pipeline.Wrap`. It sets a `password` field to a value the default redactor denies.
  It then asserts the raw value never appears in the received body or file.
- `drain/otlp` also has a golden-file test for the exact JSON bytes.
- Optional `//go:build integration` tests hit docker Loki and an OTel collector.

## Boundaries

- **Always:** an absent option falls back to env. Send identity headers through
  `internal/httpdrain`.
- **Ask first:** adding a vendor SDK, adding a sixth v1 drain, changing a default label set.
- **Never:** make a real network call in a default `go test`. Never block or panic in
  `SendBatch`. Never let a drain re-encode a value the redactor removed.

## Open Questions

None.
