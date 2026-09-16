# Spec: destinations and OpenTelemetry (track E)

> Phase 14 · depends on: `core-shape`, `core-problems`, `output-presets`, `pipeline`, and the
> `drain` conformance suite. Module ids: `trace-otel`, `trace-otellog`, `metrics-prometheus`,
> `drain-honeycomb`, `drain-elastic`, `drain-splunk`, `drain-victorialogs`, `drain-syslog`,
> `drain-cloudwatch`, and `drain-newrelic`. Project-wide rules in [SPEC.md](SPEC.md) apply. Facts
> come from [the destinations research](../tasks/research/dest.md) of 2026-09-16 and from the Go
> module proxy.

## Objective

Send the same wide event to the tools a team already runs. An OTel user gets span attributes, span
status, metrics, and log records from one event. A Prometheus user gets rate, errors, and duration
per operation. Each new drain writes its backend's wire format. It tells the user about every
event that the backend refused.

## Changes to the capability map

1. `trace-otel` splits in two. The new `trace-otellog` holds the Logs Bridge output, because
   `otel/log` is a v0 API that needs Go 1.25. `trace-otel` keeps spans and metrics on Go 1.21.
2. `drain-cloudwatch` takes a CloudWatch Logs client from the app. It does not depend on
   `client-aws`, because a drain never records calls into events.
3. `pipeline` gains `PartialError` in this phase, for backends that accept part of a batch.

## pipeline.PartialError (root)

```go
type PartialError struct {
	Retry   []int  // batch indexes to send again
	Dropped []int  // batch indexes that the backend refused for good
	Reason  string // the backend's error type or code, never an event value
}

func (e *PartialError) Error() string
func (e *PartialError) Retryable() bool           // true when Retry is not empty
func (e *PartialError) RetryAfter() time.Duration // 0
```

- On a `PartialError`, the worker passes the dropped events to `OnDropped` and counts them in
  `Stats`. It queues only the `Retry` events for the next attempt, with the normal backoff.
- The wrapped drain reports `WLOG_DRAIN_DROPPED` once per reason per minute, with the count.
- The worker ignores an index outside the batch. An index in both lists counts as dropped.

## Shared drain rules

1. Each drain follows the Drains rules in [SPEC-hardening.md](SPEC-hardening.md): `New`,
   `NewSender`, `MustNew`, and `WithPipeline`. An HTTP drain also takes `WithHTTPClient`,
   `WithTimeout`, and `WithUserAgent`. Three drains differ: `drain-syslog` has no HTTP client,
   `drain-cloudwatch` takes an SDK client in `New`, and `trace-otellog` has no pipeline, because
   the OTel SDK batches.
2. A drain reads the per-item result in every response. An HTTP 200 alone never proves that every
   event arrived.
3. A drain splits a batch before it sends, at the backend's count and byte limits. An event over
   the per-event limit is dropped with reason `too_large`, and the drain never sends it.
4. After a 413, the drain splits the batch in half and sends each half once. If a single event
   still gets 413, the drain drops it.
5. The drain retries 408, 429, 5xx, and network errors. It honors `Retry-After`, capped at
   `MaxDelay`. Every other 4xx is permanent.
6. An error or problem names the backend error type or code. It never holds an event value, a
   response body, or a credential.
7. Gzip is on by default for each backend that accepts it. `WithGzip(false)` turns it off.
8. Each drain implements `Setup`, keeps the Logger, and reports through `Logger.Report`.
9. Tests use `httptest` servers or loopback listeners. Each golden request body is written by hand
   from the vendor docs cited in the research. Where a container exists, an integration test runs
   against it.

## trace-otel (package `wlogotel`, module `trace/otel`)

```go
func Plugin(opts ...Option) (wlog.Plugin, error)        // trace ids, spans, metrics, and stats
func WithMeterProvider(mp metric.MeterProvider) Option  // default otel.GetMeterProvider()
func WithSpans(on bool) Option                          // default true
func WithMetrics(on bool) Option                        // default true
func WithStats(on bool) Option                          // default true
func WithExceptionEvent(on bool) Option                 // default true
func WithMaxOperations(n int) Option                    // default 1000 per kind
```

```go
p, err := wlogotel.Plugin()
if err != nil {
	return err
}
log := wlog.New(wlog.WithPlugins(p))
```

### Trace ids

