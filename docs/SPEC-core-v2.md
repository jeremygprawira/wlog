# Spec: core v2

> Phase 11 · root module · depends on: `core` after phase 10. Module ids: `core-shape`,
> `core-default`, `core-problems`, and `core-calls` in package `wlog`. Also `event-schema` in
> `wlog/schema`, and `output-presets` in `wlog/preset`. Project-wide rules
> in [SPEC.md](SPEC.md) apply. This spec replaces the event layout, sinks, field names, and
> configuration sections of [SPEC-core.md](SPEC-core.md).

## Objective

Give every event one versioned shape that people, agents, and code read the same way. Let a
program log with no setup. Tell the user about every event that wlog failed to deliver. Record the
calls a unit of work makes. Publish the shape as a schema, and let stdout speak each backend's
dialect.

## core-shape

### Reserved keys, in output order

| # | Key | Type | Meaning |
|---|---|---|---|
| 1 | `timestamp` | string | Start of the work, RFC 3339 UTC with nanoseconds |
| 2 | `level` | string | `debug`, `info`, `warn`, or `error` |
| 3 | `summary` | string | One sentence, see "Summary" |
| 4 | `operation` | string | The operation name for the kind, see SPEC-work |
| 5 | `kind` | string | `request`, `rpc`, `message`, `job`, `command`, `function`, `work`, or `log` |
| 6 | `outcome` | string | `success` or `error` |
| 7 | `duration_ms` | number | Float milliseconds with microsecond precision. Absent for `log` |
| 8 | `message` | string | The text of a plain log line. Absent for other kinds |
| 9 | `error` | object | The `ErrorInfo` that decided the outcome |
| 10 | `event_id` | string | UUIDv7 |
| 11 | `service` | object | `name`, `version`, `env`, `instance` |
| 12 | `trace` | object | `trace_id`, `span_id`, `parent_span_id`, `request_id`, `parent_event_id`, `parent_operation` |
| 13 | kind group | object | `http`, `rpc`, `messaging`, `job`, `cli`, or `faas`, see SPEC-work and SPEC-http-core |
| 14 | domain groups | object | `user`, `geo`, `client`, `host`, `deploy`, `llm`, in this order |
| 15 | user keys | any | Every key not reserved, sorted by name |
| 16 | `audit` | array | Audit records, up to 20 |
| 17 | `errors` | array | Earlier `ErrorInfo` values, up to 10 |
| 18 | `logs` | array | Folded log lines, up to 50 |
| 19 | `calls` | array | Call records, up to 50, see core-calls |
| 20 | `call_stats` | object | Totals per call kind |
| 21 | `feature_flags` | array | Flag evaluations, up to 50 |
| 22 | `wlog` | object | `schema_version`, `redact_fingerprint`, `sample_rate`, `dropped_fields`, `dropped_logs`, `dropped_errors`, `dropped_calls`, `dropped_audit`, `late_writes`, `unknown_keys`, `truncated` |

A kind group can hold one object named by its system for library fields, such as `messaging.kafka`
or `rpc.mcp`. The schema allows any key inside that object.

Phase 11 renames three `llm` keys: `model` becomes `request_model`, `cached_input_tokens` becomes
`cache_read_input_tokens`, and `finish_reason` becomes `finish_reasons`, an array. Track G adds the
keys for its new `Record` fields, named in snake_case.

A key with an empty string, empty object, empty array, or zero counter is left out. `kind`,
`message`, and `call_stats` are new reserved keys. `audit` changes from one object to an array.
Both changes are part of this spec, and approving the spec approves them.

### ErrorInfo v2

