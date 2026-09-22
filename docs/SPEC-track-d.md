# Spec: messages, jobs, functions, and commands (track D)

> Phase 13 · depends on: `work`, `core-calls`, `propagate`, `pipeline`, the `work` and `calls`
> conformance suites. Own-module ids: `queue-kafkago`, `queue-sarama`, `queue-franz`,
> `queue-confluent`, `queue-watermill`, `queue-sqs`, `queue-nats`, `queue-amqp`, `queue-pubsub`,
> and `queue-cloudevents`. Also `job-asynq`, `job-river`, `job-temporal`, and `job-cron`. Also
> `faas-lambda`, `faas-gcf`, `command-cobra`, `command-urfave`, and `command-kong`. Project-wide
> rules in [SPEC.md](SPEC.md) apply. Facts come from [the async research](../tasks/research/async.md)
> of 2026-09-16.

## Objective

A consumer, a job worker, a function, and a command each emit one event per unit of work. The
event has the same core fields as an HTTP request. A producer records a call and sends trace context in the
message. Short-lived processes deliver their events before they exit.

## Shared rules

1. Only some libraries have a hook with a context and a result: Watermill, asynq, River, Temporal
   activities, CloudEvents, franz-go producers, and cron. The rest get a wrapper around a user
   handler with the signature `func(ctx, msg) error`, or a per-message helper for the user's loop.
2. The wrapper decides acknowledgement from the handler error, and only where the library leaves
   that choice to the handler. The adapter doc lists the exact ack call per outcome.
3. Every wrapper and hook recovers a panic, records it with a stack, flushes in a short-lived
   runtime, and panics again. Each library keeps its own panic behavior.
4. `outcome` stays `success` or `error`. A library that decides a state after the handler returns
   records it in the group's `result` field: `retry`, `snooze`, `cancel`, `discard`, `dead_letter`,
   or `requeue`. The adapter calls `SetLevel`, which wins over the error rule: `info` for a snooze,
   and `warn` for a cancel or revoke.
5. A message's `delivery_count` comes only from a library field: SQS `ApproximateReceiveCount`,
   JetStream `NumDelivered`, and Pub/Sub `DeliveryAttempt`. Kafka and core NATS have none. A job's
   `attempt` comes from asynq retry count plus 1, River `Attempt`, and Temporal `Attempt`. Cron has
   none. A field with no source stays empty.
6. `lag_ms` comes from the message time field of each library, listed per module.
7. Message keys, bodies, attributes other than trace headers, and command argument values are
   never captured by default.
8. No adapter sets a package-level variable in a library, such as `cobra.EnableTraverseRunHooks`,
   `cli.OsExiter`, or `sarama.Logger`.
9. The `propagate` carriers cover byte-slice header values (Kafka), case-sensitive maps (NATS), and
   typed attribute maps (SQS, SNS, AMQP). A reader also checks `googclient_traceparent` for Pub/Sub.
10. A library field outside the SPEC-work table goes under `<group>.<system>`. The sarama member id
    and generation go under `messaging.kafka`. JetStream stream, consumer, and sequence go under
    `messaging.nats`. AMQP exchange and routing key go under `messaging.rabbitmq`. The Watermill
    handler name goes under `messaging.watermill`, and the Temporal workflow type and id under
    `job.temporal`.

## Messages