- `Enricher` is removed. The plugin is a `Starter`. If the unit context holds a recording span, it
  calls `propagate.ContextWith` with the span's trace id and span id. The event, head sampling,
  and every outbound call then use the OTel ids. (HTTP-21)

### Spans

- The plugin is a `Finisher`. It reads the span from the unit context with
  `trace.SpanFromContext`. If no span is recording, it does nothing.
- The OTel HTTP or gRPC middleware must wrap outside wlog. Then the span already exists at
  the start of the unit. The package doc shows the order for net/http, chi, gin, echo, and gRPC.
  If `wlog doctor` finds the OTel middleware inside wlog, it reports a warning. (HTTP-21)
- Attribute names are the `attributes` names of the `otel` output preset. A nested value becomes
  a dotted key.
- The plugin sets preset-mapped reserved keys first, then other reserved keys, then groups, then
  user keys. The SDK keeps the first 128 attributes by default and counts the rest.
- A string, bool, int, or float keeps its type. A slice of one of those types becomes a typed
  slice. Any other slice becomes one JSON string.
- `logs`, `errors`, `audit`, and `calls` never become span attributes. `call_stats` does.
- Outcome `error` sets status Error, with `error.message` as the description. Outcome `success`
  leaves the status unset.
- `error.type` is `error.code`, else `error.kind`, else `error.type`. For a request with status
  500 or higher and no error, it is the status as text.
- For an event with an `error` object, the plugin adds one span event named `exception`. It holds
  `exception.type`, `exception.message`, and `exception.stacktrace`. The plugin never calls
  `RecordError`.
- If `trace-otellog` also runs, set `WithExceptionEvent(false)`, so OTel records each exception
  once.
- The plugin sees the redacted event, because `Finisher` runs after redaction. (G1)

### Metrics

- The plugin is a `Measurer`, so a sampler never changes a metric.
- Kind `request` records `http.server.request.duration`. It is a float64 histogram in seconds,
  with the semconv buckets `0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5,
  10`.
- Its attributes are `http.request.method`, `url.scheme`, and `http.response.status_code`. For each
  of `http.route` and `error.type` that is set, it adds that attribute. A method outside the nine standard methods is
  `_OTHER`.
- Each other kind except `log` records `wlog.work.duration` in seconds, with the same buckets. Its
  attributes are `wlog.kind`, `wlog.operation`, and `wlog.outcome`. For each of
  `wlog.system` and `error.type` that is set, it adds that attribute.
- After `WithMaxOperations` distinct operations in one kind, a new operation records as `_OTHER`.
  The plugin reports `WLOG_CAP_REACHED` once per kind.
- `url.path`, user ids, and client addresses never become metric attributes.
- With `WithStats(true)`, observable counters report the Logger's `Stats`. The counters are
  `wlog.events.emitted`, `wlog.events.dropped` with `wlog.reason`, `wlog.writer.dropped`, and
  `wlog.drain.events` with `wlog.drain` and `wlog.state`.
- If an instrument fails to build, `Plugin` returns the error. Nothing panics.

Floor: otel, otel/trace, and otel/metric v1.20.0, the first release with
`WithExplicitBucketBoundaries`. Go 1.21. Tests use otel/sdk and otel/sdk/metric v1.20.0, with `tracetest` and a
`metric.ManualReader`.

## trace-otellog (package `wlogotellog`, module `trace/otellog`)

```go
func New(provider log.LoggerProvider, opts ...Option) wlog.Drain // nil uses global.GetLoggerProvider()
func WithServiceAttributes(on bool) Option                        // default false
```

- `Send` maps the redacted event to one `log.Record` and calls `Logger.Emit`. The app's provider
  owns batching, export, and the resource.
- Before it builds a record, `Send` calls `Enabled` with the severity and the event name. It skips
  a disabled record.
- The logger name is `github.com/jeremygprawira/wlog`, with the wlog version as the
  instrumentation version.
- The record gets its timestamp from `timestamp` and its observed timestamp from the clock at
  `Send`. It gets severity and severity text from `level`, the body from `summary`, and the event
  name `wlog.<kind>`.
- Attributes are the `attributes` object of the `otel` preset. An array of objects becomes an
  `attribute.Slice` of map values.
- If `ctx` holds no valid span, `Send` builds a span context from `trace.trace_id` and
  `trace.span_id`. So an event whose ids came from `propagate` also links to its trace.
- `resource.*` keys become record attributes only with `WithServiceAttributes(true)`. Use it when
  the provider resource has no `service.name`.
