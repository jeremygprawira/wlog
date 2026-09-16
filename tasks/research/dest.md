# Destinations research: OpenTelemetry, metrics, and backend wire formats

Date: 2026-09-16. Scope: facts that a wlog spec needs for OTel bridges, metric exporters,
HTTP drains and stdout presets.

How to read this report:

- Part 1 facts come from Go module source in the module cache, fetched with
  `go mod download -json <module>@latest`. Paths look like `otel@v1.46.0/attribute/value.go:201`.
- Part 2 facts come from official vendor docs, RFCs, or the vendor's own open-source client.
  Every section lists its sources first, and each fact carries a source tag.
- **UNVERIFIED** marks anything not confirmed in a primary source. The last section collects them.

wlog canonical keys used in every mapping table: `timestamp` (RFC3339Nano), `level`
(debug|info|warn|error), `summary` (planned human message), `operation`, `outcome`
(success|error), `duration_ms`, `error.{code,message,kind,status,cause,stack}`,
`service.{name,version,env}`, `trace.{request_id,trace_id,span_id}`,
`http.{method,route,path,status,duration_ms,bytes_in,bytes_out,client_ip,user_agent}`.

## Top findings (read these first)

1. **The OTel Go floor is Go 1.25 today, and it becomes Go 1.26 next release.** Every OTel module
   at v1.46.0 / v0.22.0 declares `go 1.25.0`. The v1.46.0 CHANGELOG says: "This release is the last to support Go 1.25. The next release will require at least Go 1.26."
   wlog's root floor is Go 1.23, so any OTel sub-module needs its own higher `go` line.
   `trace/otel/go.mod` already says `go 1.26.1`.
2. **`go.opentelemetry.io/otel/log` changed its value types in v0.21.0 (2026-08-03).** `log.Value`,
   `log.KeyValue` and `log.Kind` were removed. Records now use `attribute.Value` and
   `attribute.KeyValue` from the root `otel` module. Most blog posts and older bridges
   (otelslog, otelzap before the upgrade) show the removed API.
3. **`attribute` now has `MAP`, `SLICE` (heterogeneous), `BYTESLICE` and `EMPTY`.** They were added in
   v1.44.0 and v1.45.0. Spans, metrics and logs all accept them, and the OTLP exporters encode them.
   Nested wlog groups can map to `attribute.Map`. Non-OTLP backends turn them into strings, so
   flattening to dotted keys stays the safe default for spans.
4. **GenAI semantic conventions left the main repo in semconv v1.42.0 (June 2026).** They now live in
   `open-telemetry/semantic-conventions-genai`, which has no release and nothing Stable. The Go
   `semconv/v1.42.0` and `v1.43.0` packages no longer contain `gen_ai.*` keys. The last Go package
   with them is `semconv/v1.41.0`.
5. **Duration units differ by backend:** OTel metrics use seconds (`s`), ECS
   `event.duration` uses nanoseconds, Datadog `duration` uses nanoseconds, GCP `httpRequest.latency`
   uses a `"0.042s"` string, Honeycomb `duration_ms` uses milliseconds, and EMF uses a declared Unit.
6. **Partial failure inside HTTP 200 is common.** Elasticsearch/OpenSearch (`errors:true` +
   `items[]`), Honeycomb (a positional status array), CloudWatch (`rejectedLogEventsInfo`) and
   VictoriaLogs (bad lines are skipped and only logged) all do it. A drain that reads only the
   HTTP status will silently lose events.

---

# Part 1: OpenTelemetry and metrics (Go source)

## 1.1 Module versions, Go floors, stability

| Module | Latest | Published | `go` directive | Stability |
|---|---|---|---|---|
| `go.opentelemetry.io/otel` | v1.46.0 | 2026-08-25 | 1.25.0 | Stable (Traces, Metrics) |
| `go.opentelemetry.io/otel/trace` | v1.46.0 | 2026-08-25 | 1.25.0 | Stable |
| `go.opentelemetry.io/otel/metric` | v1.46.0 | 2026-08-25 | 1.25.0 | Stable |
| `go.opentelemetry.io/otel/log` | v0.22.0 | 2026-08-25 | 1.25.0 | v0, Logs signal "Beta" in README. Minor releases can add interface methods |
| `go.opentelemetry.io/otel/sdk` | v1.46.0 | 2026-08-25 | 1.25.0 | Stable |
| `go.opentelemetry.io/otel/sdk/log` | v0.22.0 | 2026-08-25 | 1.25.0 | v0 |
| `go.opentelemetry.io/otel/semconv/v1.43.0` | package inside `otel` v1.46.0 | | | Generated. Newest Go package. The spec is at v1.44.0 |
| `github.com/prometheus/client_golang` | v1.24.1 | 2026-07-24 | 1.25.0 | v1 stable. Native histograms "still an experimental feature" |

Sources: `go.mod` of each module, `proxy.golang.org/<module>/@v/<ver>.info`,
`otel@v1.46.0/README.md` (signal table: Traces Stable, Metrics Stable, Logs Beta), and
`otel@v1.46.0/CHANGELOG.md`.

Dependency weight, which matters for the stdlib-only rule. Each of these needs its own `go.mod`:

- `otel/log` requires `go-logr/logr`, `otel`, and indirectly `auto/sdk`, `otel/metric`, `otel/trace` and `xxhash`.
- `otel/trace` requires `otel` and `xxhash`.
- `client_golang` requires `beorn7/perks`, `xxhash`, `json-iterator`, `klauspost/compress`,
  `prometheus/client_model`, `prometheus/common`, `prometheus/procfs`, `golang.org/x/sys` and `protobuf`.

## 1.2 Logs Bridge API: `go.opentelemetry.io/otel/log` v0.22.0

**Package contract** (`log@v0.22.0/doc.go`):

- "This package does not conform to the standard Go versioning policy. All of its interfaces may have
  methods added to them without a package major version bump."
- An implementation must embed `embedded.X` (compile failure on new methods), the interface itself
  (runtime panic), or `noop.X` (silent default).
- A bridge only **calls** `Logger`. It does not implement it, so this risk is small for wlog.

**Interfaces** (`logger.go`, `provider.go`):

```go
type LoggerProvider interface {
    embedded.LoggerProvider
    Logger(name string, options ...LoggerOption) Logger
}
type Logger interface {
    embedded.Logger
    Emit(ctx context.Context, record Record)
    Enabled(ctx context.Context, param EnabledParameters) bool
}
type EnabledParameters struct { Severity Severity; EventName string }

func WithInstrumentationVersion(version string) LoggerOption
func WithInstrumentationAttributes(attr ...attribute.KeyValue) LoggerOption
func WithSchemaURL(schemaURL string) LoggerOption
```

- `Emit`: "The record may be held by the implementation. Callers should not mutate the record after it is passed." It must be safe for concurrent use.
- `Enabled`: "Calling Enabled is optional." "The returned value is not static and may change over time. A cached value can become stale."
- `Logger(name)`: the name should identify the instrumented package. For a bridge, that means the caller passes it in. "An empty name is invalid."
- `ctx` passed to `Emit` carries the active span. The SDK copies the trace and span IDs from it, so wlog should pass the event's original ctx.

**Record** (`record.go`). `Record` is a value type with an inline array of 5 attributes, borrowed from slog:

| Method | Type | Note |
|---|---|---|
| `SetEventName(s string)` | string | "A log record with a non-empty event name is interpreted as an event record." Names "should uniquely identify the structure of the event's attributes and body" |
| `SetTimestamp(t time.Time)` | time | When the event occurred |
| `SetObservedTimestamp(t time.Time)` | time | When it was observed. If it is zero, the SDK fills it (UNVERIFIED for v0.22.0) |
| `SetSeverity(level Severity)` | int enum | See the table below |
| `SetSeverityText(text string)` | string | The original level text, for example `"warn"` |
| `SetBody(v attribute.Value)` | attribute.Value | Any kind, including MAP |
| `SetErr(err error)` | error | If `exception.message` and `exception.type` are absent, the SDK derives them (`sdk/log@v0.22.0/logger.go:175`) |
| `AddAttributes(attrs ...attribute.KeyValue)` | | Appends |
| `WalkAttributes`, `AttributesLen`, `Clone` | | |

**Severity** (`severity.go`): Trace1..4 = 1..4, Debug1..4 = 5..8, Info1..4 = 9..12, Warn1..4 = 13..16,
Error1..4 = 17..20, Fatal1..4 = 21..24. The aliases `SeverityDebug`, `SeverityInfo`, `SeverityWarn` and `SeverityError` point to the `...1` values.
wlog mapping: debug→`SeverityDebug`(5), info→`SeverityInfo`(9), warn→`SeverityWarn`(13), error→`SeverityError`(17).

**Value kinds** (`otel@v1.46.0/attribute/value.go`, `type_string.go`):
`EMPTY, BOOL, INT64, FLOAT64, STRING, BOOLSLICE, INT64SLICE, FLOAT64SLICE, STRINGSLICE, BYTESLICE, SLICE, MAP`.
`INVALID` is deprecated and equals `EMPTY` ("an empty value is a valid value").

- Constructors: `attribute.BoolValue`, `IntValue`, `Int64Value`, `Float64Value`, `StringValue`,
  `BoolSliceValue`, `Int64SliceValue`, `Float64SliceValue`, `StringSliceValue`, `ByteSliceValue`, `SliceValue(v ...Value)`, `MapValue(v ...KeyValue)`.
- The KeyValue forms are `attribute.Bool/Int/Int64/Float64/String/...Slice/ByteSlice/Slice(k, v...)/Map(k, kv...)`.
- `MapValue` docs say: "Users should avoid providing duplicate keys" and "The order of v is not preserved." `AsMap` returns keys sorted.
- Every complex constructor carries this warning: "many observability backends are not optimized to query, index, or aggregate complex attribute values … Prefer primitive values when possible."

History (CHANGELOG):

- v1.44.0 (2026-05-27) added `BYTESLICE` and `SLICE`.
- v1.45.0 (2026-08-03) added `MAP`. It also made the ⚠️ breaking change "Use `attribute.Value` and `attribute.KeyValue` for log bodies and attributes", and removed `Kind`, `Value`, `KeyValue` and their constructors from `otel/log`.

**Global provider** (`log/global/log.go`):

```go
import "go.opentelemetry.io/otel/log/global"
global.Logger(name string, options ...log.LoggerOption) log.Logger
global.GetLoggerProvider() log.LoggerProvider
global.SetLoggerProvider(provider log.LoggerProvider)
```

- "This package is experimental. It will be deprecated and removed when the log package becomes stable. Its functionality will be migrated to go.opentelemetry.io/otel."
- Before a provider is set, the Logger is a no-op. When a provider is registered, it "is updated in place".

**SDK limits** (`sdk/log@v0.22.0/provider.go`):

- `WithAttributeCountLimit`: default **128**, env `OTEL_LOGRECORD_ATTRIBUTE_COUNT_LIMIT`. 0 drops all attributes, and a negative value means no limit.
- `WithAttributeValueLengthLimit`: default **-1** (no limit), env `OTEL_LOGRECORD_ATTRIBUTE_VALUE_LENGTH_LIMIT`. It applies to strings, string slices and byte slices.
- Duplicate keys inside MAP values are removed by default with last-value-wins semantics. `WithAllowKeyDuplication` turns that off.

**Recommended wlog → OTel log record mapping** (derived):

| wlog | Record |
|---|---|
| `timestamp` | `SetTimestamp` |
| `level` | `SetSeverity` + `SetSeverityText(level)` |
| `summary`, else `operation` | `SetBody(attribute.StringValue(...))` |
| `operation` | `SetEventName` for an operation that names a stable schema. Otherwise use attribute `wlog.operation`. Event names are meant to be low-cardinality schema ids, and an HTTP route qualifies |
| `error.message`, `error.kind`, `error.stack` | `exception.message`, `exception.type`, `exception.stacktrace` (all Stable). Do not also call `SetErr` with a synthetic error, or the SDK derives its own type string |
| `service.*` | Resource attributes, which belong to the provider and not the record. If the user's provider lacks them, add record attributes `service.name` and `service.version`, `deployment.environment.name` |
| `trace.trace_id`, `trace.span_id` | Come from `ctx`. Do not add them as attributes |
| groups (`http`, user groups) | Either `attribute.Map("http", ...)` (lossless in OTLP) or flattened `http.request.method` semconv names. Prefer semconv names for reserved keys and `attribute.Map` for user groups |
| `[]any` with mixed types | `attribute.Slice(k, ...)` |
| `[]string` | `attribute.StringSlice` |
| nil | `attribute.Value{}` (EMPTY) |

## 1.3 Tracing API: `go.opentelemetry.io/otel/trace` v1.46.0

**Span methods** (`trace@v1.46.0/span.go`):

- `SetAttributes(kv ...attribute.KeyValue)`: "If a key from kv already exists … it will be overwritten". "adding attributes at span creation using WithAttributes is preferred … as samplers can only consider information already present during span creation."
- `IsRecording() bool`: "true if the Span is active and events can be recorded."
- `SetStatus(code codes.Code, description string)`: applies only "provided the status hasn't already been set to a higher value before (OK > Error > Unset). The description is only included in a status when the code is for an error."
- `RecordError(err error, options ...EventOption)`: records an exception event. "An additional call to SetStatus is required if the Status of the Span should be set to Error".
- If ctx has no span, `SpanFromContext(ctx)` returns a no-op span. `SpanContextFromContext(ctx)` gives the IDs.
- `codes` (`otel@v1.46.0/codes/codes.go`): `Unset = 0`, `Error = 1`, `Ok = 2`. The code comment says "The Ok code in OTLP is 1", so the Go and OTLP numbers differ.

**SDK behaviour** (`sdk@v1.46.0/trace/span.go:242`, `span_limits.go`):

