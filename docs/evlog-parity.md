# evlog parity

wlog follows the evlog model: one wide event per unit of work, enriched as the work
runs, redacted once, and sent to any backend. This table maps every evlog feature to a
wlog module, a planned module, or a decision not to adopt it.

| evlog feature | wlog | Where |
|---|---|---|
| Wide events with a typed field API | built | core (`Start`, `Set`, `SetGroup`, `Append`) |
| Structured errors | built | core `error`/`errors[]`, `errors/herr` |
| Redaction and masking | built | `redact` |
| Head and tail sampling | built | `sample` |
| Transport adapters | built | `drain/axiom`, `drain/loki`, `drain/file`, `drain/webhook`, `drain/otlp` |
| Batching, retry, overflow | built | `pipeline` |
| Audit and compliance records | built | `audit` |
| Plugins and hooks | built | core `Plugin`, `RequestStarter`, `RequestFinisher` |
| Stream and live tail | built | `drain/memory` with SSE |
| Geo from CDN headers | built | `enrich.Geo` |
| Deployment and host metadata | built | `enrich.Host`, `enrich.Deployment` |
| User agent parsing | built | `enrich.UserAgent` |
| Identity headers on every send | built | `internal/httpdrain` |
| Typed field keys | built | core `Key[T]` |
| Field-name presets | built | core `FieldNames`, `FieldsFlat`, `FieldsOTel` |
| Structured logging interop | built | `log/slog`, `log/zap`, `log/zerolog`, `log/logrus` |
| Trace context | built | http-std `traceparent`, `trace/otel` |
| CLI and static analysis | built | `cmd/wlog` (`wlog map`, analyzer) |
| Test recorder | built | `wlogtest` |
| Framework middleware | built | `middleware/nethttp`, `middleware/echo`, `middleware/echo5`, `middleware/gin` |
| LLM token and cost fields | designed for | `enrich-llm`, in CAPABILITIES.md |
| Cron and script helpers | designed for | `job`, in CAPABILITIES.md |
| Queue consumers | designed for | `queue-kafka`, `queue-nats`, `queue-rabbitmq`, `queue-sqs`, in CAPABILITIES.md |
| gRPC interceptors | designed for | `grpc`, in CAPABILITIES.md |
| Outbound HTTP capture | designed for | `http-client`, in CAPABILITIES.md |
| Client and browser logging | not adopted | TypeScript and browser specific |
| Vite plugin | not adopted | TypeScript and browser specific |
| NuxtHub integration | not adopted | TypeScript and browser specific |
| Better Auth integration | not adopted | TypeScript and browser specific |
| CLI telemetry | not adopted | wlog sends no telemetry |

Error catalogs are covered by `errors/herr` instead of an evlog-specific catalog.