```go
type ErrorInfo struct {
	Code     string         `json:"code,omitempty"`
	Message  string         `json:"message,omitempty"`
	Kind     string         `json:"kind,omitempty"`
	Type     string         `json:"type,omitempty"`   // Go type, such as *catalog.Error
	Status   int            `json:"status,omitempty"`
	Cause    string         `json:"cause,omitempty"`
	Causes   []string       `json:"causes,omitempty"` // from errors.Join or several %w
	Caller   string         `json:"caller,omitempty"` // file:line of the wlog.Error call
	Stack    string         `json:"stack,omitempty"`
	Why      string         `json:"why,omitempty"`
	Fix      string         `json:"fix,omitempty"`
	Link     string         `json:"link,omitempty"`
	Attrs    map[string]any `json:"attrs,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
	Internal map[string]any `json:"internal,omitempty"`
}
```

`Caller` costs one `runtime.Caller` per `wlog.Error` call. `WithCaller(false)` turns it off.

### Summary

Core builds `summary` in the finalize stage, after redaction, from the redacted event.

| Kind | Template |
|---|---|
| `request` | `{method} {route or "unmatched"} {status} in {duration}` |
| `rpc` | `{rpc.system} {rpc.service}/{rpc.method} {rpc.status_code} in {duration}` |
| `message` | `{messaging.operation} {messaging.destination} {outcome} in {duration}`. For a `delivery_count` above 1, it adds ` delivery {n}` |
| `job` | `job {job.name} {outcome} in {duration}`. For an attempt above 1, it adds ` attempt {n}` |
| `command` | `{cli.path} exit {cli.exit_code} in {duration}` |
| `function` | `function {faas.name} {faas.trigger} {outcome} in {duration}`, plus ` cold start` |
| `work` | `{operation} {outcome} in {duration}` |
| `log` | `{message}` |

After the template, core appends `: {error.code} {error.message} (fix: {error.fix})` for an event
with an error, leaving out any empty part. It then appends up to two user keys whose names end in
`_id`, sorted by name, as ` ({key}={value})`. Duration text uses `0.84ms`, `12.8ms`, or `1.24s`.

A part whose value is masked, longer than 64 characters, or absent is left out. The summary is
at most 240 characters. `WithSummary(func(Event) string)` replaces the builder, and a panic in it
falls back to the default.

### Stages

```go
type HeadSampler interface{ Sample(level Level, traceID string) (keep bool, rate float64) }
type Keeper interface{ Keep(ctx context.Context, event Event) bool } // true forces keep
type Event interface {                                              // read-only view
	Get(path string) (any, bool) // dotted path such as "http.status"
	Kind() string
	Level() Level
}

func WithHeadSampler(s HeadSampler) Option
func WithKeepers(k ...Keeper) Option // combined with OR, after enrich
```

Stage order follows SPEC.md: head sample, enrich, tail keep, redact, finalize, drains, output
preset, writers. A head sampler that drops an event still lets a Keeper force it back. Core runs
Keepers only on events that head sampling dropped, and on events with a rate below 100.
`sample.New` returns a value that implements both interfaces.

### Plugin hooks for every kind

```go
type Starter interface {  // replaces RequestStarter
	OnStart(ctx context.Context, kind string) context.Context
}
type Finisher interface { // replaces RequestFinisher
	OnFinish(ctx context.Context, event Event)
}
```

- `Start`, `Detach`, and every `work` and `http-core` unit call each `Starter` after the event
  exists, in registration order. The returned context replaces the unit's context. A logger
  bridge uses this hook to bind a per-unit logger, so `logr.FromContext(ctx)` or
  `zerolog.Ctx(ctx)` folds into the event with no app code.
- `end` calls each `Finisher` with the read-only event after finalize and before drains. A
  `Finisher` sees `summary`, `outcome`, and the redacted fields.
- Both run under recover. A panic reports `WLOG_HOOK_PANIC` and keeps the previous context.
  (PAR-25)
- Go has no diagnostics channel like Node's. A `Finisher` or `drain-memory` `Subscribe` gives
  in-process code each finished event. (PAR-24)

```go
type Measurer interface { // metrics plugins, such as trace-otel and metrics-prometheus
	Measure(ctx context.Context, m Measure)
}