- A span that is not recording ignores `SetAttributes`, so checking `IsRecording()` first only saves the build cost.
- An invalid attribute (empty key) is dropped and counted.
- When the count limit is hit, attributes are de-duplicated, and extras are dropped and counted.
- Defaults: `DefaultAttributeCountLimit = 128` (env `OTEL_SPAN_ATTRIBUTE_COUNT_LIMIT`),
  `DefaultAttributeValueLengthLimit = -1` (unlimited, env `OTEL_SPAN_ATTRIBUTE_VALUE_LENGTH_LIMIT`).
  Event, link, and per-event and per-link attribute limits are 128 each.
- Since v1.45.0, `AttributeValueLengthLimit` applies recursively inside MAP and SLICE values, and MAP duplicate keys are removed.
- The spec defines `AttributeValueDepthLimit` default 64 (see below). **The Go SDK v1.46.0 has no depth-limit option**: a grep for "depth" in `sdk/trace` and `attribute` finds nothing. wlog's own group caps must bound depth.

**Spec on complex values** (https://opentelemetry.io/docs/specs/otel/common/, Stable section):

- `AnyValue`, including `map<string, AnyValue>`, heterogeneous arrays, byte arrays and empty values, is allowed for "Resources, Instrumentation Scopes, Metric points, Spans, Span Events, Span Links and Log Records."
- "For protocols that do not natively support some of the AnyValue types, those values SHOULD be represented as strings", which is lossy.
- Limits: `AttributeCountLimit` default 128, `AttributeValueLengthLimit` default Infinity, `AttributeValueDepthLimit` default 64. Metric attributes are exempt, and resource attributes SHOULD be exempt.

**How to flatten nested wlog values for spans** (recommendation):

1. Reserved keys go to their semconv names (table 1.6.9). Do not copy `http.*` verbatim, because wlog `http.status` is not `http.response.status_code`.
2. User groups become dot-joined keys, for example `order.id`. Stop at wlog's existing group depth and cap. This keeps Jaeger, Zipkin, X-Ray and Tempo search usable.
3. Scalar slices use typed slice attributes. Mixed slices and slices of maps become one JSON string (`attribute.String(k, json)`). For a known OTLP exporter, `attribute.Slice` is also correct.
4. `time.Time` becomes an RFC3339Nano string. For a key that ends in `.duration`, a `time.Duration` becomes a float64 in seconds. Other durations become an int64 of ms named `*_ms`.
5. Skip keys the span already has from HTTP instrumentation (`otelhttp`) to avoid duplicates. `SetAttributes` overwrites, so wlog values win.

**Span status from outcome** (semconv rules, see 1.6.1 and 1.6.8):

- `outcome=error` → `span.SetStatus(codes.Error, error.message)` + `error.type` = `error.kind`, else `error.code`, else `"_OTHER"`.
- `outcome=success` → leave Unset. Instrumentation should not set `Ok`. Only the application may.
- HTTP server: 5xx → Error with `error.type="500"` (the status code as a string). 4xx → leave Unset on server spans.
- Exceptions: the semconv "recording errors" page (Status: Development) says instrumentation "SHOULD record this exception as a log record". It is "NOT RECOMMENDED to record the same exception more than once". So prefer one OTel log record over `span.RecordError`, or pick one.

## 1.4 Metrics API: `go.opentelemetry.io/otel/metric` v1.46.0

**Meter** (`metric@v1.46.0/meter.go`) has `Int64Counter`, `Int64UpDownCounter`, `Int64Histogram`,
`Int64Gauge`, `Int64Observable*`, `Float64Counter`, `Float64UpDownCounter`, `Float64Histogram`, `Float64Gauge` and
`Float64Observable*`. Each has the signature `(name string, options ...XOption) (X, error)`.

- Options (`instrument.go`): `metric.WithDescription(string)`, `metric.WithUnit(string)`,
  `metric.WithExplicitBucketBoundaries(bounds ...float64)` (histograms), and on measurements `metric.WithAttributes(kv ...attribute.KeyValue)` or `metric.WithAttributeSet(attribute.Set)`.
  For a reused set, `WithAttributeSet` avoids a copy.
- Sync instruments have `Add(ctx, incr, ...AddOption)` or `Record(ctx, value, ...RecordOption)` and
  `Enabled(context.Context) bool`. `Record` on histograms expects non-negative values (CHANGELOG 1.45.0).
- Global: `otel.Meter(name, ...metric.MeterOption)`, `otel.GetMeterProvider()` and `otel.SetMeterProvider(mp)` live in the root `otel` module.

**Semconv instrument `http.server.request.duration`** (`semconv/v1.43.0/httpconv/metric.go:1678`, confirmed on
https://opentelemetry.io/docs/specs/semconv/http/http-metrics/):

- Histogram, unit **`s`**, description "Duration of HTTP server requests.", Stable.
- Advised buckets: `0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10`.
- Attributes:
  - Required: `http.request.method`, `url.scheme`.
  - Conditionally Required: `error.type` (if the request ended with an error), `http.response.status_code` (if sent), `http.route` (if available), `network.protocol.name` (if not `http` and the version is set).
  - Recommended: `network.protocol.version`.
  - Opt-In: `server.address`, `server.port`, `user_agent.synthetic.type`.
- Never put `url.path`, `client.address` or user ids on the metric. They are high cardinality.

```go
// Minimal wlog metric bridge, derived from the API above.
m := provider.Meter("github.com/jeremygprawira/wlog")
dur, _ := m.Float64Histogram("http.server.request.duration",
    metric.WithUnit("s"),
    metric.WithDescription("Duration of HTTP server requests."),
    metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10))
// at emit:
if dur.Enabled(ctx) {
    dur.Record(ctx, float64(durationMS)/1000, metric.WithAttributes(
        attribute.String("http.request.method", method),
        attribute.String("url.scheme", scheme),
        attribute.String("http.route", route),
        attribute.Int("http.response.status_code", status)))
}
```

A generic wlog counter should follow semconv naming, for example `wlog.events` (unit `{event}`) with
attributes `operation` and `outcome`. That name is not a semconv name, so it belongs in wlog's own namespace.

## 1.5 Prometheus: `github.com/prometheus/client_golang` v1.24.1

- `prometheus.NewRegistry() *Registry` (`registry.go:67`). It has no default collectors, and
  `NewPedanticRegistry()` checks descriptor consistency. `(*Registry).Register(c Collector) error`
  returns `AlreadyRegisteredError{ExistingCollector, NewCollector}` on duplicates. `MustRegister` panics, so wlog must use `Register` (the never-panic rule).
- `prometheus.NewHistogramVec(opts HistogramOpts, labelNames []string) *HistogramVec` (`histogram.go:1183`).
  `WithLabelValues(lvs ...string) Observer` panics on a wrong label count. `GetMetricWithLabelValues` returns an error instead, which is the never-panic choice.
- `prometheus.NewCounterVec(opts CounterOpts, labelNames []string) *CounterVec` (`counter.go:195`). It has the same `WithLabelValues` / `GetMetricWithLabelValues` pair.
- `DefBuckets = {.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}`. It lacks 0.075, 0.75 and 7.5 compared with the OTel advice.
- Native histograms (`HistogramOpts`):
  - `NativeHistogramBucketFactor float64`: >1 enables them. "A generally good trade-off … is a value of 1.1".
  - `NativeHistogramZeroThreshold`.
  - `NativeHistogramMaxBucketNumber uint32`: "highly recommended to set" when observed values come from external input (a DoS vector).
  - `NativeHistogramMinResetDuration`, `NativeHistogramMaxZeroThreshold`.
  - `NativeHistogramMaxExemplars` (default 10), `NativeHistogramExemplarTTL` (default 5m).
  - "Native Histograms are still an experimental feature … might still change their behavior or name … without a major version bump." Prometheus server needs v2.40+.
  - Classic `Buckets` and native buckets can be set together.
- Exposition: `promhttp.HandlerFor(reg prometheus.Gatherer, opts promhttp.HandlerOpts) http.Handler`, with `HandlerOpts.EnableOpenMetrics` (needed for exemplars in text format).
- Prometheus naming for the same metric: `http_server_request_duration_seconds` with labels
  `http_request_method`, `http_route`, `http_response_status_code` and `url_scheme`. This follows the OTel→Prometheus translation convention (UNVERIFIED against the current Prometheus OTLP naming doc).

## 1.6 Semantic conventions

Versions:

- Spec releases (https://github.com/open-telemetry/semantic-conventions/releases): **v1.44.0 (2026-08-04)**, v1.43.0 (07-03), v1.42.0 (06-12), v1.41.0 (04-28).
- The newest Go package is `go.opentelemetry.io/otel/semconv/v1.43.0` (`SchemaURL = "https://opentelemetry.io/schemas/1.43.0"`).
- The stability column below is taken from the generated `// Stability:` comments in `semconv/v1.43.0/attribute_group.go`. All 656 keys were extracted to [semconv143-attrs.tsv](semconv143-attrs.tsv).

### 1.6.1 HTTP server span (https://opentelemetry.io/docs/specs/semconv/http/http-spans/)

- Span name: `{method} {target}`. If `http.route` is available, it is the target. For the method `_OTHER`, the name uses `HTTP`. Kind: SERVER.
- Status: 1xx to 4xx leave it Unset. 5xx or an uninterpretable code → Error.
- `error.type`: when the status is ≥500, use the status code as a string (for example `"500"`). Otherwise use the exception type or another low-cardinality id.

| Attribute | Requirement (server span) | Stability |
|---|---|---|
| `http.request.method` | Required | Stable |
| `url.path` | Required | Stable |
| `url.scheme` | Required | Stable |
| `error.type` | Cond. Required | Stable |
| `http.request.method_original` | Cond. Required (if method was normalised) | Stable |
| `http.response.status_code` | Cond. Required | Stable |
| `http.route` | Cond. Required "If and only if it's available" | Stable |
| `network.protocol.name` | Cond. Required | Stable |
| `server.port` | Cond. Required | Stable |
| `url.query` | Cond. Required "If and only if one was received/sent" | Stable |
| `client.address` | Recommended | Stable |
| `network.peer.address`, `network.peer.port` | Recommended | Stable (UNVERIFIED stability, not in extract filter) |
| `network.protocol.version` | Recommended | Stable |
| `server.address` | Recommended | Stable |
| `user_agent.original` | Recommended | Stable |
| `client.port` | Opt-In | Stable |
| `http.request.body.size`, `http.response.body.size` | Opt-In | **Development** |
| `http.request.size`, `http.response.size` | Opt-In | **Development** |
| `http.request.header.<key>`, `http.response.header.<key>` | Opt-In | Stable (UNVERIFIED) |
| `network.local.address`, `network.local.port`, `network.transport` | Opt-In | Stable (UNVERIFIED) |
| `user_agent.synthetic.type` | Opt-In | Development |

### 1.6.2 url.*, client.*, user_agent.*

- Stable: `url.full`, `url.path`, `url.query`, `url.scheme`, `url.fragment`, `client.address`, `client.port`, `user_agent.original`.
- Development: `url.original`, `url.domain`, `url.port`, `url.template`, `url.extension`, `url.registered_domain`, `url.subdomain`, `url.top_level_domain`, `user_agent.name`, `user_agent.version`, `user_agent.os.name`, `user_agent.os.version`, `user_agent.synthetic.type`.

### 1.6.3 error.* and exception.*

- `error.type`: Stable. Low cardinality, with `_OTHER` as the fallback value.
- `exception.message`, `exception.type`, `exception.stacktrace`: Stable.
- `code.stacktrace`, `code.function.name`, `code.file.path`, `code.line.number`, `code.column.number`: Stable.
- The semconv "Recording errors" page is still Status: Development.

### 1.6.4 service.*, deployment.*, telemetry.*

- Stable: `service.name`, `service.version`, `service.namespace`, `service.instance.id`, `deployment.environment.name`, `telemetry.sdk.name`, `telemetry.sdk.language`, `telemetry.sdk.version`.
- `service.criticality` is Alpha. `service.peer.name`, `service.peer.namespace`, `deployment.id`, `deployment.name` and `deployment.status` are Development.
- wlog `service.env` → `deployment.environment.name`. The old `deployment.environment` is deprecated.

### 1.6.5 messaging.* (all Development)

- `messaging.system`, `messaging.operation.name`, `messaging.operation.type`, `messaging.destination.name`, `messaging.destination.template`, `messaging.destination.partition.id`, `messaging.destination.subscription.name`, `messaging.destination.temporary`, `messaging.destination.anonymous`, `messaging.consumer.group.name`, `messaging.message.id`, `messaging.batch.message_count`.
- Kafka: `messaging.kafka.message.key`, `messaging.kafka.offset`, `messaging.kafka.message.tombstone`.
- RabbitMQ: `messaging.rabbitmq.destination.routing_key`, `messaging.rabbitmq.message.delivery_tag`.
- v1.44.0 "Define a span per messaging operation type (create, send, receive, process, settle)". Messaging is still not Stable.

### 1.6.6 rpc.* (Release Candidate since v1.42.0)

- `rpc.system.name`, `rpc.method`, `rpc.method_original`, `rpc.response.status_code`: all `Release_Candidate` in Go v1.43.0.
- v1.42.0 notes: "Promote core RPC (plus gRPC and Apache Dubbo) semantic conventions to release candidate."
- The older `rpc.system`, `rpc.service` and `rpc.grpc.status_code` keys are gone from v1.43.0. Check before use (UNVERIFIED which replaced which).

### 1.6.7 db.* (Stable)

- Stable: `db.system.name`, `db.operation.name`, `db.query.summary`, `db.query.text`, `db.collection.name`, `db.namespace`, `db.operation.batch.size`, `db.response.status_code`, `db.stored_procedure.name`.
- Development: `db.response.returned_rows`, `db.client.connection.pool.name`, `db.client.connection.state`.
- v1.44.0 clarified that `db.query.parameter.<key>` carries sensitive data. wlog's redactor must cover it.

### 1.6.8 gen_ai.* (moved, not Stable anywhere)

- semconv v1.42.0: "All `gen_ai.*` attributes, metrics, events, and spans have been deprecated and moved to the dedicated OpenTelemetry GenAI Semantic Conventions Repository" (`open-telemetry/semantic-conventions-genai`).
- The registry page https://opentelemetry.io/docs/specs/semconv/registry/attributes/gen-ai/ now marks most `gen_ai.*` keys "Deprecated" with a "moved" note. `gen_ai.provider.name` and `gen_ai.operation.name` still show Development. `gen_ai.system` is "Replaced by `gen_ai.provider.name`".
- The new repo has no release. "no GenAI-specific span, event, metric, or attribute … is marked Stable" (third-party summary, John Hodge blog, July 2026, UNVERIFIED in the repo itself).
- Go: `semconv/v1.42.0/MIGRATION.md` lists all `GenAI*` declarations as removed. Names as of Go `semconv/v1.41.0` (all Development):

| Attribute | Type |
|---|---|
| `gen_ai.provider.name` | string (replaces `gen_ai.system`) |
| `gen_ai.operation.name` | string (`chat`, `embeddings`, `execute_tool`, `create_agent`, …) |
| `gen_ai.request.model`, `gen_ai.response.model`, `gen_ai.response.id` | string |
| `gen_ai.response.finish_reasons` | string[] |
| `gen_ai.conversation.id` | string |
| `gen_ai.usage.input_tokens` | int |
| `gen_ai.usage.output_tokens` | int |
| `gen_ai.usage.cache_read.input_tokens` | int |
| `gen_ai.usage.cache_creation.input_tokens` | int |
| `gen_ai.usage.reasoning.output_tokens` | int (present in v1.41.0 only, not in v1.40.0) |

Advice: pin wlog's LLM keys to these names, but document them as "Development, can move". Do not import a Go semconv package for them.

### 1.6.9 wlog → OTel semconv mapping (for span attributes and log attributes)

| wlog | OTel | Note |
|---|---|---|
| `timestamp` | span start/end, or log `Timestamp` | |
| `level` | log Severity. On spans, no attribute | |
| `summary` | log Body | |
| `operation` | span name, log EventName or `wlog.operation` | Low cardinality for span names |
| `outcome` | span Status (Error or Unset) | |
| `duration_ms` | span duration. Metric in seconds | |
| `error.message` | Status description + `exception.message` | |
| `error.kind` (else `error.code`) | `error.type` + `exception.type` | |
| `error.stack` | `exception.stacktrace` | |
| `error.code`, `error.status`, `error.cause` | `wlog.error.code` etc. | No semconv equivalent |
| `service.name`, `service.version` | `service.name`, `service.version` (resource) | |
| `service.env` | `deployment.environment.name` (resource) | |
| `trace.trace_id`, `trace.span_id` | span context | |
| `trace.request_id` | `http.request.header.x-request-id` (string[]) or `wlog.request_id` | No semconv request id |
| `http.method` | `http.request.method` | |
| `http.route` | `http.route` | |
| `http.path` | `url.path` | |
| `http.status` | `http.response.status_code` (int) | |
| `http.duration_ms` | `http.server.request.duration` (s) | |
| `http.bytes_in`, `http.bytes_out` | `http.request.body.size`, `http.response.body.size` | Development |
| `http.client_ip` | `client.address` | |
| `http.user_agent` | `user_agent.original` | |
| `http.request_query` | `url.query` | Redact first |
| `http.request_headers.<k>` | `http.request.header.<k>` (string[]) | Key lower-cased |

---

# Part 2: Backend wire formats

## 1. Honeycomb Events API (batch)

Sources: [H1] https://docs.honeycomb.io/api/events/create-events · [H2] https://docs.honeycomb.io/api/events/create-an-event · [H3] https://docs.honeycomb.io/api/events · [H4] https://docs.honeycomb.io/api/rate-limit · [H5] https://docs.honeycomb.io/api/authentication · [H6] https://docs.honeycomb.io/configure/environments/manage-api-keys · [H7] https://docs.honeycomb.io/troubleshoot/common-issues/data-in-honeycomb · [H8] https://docs.honeycomb.io/working-with-your-data/managing-your-data/definitions/ · [H9] https://github.com/honeycombio/libhoney-go/blob/main/transmission/transmission.go (Honeycomb's own Go client) · [H10] https://docs.honeycomb.io/api/datasets/create-a-dataset

**Request**
- `POST https://api.honeycomb.io/1/batch/{datasetSlug}` for US. `POST https://api.eu1.honeycomb.io/1/batch/{datasetSlug}` for EU. [H1]
- Headers: `X-Honeycomb-Team: <key>` (required), `Content-Type: application/json` [H2], and optionally `Content-Encoding: gzip` or `zstd` [H1]. `X-Honeycomb-Event-Time` and `X-Honeycomb-Samplerate` exist only on the single-event endpoint `/1/events/{dataset}`, which Honeycomb calls "highly discouraged" for anything beyond testing. [H2]
- Body: a JSON array of `{data, time?, samplerate?}` [H1].
  - `time`: "RFC3339 high precision format (for example, YYYY-MM-DDTHH:MM:SS.mmmZ)" or a Unix epoch. It defaults to the time the API receives the event. [H1][H3]
  - `samplerate`: an integer, the n in 1/n. It defaults to 1. [H1]
  ```json
  [{"time":"2026-09-16T10:00:00.123456789Z","samplerate":1,
    "data":{"level":"error","name":"POST /v1/charges","duration_ms":182.4,"service.name":"checkout",
            "trace.trace_id":"4bf92f35...","trace.span_id":"00f067aa...","error":true,"error.message":"card declined"}}]
  ```
- Dataset path:
  - Dataset names are case-insensitive. A dataset is created automatically on the first event, and the first event sets the display casing. [H3]
  - Auto-creation only happens "if the associated Ingest Key can implicitly create datasets" [H6].
  - Names are 1–255 chars. The slug is read-only, and no derivation rules are documented. [H10]
  - libhoney-go builds the path with `url.JoinPath(apiHost, "/1/batch", url.PathEscape(dataset))` [H9].

**Auth / key types**
- Ingest keys have the prefix `hc[x]ik_`. The header value is Key ID and Secret joined with no separator. [H5]
- Configuration keys (`hc[x]lk_`) also work but "must have the **Send Events** permission". Ingest keys are recommended. [H1][H5]
- Management keys use `Authorization: Bearer`. They are not for ingest. [H5]
- Classic: "Classic API keys operate at the Team level and access all your Classic Datasets directly". Environment keys reach a single Environment. [H5]

**Compression:** gzip and zstd are both documented [H1]. libhoney-go sends `Content-Encoding: zstd` [H9].

**Limits**
- Max "2,000 fields per event" [H1].
- "Each string field has a maximum length of 64KB" [H1].
- "The entire event must be less than 1 MB of uncompressed JSON" [H1].
- The single-event page says "request body is limited to raw (potentially compressed) size of 1MB" [H2].
- The batch body cap is not stated clearly in the docs. libhoney-go uses `apiMaxBatchSize = 5000000 // 5MB` and `apiEventSizeMax = 1_000_000` [H9].
- Max field-name length: not documented.

**Per-item errors**
- An HTTP 200 comes back with a positional array, for example `[{"status":202},{"status":400,"error":"Event has too many columns."},{"status":400,"error":"Request body is malformed and cannot be read."}]` [H1].
- libhoney matches results to events by index [H9].

**Whole-request codes** [H1][H2]
| Code | Meaning |
|---|---|
| 400 | "Request body should not be empty", "Dataset has too many columns", "Request body is malformed…", "Request body is too large" |
| 401 | "unknown API key - check your credentials" |
| 403 | "Event dropped due to administrative throttling" |
| 404 | "dataset not found" |
| 429 | "Request dropped due to rate limiting" or "Event dropped due to administrative denylist" |

**Retry**
- "The Events, Batch Events, and Query Data APIs do not include a `Retry-After` header in their `429` responses". Use exponential backoff. [H4]
- Ingest will "rate limit" (reject too many events per second) and "throttle" (drop 9 of 10 spans after several days of too many spans) [H7].

**Trace-recognised fields**
- "Every trace event must have … (`trace.trace_id`), a span identifier (`trace.span_id`), and a duration (`duration_ms`)". IDs must be strings and duration must be a number in ms. Root spans must lack `trace.parent_id`. [H7]
- Dataset Definitions (Span ID, Trace ID, Parent Span ID, Name, Service Name, Span Duration, Error, HTTP Status Code, Route, User, Log Message, Log Severity) are configurable per dataset. The docs do not list the default column names each one auto-detects. [H8]
- Log Severity accepts `trace, debug, info, warn, error, fatal, unspecified` [H8].

| wlog key | Honeycomb |
|---|---|
| timestamp | top-level `time` (outside `data`) [H1] |
| level | `data.level`, set as the Log Severity definition [H8] |
| summary | e.g. `data.message`, set as the Log Message definition [H8] |
| operation | `data.name` (Name definition) [H8] |
| outcome | keep it, and add `data.error` bool (Error definition is "boolean or string") [H8] |
| duration_ms | `data.duration_ms`, number in ms (required for traces) [H7] |
| error.code/message/kind/status/cause/stack | flat dotted keys `error.code` etc. Truncate stack to ≤64KB [H1] |
| service.name / version / env | `service.name` (Service Name definition [H8]), `service.version`, `service.env` |
| trace.trace_id / span_id | `trace.trace_id`, `trace.span_id`, as strings [H7] |
| trace.request_id | custom `trace.request_id` (no special meaning) |
| http.route | `http.route`, set as the Route definition [H8] |
| http.status | e.g. `http.status_code`, set as the HTTP Status Code definition [H8] |
| other http.* | flat dotted keys, as-is |
| sampling | top-level `samplerate` (int n) [H1] |



## 2. Elasticsearch `_bulk`, OpenSearch `_bulk`, and ECS

Sources:

- [E1] https://www.elastic.co/docs/api/doc/elasticsearch/operation/operation-bulk
- [E3] https://www.elastic.co/docs/reference/elasticsearch/configuration-reference/networking-settings
- [E5] https://www.elastic.co/docs/troubleshoot/elasticsearch/rejected-requests
- [E6] https://www.elastic.co/docs/api/doc/elasticsearch/operation/operation-security-create-api-key
- [E10] https://www.elastic.co/docs/reference/elasticsearch/rest-apis/api-conventions
- [OS1] https://docs.opensearch.org/latest/api-reference/document-apis/bulk/ (read from the source file `opensearch-project/documentation-website/_api-reference/document-apis/bulk.md`)
- [OS2] `documentation-website/_install-and-configure/configuring-opensearch/network-settings.md`
- [OS3] `documentation-website/_clients/go.md`
- [ECS] https://www.elastic.co/docs/reference/ecs (ECS **9.5.0**), with the field pages `ecs-base`, `ecs-event`, `ecs-error`, `ecs-http`, `ecs-url`, `ecs-client`, `ecs-user_agent`, `ecs-service`, `ecs-tracing` and `ecs-log`

### Request (Elasticsearch)

- `POST|PUT /_bulk` or `POST|PUT /{index}/_bulk` [E1].
- `Content-Type: application/x-ndjson` or `application/json`. The bulk API accepts "NDJSON, JSON, and SMILE; other types will result in an error response" [E10]. "Elasticsearch only supports UTF-8-encoded JSON" [E10].
- Body format [E1]:
  - Each operation is an `action_and_meta_data\n` line, then an `optional_source\n` line.
  - "The final line of data must end with a newline character (`\n`)."
  - Do not pretty-print.
- Actions: `index`, `create`, `update`, `delete`. Metadata keys are `_index`, `_id`, `routing`, `require_alias`, `dynamic_templates`, `pipeline`, `if_seq_no` and `if_primary_term` [E1].
- **Data streams:** "Data streams support only the `create` action" [E1]. With `POST /logs-wlog-default/_bulk`, the action line is `{"create":{}}`. A data stream document needs `@timestamp`.
- Query params [E1]:
  - `refresh` (`true|false|wait_for`). Leave it unset for logs.
  - `pipeline` (`_none` disables the default pipeline).
  - `timeout` (default `1m`), `wait_for_active_shards` (default `1`).
  - `require_data_stream`, `require_alias`.
  - `include_source_on_error` (default `true`). Set it to `false`, so error reasons do not echo redacted-but-sensitive source back into wlog's own logs.
- `filter_path=errors,items.*.error,items.*.status` shrinks the response. This is a common option, and [OS1] shows `filter_path=items.*.error`.

```
POST /logs-wlog-default/_bulk?filter_path=errors,items.*.status,items.*.error HTTP/1.1
Host: my-deploy.es.us-east-1.aws.elastic.cloud
Authorization: ApiKey VnVhQ2ZHY0JDZGJrUW0tZTVhT3g6dWkybHAyYXhUTm1zeWFrdzl0dk5udw==
Content-Type: application/x-ndjson
Content-Encoding: gzip

{"create":{}}
{"@timestamp":"2026-09-16T08:30:00.123456789Z","message":"order failed","log":{"level":"error"},"event":{"action":"POST /orders/:id","outcome":"failure","duration":42000000},"service":{"name":"checkout","version":"1.4.2","environment":"prod"},"trace":{"id":"4bf92f3577b34da6a3ce929d0e0e4736"},"span":{"id":"00f067aa0ba902b7"},"http":{"request":{"method":"POST","id":"r-1"},"response":{"status_code":500}},"url":{"path":"/orders/4821"},"error":{"code":"DB_TIMEOUT","type":"timeout","message":"db timeout"},"ecs":{"version":"9.5.0"}}
```

### Auth

- API key: the create-key response has `id`, `name`, `api_key` and `encoded`. `encoded` is "the base64-encoding of the UTF-8 representation of `id` and `api_key` joined by a colon". The header is `Authorization: ApiKey <encoded>` [E6].
- Basic auth (`Authorization: Basic base64(user:pass)`) is standard HTTP and works with native users (UNVERIFIED on a fetched page).
- Elastic Cloud endpoint host shapes are UNVERIFIED. Take the URL from the deployment page, not a pattern.
- OpenSearch: basic auth for self-hosted clusters. **Amazon OpenSearch Service needs SigV4.** The official Go client signs with service name `"es"` for managed domains and `"aoss"` for Serverless, through `opensearch-go/v4/signer/awsv2` [OS3]. A stdlib-only drain needs hand-written SigV4. The CloudWatch section discusses the same cost.

### Compression and size

- `http.max_content_length`: default **100mb**. "If the body is compressed, the limit applies to the HTTP request body size before compression." Raising it above 100mb "can cause cluster instability" [E3]. OpenSearch has the same 100mb default [OS2].
- Request gzip: [E3] says "Elasticsearch will compress a response if the inbound request was compressed", so compressed request bodies are accepted. `http.compression` covers responses through `Accept-Encoding`. With HTTPS on, it defaults to `false`. Otherwise it defaults to `true` [E3][OS2].
- Batch sizing: "there is no 'correct' number of actions" [E1]. OpenSearch says "Start with batches of 1,000 to 5,000 operations". "A good bulk request size typically falls between 5 MB and 15 MB" [OS1].
- OpenSearch 2.9+: document `_id` must be ≤512 bytes [OS1]. Data streams generate ids, so this does not affect wlog.
- Mapping explosion: ECS data streams use the default field limit (`index.mapping.total_fields.limit`, 1000 by default). UNVERIFIED for `logs-*-*` templates, which can raise it. Free-form wlog user keys can hit this limit. Consider putting user keys under `labels` (flat, keyword) or a `flattened` field.

### Responses, per-item errors, retry

- Failed items still get HTTP status 200. The body is `{"took":n,"errors":bool,"items":[{"<action>":{"_index","_id","status","result"?, "error"?:{"type","reason",...}}}]}`. Items come "in the same order as submitted" [E1][OS1].
- Example item failure [OS1]: `{"update":{"_index":"movies","_id":"nonexistent1","status":404,"error":{"type":"document_missing_exception","reason":"[nonexistent1]: document missing","index":"movies","shard":"0","index_uuid":"..."}}}`.
- Whole-request rejection under load is **HTTP 429** (`es_rejected_execution_exception`). "You can retry HTTP 429 errors, but it's generally best to implement exponential backoff" [E5].
- Per-item retry policy. This is derived, because the Elastic docs do not list it per status:
  - Retry an item with `status` 429 or ≥500, sending only those items.
  - Drop and count 400 items: `mapper_parsing_exception`, `document_parsing_exception`, `illegal_argument_exception`. They fail again.
  - 409 with `create` on an existing id cannot happen for data streams without `_id`.
  - Whole request: retry 429, 502, 503 and 504 with backoff. Do not retry 400, 401, 403 or 413. On 413, split the batch.

### ECS field names (ECS 9.5.0)

| ECS field | Type | Definition and notes |
|---|---|---|
| `@timestamp` | date | Required. "Date/time when the event originated" |
| `message` | match_only_text | "the log message, optimized for viewing in a log viewer" |
| `labels` | object | "Custom key/value pairs … Should not contain nested objects." Keyword values |
| `tags` | keyword[] | |
| `ecs.version` | keyword | Page not fetched (UNVERIFIED type). Set `9.5.0` |
| `log.level` | keyword | "Original log level of the log event", for example `warn`, `err` |
| `log.logger` | keyword | |
| `log.origin.file.name`, `log.origin.file.line` (long), `log.origin.function` | | |
| `event.duration` | long | "Duration of the event in **nanoseconds**" |
| `event.outcome` | keyword | Allowed: `failure`, `success`, `unknown` |
| `event.action` | keyword | "The action captured by the event" |
| `event.dataset`, `event.kind` (alert, asset, enrichment, event, metric, state, pipeline_error, signal), `event.category` (…, api, web, database, …), `event.start`, `event.end`, `event.created`, `event.id` | | |
| `service.name`, `service.version`, `service.id`, `service.type` | keyword | Core |
| `service.environment` | keyword | Extended. "**Beta and subject to change**" |
| `service.node.name` | keyword | |
| `trace.id`, `span.id`, `transaction.id` | keyword | |
| `error.message` | match_only_text | |
| `error.type` | keyword | "for example the class name of the exception" |
| `error.code` | keyword | |
| `error.id` | keyword | |
| `error.stack_trace` | wildcard (+ `.text`) | |
| `http.request.method` | keyword | "Retain its casing from the original event" |
| `http.request.id` | keyword | |
| `http.request.body.bytes`, `http.request.bytes` | long | Body only, or body + headers |
| `http.response.status_code` | long | |
| `http.response.body.bytes`, `http.response.bytes` | long | |
| `http.version` | keyword | For example `1.1` |
| `url.path`, `url.original`, `url.full` | wildcard | |
| `url.query`, `url.scheme`, `url.domain` | keyword | |
| `client.ip` | ip | |
| `client.address` | keyword | |
| `client.port` | long | |
| `user_agent.original` | keyword (+ `.text`) | "Unparsed user_agent string" |

### wlog → ECS mapping

| wlog | ECS | Rule |
|---|---|---|
| `timestamp` | `@timestamp` | As-is (RFC3339Nano) |
| `level` | `log.level` | As-is |
| `summary` | `message` | Fall back to `operation` |
| `operation` | `event.action` | |
| `outcome` | `event.outcome` | `success`→`success`, `error`→`failure` |
| `duration_ms` | `event.duration` | × 1,000,000 (ns, long) |
| `error.message` | `error.message` | |
| `error.kind` | `error.type` | |
| `error.code` | `error.code` | |
| `error.stack` | `error.stack_trace` | |
| `error.status`, `error.cause` | `labels.error_status`, `labels.error_cause`, or custom keys | No ECS field |
| `service.name`, `service.version` | `service.name`, `service.version` | |
| `service.env` | `service.environment` | Beta field |
| `trace.trace_id` | `trace.id` | |
| `trace.span_id` | `span.id` | |
| `trace.request_id` | `http.request.id` | |
| `http.method` | `http.request.method` | |
| `http.route` | `labels.http_route`, or custom `http.route` | ECS has no route field (UNVERIFIED: not found on the `ecs-http` page) |
| `http.path` | `url.path` | |
| `http.status` | `http.response.status_code` | |
| `http.duration_ms` | `event.duration` | Same event, so use one of them |
| `http.bytes_in`, `http.bytes_out` | `http.request.body.bytes`, `http.response.body.bytes` | |
| `http.client_ip` | `client.ip` | Must be a valid IP, or the `ip` mapping rejects the doc. For a value that is not surely an IP, use `client.address` |
| `http.user_agent` | `user_agent.original` | |

## 3. Splunk HTTP Event Collector (HEC)

Sources:

- [S1] https://help.splunk.com/en/splunk-enterprise/get-started/get-data-in/10.4/get-data-with-http-event-collector/format-events-for-http-event-collector
- [S2] https://help.splunk.com/en/splunk-enterprise/get-started/get-data-in/10.4/get-data-with-http-event-collector/troubleshoot-http-event-collector
- [S3] https://help.splunk.com/en/splunk-cloud-platform/get-started/get-data-in/10.5.2605/get-data-with-http-event-collector/http-event-collector-rest-api-endpoints
- [S4] https://help.splunk.com/en/splunk-enterprise/get-started/get-data-in/9.3/get-data-with-http-event-collector/set-up-and-use-http-event-collector-in-splunk-web
- [S5] OpenTelemetry Collector `splunkhecexporter` (Splunk-maintained): `exporter/splunkhecexporter/README.md`, `client.go`, `pkg/translator/splunk/common.go`, `internal/splunk/httprequest.go` in `open-telemetry/opentelemetry-collector-contrib@main`

`docs.splunk.com` and `dev.splunk.com` returned 403 or JS-only pages, so help.splunk.com and the Splunk exporter source were used.

### Endpoints [S3][S4]

- Enterprise: `<protocol>://<host>:<port>/<endpoint>`. The default port is **8088**, and SSL is set per HEC instance [S4].
- Splunk Cloud Platform [S4]:
  - AWS (standard and free trial): `<protocol>://http-inputs-<host>.splunkcloud.com:<port>/<endpoint>`.
  - Google Cloud: `http-inputs.<host>.splunkcloud.com`.
  - FedRAMP Moderate (GovCloud): `http-inputs.<host>.splunkcloudgc.com`.
  - "Port 8088 applies to free trials. Port 443 is the default for standard Cloud Platform instances."
- `POST /services/collector/event` (and `/services/collector/event/1.0`): the JSON envelope protocol [S3].
- `POST /services/collector/raw` (and `/1.0`): raw text. It needs a channel [S1][S3].
- `/services/collector/ack`: "Queries event indexing status" [S3].
- `GET /services/collector/health`: "supported in Splunk Cloud Platform and versions 6.6.0 and higher of Splunk Enterprise" [S3].

### Auth and headers

- `Authorization: Splunk <hec_token>`. Example: `curl https://hec.example.com:8088/services/collector/event -H "Authorization: Splunk B5A79AAD-D822-46CC-80D1-819F80D7BFB0"` [S4].
- `X-Splunk-Request-Channel: <GUID>` is required for `/raw` and for indexer acknowledgement [S1].
- The exporter sends `Content-Type: application/json` and gzip (`disable_compression` defaults to false). So HEC accepts `Content-Encoding: gzip` [S5]. The official help page does not state gzip (UNVERIFIED there).
- Query-string token auth exists but can be disabled. Code 16 is "Query string authorization is not enabled" [S2].

### Event envelope [S1]

| Key | Rule |
|---|---|
| `time` | "UNIX time format, in the format `<sec>.<ms>`", for example `1433188255.500`. The exporter sends float64 epoch seconds [S5] |
| `host`, `source`, `sourcetype`, `index` | Strings. A token with a default index makes `index` optional [S5] |
| `event` | "a string, a number, another JSON object" |
| `fields` | "a flat (not nested) list of explicit custom fields". These are indexed fields. Nested objects are not allowed, and they must go to `/collector/event` |

- Batching: "event objects stacked one after the other". This is concatenated JSON objects with no array and no commas. Newlines between objects are allowed [S1].
- "By batching the events, you're specifying that any event metadata within the request is to apply to all of the events contained in the request" (search snippet of [S1]). The meaning of this sentence is unclear, so set metadata on every object.
- The fetch summary said JSON arrays also work. UNVERIFIED, so do not use them.

```
POST /services/collector/event HTTP/1.1
Host: http-inputs-acme.splunkcloud.com:443
Authorization: Splunk 12345678-1234-1234-1234-1234567890AB
Content-Type: application/json
Content-Encoding: gzip

{"time":1789547400.123,"host":"web-1","source":"wlog","sourcetype":"_json","index":"main","fields":{"service":"checkout","env":"prod","level":"error"},"event":{"timestamp":"2026-09-16T08:30:00.123456789Z","level":"error","summary":"order failed","operation":"POST /orders/:id","outcome":"error","duration_ms":42,"service":{"name":"checkout","version":"1.4.2","env":"prod"},"trace":{"trace_id":"4bf92f3577b34da6a3ce929d0e0e4736","span_id":"00f067aa0ba902b7","request_id":"r-1"},"http":{"method":"POST","route":"/orders/:id","status":500},"error":{"code":"DB_TIMEOUT","message":"db timeout"}}}
{"time":1789547400.456,"host":"web-1","source":"wlog","sourcetype":"_json","event":{"level":"info","operation":"GET /health","outcome":"success","duration_ms":1}}
```

### Limits

- The Splunk exporter defaults: `max_content_length_logs` = 2,097,152 bytes (2 MiB) per request, `max_event_size` = 5,242,880 bytes (5 MiB) uncompressed per event. "Maximum allowed value is 838860800 (~ 800 MB)" [S5].
- The server-side `max_content_length` setting (limits.conf `[http_input]`, default 838,860,800 bytes) is UNVERIFIED in Splunk docs.
- `fields` values must be flat. Code 15 is "Error in handling indexed fields" [S2].

### Response codes [S2] (body is `{"text":"<msg>","code":<n>}`)

| Code | HTTP | Text |
|---|---|---|
| 0 | 200 | Success |
| 1 | 403 | Token disabled |
| 2 | 401 | Token is required |
| 3 | 401 | Invalid authorization |
| 4 | 403 | Invalid token |
| 5 | 400 | No data |
| 6 | 400 | Invalid data format |
| 7 | 400 | Incorrect index |
| 8 | 500 | Internal server error |
| 9 | 503 | Server is busy |
| 10 | 400 | Data channel is missing |
| 11 | 400 | Invalid data channel |
| 12 | 400 | Event field is required |
| 13 | 400 | Event field cannot be blank |
| 14 | 400 | ACK is disabled |
| 15 | 400 | Error in handling indexed fields |
| 16 | 400 | Query string authorization is not enabled |
| 17 | 200 | HEC is healthy |
| 18 | 503 | HEC is unhealthy, queues are full |
| 19 | 503 | HEC is unhealthy, ack service unavailable |
| 20 | 503 | HEC is unhealthy, queues are full, ack service unavailable |
| 21 | 400 | Invalid token |
| 22 | 400 | Token disabled |
| 23 | 503 | Server is shutting down |
| 24 | 200 | HEC queue is approaching its capacity limit |
| 25 | 200 | HEC ACK is approaching its capacity limit |
| 26 | 429 | HEC queue is at capacity and cannot process any more requests |
| 27 | 429 | HEC ACK channel is at capacity and cannot process any more requests |

- The body shape `{"text","code"}` comes from the code/text pairs in [S2] and community examples. The exact JSON key names are UNVERIFIED in an official page.
- Per-item errors: a malformed event in a batch returns 400 code 6 with an `"invalid-event-number": <index>` key. That key is seen in Splunk Community posts and is UNVERIFIED in official docs. **Events before the bad one can already be indexed** (UNVERIFIED). A retry of the whole batch can duplicate them, so on code 6 drop the batch or split it and resend.
- Retry policy, as in the Splunk exporter [S5]:
  - 429 and 503: retry. Honour the `Retry-After` seconds value if the response has one.
  - 400, 401, 403: permanent. Do not retry.
  - Other 5xx: retry with backoff.
  - Codes 24 and 25 are 200 warnings. Slow down.
- Indexer acknowledgement (`useACK` on the token):
  - The request must carry a channel. The response adds `"ackId": n`.
  - Poll `POST /services/collector/ack?channel=<GUID>` with `{"acks":[n,...]}` and get `{"acks":{"n":true}}`.
  - Shape UNVERIFIED (dev.splunk.com was not readable). Treat ack support as out of scope for a first drain.

### wlog → HEC mapping

| wlog | HEC | Note |
|---|---|---|
| `timestamp` | envelope `time` (epoch seconds float, ms precision) and keep it inside `event` | |
| whole event | `event` (JSON object) | Use sourcetype `_json` so Splunk search-time extracts nested keys (UNVERIFIED that `_json` is the right default) |
| `service.name` | `source` or `fields.service` | Keep `host` for the host |
| `level`, `service.env`, `outcome` | `fields.level`, `fields.env`, `fields.outcome` | Indexed, flat, low cardinality only |
| `trace.trace_id` | inside `event`. Optional `fields.trace_id` | Indexed fields cost licence and index size |
| index | config | Omit to use the token default |

## 4. VictoriaLogs JSON line ingestion

Sources:

- [V1] https://docs.victoriametrics.com/victorialogs/data-ingestion/
- [V2] https://docs.victoriametrics.com/victorialogs/keyconcepts/
- [V3] `VictoriaMetrics/VictoriaLogs@master` source: `app/vlinsert/jsonline/jsonline.go`, `app/vlinsert/insertutil/common_params.go`, `insertutil/flags.go`, `insertutil/line_reader.go`, `vendor/github.com/VictoriaMetrics/VictoriaMetrics/lib/protoparser/protoparserutil/compress_reader.go`, `vendor/.../lib/httpserver/httpserver.go`
- Latest release: v1.52.0, 2026-07-16 (GitHub API)

### Request

- `POST http://<host>:9428/insert/jsonline`. Port 9428 is the default. Any method other than POST gets 405 [V1][V3].
- `Content-Type: application/stream+json` in the docs example [V1]. The handler does not check it [V3].
- Body: one JSON object per line.

```
POST /insert/jsonline?_msg_field=summary,operation&_time_field=timestamp&_stream_fields=service.name,service.env HTTP/1.1
Host: victorialogs:9428
Content-Type: application/stream+json
Content-Encoding: zstd
AccountID: 0
ProjectID: 0

{"timestamp":"2026-09-16T08:30:00.123456789Z","level":"error","summary":"order failed","operation":"POST /orders/:id","outcome":"error","duration_ms":42,"service":{"name":"checkout","env":"prod"},"trace":{"trace_id":"4bf92f35..."},"http":{"status":500}}
```

### Parameters (query arg or header) [V1][V3]

| Query arg | Header | Meaning |
|---|---|---|
| `_msg_field` | `VL-Msg-Field` | Comma list. The first field present becomes `_msg` |
| `_time_field` | `VL-Time-Field` | Comma list. Defaults to `_time` |
| `_stream_fields` | `VL-Stream-Fields` | Fields that form the log stream. Low cardinality only. "Never associate high-cardinality fields (like trace_id, user_id, or ip) with log streams" [V2] |
| `ignore_fields` | `VL-Ignore-Fields` | Supports a `prefix*` pattern |
| `decolorize_fields` | `VL-Decolorize-Fields` | Strips ANSI colour codes |
| `extra_fields` | `VL-Extra-Fields` | `name=value` pairs to add |
| `preserve_json_keys` | `VL-Preserve-JSON-Keys` | JSON keys to keep unflattened |
| `debug` | `VL-Debug` | `1` logs the rows and does not store them |
| `AccountID`, `ProjectID` | `AccountID`, `ProjectID` headers | Multitenancy. Default tenant is `0:0` |

### Data model [V2]

- "Nested dictionaries are flattened by concatenating dictionary keys with the `.` character." So wlog `{"http":{"status":500}}` becomes the field `http.status`.
- Arrays, numbers and booleans are stored as strings. `["foo","bar"]` becomes `"[\"foo\", \"bar\"]"`.
- `_time`: RFC3339 or ISO8601, or Unix timestamps in seconds, ms, µs or ns. If it is missing or invalid, ingestion time is used.
- `_msg`: if it is missing, the value of `-defaultMsgValue` is used (default text "missing _msg field; see …") [V3].

### Compression [V3]

`Content-Encoding` values: `gzip`, `zstd`, `snappy`, `deflate`, or `""`/`none`/`identity`.

### Limits [V3]

- `-insert.maxLineSizeBytes`, default **256 KiB**. A longer line "is skipped". The code logs a warning and increments `vl_too_long_lines_skipped_total`. The flag help text also mentions a hard 2 MB ceiling ("Regardless of this flag, entries above the 2 MB …"). The rest of that text was cut off in the fetch, so treat it as UNVERIFIED.
- `-insert.maxFieldsPerLine`, default **1000**. Extra rows are dropped and counted in `vl_rows_dropped_total{reason="too_many_fields"}`.

### Responses and per-line errors [V3]

- Success: the handler writes no body. That gives net/http's default 200, with `Content-Type: application/json`. UNVERIFIED on a live server.
- Request-level errors go through `httpserver.Errorf`. The status defaults to **400** unless the error carries its own status code. Examples: bad params, and storage that `CanWriteData()` refuses (for example read-only mode or low disk). The exact status for that case is UNVERIFIED.
- **Per-line errors are not reported to the client.** Bad JSON and bad timestamps are logged as `jsonline: cannot read line #N` and counted in the errors metric. The request still succeeds, **unless every line failed**: "Return an error if no logs were processed and there were errors".
- A drain cannot learn which lines failed, so it must emit valid JSON and a valid `_time`.
- Retry: 5xx and network errors. Do not retry 400.

### Elasticsearch compatibility

`POST /insert/elasticsearch/_bulk` "Accepts logs in Elasticsearch/OpenSearch bulk API format" [V1]. The same `_msg_field` and other params apply. This lets an ES drain target VictoriaLogs.

### wlog → VictoriaLogs mapping

| wlog | VictoriaLogs | Rule |
|---|---|---|
| `timestamp` | `_time` through `_time_field=timestamp` | RFC3339Nano is accepted |
| `summary` (else `operation`) | `_msg` through `_msg_field=summary,operation` | Comma fallback list |
| `service.name`, `service.env` | `_stream_fields=service.name,service.env` | Dotted names after flattening |
| `level`, `outcome`, `operation`, `duration_ms` | Plain fields | Numbers become strings. Queries still parse them |
| `trace.*`, `http.*`, `error.*` | `trace.trace_id`, `http.status`, `error.message`, … | Automatic dot flattening. Never use them as stream fields |

## 5. New Relic Log API

Sources: [NR-API] https://docs.newrelic.com/docs/logs/log-api/introduction-log-api/ · [NR-EVT] https://docs.newrelic.com/docs/logs/log-api/log-event-data/ · [NR-TS] https://docs.newrelic.com/docs/logs/troubleshooting/no-log-data-appears-ui/ · [NR-MET] https://docs.newrelic.com/docs/data-apis/ingest-apis/metric-api/report-metrics-metric-api/ · [NR-LIC] https://docs.newrelic.com/docs/logs/logs-context/get-started-logs-context/ · [NR-GO] https://docs.newrelic.com/docs/logs/logs-context/configure-logs-context-go/ · [NR-OTEL] https://docs.newrelic.com/docs/opentelemetry/best-practices/opentelemetry-best-practices-logs/ · [NR-KEYS] https://docs.newrelic.com/docs/apis/intro-apis/new-relic-api-keys/ · [NR-SPEC] https://github.com/newrelic/newrelic-exporter-specs/blob/master/logging/README.md (a New Relic GitHub spec from 2019, not the docs site)

**Endpoints (POST)** [NR-API]
- US: `https://log-api.newrelic.com/log/v1`
- EU: `https://log-api.eu.newrelic.com/log/v1`
- Japan: `https://log-api.jp.nr-data.net/log/v1`
- FedRAMP: `https://gov-log-api.newrelic.com/log/v1`

**Headers and auth** [NR-API]
- `Content-Type` is required. Allowed values: `application/json`, `json`, `application/gzip`, `gzip`.
- For gzip, send `Content-Type: application/json` plus `Content-Encoding: gzip`.
- Auth uses the license key, either as the `Api-Key: <license key>` header or as the `?Api-Key=<license key>` query parameter ("headerless", for cloud sources that can't set headers).
- The setup text says "Api-Key or License-Key", but the header table lists only `Api-Key`. The current page does not mention `X-License-Key` or `X-Insert-Key`.
- The older Insights insert key is "not recommended"; use the license key [NR-KEYS].

**Body formats** [NR-API]
- Simplified: one JSON object, `{"timestamp":…, "message":…, "logtype":…, other fields…}`.
- Detailed: a JSON **array** of `{"common":{"timestamp"?, "attributes":{}}, "logs":[{"timestamp"?, "message" (required), "attributes":{}}]}`.
- The fields `log`, `LOG`, `MESSAGE` and `msg` are renamed to `message` on ingest.
- If `message` holds a JSON string, it is parsed and its fields are added to the event. Keep `message` as plain text.
- Nested objects are stored under flattened dotted names (`user.id`).

**Timestamp**
- [NR-API] accepts an integer of ms or seconds since epoch, or an ISO8601 string. A missing or invalid value means ingest time is used.
- [NR-EVT] says "integer representing milliseconds since Unix epoch. Seconds since epoch is also supported" and does not mention ISO strings. Integer ms is the safe choice.
- Payloads with timestamps older than 48 h may be dropped [NR-API][NR-EVT].

**Limits**
- Payload: "1MB(10^6 bytes) maximum per POST. We highly recommend using compression." UTF-8 is required [NR-API].
- At most 255 attributes per event. Attribute names up to 255 chars [NR-API].
- Values: the first 4,094 chars are stored and queryable. Longer values go to a blob of up to 128 KB, and anything beyond that is truncated [NR-EVT].
- Rate limits: 300,000 requests/min, and 10 GB/min of **uncompressed** JSON [NR-API].
- Reserved names [NR-API][NR-EVT]:
  - `accountId` and `eventType` are dropped.
  - `appId` must be an integer.
  - `entity.guid`, `entity.name` and `entity.type` are used internally, and custom values "may cause undefined behavior".
  - `instrumentation.name`, `instrumentation.provider` and `instrumentation.version` are reserved.

**Responses and retry**
- 202 means success [NR-TS].
- 403 means an invalid license key [NR-TS].
- 429 is returned "for the remainder of the minute", with `Retry-After` in seconds [NR-API].
- There are no per-item errors. Async failures appear as `NrIntegrationError` events [NR-TS].
- The Log API page does not document 400, 408, 413, 415 or 5xx. The sibling Metric API (same 1 MB limit) documents them [NR-MET]:
  - 400 invalid structure, 403 auth failure, 404 wrong path, 405 wrong method
  - 408 timeout, 411 no Content-Length, 413 over 1 MB, 414 URI too long
  - 415 bad Content-Type or Content-Encoding, 429 rate limit, 431 headers too long
  - 5xx "please retry"
- Suggested policy:
  - Honor `Retry-After` on 429.
  - Retry 408 and 5xx with backoff.
  - Split the batch on 413.
  - Drop on 400, 403 and 415.

**Recognised attributes**
- `message` is the default search field; `logtype` drives parsing rules [NR-API].
- `trace.id` and `span.id`: "Logs are correlated with a trace if trace.id and span.id attributes can be resolved" [NR-OTEL].
- APM logs-in-context adds `span.id`, `trace.id`, `hostname`, `entity.guid` and `entity.name` [NR-LIC][NR-GO].
- `level` appears in the logs-in-context example input [NR-LIC]. `log.level` is a common optional field in [NR-SPEC].
- [NR-SPEC] also lists `logger.name`, `error.message`, `error.class` and `error.stack`. It requires one JSON object per line, under 4096 bytes, with `timestamp` as int64 ms.
- For OTLP logs, the `service.name` resource attribute correlates logs with the service entity [NR-OTEL].

**Example**
```
POST /log/v1 HTTP/1.1
Host: log-api.newrelic.com
Api-Key: <LICENSE_KEY>
Content-Type: application/json
Content-Encoding: gzip

[{"common":{"attributes":{"service.name":"checkout","service.version":"1.4.2","service.env":"prod","hostname":"web-1"}},
  "logs":[{"timestamp":1789547400123,"message":"order failed",
    "attributes":{"level":"error","trace.id":"4bf92f3577b34da6a3ce929d0e0e4736","span.id":"00f067aa0ba902b7",
      "operation":"POST /orders","outcome":"error","duration_ms":42,"error.message":"db timeout","http.status":500}}]}]
```

| wlog | New Relic | Note |
|---|---|---|
| timestamp | `timestamp` (int epoch ms) | ISO8601 strings are also accepted per [NR-API] |
| level | `level` (and optionally `log.level`) | Neither is formally reserved (see UNVERIFIED) |
| summary | `message` | JSON strings get parsed, so keep it plain |
| operation, outcome, duration_ms | Same names (custom) | No reserved equivalent |
| error.message / error.stack | `error.message` / `error.stack` | From [NR-SPEC]. Long stacks go to a blob after 4,094 chars |
| error.kind | `error.class` | From [NR-SPEC] |
| error.code / status / cause | `error.code` / `error.status` / `error.cause` (custom) | |
| service.name / version / env | `service.name` / `service.version` / custom env, in `common.attributes` | **Do not** set `entity.name` [NR-API] |
| trace.trace_id / span_id | `trace.id` / `span.id` | [NR-OTEL][NR-LIC] |
| trace.request_id | Custom `request_id` | |
| http.* | Send as a nested `http` object; it is auto-flattened | No New Relic standard names |

## 6. Syslog (RFC 5424 / 5425 / 6587 / 5426, Go log/syslog)

Sources: [5424] https://www.rfc-editor.org/rfc/rfc5424 · [5425] https://www.rfc-editor.org/rfc/rfc5425 · [9662] https://www.rfc-editor.org/rfc/rfc9662 (updates 5425) · [6587] https://www.rfc-editor.org/rfc/rfc6587 · [5426] https://www.rfc-editor.org/rfc/rfc5426 · [GO] https://pkg.go.dev/log/syslog · [GOSRC] https://go.dev/src/log/syslog/syslog.go

**ABNF** [5424 §6], verbatim:
```
SYSLOG-MSG = HEADER SP STRUCTURED-DATA [SP MSG]
HEADER     = PRI VERSION SP TIMESTAMP SP HOSTNAME SP APP-NAME SP PROCID SP MSGID
PRI = "<" PRIVAL ">"      PRIVAL = 1*3DIGIT ; range 0 .. 191
VERSION = NONZERO-DIGIT 0*2DIGIT
HOSTNAME = NILVALUE / 1*255PRINTUSASCII   APP-NAME = NILVALUE / 1*48PRINTUSASCII
PROCID = NILVALUE / 1*128PRINTUSASCII     MSGID = NILVALUE / 1*32PRINTUSASCII
TIMESTAMP = NILVALUE / FULL-DATE "T" FULL-TIME
PARTIAL-TIME = TIME-HOUR ":" TIME-MINUTE ":" TIME-SECOND [TIME-SECFRAC]
TIME-SECFRAC = "." 1*6DIGIT      TIME-OFFSET = "Z" / TIME-NUMOFFSET
STRUCTURED-DATA = NILVALUE / 1*SD-ELEMENT
SD-ELEMENT = "[" SD-ID *(SP SD-PARAM) "]"
SD-PARAM = PARAM-NAME "=" %d34 PARAM-VALUE %d34
SD-ID = SD-NAME   PARAM-NAME = SD-NAME
PARAM-VALUE = UTF-8-STRING ; characters '"', '\' and ']' MUST be escaped.
SD-NAME = 1*32PRINTUSASCII ; except '=', SP, ']', %d34 (")
MSG = MSG-ANY / MSG-UTF8   MSG-UTF8 = BOM UTF-8-STRING   BOM = %xEF.BB.BF
PRINTUSASCII = %d33-126   NILVALUE = "-"
```

**PRI** [5424 §6.2.1]
- PRI = Facility×8 + Severity. It is 3 to 5 chars, with no leading zeros except `<0>`.
- Facility is 0 to 23. user = 1; local0 to local7 = 16 to 23.
- Severity: 0 Emergency, 1 Alert, 2 Critical, 3 Error, 4 Warning, 5 Notice, 6 Informational, 7 Debug.
- **wlog mapping:** debug→7, info→6, warn→4, error→3. For example, local0+error = `<131>` and user+info = `<14>`.

**Header fields**
- VERSION is `1` [5424 §6.2.2]. The HEADER must be 7-bit ASCII [5424 §6.2].
- TIMESTAMP [5424 §6.2.3]:
  - `T` and `Z` must be upper case, and `T` is required.
  - No leap seconds.
  - At most 6 fractional digits. Example 5, with nanoseconds, is invalid.
  - Use NILVALUE if there is no clock.
- HOSTNAME preference order: FQDN, then static IP, then hostname, then dynamic IP, then `-` [5424 §6.2.4].

**STRUCTURED-DATA** [5424 §6.3]
- No SP between elements, and no SP after `[`. The same SD-ID must not appear twice.
- SD is ASCII, except PARAM-VALUE, which is UTF-8.
- In PARAM-VALUE, escape `"`→`\"`, `\`→`\\` and `]`→`\]`. Any other backslash stays literal. A PARAM may repeat.
- SD-IDs [5424 §6.3.2]:
  - A name without `@` is valid only if IANA-registered.
  - A custom SD-ID must be `name@<Private Enterprise Number>`, for example `ourSDID@32473`. 32473 is reserved for documentation, so wlog needs its own PEN.
- Reserved SD-IDs [5424 §7]:
  - `timeQuality`: `tzKnown` 0/1, `isSynced` 0/1, `syncAccuracy` in µs (not allowed when isSynced=0).
  - `origin`: `ip` (repeatable), `enterpriseId`, `software` ≤48, `swVersion` ≤32.
  - `meta`: `sequenceId` 1..2147483647, `sysUpTime`, `language`.

**MSG, length and transport**
- A UTF-8 MSG **must** start with the BOM EF BB BF. Avoid octets below 32 [5424 §6.4].
- Receivers MUST accept at least 480 octets and SHOULD accept 2048. Oversize messages are truncated at the end or discarded [5424 §6.1].
- TLS (5425) MUST be supported and UDP (5426) SHOULD be supported. TLS is RECOMMENDED [5424 §5.1].

**Example**
```
<131>1 2026-09-16T08:30:00.123456Z web-1.example.com checkout 4242 - [wlog@<PEN> trace_id="4bf92f3577b34da6a3ce929d0e0e4736" outcome="error" duration_ms="42" http.status="500"] BOMorder failed
```
MSGID cannot hold "POST /orders" because of the space. Sanitize it or use `-`.

**RFC 5425 (TLS)**
- The default port is TCP **6514**, and the sender is the TLS client [5425 §4.1].
- `APPLICATION-DATA = 1*SYSLOG-FRAME` and `SYSLOG-FRAME = MSG-LEN SP SYSLOG-MSG`, where `MSG-LEN = NONZERO-DIGIT *DIGIT` is the octet count [5425 §4.3].
- Receivers MUST handle 2048 octets and SHOULD handle 8192 [5425 §4.3.1].
- The sender MUST send `close_notify` before closing. TLS 1.2 is mandatory [5425 §4.2].
- RFC 9662 (Oct 2024) updates this [9662 §4]:
  - Implementations SHOULD offer `TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256` and MAY offer `TLS_RSA_WITH_AES_128_CBC_SHA`.
  - TLS 1.2 stays mandatory.
  - Implementations SHOULD support TLS 1.3 and, if they do, MUST prefer it.

**RFC 6587 (TCP, category Historic)**
- Octet counting is `SYSLOG-FRAME = MSG-LEN SP SYSLOG-MSG`. A frame that starts with a digit uses it, and it is "reliable" [6587 §3.4.1].
- Non-transparent framing is `SYSLOG-FRAME = SYSLOG-MSG TRAILER`, with `TRAILER = LF / APP-DEFINED` (1 to 2 octets; NUL and CRLF have been seen). A frame that starts with `<` uses it [6587 §3.4.2].
- The problem: the trailer is not escaped, so a message containing LF "may be misinterpreted as multiple messages".
- TCP is not recommended for new work except to interoperate. No port is assigned; 514/tcp is common but belongs to shell [6587 §4].

**RFC 5426 (UDP)**
- Port **514** [§3.3]. Exactly one message per datagram. It may be truncated, and no extra data is allowed [§3.1].
- The maximum is 65535 minus headers.
- IPv4 receivers MUST accept 480 octets and IPv6 receivers 1180. All SHOULD accept 2048.
- When the MTU is unknown, restrict messages to 480 (IPv4) or 1180 (IPv6) [§3.2].
- Senders MUST NOT disable UDP checksums [§3.6]. There is no ack and no retransmission.

**Go `log/syslog`**
- "The syslog package is frozen and is not accepting new features" [GO].
- `Dial(network, raddr, priority, tag)` uses `net.Dial`. There is no TLS option.
- On a write failure it reconnects and writes again, which can block [GO][GOSRC].
- It is not implemented on Windows or Plan 9. On macOS 12+ the local daemon socket no longer works [GO].
- Wire format [GOSRC]:
  - Network: `"<%d>%s %s %s[%d]: %s%s"`, meaning PRI, `time.RFC3339` (seconds only), hostname, `tag[pid]: msg`.
  - Local: `time.Stamp`, with no hostname.
  - It always appends `\n`.
- The result is BSD/RFC 3164 style with LF (non-transparent) framing. There is no VERSION, SD or MSGID, so it is **not RFC 5424**. A 5424 sink needs its own formatter, which stdlib `net` and `crypto/tls` can support.

| wlog | Syslog field | Rule |
|---|---|---|
| timestamp | TIMESTAMP | UTC, upper-case `T`/`Z`, truncate to ≤6 fractional digits |
| level | PRI severity | 7/6/4/3. Facility is configurable (user=1 or local0–7=16–23) |
| service.name | APP-NAME | ≤48 PRINTUSASCII, else `-` |
| (pid) | PROCID | ≤128, or `-` |
| operation | MSGID | ≤32 PRINTUSASCII with no spaces. Sanitize or use `-` |
| (host) | HOSTNAME | ≤255, FQDN preferred |
| summary (or full JSON) | MSG | BOM + UTF-8 |
| service.version | `[origin software="wlog" swVersion="…"]` | swVersion ≤32 |
| trace.*, outcome, duration_ms, http.*, error.* | SD-PARAMs in `[wlog@PEN …]` | PARAM-NAME ≤32 chars, no `= ] "` or SP (dots are OK). Escape `" \ ]` |

## 7. AWS CloudWatch Logs PutLogEvents and EMF

Sources: [PUT] https://docs.aws.amazon.com/AmazonCloudWatchLogs/latest/APIReference/API_PutLogEvents.html · [INPUT] https://docs.aws.amazon.com/AmazonCloudWatchLogs/latest/APIReference/API_InputLogEvent.html · [REJ] https://docs.aws.amazon.com/AmazonCloudWatchLogs/latest/APIReference/API_RejectedLogEventsInfo.html · [REJENT] https://docs.aws.amazon.com/AmazonCloudWatchLogs/latest/APIReference/API_RejectedEntityInfo.html · [ENTITY] https://docs.aws.amazon.com/AmazonCloudWatchLogs/latest/APIReference/API_Entity.html · [CERR] https://docs.aws.amazon.com/AmazonCloudWatchLogs/latest/APIReference/CommonErrors.html · [QUOTA] https://docs.aws.amazon.com/AmazonCloudWatch/latest/logs/cloudwatch_limits_cwl.html · [EP] https://docs.aws.amazon.com/general/latest/gr/cwl_region.html · [SIGV4] https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_sigv.html · [EMF] https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/CloudWatch_Embedded_Metric_Format_Specification.html · [EMFMAIN] https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/CloudWatch_Embedded_Metric_Format.html · [EMFPUT] https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/CloudWatch_Embedded_Metric_Format_Generation_PutLogEvents.html · [EMFAGENT] https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/CloudWatch_Embedded_Metric_Format_Generation_CloudWatch_Agent.html · [UNITS] https://docs.aws.amazon.com/AmazonCloudWatch/latest/APIReference/API_MetricDatum.html · [LAMBDA] https://docs.aws.amazon.com/prescriptive-guidance/latest/implementing-logging-monitoring-cloudwatch/lambda-logging-metrics.html

**Request**
- Endpoint: `POST https://logs.<region>.amazonaws.com/`. Dual-stack is `logs.<region>.api.aws`; FIPS is `logs-fips.<region>.amazonaws.com` [EP].
- Headers [PUT sample]:
  - `X-Amz-Target: Logs_20140328.PutLogEvents`
  - `Content-Type: application/x-amz-json-1.1`
  - `X-Amz-Date`
  - `Authorization: AWS4-HMAC-SHA256 Credential=…, SignedHeaders=…, Signature=…`
