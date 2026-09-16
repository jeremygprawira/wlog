# Spec: propagate and work

> Phase 11 · root module · depends on: `core-shape`, `core-calls`. Module ids: `propagate` in
> package `wlog/propagate`, and `work` in package `wlog/work`. Project-wide rules in
> [SPEC.md](SPEC.md) apply.

## Objective

Give every non-HTTP adapter the same two tools. `propagate` carries trace context across process
boundaries in any header format. `work` turns one unit of work into one event with the standard
fields for its kind. A Kafka consumer, a gRPC server, a cron job, a cobra command, and a Lambda
handler then emit the same core fields. Each adapter only maps its library onto these tools.

## propagate

```go
type Carrier interface {
	Get(key string) string
	Set(key, value string)
	Keys() []string
}

type HeaderCarrier http.Header        // net/http, gRPC metadata via adapter
type MapCarrier map[string]string     // SQS attributes, Pub/Sub attributes, CloudEvents
type BytesCarrier struct{ ... }       // Kafka and NATS []byte headers, built with NewBytesCarrier

type TraceContext struct {
	TraceID      string // 32 lowercase hex
	SpanID       string // 16 lowercase hex, this unit's span
	ParentSpanID string // the caller's span, when one came in
	Sampled      bool
	TraceState   string
	RequestID    string
}

func Extract(ctx context.Context, c Carrier, opts ...Option) context.Context
func Inject(ctx context.Context, c Carrier)
func FromContext(ctx context.Context) (TraceContext, bool)
func ContextWith(ctx context.Context, tc TraceContext) context.Context // replaces ids, also on the event
func WithB3() Option    // also read b3 single and multi headers
func WithXRay() Option  // also read X-Amzn-Trace-Id and AWSTraceHeader
```

- `Extract` reads `traceparent` and `tracestate` by W3C rules. It rejects version `ff`, all-zero
  ids, and bad lengths. It accepts a future version with extra fields. With no valid parent, it
  generates a trace id. It always generates a new span id for this unit.
- `X-Request-ID` counts only at 128 characters or fewer from `[A-Za-z0-9._:-]`. Otherwise,
  `RequestID` equals the trace id.
- B3 and X-Ray formats are read-only fallbacks, and only with their option. An X-Ray root
  `1-5759e988-bd862e3fe1be46a994272793` becomes the trace id `5759e988bd862e3fe1be46a994272793`.
- `Inject` writes `traceparent`, `tracestate`, and `X-Request-ID`. Inside a call from
  `wlog.StartCall`, the span id is the call's span id, so the next service links to that call.
- `ContextWith` replaces the unit's trace context and the `trace` group of the event on `ctx`. The
  trace-otel `Starter` uses it, so a recording OTel span wins over ids that `Extract` made.
- `baggage` is never read into the event, because it can hold personal data.
- `Get` matches keys without regard to case for `HeaderCarrier`, and exactly for other carriers.

## work

### Kinds and field groups

| Kind | Group | Fields | Operation |
|---|---|---|---|
| `rpc` | `rpc` | `system`, `service`, `method`, `status_code`, `protocol`, `peer`, `stream`, `messages_sent`, `messages_received`, `request_size`, `response_size` | `{service}/{method}` |
| `message` | `messaging` | `system`, `operation`, `destination`, `consumer_group`, `message_id`, `partition`, `offset`, `delivery_count`, `redelivered`, `result`, `lag_ms`, `batch_size`, `body_size` | `{operation} {destination}` |
| `job` | `job` | `system`, `name`, `id`, `queue`, `schedule`, `attempt`, `max_attempts`, `result`, `scheduled_at`, `lag_ms` | `job {name}` |
| `command` | `cli` | `name`, `path`, `args_count`, `flags`, `exit_code` | `{path}` |
| `function` | `faas` | `system`, `name`, `version`, `trigger`, `invocation_id`, `cold_start`, `remaining_ms`, `memory_mb`, `region`, `batch_size`, `batch_failures` | `function {name}` |
| `work` | none | none | the name given to `Start` |

- `messaging.operation` is `receive`, `process`, `publish`, or `settle`, as in OTel messaging
  conventions. `messaging.system` uses OTel values: `kafka`, `rabbitmq`, `aws_sqs`, `aws_sns`,
  `gcp_pubsub`, `nats`, `watermill`, `cloudevents`.
- `cli.flags` lists flag names that were set, never their values.
- A library field outside this table goes under `<group>.<system>`, such as
  `messaging.kafka.member_id`, `rpc.mcp.tool`, or `job.temporal.workflow_id`. The adapter spec
  lists each one.
- A message uses `delivery_count`, and a job uses `attempt`.
- A message key, a message body, and command argument values are never captured unless an
  adapter option opts in.
- Kind `request` belongs to `http-core`. It uses `httpcore.Exchange`, which applies the same level
  and outcome rules.

### API