| Module | Consumer entry | Producer entry | Group fields | Library floor, Go floor |
|---|---|---|---|---|
| `queue-kafkago` | `Consume(ctx, r *kafka.Reader, fn)` uses `FetchMessage`, then `CommitMessages` after success. Never `ReadMessage`, which commits before the handler | `Writer(w)` wraps `WriteMessages` and adds headers. With `Async`, the call ends in `Completion`. `Drain(w)` ships events to a topic | system `kafka`, destination topic, partition, offset, consumer_group `Config().GroupID`, offset lag `HighWaterMark - Offset - 1`, `lag_ms` from `Time` | kafka-go v0.4.48, Go 1.21 |
| `queue-sarama` | `Handler(fn)` implements `ConsumerGroupHandler`, owns the claim loop, and calls `MarkMessage` after success. `Message(session, msg)` is the helper for a user loop | `SyncProducer(p)` and `AsyncProducer(p)` wrappers add headers before send, because `ProducerInterceptor` has no context | system `kafka`, member id, generation, offset lag from `HighWaterMarkOffset`, `lag_ms` from `Timestamp` | sarama v1.45.1 (module `github.com/IBM/sarama`), Go 1.21 |
| `queue-franz` | `Record(r *kgo.Record)` helper for the poll loop, using `r.Context` set by the fetch hook | `Hooks()` returns `HookProduceRecordBuffered` and `HookProduceRecordUnbuffered`, a complete call with no app change | system `kafka`, consumer_group from `OptValue(kgo.ConsumerGroup)` | franz-go v1.18.1, Go 1.21 |
| `queue-confluent` | `Consume(ctx, c, fn)` around `ReadMessage` with a timeout loop | `Produce(p, msg, deliveryChan)` wrapper that ends on the delivery report | system `kafka` | confluent-kafka-go v2.12.0, Go 1.21, cgo only (`//go:build cgo` files, and a cgo-free `doc.go`) |
| `queue-watermill` | `router.AddMiddleware(Middleware())`, added first so it is outermost. If the PoisonQueue middleware sets `reason_poisoned`, it records `result` `dead_letter` | `PublisherDecorator()` times `Publish` and sets `traceparent` metadata | system mapped from the subscriber type name, destination from `SubscribeTopicFromCtx`, `message_id` from `UUID`, handler name | watermill v1.4.7, Go 1.21 |
| `queue-sqs` | `Receive(ctx, client, input, fn)` loop that requests `ApproximateReceiveCount` and `SentTimestamp`, deletes on success, and leaves a failed message for its visibility timeout | `SendMessage` and `Publish` for SNS through `client-aws`, plus a `traceparent` message attribute | system `aws_sqs` or `aws_sns`, destination from the queue URL, `delivery_count`, `lag_ms` from `SentTimestamp` | sqs v1.37.14 and sns v1.33.19, Go 1.21 |
| `queue-nats` | `Handler(fn)` for core `MsgHandler` and `JetStreamHandler(fn)` for `jetstream.MessageHandler`. JetStream acks on success, and `Nak`s on error unless `TermOn(errors...)` matches. `Drain(nc, subject)` ships events | `Publish` and `PublishMsg` wrappers add headers | system `nats`, destination subject, consumer_group queue group, JetStream stream, consumer, sequence, `delivery_count` from `NumDelivered`, `lag_ms` from `Timestamp` | nats.go v1.38.0, Go 1.21 |
| `queue-amqp` | `Consume(ctx, deliveries, queue, fn)` acks on success and `Nack(false, requeue)` on error, with `RequeueOn(errors...)` | `PublishWithContext` wrapper writes `traceparent` as a string header | system `rabbitmq`, destination queue, exchange, routing key, `redelivered`, and `lag_ms` from a producer-set `Timestamp` | amqp091-go v1.15.0, Go 1.21 |
| `queue-pubsub` | `Receive(ctx, sub, fn)` acks on nil and nacks on error. The callback runs concurrently | `Publish` wrapper clones `Attributes` before adding trace headers | system `gcp_pubsub`, subscription, `delivery_count` from `DeliveryAttempt`, `lag_ms` from `PublishTime` | pubsub/v2 v2.0.1, Go 1.23 |
| `queue-cloudevents` | `client.WithObservabilityService(Observability())`, with its own recover because the SDK skips the callback on a panic | `EventDefaulter()` sets the `traceparent` extension. `protocol.IsACK` decides success | `messaging.cloudevents.event_id`, `event_source`, `event_type`, and `event_subject`, and `lag_ms` from `Time()` | sdk-go v2.15.2, Go 1.21 |

## Jobs

`queue-kafkago` and `queue-nats` each give `Factory()` for `setup.With`. The Kafka drain reads
`WLOG_KAFKA_BROKERS` and `WLOG_KAFKA_TOPIC`. The NATS drain reads `WLOG_NATS_URL` and
`WLOG_NATS_SUBJECT`. SASL, TLS, and credentials need code.