type Measure struct {
	Kind       string
	Operation  string
	Level      Level
	Outcome    string
	DurationMS float64
	Status     string // http.status, rpc.status_code, or cli.exit_code, as text
	Method     string // http.method
	Route      string // http.route
	Scheme     string // http.scheme
	System     string // rpc.system, messaging.system, job.system, or faas.system
	ErrorType  string // error.code, else error.kind, else error.type
}
```

- `end` calls each `Measurer` for every event whose kind is not `log`. The call comes before head
  sampling and the level filter, so a sampler never changes a metric.
- `Measure` holds reserved fields only. It never holds user keys, paths, bodies, or ids. Each
  string passes through the redactor's value patterns first, so G1 holds.
- A disabled or closed Logger calls no `Measurer`. A panic reports `WLOG_HOOK_PANIC`.

### Writers

```go
func WithWriter(w io.Writer, opts ...WriterOption) Option // default os.Stdout
func WithFormat(f Format) Option                          // FormatAuto, FormatJSON, FormatPretty
func WithSilent() Option                                  // no writer, drains only
func WriterSync() WriterOption                            // write on the emitting goroutine
func WriterBuffer(events int) WriterOption                // async queue size, default 4096
func WithCaller(on bool) Option                           // ErrorInfo.Caller, default on
func WithSummary(fn func(Event) string) Option
```

- The JSON writer encodes the reserved keys in the order of the table above. It encodes nested
  objects with sorted keys and HTML escaping off. It writes one line per event, in one `Write`
  call.
- The default writer is async. A bounded queue holds events, and one goroutine writes them. A full
  queue drops the oldest line, counts it, and reports `WLOG_WRITER_DROPPED`. `Logger.Flush` and
  `Logger.Close` drain the queue. `WriterSync()` restores synchronous writes.
- `FormatAuto` picks pretty for a terminal, or for env `local`, `dev`, or `development`. It picks
  JSON otherwise.

### Pretty console v2

```
ERROR POST /orders/{id} 502 in 840.2ms: PAYMENT_DECLINED card declined (order_id=4821)
  Why:  The issuer rejected the charge.
  Fix:  Ask the customer for another card.
  More: https://docs.example.com/errors/PAYMENT_DECLINED
  at orders/charge.go:88
  ├─ http   method=POST path=/orders/4821 status=502 bytes_in=76 bytes_out=37
  ├─ trace  request_id=8a3692537f716fa0 trace_id=4bf92f35…
  ├─ calls  POST api.stripe.com/v1/charges 402 801.5ms
  └─ order_id=4821
```

- Line 1 is the level, colored, then the summary without its `(fix: ...)` part, because the error
  block shows the fix. For an event with an error, the error block comes next.
- Each group is one tree line of `key=value` pairs. A nested object inside a group prints as
  `key={...}` on that line. A value longer than 80 characters ends with `…`.
- `calls` prints one line per call. `logs` prints one line per folded record.
- Core renders the whole event into one buffer and writes it once.
- Colors turn on only for a terminal, and `NO_COLOR` turns them off.

## core-default

```go
func SetDefault(l *Logger)
func Default() *Logger                  // never nil, built with New() on first use
func (l *Logger) SetEnabled(on bool)
func (l *Logger) Enabled() bool
func (l *Logger) Flush(ctx context.Context) error
func Log(ctx context.Context, level Level, msg string, kv ...any)
```

- A package function uses the Logger on `ctx`. With none, it uses `Default()`. `SetEnabled` and
  `Enabled` at package level are removed.
- The default pointer is an `atomic.Pointer[Logger]`. CLAUDE.md names it as the one allowed piece
  of package-level state.
- `Start` and `Detach` with no Logger on `ctx` use `Default()`.
- `Set`, `SetGroup`, `Append`, `SetLevel`, and `AppendLog` with no event on `ctx` do nothing.
  Each one reports `WLOG_NO_EVENT` with the key name.
- `Error` with no event on `ctx` emits a `log` event at level `error`, with the `ErrorInfo`, so no
  error is lost. It also reports `WLOG_NO_EVENT`.
- `Log` writes a plain line at any level, including `error`. `Info`, `Warn`, and `Debug` stay.
- The end func that `Start` returns stays `func()`. A `Finisher` sees the finished event, and
  `wlogtest` reads it in tests. So evlog's event return value is not adopted. (PAR-4)

## core-problems

```go
type Problem struct {
	Code    string // stable, such as WLOG_DRAIN_FAILED
	Source  string // the hook, drain, or option that reported it
	Message string // one sentence, never holding an event value
	Why     string
	Fix     string
	Link    string // https://github.com/jeremygprawira/wlog/blob/main/docs/problems.md#<code>
	Err     error
	Count   int    // reports folded into this one since the last delivery
}