```go
type Kind string

const (
	KindRequest  Kind = "request"
	KindRPC      Kind = "rpc"
	KindMessage  Kind = "message"
	KindJob      Kind = "job"
	KindCommand  Kind = "command"
	KindFunction Kind = "function"
	KindWork     Kind = "work"
)

type StatusClass int

const (
	StatusOK StatusClass = iota
	StatusClientError // warn
	StatusServerError // error
)

type Unit struct {
	Kind      Kind
	Operation string            // empty: built from Fields by the table above
	Fields    map[string]any    // the kind group, such as {"system": "kafka", ...}
	Carrier   propagate.Carrier // incoming headers, may be nil
	StartedAt time.Time         // zero: now. A message sets its enqueue time here for lag
}

func Start(ctx context.Context, log *wlog.Logger, u Unit) (context.Context, *Handle)
func Run(ctx context.Context, log *wlog.Logger, u Unit, fn func(context.Context) error, opts ...RunOption) error

func (h *Handle) Set(key string, value any)                 // one field in the kind group
func (h *Handle) Status(code string, class StatusClass)     // status_code or exit_code, and level
func (h *Handle) End(err error)                             // record err, pick level and outcome, emit

func RecoverPanics() RunOption // report the panic as an error, return it, and do not panic again
func Flush() RunOption         // call log.Flush before Run returns, for short-lived runtimes

func BatchEvent(ctx context.Context, log *wlog.Logger, u Unit, n int) (context.Context, *Handle)
func (h *Handle) Failed()      // count one failed item in batch_failures
func Ticker(ctx context.Context, log *wlog.Logger, name string, d time.Duration,
	fn func(context.Context) error) // one job event per tick, until ctx ends
```

- `Start` extracts trace context from `Carrier`, starts the event, sets `kind`, `operation`, and
  the group, and returns a context for the handler. A nil `log` means `wlog.Default()`.
- `End` records `err` through `wlog.Error`. It sets the level by the SPEC.md rule: an explicit
  `SetLevel` wins, then a recorded error gives `error`. Two cases give `warn` instead: a 4xx
  `ErrorInfo.Status`, and a client error class set by `Status`. Then the status class decides,
  then `info`. It emits once. A
  second `End` does nothing.
- `Run` calls `Start`, runs `fn`, and calls `End` with its error. By default, a panic in `fn` is
  recorded with its stack, the event emits, and the panic continues. `RecoverPanics()` returns
  the panic as an error instead.
- `StartedAt` in the past sets the group's `lag_ms` to the time between `StartedAt` and `Start`.
  The event `timestamp` stays the start of processing.
- A batch handler calls `BatchEvent` once, then `Start` once per message with the returned context.
  The parent event holds `batch_size` and `batch_failures`, and each message event links to it with
  `trace.parent_event_id`.
- `Ticker` runs `fn` on each tick of a `time.Ticker` as one `job` event with system `ticker`. It
  skips a tick while the previous run is still going, and records the skip in the next event.

### Status class tables

Adapters map their library's codes with these shared tables. Each adapter spec lists any extra
codes.

| Kind | Client error (warn) | Server error (error) |
|---|---|---|
| request | 400 to 499 | 500 to 599 |
| rpc (gRPC and Connect codes) | `Canceled`, `InvalidArgument`, `NotFound`, `AlreadyExists`, `PermissionDenied`, `FailedPrecondition`, `OutOfRange`, `Unauthenticated`, `Aborted`, `ResourceExhausted` | `Unknown`, `DeadlineExceeded`, `Unimplemented`, `Internal`, `Unavailable`, `DataLoss` |
| command | exit code 2 (usage error) | any other non-zero exit code |
| message, job, function | none | a returned error |

## Success criteria

1. `Extract` then `Inject` round-trips a valid `traceparent` with a new span id and the same trace
   id. It rejects the W3C test vectors for bad headers.
2. An X-Ray header and a B3 header each give the right trace id with their option, and nothing
   without it.
3. `Run` with a handler that returns an error emits one event with level `error`, the kind group,
   `operation` from the table, and `outcome` `error`.
4. `Run` with a panicking handler emits the event with a stack, and the panic reaches the caller.
   With `RecoverPanics()`, `Run` returns an error instead.
5. A message with `StartedAt` 2 seconds ago has `lag_ms` of about 2000, and its `timestamp` is
   the processing start.
6. `BatchEvent` with three messages, one failed, emits one parent event with `batch_size` 3 and
   `batch_failures` 1, and three linked message events. A `Ticker` with a slow `fn` skips a tick
   and records it.
7. A `StartCall` inside a handler, followed by `Inject`, writes a `traceparent` whose span id
   equals the call record's `span_id`.

## Testing

Black-box tests in `propagate_test` and `work_test`. The W3C trace context test vectors live in
`propagate/testdata/`, copied from the W3C test suite with its license noted.

## Boundaries

- **Always:** keep `propagate` free of any OpenTelemetry import. The `trace-otel` module bridges
  OTel spans.
- **Ask first:** a new kind, or a new field in a kind group.
- **Never:** read `baggage` into an event, or capture message keys, bodies, or argument values
  by default.

## Open questions

None.