- **Auth is SigV4.** AWS says "Unless you have a good reason not to, we recommend that you always use an SDK" [SIGV4]. For wlog that means either a separate module with aws-sdk-go-v2 (a third-party import), or hand-written SigV4 with crypto/hmac and sha256. Loading credentials (env, IMDS, SSO) is the hard part of doing it by hand.
- Gzip request bodies are not documented (see UNVERIFIED).

**Body** [PUT][INPUT]
- `{"logGroupName","logStreamName","logEvents":[{"timestamp":<ms>,"message":"<≥1 char>"}],"entity"?,"sequenceToken"?}`
- `logGroupName` is 1 to 512 chars matching `[\.\-_/#A-Za-z0-9]+`. `logStreamName` is 1 to 512 chars matching `[^:*]*`.
- `timestamp` is a long of ms since epoch, ≥0.

**Batch rules** [PUT]
- At most 1,048,576 bytes, counted as the UTF-8 bytes of all messages plus **26 bytes per event**.
- 1 to 10,000 events per batch. Each event at most 1 MB (the quota page says "1,024 Kilobytes" [QUOTA]).
- Rejected events do not fail the call; the rest of the batch is still processed. Events are rejected when they are:
  - more than 2 h in the future
  - older than 14 days
  - older than the retention period