func OnProblem(fn func(Problem)) Option // replaces OnError
func WithDebug(on bool) Option          // also WLOG_DEBUG=1
func Problems() []Problem               // the catalog, for wlog explain and docs
func (l *Logger) Report(p Problem)           // for drains, plugins, and presets outside core
func (l *Logger) Stats() Stats
func (l *Logger) DebugHandler() http.Handler // JSON of Stats, for a debug route
```

- `New` calls `Setup(l)` on every drain, plugin, and output preset that implements `Setup`.
  `pipeline.Wrap` implements `Setup` and passes it to its `Sender`. So a drain or plugin keeps the
  Logger and calls `Report`, with no package state.
- Every report goes through `OnProblem`, under recover. The default handler writes one JSON line
  to stderr per code per minute, with `Count`.
- With debug on, core reports `WLOG_EVENT_DROPPED` for every dropped event, with the reason:
  `sampled`, `level`, `disabled`, `closed`, or `too_large`.
- `Stats` holds emitted events, dropped events per reason, and writer drops. It also holds one
  entry per drain that implements `StatsReporter`, with queued, sent, dropped, retried, and last
  error.
- A drain whose `Send` takes over 5ms on the emitting goroutine reports `WLOG_DRAIN_SLOW` once.
- `docs/problems.md` lists every code with why, fix, and an example. For each code in
  `Problems()`, a test looks for its section, and a missing section fails the test.

Initial codes:

| Code | Reported when |
|---|---|
| `WLOG_NO_EVENT` | A write needs an event, and the context has none |
| `WLOG_LATE_WRITE` | A write lands on an event that already emitted |
| `WLOG_LOGGER_CLOSED` | An event emits after `Close` |
| `WLOG_HOOK_PANIC` | A user hook panicked, named in `Source` |
| `WLOG_DRAIN_SLOW` | A synchronous drain blocked the emitting goroutine |
| `WLOG_DRAIN_BACKPRESSURE` | A backend accepted events and warned that it is near capacity |
| `WLOG_DRAIN_FAILED` | A drain gave up on a batch |
| `WLOG_DRAIN_DROPPED` | A drain buffer dropped events |
| `WLOG_DRAIN_DISABLED` | `setup.FromEnv` skipped a drain with a missing required variable |
| `WLOG_WRITER_DROPPED` | The async writer queue dropped lines |
| `WLOG_EVENT_TOO_LARGE` | Finalize removed fields to fit the size cap |
| `WLOG_CAP_REACHED` | A key, group, array, log, error, call, or audit cap dropped a value |
| `WLOG_VALUE_UNENCODABLE` | A value became an `[unencodable]` marker |
| `WLOG_INVALID_CONFIG` | An env var or option held a bad value |
| `WLOG_AUDIT_DISABLED` | A disabled Logger dropped an audit record |
| `WLOG_AUDIT_WRITE_FAILED` | The journal failed to write or sync |
| `WLOG_SILENT_NO_DRAIN` | `WithSilent` is set, and the Logger has no drain |
| `WLOG_EVENT_DROPPED` | Debug mode only: an event was dropped, with the reason |

## core-calls

```go
type Call struct {
	Kind      string // http, db, cache, queue, rpc, llm, storage, other
	System    string // postgresql, redis, kafka, grpc, aws.s3, anthropic, ...
	Operation string // GET, SELECT, publish, /pkg.Service/Method, messages.create
	Target    string // host and route, table, topic, bucket, model
}

type CallResult struct {
	Status     string // 200, OK, NOT_FOUND
	Err        error  // counts as an error, never copied as text
	ErrCode    string // SQLSTATE, AWS error code, Redis prefix
	ErrMessage string // only a message the adapter knows is safe
	Rows       int64  // rows or items affected, 0 means unknown
	Attrs      map[string]any
}

func StartCall(ctx context.Context, c Call) (context.Context, func(CallResult))
func CallFromContext(ctx context.Context) (Call, bool) // the open call this context is inside
func CallSpanID(ctx context.Context) (string, bool)    // that call's span id, for propagate
```

- `StartCall` records the start time and returns a context holding a call span id. `propagate`
  reads it through `CallSpanID` to inject headers. The end func appends one record to `calls` and updates
  `call_stats`.
- A call record holds `kind`, `system`, `operation`, `target`, `status`, `duration_ms`,
  `span_id`, `rows`, `attrs`, and `error`. `error.code` is `ErrCode`, else the code the Logger's
  extractor finds in `Err`. `error.message` is `ErrMessage` only. Core never writes `Err.Error()`
  into a call record.
- `calls` holds 50 records. Past that, core updates `call_stats` only and counts
  `wlog.dropped_calls`.
- `call_stats` holds one object per kind: `count`, `errors`, `duration_ms` (total), and
  `max_ms`.
- With no event on `ctx`, `StartCall` returns `ctx` and a no-op end func, and reports nothing.
- An end func called after the event emitted records nothing. Debug mode reports it as
  `WLOG_LATE_WRITE`. Database rows closed after a response are the common case.
- An adapter under another adapter reads `CallFromContext`. If the context is already inside a
  call of the same kind, it skips, so an ORM query over a wrapped driver records once.
- Every end func runs its recording under recover, and never changes the caller's error.
- Calling the end func twice records once. A call started in a `Detach` child belongs to the
  child.
- Every value in `attrs` passes through the value copy and the redactor. A call record never holds
  query parameters or bodies unless an adapter's opt-in sets them.

## event-schema

- `schema/event.v1.json` is a JSON Schema (draft 2020-12) for the table above, with every kind
  group. `schema/map.v2.json` covers `wlog.map.json` version 2.
- Package `wlog/schema` embeds both files: `schema.EventV1()` and `schema.MapV2()` return bytes.
- `tools schema` tests every golden event in the repo, and every drain golden body that holds
  events, against the schema.
- The `tools` module uses `github.com/santhosh-tekuri/jsonschema/v6` for those tests. Root stays
  stdlib-only.
- A change to a reserved key changes the schema in the same commit. A breaking change bumps
  `schema_version` and adds `event.v2.json`.

## output-presets

A preset changes the JSON that a writer prints. It never changes the canonical event that drains,
samplers, keepers, `Finisher` hooks, and the audit chain see. (CORE-12, SPEC-G11)

```go
// In package wlog.
type OutputPreset interface {
	Name() string                              // default, flat, otel, ecs, gcp, datadog, emf
	Apply(event map[string]any) map[string]any // returns a new map and never changes event
	Lead() []string                            // top-level keys written first, in this order
}

