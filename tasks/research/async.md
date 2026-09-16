# Research: async work integrations for the `work` kit (messages, jobs, functions, commands)

Date: 2026-09-16. Toolchain used: go1.26.1 darwin/arm64.
Scratch module: `scratchpad/research/async-work/` (throwaway, not part of wlog).

## 0. Method and how to read this report

- Versions: `go list -m -versions` plus `go mod download -json <mod>@latest`. Source read from `~/go/pkg/mod`.
- `go` directive history: fetched the `.mod` file of every stable version from proxy.golang.org (`mods/*.txt`).
- **Effective Go floor** (the number that matters for a wlog adapter module): made a module with `go 1.21`,
  ran `GOTOOLCHAIN=local go get <mod>@<ver>`, and read the resulting `go` line. `go get` raises the line to the
  highest `go` directive in the selected build list, so this includes transitive deps (`floor/*.out`).
  "1.21" means nothing in the build list needs more than 1.21.
- API-floor versions: found by grepping older downloaded versions (bisect) or the upstream CHANGELOG. Each is marked
  with how it was found.
- Compile proof: `sigs/main.go` compiles adapter-shaped code against every library at its latest version. `go vet` passed. It covers these shapes:
  - Kafka: sarama handler and producer interceptor, the 4 franz-go hooks.
  - Queues: watermill middleware and publisher decorator, SQS attribute read, NATS core and JetStream handlers.
  - More queues: amqp091 delivery, Pub/Sub v2 Receive, CloudEvents ObservabilityService.
  - Jobs: asynq middleware, river middleware, Temporal worker and activity interceptor, cron JobWrapper.
  - Functions: Lambda generic wrapper with `lambda.StartHandlerFunc`, GCF CloudEvent.
  - It also runs three CLI behavior tests (cobra, urfave, kong). Their output is quoted in those sections.
- Anything not proven from source or a compile/run is marked **UNVERIFIED**.

wlog context that shapes the conclusions:
- The root module Go floor is 1.23 today (SPEC.md).
- CAPABILITIES says the root drops to 1.21. Each module's `go` line is "the lowest its code and dependencies need".
- Golden rules: no package-level mutable state. Never block or panic on a logging failure.

---

## 1. Summary table