| Module | Entry | Group fields and results | Library floor, Go floor |
|---|---|---|---|
| `job-asynq` | `mux.Use(Middleware())`, and `Handler(h)` for `Server.Run` without a mux. Producer: `Enqueue(ctx, client, task)` builds the task with `NewTaskWithHeaders` | system `asynq`, name `Type()`, id, queue, attempt `GetRetryCount + 1`, max attempts. `SkipRetry` gives `discard`, `RevokeTask` gives `cancel` | asynq v0.26.0 (first with task headers), Go 1.24 |
| `job-river` | `Config.Middleware` with a type that has both `Work` and `IsMiddleware`, so it also works as the older `WorkerMiddleware`. Producer: a `JobInsertMiddleware` writes `traceparent` into job metadata | system `river`, name `Kind`, id, queue, attempt, max attempts, `lag_ms` from `ScheduledAt`. `JobSnooze` gives `snooze`, `JobCancel` gives `cancel`, and a final failed attempt gives `discard` | river v0.14.1, Go 1.21. The package doc notes the MPL-2.0 license |
| `job-temporal` | `worker.Options.Interceptors` with a `WorkerInterceptor` that embeds the base types and wraps activities only | system `temporal`, name activity type, id, queue, attempt, workflow type and id, `lag_ms` from `StartedTime - ScheduledTime`. `ErrResultPending` is not a failure. Workflow code emits nothing, because wlog uses goroutines, clocks, and random ids that break replay | temporal sdk v1.33.1, Go 1.21 |
| `job-cron` | `cron.WithChain(Wrap(name, spec))` for a plain `cron.Job`, placed inside `SkipIfStillRunning`, or `Job(name, spec, fn)` for one function with a context and an error. Use one of the two, never both. `work.Ticker` covers a plain `time.Ticker` loop | system `cron`, name, schedule. `Stop()`'s context is the flush point | cron v3.0.1, Go 1.21 |

## Functions

### faas-lambda (package `wloglambda`, aws-lambda-go)

<!-- snippet:sketch -->
```go
func Wrap[TIn, TOut any](h func(context.Context, TIn) (TOut, error), opts ...Option) func(context.Context, TIn) (TOut, error)
func ProcessSQS(ctx context.Context, e events.SQSEvent, fn func(context.Context, events.SQSMessage) error) events.SQSEventResponse
func ProcessKinesis(...) events.KinesisEventResponse
func ProcessDynamoDB(...) events.DynamoDBEventResponse
func SIGTERMFlush(log *wlog.Logger) lambda.Option // lambda.WithEnableSIGTERM with a flush
```

- `Wrap` emits one `function` event per invocation. `faas.invocation_id` is the AWS request id,
  `faas.name` and `faas.version` come from `lambdacontext`, and `remaining_ms` from the context
  deadline. `trace` comes from the `x-amzn-trace-id` context value, read with `propagate.WithXRay`.
- `cold_start` is true for the first invocation in the process. An atomic flag in the wrapper
  tracks it, so it stays correct with `AWS_LAMBDA_MAX_CONCURRENCY` above 1.
- `Wrap` calls `log.Flush(ctx)` before it returns, and never `Close`. A panic flushes, then
  panics again.
- A known event type fills extra fields: API Gateway v1 and v2 and ALB fill the `http` group and
  status, and SQS, SNS, Kinesis, DynamoDB, and EventBridge fill `faas.trigger` and `batch_size`.
- `ProcessSQS` emits one `message` event per record, and returns `BatchItemFailures` for each
  record whose handler failed. The package doc says the event source needs
  `ReportBatchItemFailures`.
- DynamoDB `NewImage` and `OldImage` are never read.
- Floor: aws-lambda-go v1.54.0, Go 1.21. `examples/lambda` moves to a recipe on this module.

### faas-gcf (package `wloggcf`, functions-framework-go)

- `HTTP(fn)` wraps an HTTP function with `httpcore.NetHTTP`, and reads the execution id and
  `X-Cloud-Trace-Context`.
- `CloudEvent(fn)` wraps a CloudEvent function with the `queue-cloudevents` field set. The
  framework does not set up the context for CloudEvent functions, so the wrapper reads ids from
  the event itself.
- Both flush before returning.
- Floor: functions-framework-go v1.9.2, Go 1.21.

## Commands