func WithOutput(p OutputPreset) Option // nil means the default shape

// In package wlog/preset.
func Default() OutputPreset
func Flat() OutputPreset
func OTel() OutputPreset
func ECS() OutputPreset
func GCP(opts ...GCPOption) OutputPreset
func Datadog() OutputPreset
func EMF(opts ...EMFOption) OutputPreset
func ByName(name string) (OutputPreset, bool)                    // for WLOG_OUTPUT
func Rename(p OutputPreset, fromTo ...string) OutputPreset        // top-level keys only

func GCPProject(id string) GCPOption                // default GOOGLE_CLOUD_PROJECT, read once in GCP
func EMFNamespace(ns string) EMFOption              // default wlog
func EMFDimensions(sets ...[]string) EMFOption      // default [["service.name", "operation"]]
func EMFMetrics(keys ...string) EMFOption           // more numeric keys, with unit None
func EMFLogGroup(name string) EMFOption             // _aws.LogGroupName, for the CloudWatch agent
```

### Shared rules

- Presets apply to `FormatJSON` only. The pretty console always renders the canonical event.
- The JSON writer uses the fixed key order for `Default()`. For any other preset, it writes the
  `Lead()` keys first, then the other keys sorted by name. Nested keys are always sorted.
- A preset copies each value it keeps. It never returns a map or slice that the event holds.
- A key without a row in a preset table keeps its canonical path. "Under `attributes`" and
  "under `wlog`" name the object that holds those paths.
- A user key that has the same name as a key the preset writes moves to `wlog.fields.<key>`.
  So no user key replaces a key that a backend reads.
- `Apply` runs under recover. A panic writes the canonical event and reports `WLOG_HOOK_PANIC`
  with the preset name.
- A drain can call `Apply` on the event it received, to encode its own body. `drain-elastic`
  uses `ECS()`, and `drain-file` and `drain-cloudwatch` take `WithPreset`.
- `setup.FromEnv` reads `WLOG_OUTPUT`. An unknown name reports `WLOG_INVALID_CONFIG` and keeps
  the default.

### flat

Every nested object becomes dotted keys at every depth, such as `http.status` and
`error.code`. An array stays an array, and objects inside an array stay nested. Lead:
`timestamp`, `level`, `summary`, `operation`, `kind`, `outcome`, `duration_ms`, `message`.

### otel

The preset follows the OTel log data model and semantic conventions v1.43.0. The OTel Collector
`filelog` receiver can parse each line, and `search-recipes` ships a tested operator
configuration for it. Lead: `timestamp`, `severity_text`, `severity_number`, `body`,
`event_name`.

| Canonical | OTel | Rule |
|---|---|---|
| `timestamp` | `timestamp` | RFC 3339 with nanoseconds |
| `level` | `severity_text`, `severity_number` | Text is the wlog level. Number: debug 5, info 9, warn 13, error 17 |
| `summary` | `body` | |
| `kind` | `event_name` | `wlog.` plus the kind, such as `wlog.request` |
| `trace.trace_id`, `trace.span_id` | `trace_id`, `span_id` | Lowercase hex |
| `service.name`, `service.version` | `resource.service.name`, `resource.service.version` | |
| `service.env`, `service.instance` | `resource.deployment.environment.name`, `resource.service.instance.id` | |
| `event_id` | `attributes.log.record.uid` | |
| `error.code`, else `error.kind`, else `error.type` | `attributes.error.type` | If all three are empty, the value is `_OTHER` |
| `error.message`, `error.type`, `error.stack` | `attributes.exception.message`, `.exception.type`, `.exception.stacktrace` | |
| `http.method`, `http.path`, `http.status` | `attributes.http.request.method`, `.url.path`, `.http.response.status_code` | |
| `http.scheme`, `http.host` | `attributes.url.scheme`, `.server.address`, `.server.port` | If the host has a port, `server.port` holds it |
| `http.protocol` | `attributes.network.protocol.name`, `.network.protocol.version` | `HTTP/1.1` gives `http` and `1.1` |
| `http.client_ip`, `http.user_agent` | `attributes.client.address`, `.user_agent.original` | |
| `http.bytes_in`, `http.bytes_out` | `attributes.http.request.body.size`, `.http.response.body.size` | |
| `http.request_headers.<name>` | `attributes.http.request.header.<name>` | A one-item string array |
| `rpc.system`, `rpc.status_code` | `attributes.rpc.system.name`, `.rpc.response.status_code` | |
| `rpc.service`, `rpc.method` | `attributes.rpc.method` | `{service}/{method}` |
| `messaging.system`, `.destination`, `.consumer_group`, `.message_id` | `attributes.messaging.system`, `.messaging.destination.name`, `.messaging.consumer.group.name`, `.messaging.message.id` | |
| `messaging.operation` | `attributes.messaging.operation.type` | `publish` becomes `send` |
| `messaging.partition`, `messaging.batch_size` | `attributes.messaging.destination.partition.id`, `.messaging.batch.message_count` | The partition is a string |
| `messaging.offset` | `attributes.messaging.kafka.offset` | Only for system `kafka` |
| `faas.name`, `.version`, `.trigger`, `.invocation_id`, `.cold_start` | `attributes.faas.name`, `.faas.version`, `.faas.trigger`, `.faas.invocation_id`, `.faas.coldstart` | |
| `faas.memory_mb`, `faas.region` | `attributes.faas.max_memory`, `.cloud.region` | Memory in bytes |
| `user.id`, `user.email`, `user.name` | `attributes.user.id`, `.user.email`, `.user.name` | |
| `llm.provider`, `llm.operation` | `attributes.gen_ai.provider.name`, `.gen_ai.operation.name` | |
| `llm.request_model`, `.response_model`, `.response_id`, `.finish_reasons` | `attributes.gen_ai.request.model`, `.gen_ai.response.model`, `.gen_ai.response.id`, `.gen_ai.response.finish_reasons` | |
| `llm.input_tokens`, `.output_tokens` | `attributes.gen_ai.usage.input_tokens`, `.gen_ai.usage.output_tokens` | |
| `llm.cache_read_input_tokens`, `.cache_write_input_tokens`, `.reasoning_tokens` | `attributes.gen_ai.usage.cache_read.input_tokens`, `.gen_ai.usage.cache_write.input_tokens`, `.gen_ai.usage.reasoning.output_tokens` | Development upstream, pinned here |
| every other key | `attributes.<canonical dotted path>` | Arrays stay arrays |

A test reads `preset/testdata/semconv-1.43.0.tsv`, the attribute list from the Go
`semconv/v1.43.0` package. Every `attributes` name in this table outside `gen_ai.*` and
`http.request.header.*` must appear in that file. The `gen_ai.*` names are in
`preset/testdata/genai-names.txt`, from the GenAI conventions repository.

### ecs

The preset follows ECS 9.5.0 and the ecs-logging line rules. Lead: `@timestamp`, `log.level`,
`message`, `ecs.version`. These four are dotted top-level keys, and every other key is a nested
object.

| Canonical | ECS | Rule |
|---|---|---|
| `timestamp` | `@timestamp` | |
| `level` | `log.level` | |
| `summary` | `message` | |
| (constant) | `ecs.version` | `9.5.0` |
| `operation` | `event.action` | |
| `outcome` | `event.outcome` | `success` stays, `error` becomes `failure` |
| `duration_ms` | `event.duration` | Integer nanoseconds |
| `event_id` | `event.id` | |
| `error.message`, `error.type`, `error.code`, `error.stack` | `error.message`, `error.type`, `error.code`, `error.stack_trace` | |
| `service.name`, `service.version`, `service.env`, `service.instance` | `service.name`, `service.version`, `service.environment`, `service.node.name` | |
| `trace.trace_id`, `trace.span_id`, `trace.request_id` | `trace.id`, `span.id`, `http.request.id` | |
| `http.method`, `http.status`, `http.path`, `http.scheme` | `http.request.method`, `http.response.status_code`, `url.path`, `url.scheme` | |
| `http.host` | `url.domain`, `url.port` | If the host has a port, `url.port` holds it |
| `http.protocol` | `http.version` | `HTTP/1.1` gives `1.1` |
| `http.bytes_in`, `http.bytes_out` | `http.request.body.bytes`, `http.response.body.bytes` | |
| `http.client_ip` | `client.ip` | Only a value that `netip.ParseAddr` accepts. Other values go to `client.address` |
| `http.user_agent` | `user_agent.original` | |
| `user.id`, `user.email`, `user.name` | `user.id`, `user.email`, `user.name` | |
| user keys | `wlog.fields.<key>` | One namespace, so user keys never collide with ECS fields |
| every other key | `wlog.<canonical path>` | Such as `wlog.http.route`, `wlog.error.why`, `wlog.calls` |

### gcp

The preset follows the Google Cloud Logging special JSON fields that Cloud Run, GKE, and Cloud Run
functions read from stdout. Lead: `time`, `severity`, `message`.

| Canonical | Cloud Logging | Rule |
|---|---|---|
| `timestamp` | `time` | RFC 3339 with nanoseconds |
| `level` | `severity` | `DEBUG`, `INFO`, `WARNING`, `ERROR` |
| `summary` | `message` | |
| `trace.trace_id` | `logging.googleapis.com/trace` | `projects/{project}/traces/{id}` with a project, the bare id without one |
| `trace.span_id` | `logging.googleapis.com/spanId` | |
| `service.env`, `trace.request_id`, `kind`, `outcome` | `logging.googleapis.com/labels` | Keys `env`, `request_id`, `kind`, `outcome`. String values |
| `http.method`, `http.status`, `http.user_agent`, `http.client_ip`, `http.protocol` | `httpRequest.requestMethod`, `.status`, `.userAgent`, `.remoteIp`, `.protocol` | Status is an integer |
| `http.scheme`, `http.host`, `http.path` | `httpRequest.requestUrl` | `{scheme}://{host}{path}`. If the host is empty, the path alone |
| `http.bytes_in`, `http.bytes_out` | `httpRequest.requestSize`, `.responseSize` | Decimal strings. Google counts headers too, and wlog counts bodies only |
| `duration_ms` of a request | `httpRequest.latency` | Seconds with up to 9 decimals and an `s`, such as `0.8402s` |
| `http.request_headers.referer` | `httpRequest.referer` | If the header is not captured, the key is absent |
| `error.stack` | `stack_trace` | Error Reporting reads this key first |
| `service.name`, `service.version` | `serviceContext.service`, `.version` | Only on an event at level `error` with an `error` object |
| (constant) | `@type` | `type.googleapis.com/google.devtools.clouderrorreporting.v1beta1.ReportedErrorEvent`, on the same events |
| every other key | its canonical path in `jsonPayload` | Such as `operation`, `http.route`, `error.code` |