- **Chronological order is still required** today: "A batch of log events in a single request must be in a chronological order. Otherwise, the operation fails."
- The valid events in a batch must span 24 h or less. "Otherwise, the operation fails."
- `sequenceToken` is **ignored**. Parallel PutLogEvents calls on the same stream are allowed.

**Entity** [ENTITY]
- `keyAttributes`: 2 to 4 entries. Keys are Type, ResourceType, Identifier, Name, Environment. Key ≤32 chars, value ≤512.
- `attributes`: 0 to 10 entries. Key ≤256 chars, value ≤512.

**Throttling** [PUT][QUOTA]
- The old limit of 5 req/s per log stream "has been removed".
- The quota is now per account per region: 5,000 TPS, and it is adjustable.
- The 1 MB batch size quota is not adjustable.

**Response (HTTP 200)** [PUT][REJ][REJENT]
```
{"nextSequenceToken":"…(deprecated)",
 "rejectedLogEventsInfo":{"tooNewLogEventStartIndex":n,"tooOldLogEventEndIndex":n,"expiredLogEventEndIndex":n},
 "rejectedEntityInfo":{"errorType":"InvalidEntity|InvalidTypeValue|InvalidKeyAttributes|InvalidAttributes|EntitySizeTooLarge|UnsupportedLogGroupType|MissingRequiredFields"}}
```
- `tooNewLogEventStartIndex` is **inclusive**. `tooOldLogEventEndIndex` is **exclusive**.
- `expiredLogEventEndIndex` is described only as "The expired log events" (see UNVERIFIED).
- If the entity is rejected, "the events may still be accepted".
- These fields are the only per-item error reporting. Because the batch is sorted, rejected events form contiguous ranges at the start and end. Do not resend them.