- `Send` never calls `SetErr`, so the SDK does not derive a second exception type.
- Floor: otel v1.45.0 and otel/log v0.21.0, the first releases whose records use
  `attribute.Value`. Go 1.25. The package doc says that `otel/log` is v0 and can change in a
  minor release.

## metrics-prometheus (package `wlogprom`, module `metrics/prometheus`)

```go
func New(reg prometheus.Registerer, opts ...Option) (*Recorder, error) // a wlog.Plugin and wlog.Measurer
func Buckets(b ...float64) Option                                      // default: the semconv buckets above
func MaxOperations(n int) Option                                       // default 1000 per kind
func StatsCollector(l *wlog.Logger) prometheus.Collector
```

- The recorder has one histogram, `wlog_duration_seconds`, with labels `kind`, `operation`,
  `outcome`, and `status`. For a kind with no status, `status` is empty.
- `New` calls `Register`, never `MustRegister`. If the registry already holds the same histogram,
  `New` reuses it. Any other `AlreadyRegisteredError` returns an error.
- The recorder calls `GetMetricWithLabelValues`, never `WithLabelValues`. On a label error, it
  reports a problem and records nothing.
- The operation cap and the `_OTHER` rule match `trace-otel`.
- `StatsCollector` exports `wlog_events_emitted_total`, `wlog_events_dropped_total{reason}`,
  `wlog_writer_dropped_total`, and `wlog_drain_events_total{drain,state}`. It reads `Stats` on
  each scrape.
- The module serves no HTTP handler. The app exposes its registry with `promhttp`.
- Floor: client_golang v1.11.1, the first release free of GO-2022-0322. Go 1.21.

## drain-honeycomb (package `honeycomb`, root)

- `POST {api}/1/batch/{dataset}` with `X-Honeycomb-Team: <key>` and `Content-Type:
  application/json`. The API URL defaults to `https://api.honeycomb.io`. EU teams set
  `https://api.eu1.honeycomb.io`.
- `WithDataset` or `HONEYCOMB_DATASET` names the dataset. Without one, each event goes to the
  dataset named by its `service.name`, and the batch splits per dataset. The path segment uses
  `url.PathEscape`.
- The body is a JSON array of `{"time", "samplerate", "data"}`. `time` is `timestamp`.
  `samplerate` is `100 / wlog.sample_rate`, rounded, at least 1. Without a rate, it is 1.
- `data` holds the event as dotted keys at every depth. An array becomes one JSON string.
- With `WithSpans(true)`, the default, `data` also gets `name` from `operation` and `error` true
  for outcome `error`. If `trace.parent_span_id` is not empty, `data` also gets it as
  `trace.parent_id`.
- With `WithSpans(false)`, `data` leaves out `trace.span_id` and `trace.parent_id`. Honeycomb then
  shows the event as a log in its trace, not as a second span. If OTel spans reach the same
  environment, use this option.
- Limits: 2,000 fields per event, 64 KB per string value, 1 MB per event, and 5 MB per request
  body before compression. Extra fields are dropped in the `trace-otel` priority order. A long
  string is cut and ends with `…`.
- An HTTP 200 body is an array of `{"status", "error"}` in batch order. Status 202 is success. An
  item with status 500 or higher is retried. Any other item status is dropped, with reason
  `status_<code>`.
- A whole-request 429 has no `Retry-After`, so the drain uses its backoff. 400, 401, 403, and 404
  are permanent.

## drain-elastic (package `elastic`, root)

```go
func WithURL(url string) Option
func WithAPIKey(encoded string) Option        // Authorization: ApiKey <encoded>
func WithBasicAuth(user, password string) Option
func WithIndex(name string) Option            // default logs-wlog-default, a data stream
func WithMaxBatchBytes(n int) Option          // default 5 MiB, clamped to 100 MiB
func Template(engine Engine) []byte           // Elasticsearch or OpenSearch
```

- `POST {url}/{index}/_bulk?filter_path=errors,items.*.status,items.*.error.type` with
  `Content-Type: application/x-ndjson`. The body is `{"create":{}}` and the `ECS()` line for each
  event, and the final line ends with `\n`.
- Data streams accept only `create`, so the drain never sends `index`. The ECS line always holds
  `@timestamp`.
- The byte cap counts the body before compression, as `http.max_content_length` does.
- An HTTP 200 with `errors` true holds one item per event, in batch order. An item with status 429
  or 500 or higher is retried. Any other failed item is dropped, with its `error.type` as the
  reason.