- The preset never writes `logging.googleapis.com/operation`, because Logs Explorer groups by it.
- A user key named `stream` moves to `wlog.fields.stream`, because GKE reserves `stream`.
- The Go stack formats that Error Reporting parses are not confirmed in Google's docs. The
  preset doc says so, and `@type` keeps error events in Error Reporting either way.

### datadog

The preset follows Datadog's reserved and standard log attributes. It never writes `host`,
because a JSON host removes the host tags that the Agent adds. Lead: `timestamp`, `status`,
`message`, `service`.

| Canonical | Datadog | Rule |
|---|---|---|
| `timestamp` | `timestamp` | |
| `level` | `status` | The status remapper reads `warn` as warning |
| `summary` | `message` | |
| `service.name` | `service` | |
| `service.env`, `service.version` | `dd.env`, `dd.version` | The names the Datadog Go tracer writes |
| `trace.trace_id`, `trace.span_id` | `trace_id`, `span_id` | Lowercase hex, 32 and 16 characters |
| `trace.request_id` | `http.request_id` | |
| `operation`, `outcome` | `evt.name`, `evt.outcome` | `error` becomes `failure` |
| `duration_ms` | `duration` | Integer nanoseconds |
| `error.code`, else `error.kind`, else `error.type` | `error.kind` | |
| `error.message`, `error.stack` | `error.message`, `error.stack` | |
| `http.method`, `http.status`, `http.user_agent` | `http.method`, `http.status_code`, `http.useragent` | |
| `http.path` | `http.url_details.path` | |
| `http.scheme`, `http.host`, `http.path` | `http.url` | If the host is empty, the key is absent |
| `http.client_ip` | `network.client.ip` | |
| `http.bytes_in`, `http.bytes_out` | `network.bytes_read`, `network.bytes_written` | |
| `user.id`, `user.email`, `user.name` | `usr.id`, `usr.email`, `usr.name` | |
| (constant) | `logger.name` | `wlog` |
| every other key | its canonical path | Datadog reads nested JSON |