| Library (module path) | Latest stable | License | `go` line at latest | API floor we need (evidence) | Newest version with effective floor 1.21 | Newest with effective floor ≤1.23 |
|---|---|---|---|---|---|---|
| github.com/segmentio/kafka-go | v0.4.51 | MIT | 1.23 | v0.4.11 for `Message.HighWaterMark` (bisect: absent v0.4.10, present v0.4.11). Headers/FetchMessage/CommitMessages already in v0.4.0. `Writer.Addr` in v0.4.1 | v0.4.48 (eff. 1.21) | v0.4.51 (eff. 1.23) |
| github.com/IBM/sarama | v1.60.2 | MIT | 1.25.0 | v1.40.0 (first tag whose go.mod says `module github.com/IBM/sarama`. v1.38.1 is still `Shopify/sarama`) | v1.45.1 (eff. 1.21) | v1.46.0 (eff. 1.23.0) |
| github.com/twmb/franz-go (pkg/kgo) | v1.21.7 | BSD-3-Clause | 1.25.0 | v1.11.2 (Record.Context added v1.8.0, HookFetchRecordUnbuffered bug fixed v1.11.2, both CHANGELOG). `Client.OptValue` needs v1.15.0 (present v1.15.0, absent v1.11.2) | v1.18.1 (eff. 1.21) | v1.19.5 (eff. **1.23.8**, patch-level) |
| github.com/confluentinc/confluent-kafka-go/v2 | v2.15.1 | Apache-2.0 | 1.25.0 | v2.0.2 (first v2 stable, go 1.14 line). Needs cgo | v2.12.0 (eff. 1.21) | v2.12.0 |
| github.com/ThreeDotsLabs/watermill | v1.5.3 | MIT | 1.25.0 | v1.2.0 has HandlerMiddleware, Message.SetContext, HandlerNameFromCtx, SubscribeTopicFromCtx (grep). `Router.AddConsumerHandler` only from v1.5.1 (absent v1.5.0) | v1.4.7 (eff. 1.21) | v1.5.1 (eff. 1.23.0) |
| github.com/aws/aws-sdk-go-v2/service/sqs | v1.52.0 | Apache-2.0 | 1.24 | v1.32.0 for `MessageSystemAttributeNames` (CHANGELOG 2024-05-08). Older versions use deprecated `AttributeNames` | v1.37.14 (eff. 1.21) | v1.42.22 (eff. 1.23) |
| github.com/aws/aws-sdk-go-v2/service/sns | v1.47.0 | Apache-2.0 | 1.24 | any v1 (Publish with MessageAttributes) | v1.33.19 (eff. 1.21) | v1.39.12 (eff. 1.23) |
| github.com/nats-io/nats.go (+ /jetstream) | v1.53.1 | Apache-2.0 | 1.25.0 | core: any recent. `jetstream` pkg exists in v1.28.0 as a "preview". The preview warning is gone by v1.33.1 (present in v1.32.0). `PushConsumer` present by v1.45.0 | v1.38.0 (eff. 1.21) | v1.48.0 (eff. 1.23.0) |
| github.com/rabbitmq/amqp091-go | v1.15.0 | BSD-2-Clause | 1.20 | v1.4.0 `PublishWithContext` (CHANGELOG #96), v1.9.0 `ConsumeWithContext` (CHANGELOG #192) | v1.15.0 (eff. 1.21) | v1.15.0 |
| cloud.google.com/go/pubsub/v2 | v2.7.0 | Apache-2.0 | 1.25.0 | v2.0.0 (2025-07-16) | none | v2.0.1 (eff. 1.23.0) |
| cloud.google.com/go/pubsub (v1) | v1.51.1 | Apache-2.0 | 1.25.0 | **Deprecated** (package doc: "use cloud.google.com/go/pubsub/v2", fixes until 2026-12-31) | v1.45.3 by `go` line (eff. not measured, UNVERIFIED) | v1.50.0 by `go` line |
| github.com/cloudevents/sdk-go/v2 | v2.16.2 | Apache-2.0 | 1.23.0 | v2.4.0 already has `client.ObservabilityService`, `WithEventDefaulter`, tracing extension (grep) | v2.15.2 (eff. 1.21) | v2.16.2 (eff. 1.23.0) |
| github.com/hibiken/asynq | v0.26.0 | MIT | 1.24.0 | v0.26.0 for `Task.Headers()` / `NewTaskWithHeaders` (CHANGELOG). Without headers: v0.25.0 (RevokeTask, IsPanicError) or v0.12.0 (GetQueueName) | v0.24.1 by `go` line (1.14), eff. UNVERIFIED | v0.25.1 (eff. 1.22). v0.26.0 eff. **1.24.0** |
| github.com/riverqueue/river | v0.47.0 | MPL-2.0 | 1.26.0 | v0.13.0 WorkerMiddleware (`Config.WorkerMiddleware`, now deprecated). v0.19.0 `Config.Middleware`. v0.21.0 HookWorkEnd, v0.24.0 HookWorkEnd gets JobRow (breaking). v0.41.0 `Config.Plugins` (all CHANGELOG) | v0.14.1 (eff. 1.21) | v0.24.0 (eff. 1.23.0). v0.16–v0.18 eff. 1.22.0 |
| go.temporal.io/sdk | v1.49.0 | MIT | 1.26.0 | v1.12.0 already has `interceptor.WorkerInterceptor`, `interceptor.Header` (grep) | v1.33.1 (eff. 1.21) | v1.41.1 (eff. 1.23.0) |
| github.com/robfig/cron/v3 | v3.0.1 (2020-01-04, unmaintained since) | MIT | 1.12 | v3.0.0 | v3.0.1 (eff. 1.21) | v3.0.1 |
| github.com/aws/aws-lambda-go | v1.55.0 | Apache-2.0 | **1.26** | v1.54.0 has `StartHandlerFunc` generics, `lambdacontext.MaxConcurrency`, `WithEnableSIGTERM`, `TenantID` (all absent in v1.30.0). `SQSEventResponse` already in v1.30.0 | v1.54.0 (eff. 1.21) | v1.54.0 |
| github.com/GoogleCloudPlatform/functions-framework-go | v1.9.2 | Apache-2.0 | 1.21 | v1.9.0 for `funcframework.ExecutionIDFromContext` (absent v1.8.1). `functions.CloudEvent` in v1.5.0 | v1.9.2 (eff. 1.21) | v1.9.2 |
| github.com/spf13/cobra | v1.10.2 | Apache-2.0 | 1.15 | v1.2.0 `ExecuteContextC` (absent v1.1.0). `SetContext` present v1.5.0 (absent v1.3.0, v1.4.0 not verified). `EnableTraverseRunHooks` v1.8.0 | v1.10.2 (eff. 1.21) | v1.10.2 |
| github.com/urfave/cli/v3 | v3.12.0 | MIT | 1.22 | v3.1.0 first stable (2025-03-31). `BeforeFunc` already returns `(context.Context, error)` there | **none** (every stable v3 is eff. 1.22) | v3.12.0 (eff. 1.22) |
| github.com/alecthomas/kong | v1.16.1 | MIT | 1.20 | v1.4.0 `AfterRun` hook (absent v1.3.0). v1.9.0 `kong.ExitCoder` + usage-error exit 80 (absent v1.8.0) | v1.16.1 (eff. 1.21) | v1.16.1 |

Key Go-floor takeaways:

- For adapters that must work at Go 1.21, pin these versions:
  - Kafka: kafka-go v0.4.48, sarama v1.45.1, franz-go v1.18.1, confluent v2.12.0.
  - Queues: watermill v1.4.7, sqs v1.37.14, sns v1.33.19, nats v1.38.0, amqp091 v1.15.0, cloudevents v2.15.2.
  - Jobs: river v0.14.1 (old `Config.WorkerMiddleware` only), temporal v1.33.1, cron v3.0.1.
  - Functions and CLI: lambda v1.54.0, GCF v1.9.2, cobra and kong at latest.
- Cannot reach 1.21: Pub/Sub v2 (1.23.0), urfave/cli v3 (1.22), asynq with headers (1.24.0).
- franz-go v1.19.x needs `go 1.23.8`. Pinning v1.19.5 forces a patch-level `go` line.
- Pinning an old floor in `require` does not stop users from building with the latest. CI must test the floor and the latest.

---

## 2. OpenTelemetry semantic conventions mapping

Source: `go.opentelemetry.io/otel@v1.46.0/semconv/v1.43.0` (`SchemaURL = https://opentelemetry.io/schemas/1.43.0`).
All `messaging.*` and `faas.*` keys below are **Stability: Development**. `process.exit.code` is **Release_Candidate**.

| wlog draft field | OTel key (verified in semconv v1.43.0) | Notes |
|---|---|---|
| messaging.system | `messaging.system` | Enum values: `kafka`, `rabbitmq`, `aws_sqs`, `gcp_pubsub`. Others: `activemq`, `eventgrid`, `eventhubs`, `servicebus`, `jms`, `rocketmq`, `pulsar`. SNS is **`aws.sns`** with a dot, unlike `aws_sqs`. **No `nats` value**. Use `nats` (UNVERIFIED that the spec allows custom values) |
| destination | `messaging.destination.name` | topic / queue / subject |
| subscription | `messaging.destination.subscription.name` | Pub/Sub subscription, SNS->SQS |
| operation | `messaging.operation.type` = `create`/`send`/`receive`/`process`/`settle`, `messaging.operation.name` (free text, examples "ack", "nack", "send") | consumer event = `process`, producer call = `send` |
| message_id | `messaging.message.id` | |
| partition | `messaging.destination.partition.id` (string) | |
| offset | `messaging.kafka.offset` | Kafka only |
| key | `messaging.kafka.message.key` | Kafka only. Redaction concern: keys can hold user ids |
| consumer_group | `messaging.consumer.group.name` | Kafka group, NATS queue group / durable consumer, SQS n/a |
| batch size | `messaging.batch.message_count` | Lambda SQS/Kinesis batches |
| body size | `messaging.message.body.size` | |
| conversation id | `messaging.message.conversation_id` | AMQP CorrelationId |
| delivery/attempt count | **no generic key**. System-specific: `messaging.gcp_pubsub.message.delivery_attempt`, `messaging.servicebus.message.delivery_count` | wlog needs its own key, for example `messaging.delivery_count` or `job.attempt` |
| rabbit routing key | `messaging.rabbitmq.destination.routing_key`, `messaging.rabbitmq.message.delivery_tag` | |
| pubsub ordering key | `messaging.gcp_pubsub.message.ordering_key` | |
| lag_ms | **none** | wlog-specific |
| outcome | **none** for messaging. `error.type` exists | wlog-specific |
| faas cold start | `faas.coldstart` | **Mismatch**: current `examples/lambda` emits `faas.cold_start` |
| faas request id | `faas.invocation_id` | current example emits `faas.request_id` |
| faas name/version | `faas.name`, `faas.version`, `faas.instance`, `faas.max_memory` | |
| faas trigger | `faas.trigger` = `datasource`/`http`/`pubsub`/`timer`/`other` | SQS/SNS = pubsub, DynamoDB/Kinesis = datasource (mapping UNVERIFIED against spec prose), EventBridge schedule = timer |
| faas cron / time | `faas.cron`, `faas.time` | |
| lambda arn | `aws.lambda.invoked_arn`, `aws.request_id` | |
| cloud | `cloud.provider`, `cloud.region`, `cloud.account.id`, `cloud.platform`, `cloud.resource_id` | |
| cloudevents | `cloudevents.event_id`, `cloudevents.event_source`, `cloudevents.event_spec_version`, `cloudevents.event_subject`, `cloudevents.event_type` | |
| CLI exit code | `process.exit.code` (int) | also `process.command`, `process.command_args`, `process.command_line` (args can leak secrets, redact) |
| jobs (asynq/river/temporal/cron) | **no job semconv** in v1.43.0 (only `cicd.pipeline.*`) | wlog `job.*` group is free to define |

---

## 3. Kafka

### 3.1 segmentio/kafka-go (v0.4.51)

Signatures (reader.go, writer.go):

```go
func (r *Reader) FetchMessage(ctx context.Context) (Message, error)   // no commit
func (r *Reader) ReadMessage(ctx context.Context) (Message, error)    // FetchMessage + CommitMessages when GroupID != ""
func (r *Reader) CommitMessages(ctx context.Context, msgs ...Message) error
func (r *Reader) Config() ReaderConfig          // GroupID, Topic, GroupTopics
func (r *Reader) Lag() int64                    // -1 when GroupID is set
func (w *Writer) WriteMessages(ctx context.Context, msgs ...Message) error
type Message struct { Topic string; Partition int; Offset, HighWaterMark int64; Key, Value []byte; Headers []Header; WriterData interface{}; Time time.Time }
type Header = protocol.Header // struct{ Key string; Value []byte }
```

No hooks or middleware. Adapter = helper around the fetch loop (`Consume(ctx, r, handler)`) and a writer helper.

Field sources:
- destination: `msg.Topic`. partition: `msg.Partition`. offset: `msg.Offset`. key: `msg.Key`.
- consumer_group: `r.Config().GroupID`.
- lag (offsets): `msg.HighWaterMark - msg.Offset - 1`. Source: FetchMessage sets `r.lag = watermark - (Offset+1)`, and `Batch.ReadMessage` copies `batch.highWaterMark` into every message. `Reader.Lag()` is -1 with groups, so use the per-message value.
- lag_ms: `time.Since(msg.Time)`. `msg.Time = makeTime(timestamp)` from the record. It is producer CreateTime or broker LogAppendTime by topic config (Kafka semantics UNVERIFIED in this client).
- message_id: Kafka has none. Use topic/partition/offset.
- delivery count: Kafka has none. Redelivery happens after restart/rebalance from last committed offset.

Trace propagation: read `msg.Headers` (loop, `Key == "traceparent"`). Write by appending `kafka.Header{Key, Value}` before `WriteMessages`. Header keys are case-sensitive byte comparison (plain slice, no helper).

Outcome / ack:
- Kafka has no nack. "Ack" = `CommitMessages`. Committing the highest offset commits all earlier ones on that partition (doc comment).
- `ReadMessage` commits **before** the handler runs (doc says so). The adapter must use FetchMessage, then commit after success.
- Handler error: do not commit, the loop decides retry or skip. Outcome field = error, not a broker state.
- `WriteMessages` with a canceled ctx: the doc says there are "no guarantees" and messages can still be written. The error can be `kafka.WriteErrors` (per message).
- `Writer.Async=true`: WriteMessages returns immediately, results come in `Completion func(messages []Message, err error)`. A producer call event must hook Completion to get the real result.

Gotchas: After `Close`, `FetchMessage` returns `io.EOF`. `CommitMessages` without GroupID returns error. Writer batches (BatchTimeout) so a synchronous WriteMessages call duration includes batching wait.

Kafka drain note (CAPABILITIES wants one): `Writer` is safe for concurrent use and batches, `Async` gives non-blocking writes (gate G3).

### 3.2 IBM/sarama (v1.60.2)

```go
type ConsumerGroupHandler interface {
    Setup(ConsumerGroupSession) error
    Cleanup(ConsumerGroupSession) error
    ConsumeClaim(ConsumerGroupSession, ConsumerGroupClaim) error
}
type ConsumerGroupSession interface { Claims() map[string][]int32; MemberID() string; GenerationID() int32;
    MarkOffset(topic string, partition int32, offset int64, metadata string); Commit();
    ResetOffset(topic string, partition int32, offset int64, metadata string);
    MarkMessage(msg *ConsumerMessage, metadata string); Context() context.Context }
type ConsumerGroupClaim interface { Topic() string; Partition() int32; InitialOffset() int64; HighWaterMarkOffset() int64; Messages() <-chan *ConsumerMessage }
type ConsumerMessage struct { Headers []*RecordHeader; Timestamp, BlockTimestamp time.Time; Key, Value []byte; Topic string; Partition int32; Offset int64 }
type RecordHeader struct { Key, Value []byte }
type ConsumerInterceptor interface { OnConsume(*ConsumerMessage) }   // Config.Consumer.Interceptors
type ProducerInterceptor interface { OnSend(*ProducerMessage) }      // Config.Producer.Interceptors
type ProducerMessage struct { Topic string; Key, Value Encoder; Headers []RecordHeader; Metadata any; Offset int64; Partition int32; Timestamp time.Time; ... }
SyncProducer.SendMessage(msg *ProducerMessage) (partition int32, offset int64, err error) // no ctx
```

Where to hook:
- **Consumer: wrap the `ConsumerGroupHandler`**, not the ConsumerInterceptor. `OnConsume` runs in `partitionConsumer.interceptors` before the message reaches the channel. It has no ctx, no handler result, no timing. It can only read headers.
- The adapter cannot wrap per-message processing from outside `ConsumeClaim` because the user loop reads `claim.Messages()` itself. Two options:
  - (a) A helper the user calls per message inside their loop (`ctx, end := work.Message(session.Context(), msg)`).
  - (b) A handler type that takes `func(ctx, *ConsumerMessage) error` and owns the loop. Then it also owns MarkMessage.
  - UNVERIFIED which upstream otel contrib uses, not read.
- **Producer: `ProducerInterceptor.OnSend` can inject headers but has no ctx**, so it cannot read the caller's trace. It runs in the async dispatcher goroutine (`async_producer.go` dispatcher loop). Injection must happen in a wrapper before `SendMessage` / `Input() <-`.

Field sources:
- topic `msg.Topic` or `claim.Topic()`, partition `msg.Partition`, offset `msg.Offset`.
- offset lag: `claim.HighWaterMarkOffset() - msg.Offset - 1`. The claim doc calls the HWM the "offset that will be used for the next message".
- lag_ms: `time.Since(msg.Timestamp)`. Kafka sets the timestamp only for 0.10+.
- consumer group: the group id passed to `NewConsumerGroup`. The session does not expose it (UNVERIFIED that no getter exists).
- member id `session.MemberID()`, generation `session.GenerationID()`.

Headers: consume `[]*RecordHeader` (pointers), produce `[]RecordHeader` (values). Keys are `[]byte`. Producing headers requires `Config.Version >= V0_11_0_0`, otherwise the message fails with `ConfigurationError("Producing headers requires Kafka at least v0.11")`. Default `Config.Version`: `V2_8_0_0` in v1.60.2, `V2_1_0_0` in v1.45.1, `V1_0_0_0` in v1.40.0, so all floors are fine by default.

Outcome: no nack. Success = `session.MarkMessage(msg, "")` (marks, auto-commit flushes later). The MarkOffset doc warns that a crash can lose the mark before commit. Rewind = `ResetOffset`. Handler error = do not mark.

Gotchas:
- `ConsumeClaim` must return after `session.Context()` is done, not only after Messages() closes (handler doc comment).
- Must finish within `Config.Consumer.Group.Session.Timeout` before rebalance (claim doc).
- Interceptors run inside `safelyApplyInterceptor` with `recover`, so a panicking interceptor is logged and swallowed.
- Module path moved from `github.com/Shopify/sarama` at v1.40.0. Users on Shopify path need a different import (not supported by one module).

### 3.3 twmb/franz-go pkg/kgo (v1.21.7)

```go
type HookProduceRecordBuffered   interface { OnProduceRecordBuffered(*Record) }
type HookProduceRecordUnbuffered interface { OnProduceRecordUnbuffered(*Record, error) }
type HookFetchRecordBuffered     interface { OnFetchRecordBuffered(*Record) }
type HookFetchRecordUnbuffered   interface { OnFetchRecordUnbuffered(r *Record, polled bool) }
kgo.WithHooks(hooks ...Hook) Opt
type Record struct { Key, Value []byte; Headers []RecordHeader; Timestamp time.Time; Topic string; Partition int32;
    Attrs RecordAttrs; ProducerEpoch int16; ProducerID int64; LeaderEpoch int32; Offset int64; Context context.Context }
type RecordHeader struct { Key string; Value []byte }
func (cl *Client) Produce(ctx context.Context, r *Record, promise func(*Record, error))
func (cl *Client) ProduceSync(ctx context.Context, rs ...*Record) ProduceResults
func (cl *Client) PollFetches(ctx context.Context) Fetches
func (fs Fetches) EachRecord(fn func(*Record)); func (fs Fetches) EachPartition(fn func(FetchTopicPartition)); func (fs Fetches) RecordsAll() iter.Seq[*Record]
type FetchPartition struct { Partition int32; Err error; HighWatermark, LastStableOffset, LogStartOffset int64; Records []*Record }
func (cl *Client) CommitRecords(ctx context.Context, rs ...*Record) error
func (cl *Client) MarkCommitRecords(rs ...*Record)   // only with AutoCommitMarks()
func (cl *Client) GroupMetadata() (memberID string, generation int32)
func (cl *Client) OptValue(opt any) any              // cl.OptValue(kgo.ConsumerGroup) -> group name string
```

Per-record ctx:
- **Produce**: `produce()` sets a nil `r.Context` to `ctx`, **then** calls `OnProduceRecordBuffered` (producer.go). So the produce hook can read the caller ctx and inject `traceparent` into `r.Headers`, and start a timer. `OnProduceRecordUnbuffered(r, err)` fires just before the promise, giving outcome and partition/offset. This pair is a complete producer "call" with no user code change.
- **Fetch**: `OnFetchRecordBuffered` can extract headers and set `r.Context` (doc: "It can also be set in a consumer hook to propagate enrichment to consumer clients"). `OnFetchRecordUnbuffered(r, polled=true)` fires on the polling goroutine **before the poll returns**, so it cannot see handler outcome or duration. For discarded records it fires async with polled=false.
- Therefore consumer processing events need a per-record helper the user calls in their loop: `ctx := r.Context` then `work.Message(ctx, r)` / `end(err)`.

Field sources:
- topic `r.Topic`, partition `r.Partition`, offset `r.Offset`, key `r.Key`, lag_ms `time.Since(r.Timestamp)`.
- offset lag: `p.HighWatermark - r.Offset - 1`. Record has no HWM, only FetchPartition has it, so use EachPartition.
- group `cl.OptValue(kgo.ConsumerGroup)` (doc example), member `GroupMetadata()`.

Outcome: `CommitRecords` / `MarkCommitRecords` (needs `AutoCommitMarks()`), else autocommit commits polled records. No nack. `BlockRebalanceOnPoll` + `AllowRebalance` matter for at-least-once (options exist, semantics not read, UNVERIFIED).

Gotchas: hooks run on client hot paths (must not block, G3). Before v1.11.2, a hook that implemented both `HookFetchRecordBuffered` and `HookFetchRecordUnbuffered` never got the Unbuffered call (CHANGELOG). The `kmsg` package is a separate module (`github.com/twmb/franz-go/pkg/kmsg v1.13.1`).

### 3.4 confluentinc/confluent-kafka-go/v2 (v2.15.1)

```go
type Message struct { TopicPartition TopicPartition; Value, Key []byte; Timestamp time.Time; TimestampType TimestampType; Opaque interface{}; Headers []Header; LeaderEpoch *int32 }
type TopicPartition struct { Topic *string; Partition int32; Offset Offset; Metadata *string; Error error; LeaderEpoch *int32 }
type Header struct { Key string; Value []byte }
func (c *Consumer) ReadMessage(timeout time.Duration) (*Message, error)   // no ctx
func (c *Consumer) Poll(timeoutMs int) Event
func (c *Consumer) CommitMessage(m *Message) ([]TopicPartition, error)
func (c *Consumer) StoreMessage(m *Message) ([]TopicPartition, error)
func (c *Consumer) GetWatermarkOffsets(topic string, partition int32) (low, high int64, err error)
func (c *Consumer) GetConsumerGroupMetadata() (*ConsumerGroupMetadata, error)
func (p *Producer) Produce(msg *Message, deliveryChan chan Event) error   // async, no ctx
```

- cgo: README "CGO_ENABLED must NOT be set to 0". Default build links a bundled static librdkafka for darwin amd64/arm64, glibc linux amd64/arm64/s390x, windows amd64. `-tags musl` for Alpine. `-tags dynamic` links system librdkafka via pkg-config (needed for GSSAPI/Kerberos).
- **Verified**: `CGO_ENABLED=0 go build` of a program using `kafka.Message` fails with `undefined: kafka.Message` (the package compiles to almost nothing). So `queue/confluent` needs `//go:build cgo` on adapter files plus a cgo-free `doc.go`, and CI `go test ./...` with cgo off must skip it.
- Headers: Produce doc says `msg.Headers` requires librdkafka >= 0.11.4 and broker >= 0.11.0.0. `produceBatch` does not support headers.
- No hooks/interceptors in the Go client. Adapter = helpers around ReadMessage/Poll and Produce + delivery report (`deliveryChan` or `Events()`).
- Fields: topic `*m.TopicPartition.Topic`, partition, offset `int64(m.TopicPartition.Offset)`, lag_ms `time.Since(m.Timestamp)`, offset lag via `GetWatermarkOffsets` (cached, no network, UNVERIFIED) minus offset.
- ReadMessage returns `(msg, err)` for partition errors and `(nil, err)` with `err.(kafka.Error).IsTimeout()` on timeout (doc).

---

## 4. ThreeDotsLabs/watermill (v1.5.3)

```go
type HandlerFunc func(msg *Message) ([]*Message, error)
type NoPublishHandlerFunc func(msg *Message) error
type HandlerMiddleware func(h HandlerFunc) HandlerFunc
type PublisherDecorator func(pub Publisher) (Publisher, error)
type SubscriberDecorator func(sub Subscriber) (Subscriber, error)
type Metadata map[string]string   // Get(key) string, Set(key, value)
type Message struct { UUID string; Metadata Metadata; Payload Payload; ... ctx context.Context }
func (m *Message) Context() context.Context   // never nil, defaults to Background
func (m *Message) SetContext(ctx context.Context)
func (m *Message) Copy() *Message               // drops ctx
func (m *Message) CopyWithContext() *Message
func HandlerNameFromCtx(ctx) string; SubscriberNameFromCtx; PublisherNameFromCtx; SubscribeTopicFromCtx; PublishTopicFromCtx
func (r *Router) AddMiddleware(m ...HandlerMiddleware)        // router level
func (h *Handler) AddMiddleware(m ...HandlerMiddleware)       // handler level
func (r *Router) AddPublisherDecorators(dec ...PublisherDecorator)
func MessageTransformPublisherDecorator(transform func(*Message)) PublisherDecorator
Publisher.Publish(topic string, messages ...*Message) error   // no ctx argument
```

How it runs (router.go):
- The router wraps each handler's subscriber with a transform (`addHandlerContext`). It runs before user subscriber decorators. It puts these values into `msg.Context()`: handler name, publisher and subscriber type names, subscribe topic, publish topic. So middleware can read them with the `*FromCtx` helpers.
- Middleware order: the first added middleware runs first (outermost), per the router comment. Router-level and handler-level share one list in add order.
- `handleMessage` runs each message in its own goroutine. Handler error (except `context.Canceled` logging) → `msg.Nack()`. Panic → recovered, logged, `msg.Nack()`. Publishing produced messages fails → Nack. Success → `msg.Ack()`.

Field sources: messaging.system: not exposed generically. `SubscriberNameFromCtx` gives a Go type name like `kafka.Subscriber` (doc example) that can be mapped. destination `SubscribeTopicFromCtx`. message_id `msg.UUID` (watermill UUID, not the broker id). handler/operation `HandlerNameFromCtx`. Partition/offset: backend-specific ctx helpers in separate modules like watermill-kafka (**UNVERIFIED**, not downloaded). Delivery count: none generic.

Trace propagation: `msg.Metadata.Get/Set("traceparent")`. Each pubsub backend maps Metadata to broker headers (UNVERIFIED per backend). Producer: `MessageTransformPublisherDecorator` sees each message before Publish, reads `msg.Context()` and sets metadata. It cannot time the Publish call. A custom `PublisherDecorator` that wraps `Publish` can time it (the call covers all messages in the batch).

Retry and poison queue behavior (middleware/retry.go, poison.go):
- `Retry` calls the inner handler up to MaxRetries+1 times on the same `*Message`. `ResetContextOnRetry` restores the original ctx per attempt. If wlog middleware is **inside** Retry, it emits one event per attempt (good for attempt field, attempt number = own counter or `OnRetryHook`). If **outside**, one event for the whole retry sequence, with the last error.
- `PoisonQueue` publishes the failed message to a poison topic with metadata `reason_poisoned`, `topic_poisoned`, `handler_poisoned`, `subscriber_poisoned`, then **returns nil**. If wlog middleware is outside PoisonQueue, it sees success (message is acked). If inside, it sees the error. Recommend placing wlog outermost and having it detect poisoning, or document "add wlog after PoisonQueue". Poison publish failure returns `errors.Join(err, publishErr)`.
- `Recoverer` middleware turns panics into errors, otherwise the router recovers them outside all middleware (wlog must recover and re-panic to record a panic).

Gotcha: `Message.Copy()` drops ctx. Produced messages get handler context added by the router (`addHandlerContext(producedMessages...)`), not the consumed message's trace, unless the handler used `CopyWithContext` or set it.

---

## 5. AWS SQS and SNS (aws-sdk-go-v2)

SQS (v1.52.0):

```go
func (c *Client) ReceiveMessage(ctx, *ReceiveMessageInput, ...func(*Options)) (*ReceiveMessageOutput, error)
type ReceiveMessageInput struct { QueueUrl *string; AttributeNames []types.QueueAttributeName /*Deprecated*/; MaxNumberOfMessages int32;
    MessageAttributeNames []string; MessageSystemAttributeNames []types.MessageSystemAttributeName; ReceiveRequestAttemptId *string; VisibilityTimeout, WaitTimeSeconds int32 }
type types.Message struct { Attributes map[string]string; Body, MD5OfBody, MD5OfMessageAttributes *string;
    MessageAttributes map[string]MessageAttributeValue; MessageId, ReceiptHandle *string }
type types.MessageAttributeValue struct { DataType *string; BinaryValue []byte; StringValue *string; ... }
SendMessageInput { MessageBody, QueueUrl *string; DelaySeconds int32; MessageAttributes map[string]types.MessageAttributeValue;
    MessageDeduplicationId, MessageGroupId *string; MessageSystemAttributes map[string]types.MessageSystemAttributeValue }
DeleteMessage, ChangeMessageVisibility, SendMessageBatch (up to 10 messages)
```

System attribute names (enums.go): `All`, `SenderId`, `SentTimestamp`, `ApproximateReceiveCount`, `ApproximateFirstReceiveTimestamp`, `SequenceNumber`, `MessageDeduplicationId`, `MessageGroupId`, `AWSTraceHeader`, `DeadLetterQueueSourceArn`. For sends, only `AWSTraceHeader` is allowed in `MessageSystemAttributes` (doc: "the only supported message system attribute is AWSTraceHeader", value must be an X-Ray header).

- `Message.Attributes` holds **only the system attributes requested** in `MessageSystemAttributeNames` (doc: "A map of the attributes requested in ReceiveMessage"). The adapter must request `ApproximateReceiveCount`, `SentTimestamp` (or `All`), otherwise redelivery and lag are missing.
- `MessageAttributes` holds user attributes, only those named in `MessageAttributeNames` (use `All` or `.*`, UNVERIFIED exact wildcard syntax from source).
- attempt = `strconv.Atoi(Attributes["ApproximateReceiveCount"])` (starts at 1, AWS semantics UNVERIFIED). redelivered = count > 1.
- lag_ms = now − `SentTimestamp` (epoch ms, doc verified). Also `ApproximateFirstReceiveTimestamp`.
- destination = queue name parsed from QueueUrl. message_id = `MessageId`. FIFO: `MessageGroupId`, `SequenceNumber`.
- Trace: W3C goes in `MessageAttributes["traceparent"] = {DataType: "String", StringValue: ...}`. X-Ray uses the system attribute `AWSTraceHeader`. Limit of 10 message attributes per message: **UNVERIFIED** (AWS docs, not in SDK source).
- Outcome: success = `DeleteMessage(ReceiptHandle)`. Failure = no delete (visible again after VisibilityTimeout) or `ChangeMessageVisibility(0)` for fast retry. DLQ after maxReceiveCount is queue config (UNVERIFIED).
- There is no consumer loop in the SDK. The adapter is a receive-loop helper plus a per-message wrapper. For the producer "call", use the planned `client-aws` smithy middleware or wrap `SendMessage`.

SNS (v1.47.0): `Publish(ctx, *PublishInput)`, `PublishInput{ Message, MessageAttributes map[string]types.MessageAttributeValue, MessageDeduplicationId, MessageGroupId, MessageStructure, PhoneNumber, Subject, TargetArn, TopicArn }`. Trace = `MessageAttributes["traceparent"]`.
- SNS→SQS: attributes arrive as SQS MessageAttributes only with raw message delivery on, otherwise inside the JSON envelope body under `MessageAttributes` with `{Type, Value}`. **UNVERIFIED** (AWS docs). The SDK mentions `RawMessageDelivery` as a subscription attribute only. The Lambda SNS fixture shows the `{"Type","Value"}` shape (verified, `events/testdata/sns-event.json`).

---

## 6. NATS (nats-io/nats.go v1.53.1)

Core:

```go
type Msg struct { Subject, Reply string; Header Header; Data []byte; Sub *Subscription }
type Header map[string][]string   // Add/Set/Get/Values/Del, all CASE-SENSITIVE (doc)
type MsgHandler func(msg *Msg)
func (nc *Conn) Subscribe(subj string, cb MsgHandler) (*Subscription, error)
func (nc *Conn) QueueSubscribe(subj, queue string, cb MsgHandler) (*Subscription, error)
func (nc *Conn) PublishMsg(m *Msg) error          // no ctx
func (nc *Conn) RequestMsgWithContext(ctx, msg *Msg) (*Msg, error)
Subscription{ Subject string; Queue string; ... }
```

- Core NATS has no ack, no redelivery, no delivery count. Handler returns nothing, so outcome only comes from a wlog wrapper signature like `func(ctx, *nats.Msg) error`.
- Trace: `m.Header.Get("traceparent")`. Because Header is case-sensitive and not canonicalized, inject lowercase `traceparent` and read exactly that (and maybe `Traceparent`).
- consumer group = `m.Sub.Queue`. destination = `m.Subject` (or `m.Sub.Subject` for wildcard pattern = `messaging.destination.template`).
- `ErrNoResponders` for request/reply.

JetStream (new `jetstream` package):

```go
type Msg interface { Metadata() (*MsgMetadata, error); Data() []byte; Headers() nats.Header; Subject() string; Reply() string;
    Ack() error; DoubleAck(context.Context) error; Nak() error; NakWithDelay(time.Duration) error; InProgress() error; Term() error; TermWithReason(string) error }
type MsgMetadata struct { Sequence SequencePair{Consumer, Stream uint64}; NumDelivered, NumPending uint64; Timestamp time.Time; Stream, Consumer, Domain string }
type MessageHandler func(msg Msg)
Consumer.Consume(handler MessageHandler, opts ...PullConsumeOpt) (ConsumeContext, error)
Consumer.Messages(opts ...PullMessagesOpt) (MessagesContext, error)   // Next(opts ...NextOpt) (Msg, error)
Consumer.Fetch(batch int, opts ...FetchOpt) (MessageBatch, error); Consumer.Next(opts ...FetchOpt) (Msg, error)
PushConsumer.Consume(handler MessageHandler, opts ...PushConsumeOpt) (ConsumeContext, error)   // present by v1.45.0
Publisher.Publish(ctx, subject string, payload []byte, opts ...PublishOpt) (*PubAck, error)
Publisher.PublishMsg(ctx, msg *nats.Msg, opts ...PublishOpt) (*PubAck, error)
PublishAsync / PublishMsgAsync (PubAckFuture)
```

- `Metadata()` parses the **reply subject** (no network call). Errors with `ErrNotJSMessage` for non-JetStream messages.
- attempt = `md.NumDelivered` (doc: "number of times this message was delivered"). redelivered = NumDelivered > 1. lag_ms = now − `md.Timestamp` ("time the message was originally stored on a stream"). backlog = `md.NumPending`. stream = `md.Stream`, consumer = `md.Consumer`, sequence = `md.Sequence.Stream`.
- Outcome mapping:
  - success → `Ack()`.
  - retryable → `Nak()` or `NakWithDelay(d)`. `Nak()` redelivers at once and ignores AckWait and Backoff (doc).
  - permanent → `Term()` or `TermWithReason`. The reason needs server 2.10.4 or later.
  - Double ack returns `ErrMsgAlreadyAckd`. Default consumer `AckPolicy` is `AckExplicitPolicy` (doc in consumer_config.go).
- `MsgIdHdr = "Nats-Msg-Id"` is the dedup header. If it is present, use it as message_id. `Nats-Stream`, `Nats-Sequence`, `Nats-Time-Stamp` header constants exist for direct-get/republish.
- Legacy `nats.JetStreamContext` also has `(*nats.Msg).Metadata()`, `Ack/Nak/Term/InProgress` with the same MsgMetadata shape.
- Gotcha: the handler signature returns nothing. So wlog must offer `func(ctx, jetstream.Msg) error` and decide the ack from the error. No hook shows which ack the user called.

NATS drain: `PublishAsync` is non-blocking. Core `Publish` buffers in the client (flush semantics UNVERIFIED).

---

## 7. RabbitMQ (rabbitmq/amqp091-go v1.15.0)

```go
type Delivery struct { Acknowledger Acknowledger; Headers Table; ContentType, ContentEncoding string; DeliveryMode, Priority uint8;
    CorrelationId, ReplyTo, Expiration, MessageId string; Timestamp time.Time; Type, UserId, AppId string;
    ConsumerTag string; MessageCount uint32; DeliveryTag uint64; Redelivered bool; Exchange, RoutingKey string; Body []byte }
type Publishing struct { Headers Table; ContentType, ContentEncoding string; DeliveryMode, Priority uint8; CorrelationId, ReplyTo, Expiration, MessageId string; Timestamp time.Time; Type, UserId, AppId string; Body []byte }
type Table map[string]any
func (ch *Channel) Consume(queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args Table) (<-chan Delivery, error)
func (ch *Channel) ConsumeWithContext(ctx, queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args Table) (<-chan Delivery, error)
func (ch *Channel) PublishWithContext(ctx, exchange, key string, mandatory, immediate bool, msg Publishing) error
func (ch *Channel) PublishWithDeferredConfirmWithContext(ctx, exchange, key string, mandatory, immediate bool, msg Publishing) (*DeferredConfirmation, error)
func (d Delivery) Ack(multiple bool) error; Nack(multiple, requeue bool) error; Reject(requeue bool) error
```

- Table value types (doc): bool, byte, int8, float32, float64, int, int16, int32, int64, uint16, uint32, nil, string, time.Time, Decimal, Table, []byte, []any. Functions that take a table fail at once for a value of an unsupported type. Reading needs type assertions. **Put `traceparent` as `string`.** A header from another client can arrive as `[]byte` (UNVERIFIED), so read both.
- Fields:
  - destination = queue name. Delivery does not have it, so the adapter must receive it. Also `Exchange` and routing key `RoutingKey`.
  - message_id `MessageId` (app-set, often empty), conversation `CorrelationId`, delivery tag `DeliveryTag`, consumer `ConsumerTag`.
  - lag_ms from `Timestamp`. Only a producer that sets `Timestamp` makes this usable.
- Redelivery: only `Redelivered bool` from the protocol. Counts come from server headers: quorum queues `x-delivery-count`, DLX `x-death` (array of tables with `count`). **UNVERIFIED** (server behavior, the client source has no reference to either key, only `QueueTypeQuorum = "quorum"`).
- Outcome: success `Ack(false)`, retry `Nack(false, true)`, dead-letter/drop `Nack(false, false)` or `Reject(false)`. With `autoAck=true` the outcome cannot change delivery.
- Gotcha: `PublishWithContext` only reads `ctx.Done()` once before calling `Publish` (source). The ctx does not cancel a blocked publish (CHANGELOG issue #329 closed in v1.12.0, but v1.15.0 code still selects once). Publisher acks (confirm mode) need `PublishWithDeferredConfirmWithContext` + `DeferredConfirmation`.

---

## 8. Google Cloud Pub/Sub

**Current = `cloud.google.com/go/pubsub/v2`** (v2.7.0). The v1 package doc says the package is deprecated and tells all users to move to v2. It says v1 gets bug fixes and security patches until December 31st, 2026.

```go
func (c *Client) Subscriber(nameOrID string) *Subscriber
func (s *Subscriber) Receive(ctx context.Context, f func(context.Context, *Message)) error
func (s *Subscriber) ID() string; String() string
func (c *Client) Publisher(topicNameOrID string) *Publisher
func (t *Publisher) Publish(ctx context.Context, msg *Message) *PublishResult   // PublishResult.Get(ctx) (serverID string, err error)
type Message = ipubsub.Message   // cloud.google.com/go/internal/pubsub, shared by v1 and v2
type Message struct { ID string; Data []byte; Attributes map[string]string; PublishTime time.Time; DeliveryAttempt *int; OrderingKey string }
func (m *Message) Ack(); Nack(); AckWithResult() *AckResult; NackWithResult() *AckResult
```

v1 has the same `Message` alias, `(*Subscription).Receive(ctx, func(ctx, *Message))`, `(*Topic).Publish(ctx, *Message)`.

- Receive calls f **concurrently** from multiple goroutines (doc). Only one Receive per Subscriber at a time. After Receive's ctx is done, the ctx passed to f is canceled.
- The callback returns nothing. No hook shows whether the user called Ack or Nack. A wlog signature `func(ctx, *Message) error` can ack on nil and nack on error.
- attempt = `*m.DeliveryAttempt`. Doc: "If dead lettering is enabled, this will be set on all attempts, starting with value 1. Otherwise, the value will be nil."
- lag_ms = now − `PublishTime`. message_id = `ID`. subscription = `s.ID()`. destination topic: not on Message (subscription config, UNVERIFIED getter).
- Trace: `Attributes["traceparent"]`. **Gotcha: the client's built-in OTel tracing prefixes keys with `googclient_`.** The option is `ClientConfig.EnableOpenTelemetryTracing`. The source is trace.go `googclientPrefix`. So its header is `googclient_traceparent`. It clones Attributes before injecting (issue 11314). wlog must read `googclient_traceparent` as a fallback. It must clone the map before writing, because concurrent publishers share maps.
- The client's own spans use semconv v1.26.0 attrs including `messaging.gcp_pubsub.message.delivery_attempt`.
- Receive extends ack deadlines automatically up to `MaxExtension`. Attributes limit (100) UNVERIFIED.

---

## 9. CloudEvents (cloudevents/sdk-go/v2 v2.16.2)

Receiver fn signatures (client/receiver.go, reflection based):

```
func()                                      func() protocol.Result
func(context.Context)                       func(context.Context) protocol.Result
func(event.Event)                           func(event.Event) protocol.Result
func(context.Context, event.Event)          func(context.Context, event.Event) protocol.Result
func(event.Event) *event.Event              func(event.Event) (*event.Event, protocol.Result)
func(context.Context, event.Event) *event.Event
func(context.Context, event.Event) (*event.Event, protocol.Result)
type ReceiveFull func(context.Context, event.Event) protocol.Result
Client.Send(ctx, event.Event) protocol.Result; Request(ctx, event.Event) (*event.Event, protocol.Result); StartReceiver(ctx, fn interface{}) error
```

Hook: `client.WithObservabilityService(ObservabilityService)`:

```go
type ObservabilityService interface {
    InboundContextDecorators() []func(context.Context, binding.Message) context.Context
    RecordReceivedMalformedEvent(ctx context.Context, err error)
    RecordCallingInvoker(ctx context.Context, event *event.Event) (context.Context, func(errOrResult error))
    RecordSendingEvent(ctx context.Context, event event.Event) (context.Context, func(errOrResult error))
    RecordRequestEvent(ctx context.Context, event event.Event) (context.Context, func(errOrResult error, event *event.Event))
}
```

- `RecordCallingInvoker` returns a ctx that is passed to the user fn (invoker.go: `ctx, cb = RecordCallingInvoker(ctx, e); resp, result = r.fn.invoke(ctx, e); defer cb(result)`). Clean one-event-per-event hook.
- **Gotcha**: `defer cb(result)` is registered **after** `invoke`. A panic in the user fn skips `cb`. The invoker's own recover converts the panic to a result, but the observability callback misses it. wlog must recover itself.
- **Gotcha**: results are errors, and ACK is a non-nil `*protocol.Receipt{ACK: true}`. Use `protocol.IsACK(result)` (nil also counts as ACK), `IsNACK`, `IsUndelivered`. `if err != nil` misreports ACKed sends as failures.
- Sending: `RecordSendingEvent` gets the event **by value** and returns only a ctx, so it cannot inject `traceparent`. Use `client.WithEventDefaulter(func(ctx, event.Event) event.Event)`, which runs before `Validate` and send and can `e.SetExtension("traceparent", tp)`. Or set the extension before `Send`.
- Tracing extension: `extensions.TraceParentExtension = "traceparent"`, `TraceStateExtension = "tracestate"`, `GetDistributedTracingExtension(event)`. `client.WithTracePropagation()` is **Deprecated, a no-op**, with a comment that tells users not to use the distributed tracing extension to propagate traces (links spec). So prefer protocol headers (HTTP `traceparent`, Kafka header) captured through `InboundContextDecorators` (`binding.Message`), and fall back to the extension.
- Fields (accessor names are UNVERIFIED, not grepped):
  - `cloudevents.event_id` = `e.ID()`, `event_source` = `e.Source()`, `event_type` = `e.Type()`.
  - `event_subject` = `e.Subject()`, `event_spec_version` = `e.SpecVersion()`, lag = now − `e.Time()`.
- Concurrency: `WithPollGoroutines` default GOMAXPROCS. `WithBlockingCallback` serializes per poll goroutine.

---

## 10. hibiken/asynq (v0.26.0)

```go
type Handler interface { ProcessTask(context.Context, *Task) error }
type HandlerFunc func(context.Context, *Task) error
type MiddlewareFunc func(Handler) Handler
func (mux *ServeMux) Use(mws ...MiddlewareFunc)
func GetTaskID(ctx) (string, bool); GetRetryCount(ctx) (int, bool); GetMaxRetry(ctx) (int, bool); GetQueueName(ctx) (string, bool)
func (t *Task) Type() string; Payload() []byte; Headers() map[string]string; ResultWriter() *ResultWriter
func NewTask(typename string, payload []byte, opts ...Option) *Task                      // headers nil
func NewTaskWithHeaders(typename string, payload []byte, headers map[string]string, opts ...Option) *Task   // maps.Clone
func (c *Client) EnqueueContext(ctx, task *Task, opts ...Option) (*TaskInfo, error)
var SkipRetry, RevokeTask error; Config.IsFailure func(error) bool; Config.ErrorHandler ErrorHandler; ErrLeaseExpired
```

- Middleware only applies through `ServeMux.Handler` (servemux.go loops `mux.mws`). `Server.Run(handler)` with a plain HandlerFunc gets no mux middleware. Offer a `Handler` wrapper that works both ways.
- Fields: task id `GetTaskID`, queue `GetQueueName`, kind `t.Type()`, max = `GetMaxRetry`.
- attempt = `GetRetryCount + 1`. The doc defines the retry count as the number of retries so far. No enqueue time or lag in ctx (UNVERIFIED that no helper exists).
- Trace: `t.Headers()["traceparent"]`. Headers only exist from v0.26.0 (go 1.24.0 effective). `NewTask` sets headers to nil, and there is no setter, so the producer side must build tasks with `NewTaskWithHeaders`. There is no client middleware.
- Outcome (processor.go `handleFailedMessage`): `errors.Is(err, RevokeTask)` → marked done, no retry, no archive. `msg.Retried >= msg.Retry || errors.Is(err, SkipRetry)` → archived (dead). Else → retry, and `IsFailure(err)==false` means it does not increment retried or failure stats. nil → succeeded.
- **Gotcha: the processor can finish before the middleware.** exec() selects on `p.abort` (shutdown → requeue), `lease.Done()` (→ ErrLeaseExpired), `ctx.Done()` (deadline → failure) and the result channel. The handler goroutine keeps running. So the middleware's view of the error can differ from the recorded state, and the event can end after the task was already retried.
- **Gotcha: panics** are recovered in `perform`, outside the middleware, and become `*errors.PanicError` (`asynq.IsPanicError` from v0.25.0). wlog middleware must recover and re-panic to record them.
- `ErrorHandler.HandleError(ctx, task, err)` gets a new Task built from msg, not the one the middleware saw.
- IsFailure option added v0.18.5 (CHANGELOG placement).

---

## 11. riverqueue/river (v0.47.0)

```go
// rivertype
type Middleware interface { IsMiddleware() bool }
type WorkerMiddleware interface { Middleware; Work(ctx context.Context, job *JobRow, doInner func(context.Context) error) error }
type JobInsertMiddleware interface { Middleware; InsertMany(ctx, manyParams []*JobInsertParams, doInner func(context.Context) ([]*JobInsertResult, error)) ([]*JobInsertResult, error) }
type HookWorkBegin interface { Hook; WorkBegin(ctx, job *JobRow) error }
type HookWorkEnd   interface { Hook; WorkEnd(ctx, job *JobRow, err error) error }
type JobRow struct { ID int64; Attempt int; AttemptedAt *time.Time; AttemptedBy []string; CreatedAt time.Time; EncodedArgs []byte;
    Errors []AttemptError; FinalizedAt *time.Time; Kind string; MaxAttempts int; Metadata []byte; Priority int; Queue string;
    ScheduledAt time.Time; State JobState; Tags []string; UniqueKey []byte; UniqueStates []JobState }
type JobInsertParams struct { ...; Kind string; MaxAttempts int; Metadata []byte; Queue string; ... }
// river
river.Config{ Middleware []rivertype.Middleware; Hooks []rivertype.Hook; Plugins ...; WorkerMiddleware []rivertype.WorkerMiddleware /*Deprecated*/ }
river.MiddlewareDefaults, river.WorkerMiddlewareFunc, river.JobInsertMiddlewareFunc, river.HookWorkEndFunc
river.JobCancel(err) error; river.JobSnooze(d) error; river.MetadataSet(ctx, key, value) error
```

- Attempt semantics (JobRow doc): inserted at 0, incremented to 1 on first work, "Attempt will decrement on snooze".
- Execution order (internal/jobexecutor): middleware chain (global plugins, then per-worker) → doInner: HookWorkBegin → **args unmarshal** → job timeout ctx → `Work` → HookWorkEnd (each can replace err). So middleware runs before args are decoded (it sees `EncodedArgs` only).
- Outcome mapping (reportResult / reportError):
  - `JobSnoozeError` → scheduled again, `snoozes` counter in metadata, attempt decremented.
  - `JobCancelError` → `cancelled`. The ErrorHandler can also cancel (`SetCancelled`) **after** middleware returns.
  - soft stop (client shutdown, `context.Cause(ctx)` is ErrStop and err is Canceled) → made available again with attempt − 1, not an error.
  - error and `Attempt >= MaxAttempts` → `discarded`, else `retryable` with retry policy time.
  - nil → `completed`.
  - no registered worker → `UnknownJobKindError`. Middleware never runs.
- **Gotcha: panics are recovered in `execute`, outside the middleware chain**, then reported as PanicVal. Middleware must recover/re-panic to see them.
- Trace propagation: `JobRow.Metadata` is JSON `[]byte`. Producer side: a `JobInsertMiddleware` can edit `JobInsertParams.Metadata` before insert. Keys prefixed `river:` are reserved (MetadataSet doc). Worker side: parse `job.Metadata`. `MetadataSet` (v0.39.0) stages metadata. River writes it at the end of the attempt.
- Floors: `Config.WorkerMiddleware` works from v0.13.0 and still exists (deprecated) in v0.47.0. In v0.13/v0.14 `WorkerMiddleware` had no `IsMiddleware`, and `MiddlewareDefaults` did not exist. A type implementing both `Work` and `IsMiddleware()` directly compiles against both (proved against v0.47.0 in `sigs`, v0.14.1 signature identical by grep).
- Postgres driver: `riverpgxv5` pulls pgx v5 (module require), which drives the Go floor.
- River is pre-1.0 and MPL-2.0 (file-level copyleft, fine to depend on, note for license review).

---

## 12. Temporal (go.temporal.io/sdk v1.49.0)

```go
type interceptor.WorkerInterceptor interface {
    InterceptActivity(ctx context.Context, next ActivityInboundInterceptor) ActivityInboundInterceptor
    InterceptWorkflow(ctx workflow.Context, next WorkflowInboundInterceptor) WorkflowInboundInterceptor
    InterceptNexusOperation(ctx context.Context, next NexusOperationInboundInterceptor) NexusOperationInboundInterceptor
    mustEmbedWorkerInterceptorBase()
}
type ActivityInboundInterceptor interface { Init(outbound ActivityOutboundInterceptor) error; ExecuteActivity(ctx context.Context, in *ExecuteActivityInput) (any, error); mustEmbed... }
interceptor.WorkerInterceptorBase, interceptor.ActivityInboundInterceptorBase{Next}
func interceptor.Header(ctx context.Context) map[string]*commonpb.Payload
func interceptor.WorkflowHeader(ctx workflow.Context) map[string]*commonpb.Payload
func activity.GetInfo(ctx context.Context) activity.Info
func workflow.GetInfo(ctx workflow.Context) *workflow.Info
func workflow.IsReplaying(ctx workflow.Context) bool
worker.Options.Interceptors []interceptor.WorkerInterceptor; client.Options.Interceptors []interceptor.ClientInterceptor
```

- Must embed the Base structs (unexported `mustEmbed...` methods). Embedding gives forward compatibility (Nexus method added later, absent in v1.26.0).
- Client options interceptors that also implement WorkerInterceptor are used for workers too, and "wrap the ones" in worker options. Do not register the same one in both (doc).
- ActivityInfo fields: TaskToken, WorkflowType, WorkflowNamespace, WorkflowExecution{ID, RunID}, ActivityID, ActivityRunID, ActivityType{Name}, TaskQueue, Namespace, HeartbeatTimeout, ScheduleToCloseTimeout, StartToCloseTimeout, ScheduledTime, StartedTime, Deadline, **Attempt int32 ("starts from 1")**, IsLocalActivity, Priority, RetryPolicy.
- WorkflowInfo fields: WorkflowExecution, OriginalRunID, FirstRunID, WorkflowType, TaskQueueName, timeouts, Namespace, Attempt, WorkflowStartTime, CronSchedule, ContinuedExecutionRunID, Parent/RootWorkflowExecution, Memo, SearchAttributes, RetryPolicy, Priority, ...
- Header: `interceptor.Header(ctx)` is non-nil only inside `ActivityInboundInterceptor.ExecuteActivity`, `ClientOutboundInterceptor.ExecuteActivity`, `ExecuteWorkflow`, `SignalWithStartWorkflow` (doc). Values are `*commonpb.Payload` (go.temporal.io/api), so encode with `converter.GetDefaultDataConverter().ToPayload(map[string]string{...})` like the built-in tracer. Built-in tracing interceptor uses `TracerOptions.HeaderKey` (OTel contrib default `_tracer-data`, UNVERIFIED, only the option is verified). Reusing the same key keeps interop.
- Outcome: activity error → retry per RetryPolicy. `temporal.NewNonRetryableApplicationError` → no retry. `activity.ErrResultPending` → async completion, **not a failure**. `temporal.IsCanceledError`, `IsTimeoutError`, `IsApplicationError` exist.
- **Workflow rules (workflow/doc.go, verified text)**: execution must be deterministic and idempotent. The rules:
  - Use `workflow.Now` and `workflow.Sleep`, not `time`.
  - Use `workflow.Go`, `Channel`, and `Selector`, not goroutines, chan, and select.
  - Log through `workflow.GetLogger`. Do not range over maps.
  - Do not change external systems except through activities.
- The `IsReplaying` doc forbids commands based on the flag. It says custom logging must ignore its own failures.
- During replay, the built-in tracer returns a no-op span and skips signal and query tracing.
- So in workflow code wlog must NOT do these things:
  - start goroutines (drain or sampler workers), or read `time.Now`.
  - generate random ids with math/rand or crypto. They are not deterministic. The tracer uses an `IdempotencyKey` instead.
  - do network I/O, block, or iterate maps for output order.
- In workflow code wlog can do these things:
  - emit only for `!workflow.IsReplaying(ctx)`.
  - take time from `workflow.Now` and put fields from `workflow.GetInfo`.
- `workflow.Context` is not `context.Context`, so wlog's ctx-based API cannot attach directly (value via `workflow.WithValue`, UNVERIFIED as a design). Workflow task retries or worker cache eviction can re-run non-replayed code, so duplicates are possible (UNVERIFIED).
- Temporal SDK pulls grpc, gogo/api, nexus deps (heavy module, `go.temporal.io/api`).

---

## 13. robfig/cron/v3 (v3.0.1)

```go
type Job interface { Run() }
type FuncJob func()
type JobWrapper func(Job) Job
type Chain struct{...}; func NewChain(c ...JobWrapper) Chain; func (c Chain) Then(j Job) Job   // NewChain(m1,m2,m3).Then(job) == m1(m2(m3(job)))
func WithChain(wrappers ...JobWrapper) Option
func Recover(logger Logger) JobWrapper; DelayIfStillRunning(logger); SkipIfStillRunning(logger)
func (c *Cron) AddFunc(spec string, cmd func()) (EntryID, error); AddJob(spec string, cmd Job) (EntryID, error)
func (c *Cron) Stop() context.Context   // done when running jobs finish
type Entry struct { ID EntryID; Schedule Schedule; Next, Prev time.Time; WrappedJob Job; Job Job }
```

- `Run()` has no ctx, no error, no name, no schedule. A JobWrapper does not get the entry id or spec. The wrapper API has to take a name/spec itself: `cronwlog.Wrap(name, spec)` or `AddJob(spec, wlog.Job(name, fn func(ctx) error))`. Outcome from `error` return or panic only.
- **Gotcha: doc.go says "Recover any panics from jobs (activated by default)". But `New()` sets `chain: NewChain()`, which is empty.**
- Without `cron.Recover` from the user, a panicking job crashes the process. The wlog wrapper must record the panic, then re-panic to keep the user's choice.
- `SkipIfStillRunning` skips silently at Info level. A wlog wrapper outside it records a skipped run as success. So place wlog inside it. A "skipped" outcome is possible only from outside.
- `startJob` runs each job in its own goroutine. `Stop()` returns a ctx that is done after running jobs finish, which is the flush point for short-lived processes.
- Scheduled time vs actual start: `Entry.Prev` is available via `c.Entry(id)` only, not in the wrapper.

---

## 14. AWS Lambda (aws/aws-lambda-go v1.55.0)

`lambda.Start(handler interface{})` valid signatures (entry.go doc):

```
func ()                      func (TIn)                     func () error               func (TIn) error
func () (TOut, error)        func (TIn) (TOut, error)       func (context.Context)      func (context.Context) error
func (context.Context) (TOut, error)                        func (context.Context, TIn)
func (context.Context, TIn) error                           func (context.Context, TIn) (TOut, error)
```

TOut can implement io.Reader for raw bytes. The runtime calls Close on an io.Closer. Generic compile-time proof:
`lambda.StartHandlerFunc[TIn, TOut any, H HandlerFunc[TIn, TOut]](handler H, options ...Option)` with `HandlerFunc[TIn,TOut] interface{ func(context.Context, TIn) (TOut, error) }` (build tag go1.18). Options: `WithContext`, `WithSetEscapeHTML`, `WithUseNumber`, `WithDisallowUnknownFields`, `WithEnableSIGTERM(callbacks ...func())` ("SIGKILL will occur ~500ms after SIGTERM").

lambdacontext:

```go
type LambdaContext struct { AwsRequestID string; InvokedFunctionArn string; Identity CognitoIdentity; ClientContext ClientContext; TenantID string }
func FromContext(ctx) (*LambdaContext, bool)
package vars (set in init from env): FunctionName (AWS_LAMBDA_FUNCTION_NAME), FunctionVersion, MemoryLimitInMB, LogGroupName, LogStreamName
func MaxConcurrency() int   // AWS_LAMBDA_MAX_CONCURRENCY, default 1
```

Invoke loop (invoke_loop.go):
- ctx = `context.WithDeadline(baseContext, deadline)`, so **remaining time = `time.Until(deadline)` from `ctx.Deadline()`**.
- Trace id: `ctx.Value("x-amzn-trace-id")` (a **string key**, staticcheck nolint) always. Only for `MaxConcurrency()==1` does it set env `_X_AMZN_TRACE_ID`.
- **Concurrency**: with Go >= 1.22 builds, `MaxConcurrency() > 1` starts N goroutines each running the invoke loop (invoke_loop_gte_go122.go). Handlers run concurrently in one process (Lambda managed instances, platform name UNVERIFIED). Cold-start detection must be concurrency-safe, and env-based trace ids are wrong there.
- **Panics**: `callBytesHandlerFunc` recovers, reports the failure, and then the loop returns an error that says the process must exit after a handler panic. The process exits. The wrapper must flush in its own defer before re-panicking.
- Cold start: no API in the library. Detect "first invocation in this process" with state in the wrapper closure (current `examples/lambda` does this with a mutex, which fits the no-package-state rule). `AWS_LAMBDA_INITIALIZATION_TYPE` (on-demand / provisioned-concurrency / snap-start) is not referenced by the library, **UNVERIFIED**.

Events (package events, verified structs):
- `APIGatewayProxyRequest{Resource, Path, HTTPMethod, Headers, MultiValueHeaders, QueryStringParameters, MultiValueQueryStringParameters, PathParameters, StageVariables, RequestContext APIGatewayProxyRequestContext{Stage, DomainName, RequestID, ExtendedRequestID, Protocol, ResourcePath, Path, HTTPMethod, RequestTime, RequestTimeEpoch, APIID, ...}, Body, IsBase64Encoded}` → `APIGatewayProxyResponse{StatusCode, ...}`. Route = `RequestContext.ResourcePath`.
- `APIGatewayV2HTTPRequest{Version, RouteKey, RawPath, RawQueryString, Cookies, Headers, QueryStringParameters, PathParameters, RequestContext APIGatewayV2HTTPRequestContext{RouteKey, Stage, RequestID, APIID, DomainName, ...}, StageVariables, Body, IsBase64Encoded}` with HTTP details in type `APIGatewayV2HTTPRequestContextHTTPDescription{Method, Path, Protocol, SourceIP, ...}` (the RequestContext field name `HTTP` is UNVERIFIED) → `APIGatewayV2HTTPResponse{StatusCode}`.
- `ALBTargetGroupRequest{HTTPMethod, Path, QueryStringParameters, MultiValueQueryStringParameters, Headers, MultiValueHeaders, RequestContext{ELB ELBContext{TargetGroupArn}}, IsBase64Encoded, Body}` → `ALBTargetGroupResponse{StatusCode}`. ALB has no request id field in the event.
- `SQSEvent{Records []SQSMessage{MessageId, ReceiptHandle, Body, Md5OfBody, Md5OfMessageAttributes, Attributes map[string]string, MessageAttributes map[string]SQSMessageAttribute{StringValue *string, BinaryValue, StringListValues, BinaryListValues, DataType}, EventSourceARN, EventSource, AWSRegion}}`. Fixture shows `attributes` include `ApproximateReceiveCount`, `SentTimestamp`, `SenderId`, `ApproximateFirstReceiveTimestamp` (verified testdata).
- Batch responses (streams.go): `SQSEventResponse{BatchItemFailures []SQSBatchItemFailure{ItemIdentifier}}`, `KinesisEventResponse{...KinesisBatchItemFailure{ItemIdentifier}}`, `DynamoDBEventResponse{...DynamoDBBatchItemFailure{ItemIdentifier}}`, also `KinesisTimeWindowEventResponse`, `DynamoDBTimeWindowEventResponse`.
- `SNSEvent{Records []SNSEventRecord{EventVersion, EventSubscriptionArn, EventSource, SNS SNSEntity{Signature, MessageID, Type, TopicArn, MessageAttributes map[string]interface{}, SignatureVersion, Timestamp time.Time, SigningCertURL, Message, UnsubscribeURL, Subject}}}`. Attribute values are `{"Type":..,"Value":..}` maps.
- `EventBridgeEvent = CloudWatchEvent{Version, ID, DetailType, Source, AccountID, Time, Region, Resources, Detail json.RawMessage}`.
- `KinesisEvent{Records []KinesisEventRecord{AwsRegion, EventID, EventName, EventSource, EventSourceArn, EventVersion, InvokeIdentityArn, Kinesis KinesisRecord{ApproximateArrivalTimestamp, Data, EncryptionType, PartitionKey, SequenceNumber, KinesisSchemaVersion}}}`.
- `DynamoDBEvent{Records []DynamoDBEventRecord{AWSRegion, Change DynamoDBStreamRecord{ApproximateCreationDateTime, Keys, NewImage, OldImage, SequenceNumber, SizeBytes, ...}, EventID, EventName, ...}}`. NewImage/OldImage are item data (redaction risk, never capture).

Batch gotchas:
- Returning an error fails the whole batch. Partial retries need the event source mapping `FunctionResponseTypes: ReportBatchItemFailures`, else `BatchItemFailures` is ignored (**UNVERIFIED**, AWS docs).
- `ItemIdentifier` = SQS `messageId`, Kinesis/DynamoDB `SequenceNumber` (**UNVERIFIED**, only the field is in source).
- Design choice for the kit: one invocation event with `messaging.batch.message_count` and failed count, and/or one event per record through a per-record helper. The per-record outcome must feed `BatchItemFailures`, so a helper like `ProcessSQS(ctx, e, func(ctx, SQSMessage) error) SQSEventResponse` produces both.
- Generic wrapper: `func Wrap[TIn, TOut any](h func(context.Context, TIn) (TOut, error)) func(context.Context, TIn) (TOut, error)` compiles with `lambda.StartHandlerFunc` and can type-switch `any(in)` on event types (proved in `sigs`). Use `WithEnableSIGTERM(flush)` for spindown flush.
- Go floor: v1.55.0 raised `go` to 1.26. v1.54.0 already has everything above and is effective 1.21.

---

## 15. GCP functions-framework-go (v1.9.2)

```go
func functions.HTTP(name string, fn func(http.ResponseWriter, *http.Request))
func functions.CloudEvent(name string, fn func(context.Context, cloudevents.Event) error)   // cloudevents = github.com/cloudevents/sdk-go/v2
func functions.Typed(name string, fn interface{})
funcframework.RegisterHTTPFunctionContext / RegisterCloudEventFunctionContext / RegisterEventFunctionContext / Start(port) / StartHostPort(host, port)
funcframework.ExecutionIDFromContext(ctx) string; TraceIDFromContext(ctx) string; SpanIDFromContext(ctx) string; LogWriter(ctx) io.WriteCloser  // v1.9.0+
```

- Registration fails with `log.Fatalf`. `FUNCTION_TARGET` selects the function served at "/".
- HTTP and legacy event wrappers call `setupRequestContext`. It does two things:
  - It sets a ctx deadline from env `CLOUD_RUN_TIMEOUT_SECONDS`.
  - It reads logging ids from header `Function-Execution-Id` (a random id replaces a missing one) and `X-Cloud-Trace-Context` (`TRACE/SPAN;o=1`).
- **Gotcha: `wrapCloudEventFunction` does not call `setupRequestContext`**, so `ExecutionIDFromContext` is empty and there is no timeout ctx for CloudEvent functions (source).
- Panics: HTTP path recovers and writes a generic 500 (`recoverPanic(w, ..., false)`). CloudEvent path recovers, logs, and **re-panics** (`shouldPanic=true`), then the CloudEvents invoker converts it to a result. Errors from CloudEvent fns are printed to stderr in GCP error format.
- On GCF (`K_SERVICE` set), the wrapper prints empty lines to stdout/stderr after each call to force log flush. wlog must flush its own drains before returning.
- Adapter shape: wrap the user fn before `functions.HTTP/CloudEvent` (no middleware API). HTTP path can reuse http-core. CloudEvent path uses the CloudEvents field set (section 9).
- Depends on cloudevents sdk-go v2.15.2 (go.mod).

---

## 16. CLI frameworks

### 16.1 spf13/cobra (v1.10.2)

```go
PersistentPreRun(E), PreRun(E), Run(E), PostRun(E), PersistentPostRun(E)  func(cmd *Command, args []string) [error]
func (c *Command) ExecuteContext(ctx) error; ExecuteContextC(ctx) (*Command, error); Context() context.Context; SetContext(ctx)
func (c *Command) CommandPath() string; Name() string; CalledAs() string; Root() *Command
var cobra.EnableTraverseRunHooks bool   // package global, v1.8.0
SilenceErrors, SilenceUsage bool
```

Verified behavior (command.go `execute`, `ExecuteC`):
- Order: ParseFlags → help/version → `Runnable()` else `flag.ErrHelp` → `ValidateArgs` → PersistentPreRun(E) → PreRun(E) → required flags/groups → Run(E) → PostRun(E) → PersistentPostRun(E).
- **Only the first persistent pre/post hook found walking up from the leaf runs**, unless the global `EnableTraverseRunHooks` is true. A subcommand with its own persistent hook silently hides a wlog `PersistentPreRunE` on root. `E` variants take precedence over non-E.
- **An error from RunE, PreRunE, arg rules, or a persistent pre hook stops the post hooks.** Run output: `cobra: leaf="app sync" err=boom persistentPostRan=false`. So "end the event in PersistentPostRunE" loses every failed run.
- `ExecuteContextC` returns the resolved leaf command **even on error** (also on unknown-command errors, where it returns the deepest found cmd or root). So the reliable adapter is at `main`: `ctx, end := work.Command(ctx); cmd, err := root.ExecuteContextC(ctx); end(cmd.CommandPath(), exitCode(err))`.
- Context flows root → leaf only for a leaf with a nil ctx (`if cmd.ctx == nil { cmd.ctx = c.ctx }`). Reusing a command tree (tests) keeps a stale ctx. `cmd.SetContext` inside hooks is not returned to the caller.
- Help requested (`flag.ErrHelp`) returns `(cmd, nil)`.
- Cobra never calls `os.Exit`. Exit code is the app's choice. Events must be flushed before `os.Exit`, which skips defers.
- `CommandPath()` = names from root, for example `app sync`. Args can hold secrets, so do not capture them by default.

### 16.2 urfave/cli/v3 (v3.12.0)

```go
type BeforeFunc func(context.Context, *Command) (context.Context, error)
type AfterFunc func(context.Context, *Command) error
type ActionFunc func(context.Context, *Command) error
type ExitErrHandlerFunc func(context.Context, *Command, error)
type OnUsageErrorFunc func(ctx context.Context, cmd *Command, err error, isSubcommand bool) error
type ExitCoder interface { error; ExitCode() int }
func Exit(message any, exitCode int) ExitCoder
func HandleExitCoder(err error)   // prints, then OsExiter(code) for ExitCoder or MultiError (last code or 1)
var OsExiter = os.Exit; var ErrWriter io.Writer = os.Stderr   // package globals
func (cmd *Command) Run(ctx context.Context, osArgs []string) error; FullName() string; Lineage() []*Command; Root() *Command
```

Verified behavior (command_run.go):
- `run` puts the command in ctx and resolves subcommands recursively. At the leaf the order is:
  - the arg validator func, then `runBefore(ctx, chain)`.
  - `runBefore` runs **every** `Before` from root to leaf in order and threads the returned ctx.
  - flag actions, then required flags and args, then `Action`.
- Each command in the chain registers its `After` as a defer in its own `run`. The defer closure reads the `ctx` variable. The subcommand's returned ctx overwrites that variable on purpose (source comment). So root `After` sees the ctx from `Before`. After also runs for a failed Action. After does not run after help output.
- **Gotcha, verified by running**: an Action error goes through `handleExitCoder`, which walks to the root.
  - The root `ExitErrHandler` gets the error. Without one, `HandleExitCoder(err)` calls `OsExiter(code)`.
  - This happens **inside Run, before After and before Run returns**.
  - Output with OsExiter replaced: `urfave: OsExiter(3) called inside Run` then `urfave: After ran`. With the real `os.Exit`, After, the wlog end, and flush never run. A plain (non-ExitCoder) error does not exit (`urfave: plain error returned=plain`).
- So the adapter must set the root `ExitErrHandler`. It records the code, flushes, then calls `cli.HandleExitCoder` to keep the behavior. The other choice is to wrap `Run` with a handler that does not exit. Mutating the global `OsExiter` breaks the no-package-state rule.
- Exit code: ExitCoder → `ExitCode()`, MultiError → last ExitCoder or 1, other error → app decides (usually 1), nil → 0. Usage errors print "Incorrect Usage" and return the error (no exit code type).
- Command name: `cmd.FullName()` (verified exists). Leaf is the `*Command` passed to Action/After.
- Go floor 1.22 for every stable v3 (verified). v3.0.0 was only alpha/beta. v3.1.0 is the first stable (2025-03-31).

### 16.3 alecthomas/kong (v1.16.1)

```go
func kong.Parse(cli any, options ...Option) *Context        // calls parser.FatalIfErrorf → exits on error
func kong.New(grammar any, options ...Option) (*Kong, error)
func (k *Kong) Parse(args []string) (*Context, error)       // errors are *ParseError (ExitCode 80 for usage errors)
func (c *Context) Run(binds ...any) error                   // RunNode on selected node, then applyHook "AfterRun", errors.Join
func (c *Context) Command() string                          // e.g. "rm <path>" (positional placeholders included)
func (c *Context) Selected() *Node; (*Node).FullPath() string   // "app sync"
func (c *Context) BindTo(impl, iface any); kong.Bind(...), kong.BindTo(impl, iface) Option
func kong.Exit(exit func(int)) Option
type ExitCoder interface { ExitCode() int }                 // v1.9.0
hooks (duck-typed methods on nodes, any bindable args): BeforeReset, BeforeResolve, BeforeApply, AfterApply, AfterRun (v1.4.0)
```

Verified behavior:
- **`AfterRun` also runs for a failed `Run`** (`kong: AfterRun ran` with run error). The README says a manual os.Exit() skips it.
- **`ctx.Run(context.Background())` does NOT bind `context.Context`.** Run output: `couldn't find binding of type context.Context for parameter 0 ... use kong.Bind(context.Context)`. It binds the concrete type. Correct: `kctx.BindTo(ctx, (*context.Context)(nil))` (run output: ctx value visible, `selected path = kongctx sync`).
- `RunNode` calls `Run` on the selected node **and every ancestor that has a `Run` method** (leaf first), stopping at the first error.
- Exit paths that bypass the event: global `kong.Parse` calls `FatalIfErrorf` → `k.Exit(exitCodeFromError(err))`. Parse errors exit **80** (`exitUsageError`, from square/exit, verified run: `parse error exit code=80`). The `--help` flag calls `ctx.Kong.Exit(0)` during parse (help.go). Default `Exit` is `os.Exit` (UNVERIFIED default, only the option is verified). Adapter must use `kong.New` + `k.Parse` and/or `kong.Exit(func(int))` to record and flush before exiting.
- `ctx.FatalIfErrorf(err)` after Run exits with `ExitCoder.ExitCode()` or 1.

---

## 17. Cross-cutting findings for the `work` kit spec

1. **Hook vs helper.** True hooks with ctx and outcome exist only for: watermill (HandlerMiddleware), asynq (MiddlewareFunc), river (WorkerMiddleware), Temporal (ActivityInboundInterceptor), CloudEvents (ObservabilityService), franz-go **producer** hooks, cron (JobWrapper, no ctx). Everything else needs a wrapper around a user handler `func(ctx, <msg>) error`, or a per-message helper in the user's loop. That covers kafka-go, sarama consume, franz-go consume, confluent, SQS, NATS, AMQP, Pub/Sub, Lambda, and GCF.
2. **Panics are recovered outside the hook** in watermill (router), asynq (perform), river (execute), Lambda (then process exits), CloudEvents (callback skipped). The kit's `Run` and end must recover, record `outcome=panic`, flush in short-lived runtimes, then re-panic.
3. **The framework can decide the final state after the hook returns**: asynq (lease, deadline, abort race), river (ErrorHandler cancel, snooze, soft stop), watermill (PoisonQueue turns an error into an ack), Temporal (`ErrResultPending`). Name the outcome from the handler's point of view: "error", "retry", "snooze", "cancel", "discard". Claim a broker state only where the library reports it.
4. **Attempt and redelivery sources**:
   - Queues: SQS `ApproximateReceiveCount` (must be requested), JetStream `NumDelivered`, Pub/Sub `DeliveryAttempt` (nil without a dead-letter policy).
   - AMQP `Redelivered` (server headers UNVERIFIED), Lambda SQS `Attributes["ApproximateReceiveCount"]`.
   - Jobs: asynq `GetRetryCount+1`, river `JobRow.Attempt` (decrements on snooze), Temporal `Info.Attempt` (from 1).
   - None: Kafka (all 4 clients), NATS core, cron.
5. **Lag sources**:
   - Queues: Kafka record timestamp and HWM, SQS `SentTimestamp`, JetStream `Timestamp`, Pub/Sub `PublishTime`, SNS `Timestamp`.
   - Streams and events: Kinesis `ApproximateArrivalTimestamp`, DynamoDB `ApproximateCreationDateTime`, EventBridge `Time`, CloudEvents `Time()`.
   - Jobs: river `ScheduledAt`, Temporal activity `StartedTime - ScheduledTime`. asynq has none.
   - AMQP has lag only for a producer that set `Timestamp`.
6. **Trace carriers**:
   - Kafka headers (`[]byte` values, case-sensitive), SQS and SNS MessageAttributes (`DataType: String`), NATS `Header` (case-sensitive map).
   - AMQP `Table` (write `string`), Pub/Sub `Attributes` (also read `googclient_traceparent`), watermill `Metadata`.
   - CloudEvents protocol headers, then the `traceparent` extension.
   - asynq `NewTaskWithHeaders` (v0.26.0+), river `Metadata` JSON (no `river:` prefix), Temporal header Payload through the data converter. The `propagate` module needs a carrier interface that covers `[]byte` values and case-sensitive maps.
7. **Short-lived runtimes and exits**:
   - Lambda: flush in a defer before re-panic, and use `WithEnableSIGTERM`. GCF: flush before return. cron: wait on the `Stop()` ctx.
   - cobra: flush before `os.Exit`. urfave: the default exit handler calls `os.Exit` inside Run. kong: `Parse` and `--help` exit inside the library.
8. **Package-level state traps in upstream APIs**: `cobra.EnableTraverseRunHooks`, `cli.OsExiter`/`cli.ErrWriter`, `sarama.Logger`. Adapters must not set them (golden rule), only document them.
9. **Semconv naming**: messaging keys map cleanly. The existing Lambda example uses `faas.cold_start`/`faas.request_id`, while semconv is `faas.coldstart`/`faas.invocation_id`. Decide before the `work` spec freezes field names. `messaging.system` for SNS is `aws.sns` and for SQS is `aws_sqs` (inconsistent upstream, verified).
10. **Build constraints**: confluent needs `//go:build cgo` files and a cgo-free package file. urfave v3 and Pub/Sub v2 cannot meet a 1.21 floor. asynq headers force 1.24.

## Appendix: scratch artifacts

- `async-work/mods/*.txt`: `go` directive for every stable version of each module.
- `async-work/floor/*.out`: effective Go floor per candidate version.
- `async-work/sigs/main.go`: compile proof and cobra, urfave, kong runtime tests.
- `async-work/kongctx/main.go`: kong context binding proof.
- `async-work/confl/main.go`: confluent `CGO_ENABLED=0` build failure proof.