- The drain never reads `error.reason`, because a reason can quote a field value.
- A whole-request 429, 502, 503, or 504 is retried. 400, 401, and 403 are permanent.
- Amazon OpenSearch Service needs SigV4. Pass a signing `http.Client` through `WithHTTPClient`. The
  package doc shows one built with the aws-sdk-go-v2 `v4.Signer`, so the root module gains no
  dependency.
- `Template` returns a composable index template for `logs-wlog-*` with `data_stream` on and
  priority 200. It maps `@timestamp` as `date`, `event.duration` and `http.response.status_code`
  as `long`, and `client.ip` as `ip`. Ids, `log.level`, and `service.*` are `keyword`.
- The template maps `wlog.fields` as `flattened` for Elasticsearch and `flat_object` for
  OpenSearch. So free-form user keys never grow the mapping past its field limit.
- The drain never installs the template. The doc gives the `PUT _index_template/logs-wlog`
  command, and `wlog doctor` prints it.

## drain-splunk (package `splunk`, root)

- `POST {url}/services/collector/event` with `Authorization: Splunk <token>` and `Content-Type:
  application/json`.
- Every request sends `X-Splunk-Request-Channel` with one random UUID per drain. So a token with
  indexer acknowledgement on also accepts events. The drain never polls acknowledgements.
- The body is one envelope per event, joined with `\n`. The envelope holds `time`, `host`,
  `source`, `sourcetype`, `index`, `fields`, and `event`.
- `time` is epoch seconds with three decimals. `host` is `service.instance`. `source` defaults to
  `wlog`, and `sourcetype` defaults to `_json`. If no index is set, the envelope has no `index`.
- `fields` holds flat strings with low cardinality: `level`, `kind`, `outcome`, `service`, and
  `env`. `event` is the canonical event.
- The batch cap is 1 MiB before compression, because Splunk docs do not state the server limit.
  `WithMaxBatchBytes` changes it.
- The response body is `{"text", "code"}`. Code 0 is success. Codes 24 and 25 are success with a
  capacity warning, and the drain reports `WLOG_DRAIN_BACKPRESSURE` once per minute.
- HTTP 429 and 503, and codes 9, 18, 19, 20, 23, 26, and 27, are retried with `Retry-After`.
- Code 6 means one event in the batch is bad, and earlier events can already be indexed. The drain
  splits the batch in half and sends each half once. A single event that still gets code 6 is
  dropped with reason `hec_code_6`.
- Every other code is permanent, with reason `hec_code_<n>`.

## drain-victorialogs (package `victorialogs`, root)

- `POST {url}/insert/jsonline?_msg_field=summary&_time_field=timestamp&_stream_fields=service.name,service.env`.
  Each line is the canonical event.
- `WithStreamFields` changes the stream fields. `New` rejects `trace.*`, `event_id`, `user.*`,
  `http.path`, and `http.client_ip`, because stream fields must have low cardinality.
- `WithTenant(accountID, projectID)` sends the `AccountID` and `ProjectID` headers. Without it, the
  drain sends neither header.
- VictoriaLogs never reports a bad line to the client. If every line fails, it fails the
  request. Otherwise it returns success. So the drain sends only lines that it encoded, each with
  a valid `timestamp`.
- VictoriaLogs skips a line over `-insert.maxLineSizeBytes` with no error. The default is 256 KiB,
  so the drain drops a longer line with reason `too_large`. `WithMaxLineBytes` changes the limit.
- 400 is permanent. 5xx and network errors are retried.

## drain-syslog (package `syslog`, root)

```go
func WithAddr(addr string) Option               // host:port
func WithNetwork(network string) Option         // tls (default), tcp, or udp
func WithTLSConfig(c *tls.Config) Option        // MinVersion is raised to TLS 1.2
func WithFacility(f Facility) Option            // default User
func WithAppName(name string) Option            // default service.name
func WithStructuredData(sdID string) Option     // name@<your enterprise number>, off by default
func WithBOM(on bool) Option                    // default true
func WithMaxUDPBytes(n int) Option              // default 2048
```

The drain writes RFC 5424 frames itself. Go's `log/syslog` is frozen, has no TLS, and writes the
older BSD format.