### emf

The preset writes CloudWatch Embedded Metric Format, so Lambda and the CloudWatch agent extract
metrics from stdout with no API call. Lead: `_aws`, `timestamp`, `level`, `summary`.

- The event is flattened like `flat`, because EMF reads only root keys. A dimension or metric name
  matches a literal root key such as `service.name`.
- `_aws.Timestamp` is the event start in epoch milliseconds.
- `_aws.CloudWatchMetrics` holds one directive with the namespace, the dimension sets, and the
  metrics. The default metrics are `duration_ms` with unit `Milliseconds` and `error_count`
  with unit `Count`. `error_count` is 1 for outcome `error` and 0 otherwise.
- The preset leaves out a dimension set with a missing key. With no set left, it writes one
  empty set. A dimension value is a string, cut to 1024 characters.
- An event of kind `log` gets no `_aws` object, because it has no duration.
- At most 30 keys per dimension set and 100 metrics apply. Extra ones are dropped at
  construction, and `EMF` reports `WLOG_INVALID_CONFIG`.
- Each unique set of dimension values is one billed custom metric. The default set is
  `service.name` and `operation`, and the doc says to keep dimension values low in count.

### Rename

`Rename(preset.Flat(), "summary", "msg")` renames a top-level key after `Apply`, and the lead
order uses the new name. If the count of names is odd or a name is empty, `Rename` returns the preset unchanged.
`WithOutput` then reports `WLOG_INVALID_CONFIG`.
## Success criteria