| Module | Entry | Notes | Library floor, Go floor |
|---|---|---|---|
| `command-cobra` | `code := wlogcobra.Execute(ctx, root)` in `main`. It runs `ExecuteContextC`, records `cli.path` from the returned command and `exit_code` from the error, flushes, and returns the code for `os.Exit` | Persistent post hooks never run after an error, so the event never ends in a hook | cobra v1.2.0, Go 1.21 |
| `command-urfave` | `wlogurfave.Run(ctx, cmd, args)` sets the root `ExitErrHandler` to record and flush before `cli.HandleExitCoder` exits | urfave exits inside `Run` for an exit-coder error | urfave/cli v3.1.0, Go 1.22 |
| `command-kong` | `wlogkong.Run(ctx, parser, args)` uses `kong.New` and `Parse`, binds the context with `BindTo`, and passes `kong.Exit` a function that records and flushes | `kong.Parse` and `--help` exit inside the library | kong v1.9.0, Go 1.21 |

`cli.flags` lists flag names that were set, never their values. A usage error gives exit code 2
for cobra and urfave, and 80 for kong, and level `warn`.

If `WLOG_DRAINS` is set, `cmd/wlog` records its own runs through `work` kind `command`. It then
writes nothing to stdout and sends each event to those drains. A normal run prints nothing
extra. (PAR-38, BET-24)

## Success criteria

1. Each consumer and job module passes the `work` suite. Each producer passes the `calls` suite.
   The Kafka and NATS drains pass the core part of the `drain` suite and scenario 2.
2. A kafka-go consumer whose handler fails does not commit, and the next fetch sees the same
   offset.
3. A Watermill handler that fails into PoisonQueue records `result` `dead_letter` and level `error`.
4. An SQS message received for the third time records `delivery_count` 3.
5. A River job that snoozes records `result` `snooze` and level `info`.
6. A Temporal workflow replay emits no event, and each activity attempt emits one.
7. A Lambda handler behind `pipeline.Wrap` delivers every event across three warm invocations, and
   a panicking invocation still delivers its event.
8. `ProcessSQS` with one failing record of three returns that record's id in `BatchItemFailures`.
9. A cobra `RunE` error, a urfave exit-coder error, and a kong parse error each record the right
   exit code and flush before the process exits.
10. Each module passes `tools floor` at its library floor. `queue-confluent` builds and tests with
    cgo, and its cgo-free build passes.

## Testing

Brokers are fakes or in-memory where a library allows it: Watermill's go channel pub/sub, NATS
server embedded in tests, miniredis for asynq, River's test driver, Temporal's test suite, and
Lambda event fixtures. Kafka, RabbitMQ, SQS (LocalStack), and Pub/Sub (emulator) run only under
`make integration`.

## Boundaries

- **Always:** flush before a short-lived process returns or exits.
- **Ask first:** capturing any message key, body, or command argument.
- **Never:** call wlog core from Temporal workflow code, or set a library's package-level variable.

## Shipped API notes (2026-09-22)

The code follows the tables above, with these differences. Each one is deliberate.

- Every entry takes a `*wlog.Logger`. The tables show the short form.
- `queue-kafkago.Consume` takes a `Fetcher`, so a test drives it with a fake reader.
  `queue-kafkago.Enqueue` does not exist. `Writer` and `Drain` take a `*kafka.Writer`.
- `queue-sarama.AsyncProducer` takes the `*sarama.Config` and requires
  `Producer.Return.Successes`, because a call ends at the broker report.
- `queue-franz.Record` takes the logger and the client.
- `job-asynq.Enqueue` builds the task from a type name and a payload, because asynq has no
  header setter.
- `job-river.New` returns the worker middleware, and `InsertMiddleware` returns the insert
  middleware.
- `job-cron.Job` takes the logger, the name, and the schedule. `Wrap` is a per-entry wrapper,
  so a caller uses `Wrap` or `Job`, never both.
- `command-kong.Run` takes a grammar and `kong.Option` values.
- `queue-cloudevents.Recover` wraps one receiver function, because the observability hook
  cannot recover a panic. The setup in the package doc shows it.
- Each adapter holds its own carrier for its library, next to the shared carriers in
  `propagate`. Rule 9 names the shared ones only.
- `messaging.nats.subscription` records the pattern a core NATS subscription matched, under
  rule 10.
- `faas-lambda.ProcessSQS` and its siblings record `faas.batch_failures` on an open
  invocation event.
- `queue-sqs.SendMessage` records the queue call itself, and `client-aws` records the rpc
  call of the same send. Both stay, because a reader of the queue side and a reader of the
  transport side ask different questions.

## Open questions

None.