| RFC 5424 part | Value |
|---|---|
| PRI | facility × 8 + severity. Severity: debug 7, info 6, warn 4, error 3 |
| VERSION | `1` |
| TIMESTAMP | `timestamp` in UTC with exactly 6 fractional digits and `Z` |
| HOSTNAME | `service.instance`, else `os.Hostname()`, else `-`. At most 255 characters |
| APP-NAME | `WithAppName`, else `service.name`, else `-`. At most 48 characters |
| PROCID | `os.Getpid()` |
| MSGID | `kind` |
| STRUCTURED-DATA | `-`, or one element with `level`, `kind`, `outcome`, `trace_id`, and `request_id` |
| MSG | The BOM unless `WithBOM(false)`, then the canonical event as JSON |

- A header character outside ASCII 33 to 126 becomes `_`. An empty header field is `-`.
- In a PARAM-VALUE, the drain escapes `"`, `\`, and `]` with a backslash.
- `WithStructuredData` accepts only an SD-ID of the form `name@digits`, with no `=`, space, `]`,
  or `"`. Use your own IANA Private Enterprise Number. `32473` is reserved for documentation.
- TCP and TLS use octet-counting frames: the length, one space, then the message. The default port
  for TLS is 6514.
- UDP sends one message per datagram. If a frame is over `WithMaxUDPBytes`, MSG becomes `summary`
  plus ` event_id=<id>`, and the drain counts one truncation.
- The drain dials on the first batch, with a 5 second timeout. After a write error, it closes the
  connection and returns a retryable error, and the next attempt dials again. A retry can repeat
  frames, so delivery is at least once.
- `Close` closes the connection, so TLS sends `close_notify`.
- Setup vars: `WLOG_SYSLOG_ADDR`, `WLOG_SYSLOG_NETWORK`, `WLOG_SYSLOG_APP_NAME`,
  `WLOG_SYSLOG_FACILITY`, and `WLOG_SYSLOG_SD_ID`.

## drain-cloudwatch (package `cloudwatch`, module `drain/cloudwatch`)

```go
type API interface {
	PutLogEvents(ctx context.Context, in *cloudwatchlogs.PutLogEventsInput,
		opts ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.PutLogEventsOutput, error)
	CreateLogStream(ctx context.Context, in *cloudwatchlogs.CreateLogStreamInput,
		opts ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.CreateLogStreamOutput, error)
}

func New(client API, group string, opts ...Option) (wlog.Drain, error)
func WithStream(name string) Option              // default os.Hostname()
func WithPreset(p wlog.OutputPreset) Option      // such as preset.EMF()
func WithCreateStream(on bool) Option            // default true
func Factory(client API) setup.Factory           // WLOG_CLOUDWATCH_GROUP, WLOG_CLOUDWATCH_STREAM
```

- The app builds the client with `cloudwatchlogs.NewFromConfig(cfg)`. So credentials, region, and
  SDK retries stay with the app.
- Each log event has `timestamp`, the event start in epoch milliseconds, and `message`, the event
  JSON. With `WithPreset`, the message is the preset output.
- Before it sends, the drain sorts the batch by timestamp. It splits the batch at 10,000 events,
  at 1,048,576 bytes counted as message bytes plus 26 per event, and at a 24 hour span.
- A 200 response can hold `rejectedLogEventsInfo`. Events from `tooNewLogEventStartIndex` to the
  end are dropped with reason `too_new`. Events before `tooOldLogEventEndIndex` are dropped with
  reason `too_old`.
- Events before `expiredLogEventEndIndex` are dropped with reason `expired`. AWS docs do not say
  if this index is inclusive, so the drain treats it as exclusive, like `tooOldLogEventEndIndex`.
- The drain maps each rejected index back to its place in the original batch.
- The drain reads the error code through `smithy.APIError`.
- `ResourceNotFoundException` starts one `CreateLogStream` call, then a retryable error. The drain
  never creates a log group.
- `ThrottlingException`, `ServiceUnavailableException`, `InternalFailure`,
  `RequestTimeoutException`, and `ExpiredTokenException` are retried.
- `InvalidParameterException`, `AccessDeniedException`, and `UnrecognizedClientException` are
  permanent.
- The drain never sends `sequenceToken`, because AWS ignores it.
- Floor: cloudwatchlogs v1.15.22 with aws-sdk-go-v2 v1.17.1 and smithy-go v1.13.4. Go 1.21.

## drain-newrelic (package `newrelic`, root)