1. A golden test per kind holds one full event in JSON with the fixed key order. Each golden is
   valid against `schema/event.v1.json`. (core-shape, event-schema)
2. For a request that fails with a catalog error, `summary` equals
   `POST /orders/{id} 502 in 840.2ms: PAYMENT_DECLINED card declined (fix: Ask the customer for another card.) (order_id=4821)`.
   A masked value never appears in a summary, proven by the event-shape fuzz test.
3. The pretty console golden matches the layout above, and 100 concurrent events never mix
   lines.
4. With the writer blocked, 10,000 emits finish in under 1 second. The dropped lines appear in
   `Stats` and in one `WLOG_WRITER_DROPPED` report.
5. `wlog.Info(context.Background(), "started")` with no setup writes one line through
   `Default()`. `wlog.Error` with no event writes one `log` event at level `error`.
6. Each problem code has a test that triggers it, and a section in `docs/problems.md`.
7. With debug on, each drop reason reports `WLOG_EVENT_DROPPED` with that reason.
8. 60 `StartCall` records on one event keep 50 in `calls`. `call_stats` counts all 60, and
   `wlog.dropped_calls` is 10.
9. For each preset, a golden request event, a golden error event, and a golden `log` event
   match. The canonical event that reaches a drain is the same with every preset.
   (output-presets)
10. Every OTel attribute name in the preset table appears in `semconv-1.43.0.tsv` or
    `genai-names.txt`. A user key named `message` moves to `wlog.fields.message` in the ECS,
    GCP, and Datadog output.
11. The EMF golden has a valid `_aws` object for a request, and no `_aws` object for a `log`
    event. A dimension set with a missing key is left out.
12. With a head sampler that keeps 0%, a `Measurer` still receives 100 of 100 request events.
    A redactor value pattern that matches an operation masks it in `Measure` too.
13. `BenchmarkEmit_RequestEvent`: finalize, the ordered JSON writer, and the async queue together
    add at most 3µs p50 over phase 10.

## Testing

Black-box tests in `package wlog_test`, `schema_test`, and `preset_test`. Golden files live under
`testdata/shape/`, `testdata/pretty/`, and `preset/testdata/`. Each golden is written by hand
from this spec, never produced by the code under test. The event-shape fuzz test from phase 10
also fuzzes summaries and presets.

## Boundaries

- **Always:** change `schema/event.v1.json` in the same commit as any reserved key.
- **Ask first:** a new reserved key, a new problem code prefix, or a change to the key order.
- **Never:** let a preset change the map a drain receives. Never put an event value in a
  `Problem` message.

## Open questions

None.