**Errors** [PUT][CERR]
- From the PutLogEvents page:
  - `InvalidParameterException` 400
  - `ResourceNotFoundException` 400 (the group or stream is missing)
  - `ServiceUnavailableException` 500
  - `UnrecognizedClientException` 400 (the common errors page lists it as 403)
- `DataAlreadyAcceptedException` and `InvalidSequenceTokenException` are no longer returned.
- From the common errors page:
  - `ThrottlingException` **400** ("AWS SDKs automatically retry")
  - `ServiceUnavailable` 503 ("Try again later")
  - `InternalFailure` 500
  - `RequestTimeoutException` 408 ("Try again")
  - `RequestEntityTooLargeException` 413
  - `ExpiredTokenException`, `AccessDeniedException` and `IncompleteSignature`, all 403
  - `MalformedHttpRequestException` 400 (the body can't be decompressed)
  - `ValidationError` 400
- Retry policy:
  - Retry throttling (identified by the error type in the 400 JSON body), 500, 503 and 408 with backoff.
  - On an expired token, refresh credentials and retry.
  - Do not retry InvalidParameter or AccessDenied.
  - On ResourceNotFound, create the stream first.

**Example**
```
POST / HTTP/1.1
Host: logs.us-east-1.amazonaws.com
X-Amz-Target: Logs_20140328.PutLogEvents
Content-Type: application/x-amz-json-1.1
X-Amz-Date: 20260916T083000Z
Authorization: AWS4-HMAC-SHA256 Credential=<AKID>/20260916/us-east-1/logs/aws4_request, SignedHeaders=content-type;host;x-amz-date;x-amz-target, Signature=<sig>
x-amzn-logs-format: json/emf

{"logGroupName":"/app/checkout","logStreamName":"web-1","logEvents":[{"timestamp":1789547400123,"message":"{\"level\":\"error\",\"operation\":\"POST /orders\",\"duration_ms\":42}"}]}
```

**EMF specification** [EMF]
- The LogEvent message MUST be a valid JSON object with nothing before or after it.
- The root `"_aws"` is required. It holds `CloudWatchMetrics` (an array of MetricDirective) and `Timestamp` (ms since epoch). The JSON schema marks both as required.
- Objects defined by the spec MUST NOT have extra members. Unknown members are ignored, and names are case-sensitive.
- **MetricDirective:**
  - `Namespace`: schema allows 1 to 1024 chars.
  - `Dimensions`: an array of DimensionSet, at least 1.
  - `Metrics`: at most **100** MetricDefinitions.
- **DimensionSet:**
  - At most **30** keys; it may be empty.
  - Each key names a root member whose value must be a **string of ≤1024 chars**. The schema caps the reference at 250 chars.
  - Each set creates a new metric. Beware high-cardinality dimensions such as requestId.
- **MetricDefinition:**
  - `Name` is required (schema: 1 to 1024 chars).
  - `Unit` is optional, default None.
  - `StorageResolution` is optional: 1 or 60, default 60.
- **Targets:**
  - They must be root-level and cannot be nested. A reference `"A.a"` matches the literal key `"A.a"`, not `{"A":{"a":…}}`, so dotted wlog keys must be flattened.
  - A metric target is a number or an array of up to **100** numbers. A dimension target is a string.
- Size limits are the same as for log events: at most 1 MB.
- Delivery is at least once, so duplicate metric values are possible.
- Only `logs:PutLogEvents` permission is needed, not `cloudwatch:PutMetricData` [EMFMAIN].
- **Units** [UNITS][EMF schema]: Seconds, Microseconds, Milliseconds, Bytes, Kilobytes, Megabytes, Gigabytes, Terabytes, Bits, Kilobits, Megabits, Gigabits, Terabits, Percent, Count, Bytes/Second, Kilobytes/Second, Megabytes/Second, Gigabytes/Second, Terabytes/Second, Bits/Second, Kilobits/Second, Megabits/Second, Gigabits/Second, Terabits/Second, Count/Second, None.
- **Entity in EMF:** without a request entity, root keys `Service` + `Environment` create a Service entity. Platform keys such as `Lambda.Function`, `EC2.InstanceId` and `K8s.Cluster` set PlatformType [EMF].

**EMF ingestion**
- **PutLogEvents:** the header `x-amzn-logs-format: json/emf` is optional ("it's not required") [EMFPUT].
- **CloudWatch agent** [EMFAGENT]:
  - Requires version ≥1.230621.0.
  - Config: `{"logs":{"metrics_collected":{"emf":{}}}}`.
  - Listens on TCP or UDP, default `tcp:25888`. Libraries use `AWS_EMF_AGENT_ENDPOINT=tcp://127.0.0.1:25888`.
  - Each event must include `LogGroupName` (the example puts it inside `_aws`) and must be one line with no `\n`.
- **Lambda:**
  - "You do not need to install the CloudWatch agent" [EMFAGENT].
  - Lambda streams stdout and stderr to `/aws/lambda/<fn>`, and "the integrated Lambda CloudWatch logging facility is configured to process and extract appropriately formatted embedded metric format statements" [LAMBDA].
  - Metrics are lost if the function times out before they flush [EMFAGENT].

EMF stdout line:
```
{"_aws":{"Timestamp":1789547400123,"CloudWatchMetrics":[{"Namespace":"wlog","Dimensions":[["service.name","operation"]],"Metrics":[{"Name":"duration_ms","Unit":"Milliseconds","StorageResolution":60}]}]},"service.name":"checkout","operation":"POST /orders","duration_ms":42,"level":"error","outcome":"error","trace.request_id":"r-1"}
```

| wlog | PutLogEvents | EMF |
|---|---|---|
| timestamp | `logEvents[].timestamp` (epoch ms; sort the batch by it) | `_aws.Timestamp` (epoch ms) |
| whole event | `logEvents[].message` (JSON string, 1 char to 1 MB) | Root members, flattened to literal dotted keys |
| duration_ms, http.duration_ms | Inside message | Metric target, Unit `Milliseconds` |
| http.bytes_in / bytes_out | Inside message | Metric target, Unit `Bytes` |
| service.name, service.env, operation, http.route, http.method, outcome | Inside message | Dimension targets (string values, low cardinality only) |
| http.status | Inside message | Dimension only if stringified |
| service.name / service.env | `entity.keyAttributes {Type:"Service",Name,Environment}` | Root `Service` / `Environment` for the entity |
| trace.*, error.*, summary | Inside message | Plain root members, never dimensions |
| log group / stream | Configuration | Agent: `_aws.LogGroupName` |

## 8. Google Cloud Logging structured logging (stdout)

Sources: [SL] https://docs.cloud.google.com/logging/docs/structured-logging (cloud.google.com now redirects to docs.cloud.google.com) · [AG] https://docs.cloud.google.com/logging/docs/agent/logging/configuration (special fields and timestamp processing) · [OPS] https://docs.cloud.google.com/logging/docs/agent/ops-agent/configuration · [LE] https://docs.cloud.google.com/logging/docs/reference/v2/rest/v2/LogEntry · [Q] https://docs.cloud.google.com/logging/quotas · [ER] https://docs.cloud.google.com/error-reporting/docs/formatting-error-messages · [RUN] https://docs.cloud.google.com/run/docs/logging · [GKE] https://docs.cloud.google.com/kubernetes-engine/docs/concepts/about-logs

**Where it works**
- An integrated agent reads stdout/stderr JSON on GKE, App Engine flexible and Cloud Run functions [SL].
- Cloud Run (services, jobs and worker pools) [RUN]:
  - It puts a "single line of serialized JSON" into jsonPayload.
  - Special fields are "stripped from the jsonPayload and are written to the corresponding field", per [AG].
  - "Exceptions contained in these logs are captured by and reported in Error Reporting".
- GKE [GKE]:
  - It reads single-line JSON from stdout and stderr.
  - Without `severity`, stdout is INFO and stderr is ERROR.
  - Duplicate JSON keys are not supported.
  - `stream` is reserved and "might cause unexpected behavior and logs dropped".
  - An oversize LogEntry is **dropped** for jsonPayload and truncated for textPayload.
- **The Ops Agent (Compute Engine) uses different names** [OPS]. This applies to the fluent_forward and tcp receivers and the parse_json processor:
  - Time: `timestamp:{seconds,nanos}`, or `timestampSeconds`+`timestampNanos`.
  - Prefixed keys: `logging.googleapis.com/httpRequest`, `logging.googleapis.com/severity`, and `labels`, `operation`, `sourceLocation`, `trace`, `spanId` under the same prefix.
  - There is **no** `insertId` or `trace_sampled`.
  - Plain `severity`, `httpRequest` and `time` need processors: parse_json `time_key`/`time_format`, or modify_fields `move_from`.

**Special fields (integrated and legacy agent)** [SL][AG]; formats from [LE]

| JSON key | LogEntry field | Format and notes |
|---|---|---|
| `severity` | severity | DEFAULT(0) DEBUG(100) INFO(200) NOTICE(300) WARNING(400) ERROR(500) CRITICAL(600) ALERT(700) EMERGENCY(800) [LE]. The agent "attempts to match a variety of common severity strings" [SL] |
| `message` | textPayload or jsonPayload.message | textPayload only if it is the sole field left and detect_json is off. Put exception stack traces here for Error Reporting [SL] |
| `log` | textPayload | Legacy GKE only |
| `httpRequest` | httpRequest | HttpRequest object (see below) |
| time fields | timestamp | Search order: `timestamp:{seconds,nanos}`, then `timestampSeconds`+`timestampNanos`, then `time` (RFC 3339 string). The field used is removed; unused time fields stay in jsonPayload [AG] |
| `logging.googleapis.com/insertId` | insertId | Entries with the same timestamp and insertId dedupe in query results, not in exports [LE] |
| `logging.googleapis.com/labels` | labels | Structured record of string→string. At most 64 per entry; key 512 B and value 64 KiB, truncated beyond that [Q] |
| `logging.googleapis.com/operation` | operation | `{id, producer, first, last}`. Logs Explorer groups by it |
| `logging.googleapis.com/sourceLocation` | sourceLocation | `{file, line, function}` |
| `logging.googleapis.com/spanId` | spanId | 16-char hex of 8 bytes, not zero [LE] |
| `logging.googleapis.com/trace` | trace | `[TRACE_ID]` or legacy `projects/[PROJECT_ID]/traces/[TRACE_ID]` [SL] |
| `logging.googleapis.com/trace_sampled` | traceSampled | Boolean |

**Trace format caveats**
- The [SL] note that allows the bare ID starts with "If not writing to stdout or stderr".
- The Cloud Run samples build `projects/${project}/traces/${trace}` from `X-Cloud-Trace-Context` [RUN].
- The LogEntry reference now prefers the bare ID and calls the resource name "supported, but … not recommended" [LE].
- For stdout, `projects/<id>/traces/<32hex>` is the safe choice.

**HttpRequest** [LE]
- `requestMethod`: string.
- `requestUrl`: string with scheme, host, path and query.
- `requestSize`: **string** (int64), counting request headers plus body.
- `status`: integer.
- `responseSize`: **string** (int64), counting headers plus body.
- `userAgent`, `referer`: strings.
- `remoteIp`, `serverIp`: may include a port.
- `latency`: a Duration string, "seconds with up to nine fractional digits, ending with 's'", for example `"3.5s"`.
- `protocol`: for example `"HTTP/1.1"`.
- Cache fields: `cacheLookup`, `cacheHit`, `cacheValidatedWithOriginServer`, `cacheFillBytes`.

**Error Reporting** [ER]
- The entry needs a stack trace or a ReportedErrorEvent.
- In jsonPayload, the fields `stack_trace`, `exception` and `message` are checked **in that order**. `message` counts only if it holds a stack trace "in one of the supported programming language formats". Otherwise all fields are searched.
- For a text-only error, set `"@type":"type.googleapis.com/google.devtools.clouderrorreporting.v1beta1.ReportedErrorEvent"`. With it, Error Reporting "always evaluates the log entry as though all required fields are present". Without `@type`, it looks for `serviceContext`.
- ReportedErrorEvent fields:
  - `eventTime`
  - `serviceContext{service (required), version}`
  - `message` (required)
  - `context{httpRequest{method,url,userAgent,referrer,responseStatusCode,remoteIp}, user, reportLocation{filePath,lineNumber,functionName}}`. `reportLocation` is required if there is no stack.
- `context.httpRequest` uses **different names** from LogEntry.httpRequest.
- Supported resources include `cloud_run_revision`, `cloud_run_jobs`, `k8s_container`, `gce_instance`, `cloud_function` and `gae_app`.

**Size limit**
- A LogEntry is at most 256 KiB. This "is approximate and is based on internal data sizes" and cannot be increased. Audit log entries get 512 KiB [Q].

Example line (Cloud Run / GKE):
```
{"severity":"ERROR","message":"order failed","time":"2026-09-16T08:30:00.123456789Z","httpRequest":{"requestMethod":"POST","requestUrl":"https://api.example.com/orders","status":500,"requestSize":"512","responseSize":"87","userAgent":"curl/8.5","remoteIp":"203.0.113.7","latency":"0.042s","protocol":"HTTP/1.1"},"logging.googleapis.com/trace":"projects/my-proj/traces/4bf92f3577b34da6a3ce929d0e0e4736","logging.googleapis.com/spanId":"00f067aa0ba902b7","logging.googleapis.com/trace_sampled":true,"logging.googleapis.com/labels":{"request_id":"r-1","env":"prod"},"@type":"type.googleapis.com/google.devtools.clouderrorreporting.v1beta1.ReportedErrorEvent","serviceContext":{"service":"checkout","version":"1.4.2"},"stack_trace":"goroutine 1 [running]:\n…","operation":"POST /orders","outcome":"error","duration_ms":42,"http":{"route":"/orders"},"error":{"code":"DB_TIMEOUT","kind":"timeout"}}
```

| wlog | GCP key | Note |
|---|---|---|
| timestamp | `time` (RFC 3339, nanos OK) | A string `timestamp` is not in the agent search rules. On the Ops Agent, use `timestampSeconds`/`Nanos` |
| level | `severity` | debug→DEBUG, info→INFO, warn→WARNING, error→ERROR |
| summary | `message` | |
| operation | jsonPayload `operation` | Not `logging.googleapis.com/operation`, which is a grouping object with different semantics |
| outcome, duration_ms | jsonPayload | |
| error.stack | `stack_trace` | Error Reporting checks it first |
| error.message (no stack) | `message` + `@type` ReportedErrorEvent | |
| error.code / kind / status / cause | jsonPayload `error.*` | |
| service.name / version | `serviceContext.service` / `.version` | For Error Reporting |
| service.env | `logging.googleapis.com/labels.env` | |
| trace.trace_id | `logging.googleapis.com/trace` | Needs the project ID for the `projects/…` form |
| trace.span_id | `logging.googleapis.com/spanId` | 16 hex chars |
| trace.request_id | `logging.googleapis.com/labels.request_id` | |
| http.method | `httpRequest.requestMethod` | |
| http.path (+ scheme/host/query) | `httpRequest.requestUrl` | A full URL is expected |
| http.route | jsonPayload `http.route` | No HttpRequest field |
| http.status | `httpRequest.status` (int) | |
| http.duration_ms | `httpRequest.latency` ("0.042s") | |
| http.bytes_in / bytes_out | `httpRequest.requestSize` / `responseSize` (strings) | GCP counts headers plus body |
| http.client_ip | `httpRequest.remoteIp` | |
| http.user_agent | `httpRequest.userAgent` | |

## 9. Datadog JSON log preset (stdout)

Sources: [ATTR] https://docs.datadoghq.com/logs/log_configuration/attributes_naming_convention/ · [STD] https://docs.datadoghq.com/standard-attributes/?product=log+management · [PRE] https://docs.datadoghq.com/logs/log_configuration/pipelines/#preprocessing · [STAT] https://docs.datadoghq.com/logs/log_configuration/processors/log_status_remapper/ · [OTEL] https://docs.datadoghq.com/tracing/other_telemetry/connect_logs_and_traces/opentelemetry/ · [IDS] https://docs.datadoghq.com/tracing/guide/span_and_trace_id_format/ · [TID] https://docs.datadoghq.com/opentelemetry/reference/trace_ids/ · [UST] https://docs.datadoghq.com/getting_started/tagging/unified_service_tagging/ · [DDGO] https://docs.datadoghq.com/tracing/other_telemetry/connect_logs_and_traces/go/ · [GOLOG] https://docs.datadoghq.com/logs/log_collection/go/

**Reserved attributes** [ATTR]
- `host`, `source`, `status`, `service`, `trace_id` and `message`.
- `service` "is used to switch from Logs to APM, so make sure you define the same value".

**Default JSON preprocessing** [PRE]
- **Source:** `ddsource`.
- **Host:** `host`, `hostname`, `syslog.hostname`. In Kubernetes, a JSON host overrides the Agent hostname and the log loses host tags, so a preset should **not** emit host.
- **Date:** `@timestamp`, `timestamp`, `_timestamp`, `Timestamp`, `eventTime`, `date`, `published_date`, `syslog.timestamp`. Formats: ISO8601, UNIX ms epoch, RFC3164. "Datadog rejects a log entry if its official date is older than 18 hours in the past."
- **Message:** `message`, `msg`, `log`.
- **Status:** `status`, `severity`, `level`, `syslog.severity`.
- **Service:** `service`, `syslog.appname`, `dd.service`.
- **Trace ID:** `dd.trace_id`, `contextMap.dd.trace_id`, `named_tags.dd.trace_id`, `trace_id`.
- **Span ID:** `dd.span_id`, `contextMap.dd.span_id`, `named_tags.dd.span_id`, `span_id`.

**Status remapper** [STAT]
- Integers 0 to 7 follow syslog severity.
- Strings match by case-insensitive prefix:
  - `emerg` or `f` → emerg
  - `a` → alert
  - `c` → critical
  - `err` → error
  - `w` → warning
  - `n` → notice
  - `i` → info
  - `d`, `t`, `v`, `trace` or `verbose` → debug
  - `o` or `s`, or OK / Success → OK
  - anything else → info
- So wlog's `debug`, `info`, `warn` and `error` all map correctly as-is.

**Standard attributes for logs** [STD]
- `network.client.ip` (string) and `network.client.port` (number).
- `network.bytes_read` (number, client to server) and `network.bytes_written` (number, server to client).
- HTTP strings: `http.url`, `http.method`, `http.version`, `http.referer`, `http.request_id`, `http.useragent`. `http.status_code` is also listed as a string.
- `http.url_details.{host,port,path,queryString,scheme}`, produced by the URL parser.
- `logger.name`, `logger.thread_name`, `logger.method_name`, `logger.version`.
- `error.kind` ("error type or kind (or code in some cases)"), `error.message`, `error.stack`.
- `duration`: number in **nanoseconds**.
- `usr.id`, `usr.name`, `usr.email`.
- `evt.name` ("shared name across events generated by the same activity") and `evt.outcome` ("result of the event (for example, `success`, `failure`)").
- `http.route` ("matched route (path template)") and `http.client_ip` are listed for **APM only**.
- Doc error: the `http.method` row describes it as "The port of the client".

**env / version** [UST][DDGO][GOLOG]
- Unified service tagging uses the `env`, `service` and `version` tags, set through `DD_ENV`, `DD_SERVICE` and `DD_VERSION` plus container labels [UST].
- The Go tracer injects `dd.trace_id`, `dd.span_id`, `dd.service`, `dd.env` and `dd.version`. Custom parsing must keep them as **strings** [DDGO].
- Datadog recommends JSON logs. Go `FATAL` maps to Emergency [GOLOG].

**OTel trace IDs**
- The Agent "automatically detects the `dd.trace_id` and `dd.span_id` convention … as well as the OpenTelemetry standards `trace_id` and `span_id`" [OTEL].
- For file-scraped logs, `trace_id` must be 32-char lowercase hex and `span_id` 16-char lowercase hex, with no `0x` [OTEL].
- SDKs generate 128-bit trace IDs by default. 128-bit or 64-bit trace IDs are accepted; span IDs are 64-bit [IDS].
- The randomness is in the lower 64 bits, so full and truncated IDs match the same trace [TID].
- The older advice to convert to a decimal lower-64-bit `dd.trace_id` is **not in the current docs** (see UNVERIFIED).

Example line:
```
{"timestamp":"2026-09-16T08:30:00.123456789Z","status":"error","message":"order failed","service":"checkout","env":"prod","version":"1.4.2","trace_id":"4bf92f3577b34da6a3ce929d0e0e4736","span_id":"00f067aa0ba902b7","duration":42000000,"evt":{"name":"POST /orders","outcome":"failure"},"error":{"kind":"timeout","message":"db timeout","stack":"goroutine 1 …"},"http":{"method":"POST","route":"/orders","url_details":{"path":"/orders"},"status_code":"500","request_id":"r-1","useragent":"curl/8.5"},"network":{"client":{"ip":"203.0.113.7"},"bytes_read":512,"bytes_written":87}}
```

| wlog | Datadog | Note |
|---|---|---|
| timestamp | `timestamp` (keep) | In the date list. ISO8601 is OK. Rejected if older than 18 h |
| level | `status` (or keep `level`) | Both are preprocessed. `warn` becomes warning by prefix |
| summary | `message` | |
| operation | `evt.name` (or custom `operation`) | |
| outcome | `evt.outcome` | Doc example values are success/failure |
| duration_ms | `duration` = ms × 1,000,000 | Only one `duration` exists |
| error.kind (or error.code) | `error.kind` | |
| error.message / error.stack | `error.message` / `error.stack` | |
| error.status / error.cause | Custom `error.status` / `error.cause` | |
| service.name | `service` | |
| service.version / service.env | `version` / `env` | Whether these become tags from JSON is unverified |
| trace.trace_id / span_id | `trace_id` (32 hex) / `span_id` (16 hex) | |
| trace.request_id | `http.request_id` | |
| http.method | `http.method` | |
| http.route | `http.route` | APM standard; a custom facet for logs |
| http.path | `http.url_details.path` (or `http.url`) | |
| http.status | `http.status_code` | Listed as a string |
| http.duration_ms | `duration` (ns) | |
| http.bytes_in / bytes_out | `network.bytes_read` / `network.bytes_written` | |
| http.client_ip | `network.client.ip` | |
| http.user_agent | `http.useragent` | |
| (host) | Omit | The Agent sets it |


---

# UNVERIFIED items (all sections)

- **Part 1 (OTel, metrics, semconv):**
  - Whether `sdk/log` v0.22.0 fills `ObservedTimestamp` when it is zero.
  - Stability of `network.peer.*`, `network.local.*`, `network.transport` and `http.request.header.<key>` (from the spec page, not the Go extract).
  - Which v1.43.0 `rpc.*` keys replaced the old `rpc.system`, `rpc.service` and `rpc.grpc.status_code`.
  - That no GenAI key in `semantic-conventions-genai` is Stable (third-party blog). The repo showed no releases at fetch time.
  - Prometheus label and metric names for OTel-translated HTTP metrics.
  - The Go SDK has no `AttributeValueDepthLimit` (found by grep, not by a doc statement).
- **Honeycomb:**
  - The batch body size cap is not in the docs. libhoney-go uses 5 MB for a batch and 1 MB per event.
  - The maximum field-name length is not in the docs.
  - Which column names each Dataset Definition detects by default.
- **Elasticsearch, OpenSearch, ECS:**
  - Elastic Cloud endpoint host pattern. Basic auth wording on an official page.
  - `ecs.version` field type (the page was not fetched).
  - Default `index.mapping.total_fields.limit` for the `logs-*-*` template.
  - Whether ECS 9.5.0 has any route field (none on the `ecs-http` page).
  - That OpenSearch data streams also accept only `create` (not found in the fetched OpenSearch page).
  - An explicit official statement that request `Content-Encoding: gzip` is accepted. It is inferred from [E3].
- **Splunk HEC:**
  - The JSON key names `text` and `code` in responses.
  - `invalid-event-number` in batch errors, and whether earlier events in the batch are indexed (community posts only).
  - Server `max_content_length` default. Gzip support on an official page (seen only in the Splunk exporter).
  - Whether JSON arrays are accepted as a batch.
  - The ack request and response shapes. Whether `_json` is the right default sourcetype.
- **VictoriaLogs:**
  - The exact success status (200 from net/http default) and the status when storage refuses writes.
  - The full text of the 2 MB hard line cap in the `-insert.maxLineSizeBytes` help.

- **New Relic:**
  - `X-License-Key` and `X-Insert-Key` headers are not on the current Log API page (only `Api-Key`, as header or query).
  - Whether the 1 MB limit is measured compressed or uncompressed.
  - What 400, 408, 413, 415 and 5xx mean for the Log API (only the Metric and Event API pages document them).
  - Whether `level`, `log.level` or `severity` drives the Logs UI level facet.
  - Whether `service.name` is special in Log API JSON (it is documented only for OTLP).
  - Precision kept for ISO8601 timestamps.
- **Syslog:** pkg.go.dev shows a "This package is not in the latest version of its module" banner for log/syslog go1.27.1. The source still exists on Go master, so this looks like a site quirk.
- **AWS:**
  - Whether PutLogEvents supports `Content-Encoding: gzip`.
  - Whether the SigV4 signing name `logs` in the example credential scope is correct.
  - Whether `expiredLogEventEndIndex` is inclusive or exclusive.
  - `_aws.LogStreamName` for the agent (only `LogGroupName` is documented).
- **GCP:**
  - Whether a top-level RFC 3339 string `timestamp` is honoured on Cloud Run or GKE (documented: `time`, `timestampSeconds`/`Nanos`, `timestamp{seconds,nanos}`).
  - Whether a bare `[TRACE_ID]` works for stdout on Cloud Run.
  - Which exact Go stack-trace formats Error Reporting parses from `stack_trace`/`message`.
- **Datadog:**
  - Whether JSON attributes `env`/`version` (or `dd.env`/`dd.version`) become tags without pipeline config.
  - Whether a literal dotted key such as `"http.method"` is treated the same as the nested form.
  - The legacy advice to convert OTel trace IDs to decimal lower-64-bit `dd.trace_id` values, which is not in the current docs.
  - Timestamp precision beyond ms.