- `POST {endpoint}/log/v1` with `Api-Key: <license key>` and `Content-Type: application/json`.
- The endpoint follows `WithRegion` or `NEW_RELIC_REGION`: `us` gives `log-api.newrelic.com`,
  `eu` gives `log-api.eu.newrelic.com`, `jp` gives `log-api.jp.nr-data.net`, and `fedramp` gives
  `gov-log-api.newrelic.com`.
- The body is `[{"logs": [...]}]`. Each log has `timestamp` in epoch milliseconds, `message` from
  `summary`, and `attributes`.
- `attributes` holds the event as dotted keys. `trace.trace_id` becomes `trace.id`,
  `trace.span_id` becomes `span.id`, `error.kind` becomes `error.class`, and `service.instance`
  becomes `hostname`.
- `message` is always plain text. New Relic parses a JSON message into fields, so the drain never
  sends one.
- A user key named `accountId`, `appId`, `eventType`, `entity.guid`, `entity.name`, `entity.type`,
  `instrumentation.name`, `instrumentation.provider`, or `instrumentation.version` moves to
  `wlog.fields.<name>`.
- Limits: 1,000,000 bytes per request, counted before compression because the docs do not say
  which. At most 255 attributes per log, kept in the `trace-otel` priority order. The drain
  reports extra attributes with `WLOG_CAP_REACHED`.
- 202 is success. 429 is retried with `Retry-After`. 408 and 5xx are retried. 400, 403, and 415 are
  permanent.
- New Relic reports some failures later as `NrIntegrationError` events. The package doc gives the
  NRQL query that finds them.

## Setup vars

[SPEC-setup.md](SPEC-setup.md) lists the vars for every drain in this track. `drain-cloudwatch`
joins through `cloudwatch.Factory(client)`, because the app builds the AWS client.

## Success criteria

1. A `Sender` returns `PartialError{Retry: [1], Dropped: [2]}` for a batch of three.
   `OnDropped` receives event 2, and the next attempt sends only event 1. (pipeline)
2. With `tracetest`, a request that ends 502 with a catalog error gives a span with
   `http.request.method`, `http.route`, `http.response.status_code` 502, and status Error. A
   masked field stays masked. No `logs` or `calls` attribute exists. (trace-otel)
3. With a `ManualReader` and a head sampler that keeps 0%, 100 requests give a histogram count of
   100. 1,001 distinct operations give one `_OTHER` series and one `WLOG_CAP_REACHED`.
   (trace-otel)
4. With `logtest` v0.21.0, an error request gives one record with severity 17, event name
   `wlog.request`, and the summary as its body. Without a span on `ctx`, the record still holds
   the event's trace id. (trace-otellog)
5. `testutil.CollectAndCompare` matches a golden exposition for three events. Calling `New` twice
   on one registry works. (metrics-prometheus)
6. Each HTTP drain matches its golden request body. The status table test covers 2xx, 400, 401,
   403, 408, 413, 429, and 5xx.
7. Per-item tests prove the exact `Retry` and `Dropped` sets: Honeycomb positional statuses,
   Elasticsearch items, Splunk code 6 splits, and CloudWatch rejected ranges.
8. Every syslog frame parses with the RFC 5424 ABNF test parser. A TLS test runs on a loopback
   listener. A UDP event over 2,048 bytes arrives as its summary form.
9. CloudWatch batches with unsorted timestamps arrive sorted. A batch that spans 25 hours becomes
   two calls. `ResourceNotFoundException` leads to one `CreateLogStream` and then success.
10. Integration tests pass against Elasticsearch 8, OpenSearch 2, Splunk Enterprise, and
    VictoriaLogs v1.52.0. Each test installs what it needs, sends the golden events, and finds
    them with a query on `operation` or its mapped name.
11. `tools floor` builds `trace-otel`, `metrics-prometheus`, and `drain-cloudwatch` at Go 1.21, and
    `trace-otellog` at Go 1.25.
12. Every drain here passes the parts of the `drain` suite that apply to it, including the leak
    test. Each HTTP drain passes the HTTP part.

## Testing

Black-box tests in each package. Golden bodies live in each module's `testdata/`, written from the
research, never from the encoder. Tests never call a real vendor endpoint. Integration tests run
behind the `integration` build tag with Compose health probes.

## Boundaries

- **Always:** read every per-item result, and keep metric attributes low in cardinality.
- **Ask first:** a dependency beyond the ones named here, or a metric or attribute rename after
  v1.0.0.
- **Never:** put a response body or reason text into a problem. Never install an index template,
  create a log group, or start a span.

## Open questions

None.
