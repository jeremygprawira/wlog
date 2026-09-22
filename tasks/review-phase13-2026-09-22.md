# Review: phase 13 (Track D), 2026-09-22

Scope: the 13 tasks 13-D-1 to 13-D-13 (commits after tag v0.7.0, up to 84da85b), their
shared-code changes, and a regression run of phases 10 to 12. Four reviewers read the 19
new modules against `docs/SPEC-track-d.md`. Each finding comes from the code, a gate run, or a
throwaway probe test that ran in the scratchpad through `go test -overlay` or a scratch copy.
The review changed no repository file. The only exception is this report.

## Result

Every gate passes except `tools vuln`, and that failure comes from the local Go toolchain, not
from phase 13 code. The `Verify` commands of phases 10, 11, 12, and 13 all pass, so no earlier
task proof broke. But the passing tests hide real bugs. After merging the duplicates across
reviewers, the review found 15 high, 29 medium, and 32 low findings. Most of the high findings are in behavior that no test exercises: panics,
commit order after a failure, blocking producers, and trace links.

Phase 13 changed four pieces of shared code. One of them contains a regression (S-1).

| Shared change | Commit | Effect on earlier code |
|---|---|---|
| `work.Start` reads the carrier after the event starts | b6bca6f | Fixes incoming trace links. Breaks the trace of a batch child (S-1) |
| `work.Handle.End` keeps an explicit level | 354dc16 | Correct, and no earlier caller breaks. It needs the new public `wlog.CurrentLevel` (X-6) |
| `client/aws/go.mod` floor raised to aws-sdk-go-v2 v1.36.1 | 6e40abb, 2d6519a | A tagged module now forces users to a newer SDK, and the spec still says v1.17.0 (S-2) |
| With `WLOG_DRAINS` set, `cmd/wlog` records its own runs | dde9a2f | Without `WLOG_DRAINS`, nothing changes. With it, the event has wrong levels (F-4, F-17) |

## Gate results

| Gate | Result |
|---|---|
| `go build ./...` | pass |
| Root race tests (`GOWORK=off`) and race tests of all 57 modules in isolation | pass |
| Race tests of the 19 new modules, with and without the workspace | pass |
| `make lint` | pass, 0 issues in 58 runs |
| `tools tidy -check`, `requires`, `ste`, `snippets`, `schema`, `pkgstate` | pass |
| `tools cover -min 85` (root module only) | pass. Not gated for new modules: sarama 71.1%, cloudevents 81.5%, lambda 82.1%, urfave 84.6% |
| `make map` | pass, score 100 |
| `verifyplan -only 13-` | pass |
| `verifyplan -only 10-`, `11-`, `12-` (10- includes `make fuzz FUZZTIME=30s`) | pass |
| `tools floor -libs` | pass (exit 0) |
| `queue/confluent` with `CGO_ENABLED=0 go build ./...` | pass, only `doc.go` builds |
| `tools vuln` | fail. All 57 modules hold Go standard library issues of go1.26.1, fixed in go1.26.4 to go1.26.6. `examples` also holds `google.golang.org/grpc` v1.84.0 (GO-2026-6443, grpc-service example, older than phase 13, no released fix) |
| `tools release -version v0.8.0` (dry run) | pass. The tag order is correct. See T-1 for a display bug |

## Decisions for you

These findings need an owner decision before a fix, because each one changes a spec rule, adds a
dependency, or touches an ask-first boundary.

1. Panic policy (X-1). Track D rule 3 says "panics again". The code recovers the panic in 11
   consumer entries. Choose one of the two: remove `work.RecoverPanics()`, or amend rule 3 for
   consumers.
2. `wlog.CurrentLevel` (X-6). This is new public API after the v0.7.0 tag, which is an
   ask-first item. Keep it and add it to SPEC-core-v2, or move the need into an internal package.
3. The `client/aws` floor (S-2). Revert to v1.17.0 with the workspace fix, or keep v1.36.1 and
   update SPEC-track-b and CHANGELOG.
4. The package name `wlogkafka` (X-7). SPEC.md names it `wlogkafkago`. Rename it before the
   first tag, or change both specs.
5. The embedded NATS test server (Q-11). It is a new test dependency, `nats-server/v2`, so you
   must approve it first.
6. The new field `messaging.nats.subscription` (Q-14). SPEC-work says a new group field is ask
   first.
7. The kafka-consumer recipe writes the message key into the event (F-21). The Track D boundary
   says capturing a key is ask first.

## Suggested fix order

1. Stop message loss: X-1, K-1, K-3, K-10, Q-2, Q-3.
2. Stop blocking and leaks: K-4, K-5, K-9, Q-5.
3. Fix the shared regression S-1, then F-1 and F-2 (events lost at exit).
4. Fix the floors: S-2, S-3.
5. Fix J-3 (every CloudEvents ACK logs as an error).
6. Wire the conformance suites through the real entries (X-2), so a later change cannot hide these bugs.
7. Sync the specs and the CHANGELOG (X-6, X-9).

---

## Shared code and floors

### S-1 · high · work · A batch child leaves the batch trace when its message has no traceparent
- Where: `work/work.go:81-86` (change from b6bca6f)
- What: `work.Start` now calls `propagate.Extract` after `startEvent`. `Detach` gives a batch
  child the batch trace, but `Extract` always writes a trace, so an empty carrier gives a random
  trace id and request id. The child keeps `parent_span_id` of the batch span, which now lives in
  another trace. Every adapter wraps the message headers, and empty headers too, so a
  carrier is never nil in practice.
- Evidence: a probe test with `BatchEvent` plus `Start` and an empty `propagate.MapCarrier{}`
  fails on HEAD (`child trace_id 218cbe54... != batch trace_id 4bf92f35...`). The same test
  passes on v0.7.0, but v0.7.0 ignored the incoming traceparent completely. So phase 13 fixed
  one case and broke the other. The jobs reviewer reproduced it on fe8e9fa.
- Impact: `wloglambda.ProcessSQS` inside `Wrap` hits this for every record without a
  traceparent attribute, which is the usual case. Any user of `work.BatchEvent` hits it too.
- Fix: before `startEvent`, record `parent := wlog.HasEvent(ctx)`. If there is no parent, or
  the carrier holds a `traceparent`, extract. Otherwise keep the batch trace.
  Add a `BatchEvent` test with an empty carrier.

### S-2 · high · client/aws · The SDK floor of a tagged module moved, and the move was not needed
- Where: `client/aws/go.mod`, `client/aws/aws_test.go:177-181`, `docs/SPEC-track-b.md:65`
- What: aws-sdk-go-v2 v1.17.0 became v1.36.1, smithy-go v1.13.3 became v1.22.2, and s3 v1.29.0
  became v1.75.0. `client/aws/v0.7.0` is tagged, so users of the next tag must take v1.36.1.
  The spec row still promises v1.17.0, and CHANGELOG has no entry. `aws.go` did not change.
- Cause: in the workspace, `queue/sqs` lifts the SDK to v1.36.1 while the `client/aws` test
  keeps s3 v1.29.0 (`PutObject: not found, ResolveEndpointV2`). The old known-broken entry came
  from `floor -libs`, which runs `go get -u ./...` without `-t` (`tools/cmd/floor/main.go:187`).
- Evidence: the queue reviewer restored the v0.7.0 `go.mod` and test and added two versioned
  replaces to `go.work` (s3 v1.29.0 to v1.75.0, credentials v1.12.0 to v1.17.10). The tests then
  pass in the workspace and with `GOWORK=off`. With `go get -u -t ./...`, the v0.7.0 set also
  passes on the newest SDK. The `BaseEndpoint` test change was not needed either.
- Fix: decision 3. The smaller fix restores v0.7.0, adds the two replaces to `go.work`, and adds
  `-t` to the floor tool.

### S-3 · high · job/asynq, job/river · The Go floor is 1.26, but the spec says 1.24 and 1.21
- Where: `job/asynq/go.mod:3,20-21`, `job/river/go.mod:3,21-23`
- What: both modules pin indirect `golang.org/x` versions far above what their library needs.
  asynq v0.26.0 needs x/sys v0.37.0 and x/time v0.14.0, but the module pins x/sys v0.48.0 (Go
  1.26). river v0.14.1 needs x/text v0.19.0 and x/sync v0.8.0, but the module pins x/text
  v0.42.0, x/sync v0.23.0, and x/crypto v0.57.0 (each Go 1.26). A user on Go 1.25 or older
  cannot use either module. Commit 354dc16 says river "keeps the Go 1.21 floor", which is false.
- Why no gate caught it: `tools floor` tests the go line of each module, so it tests at 1.26 and
  passes. See T-2.
- Fix: lower the indirect requirements to the library needs, set `go 1.24.0` and `go 1.21`, and
  test with `GOTOOLCHAIN`. Do not run `go work sync` on these modules, because the workspace
  selects the newer versions.

## Cross-cutting findings

### X-1 · high · 11 consumer entries · A handler panic is recovered and never raised again
- Where: the `process` helper with `work.RecoverPanics()` in `queue/kafkago/consume.go:85`,
  `queue/sarama/consume.go:72`, `queue/confluent/consume.go:77`, `queue/sqs/consume.go:80-82`,
  `queue/nats/consume.go:80-85`, `queue/amqp/consume.go:84-86`, `queue/pubsub/consume.go:43-45`,
  and `faas/lambda/wrap.go:82-84` (used by ProcessSQS, ProcessKinesis, ProcessDynamoDB). The
  franz `Record` helper doc calls `end(err)` without a defer, so a panic there emits no event.
- What: Track D rule 3 says "records it with a stack, flushes in a short-lived runtime, and
  panics again". Probes show `panicked=false` for each entry. AMQP then requeues the message
  forever, JetStream and Pub/Sub nak it forever, a core NATS panic disappears, and sarama and
  confluent commit past it (K-1). The lambda comment says the recovery exists "so a test
  continues after the panic scenario".
- Correct today: watermill, asynq, river, temporal, cron, gcf, and the command adapters call
  plain `work.Run` on their real path.
- Fix: decision 1. To follow the spec, drop `RecoverPanics()` from the real entries and keep it
  only in the test factory.

### X-2 · medium · all 19 modules · The conformance suites never run the adapter code
- Where: every work and calls factory in the `_test.go` files of the 19 modules.
- What: each work factory calls a `process` helper that is line for line the suite's own
  reference fake (`internal/conformance/work/suite_test.go:42-44`). Each calls factory calls
  `wlog.StartCall` itself and never goes through the real producer wrapper. In cloudevents,
  asynq, river, temporal, cron, watermill, gcf, and the three command modules, `process` has no
  caller outside tests.
- Evidence: in scratch copies, the reviewers removed the real entry points, gutting `Execute`,
  `HTTP`, `CloudEvent`, and `Middleware`, and removed `StartCall` from each producer. Every work
  and calls conformance test still passed. So success criterion 1 is not proven for any module.
- Root cause: the suite hands every factory the rpc, command, and function kinds, which a queue
  or job adapter cannot produce.
- Fix: let a factory declare the kinds it produces, and skip the rest. Route each factory
  through the real entry with a fake client. Move the `process` helpers into test files.

### X-3 · medium · 14 of 19 modules · The package doc shows no setup line, and no tool compiles one
- Where: sarama, confluent, sqs, nats, amqp, pubsub, asynq, river, temporal, cron, lambda, gcf,
  cobra, urfave, kong. Only kafkago, franz, watermill, and cloudevents show one.
- What: plan shared condition 3 says "The package doc shows the one-line setup, and `tools
  snippets` compiles it". But `tools snippets` reads only Markdown fences, so it never compiles
  a Go package doc. No `Example` test exists in any phase 13 module.
- Fix: add the setup block to each doc. Add an `Example` function per module, or put each setup
  line in a guide that `tools snippets` reads.

### X-4 · low · all 19 modules · `wlog init` detects none of the new libraries
- Where: `cmd/wlog/cmd/init/init.go:141-165` detects only echo, gin, mux, and net/http.
- What: shared condition 4 is half met. The CAPABILITIES rows exist (lines 274 to 292). The
  phase 12 modules have the same gap, and 14-G-7 (cli-init v2) owns detection.
- Fix: write that deferral into the plan, so the condition no longer reads as met.

### X-5 · low · all 19 modules · Doc comments break the Simple English limits
- What: `ste -comments` shows about 25 sentences over 25 words in the new files and the banned
  modal `may` in about 7 places. Examples: `queue/kafkago/produce.go:54`,
  `queue/franz/hooks.go:53`, `command/cobra/cobra.go:112`, `command/urfave/urfave.go:181`,
  `command/kong/kong.go:137`. `level.go:29` uses the modal `would`. The `-comments` flag is not enforced
  yet (930 hits repo-wide).
- Fix: split the long sentences, and change `may` to `can`.

### X-6 · medium · specs · The spec was not updated before behavior changed
- `wlog.CurrentLevel` (`level.go:26-38`) appears in no spec, only in CHANGELOG. It is public API
  after the v0.7.0 tag (decision 2). The `End` doc (`work/work.go:131-136`) does not say that an
  explicit level wins.
- The entry signatures of all 19 modules differ from the Track D tables. Each takes a
  `*wlog.Logger`. Kafka `Consume` takes a `Fetcher`, and franz `Record` takes the client. asynq
  `Enqueue` takes a type name and a payload. cron `Job` takes a log, a name, and a schedule.
  kong `Run` takes a grammar and options.
- Rule 9 says the `propagate` package holds the Kafka, NATS, SQS, and AMQP carriers. Each
  adapter holds its own carrier instead (Q-21).
- The CloudEvents row asks the observability hook to recover a panic, which a hook cannot do.
  The code adds a separate `Recover(fn)` (J-6).
- Fix: update SPEC-track-d, SPEC-core-v2, and SPEC-work to the shipped API.

### X-7 · low · queue/kafkago · The package name differs from SPEC.md
- Where: `queue/kafkago/doc.go:20` says `package wlogkafka`. `docs/SPEC.md:249` and
  `docs/SPEC-setup.md:34,95` say `wlogkafkago`. Every other adapter follows `wlog<dir>`. The
  recipe has to alias the import.
- Fix: decision 4.

### X-8 · low · examples · The recipe goldens hold values the code never makes, and the test never compares them
- Where: `examples/{kafka-consumer,cron-job,lambda}/testdata/event.json`
- What: `conformance.Normalize` drops every trace id and every `_ms` key before the compare. So
  the kafka golden shows `span_id` equal to `parent_span_id`, which `Extract` never produces, and
  no test fails. The cron golden uses the W3C example trace id on a job with no parent. All three
  share one template. No git history shows a capture, so the provenance is unconfirmed.
- Fix: give each golden values the code can produce. Compare the trace shape in the test, for
  example that `span_id` differs from `parent_span_id`.

### X-9 · low · CHANGELOG, commits · The change record is incomplete
- CHANGELOG has no entry for the 19 new modules, the `cmd/wlog` self event, the argument
  dispatch change for `wlog rules|schema|version|env`, the `client/aws` floor, or the removal of
  the tagged `examples/lambda` module. v0.7.0 is tagged, but there is no `[0.7.0]` section.
- The plan says `go.work`, `CHANGELOG.md`, and shared files get their own commit. Phase 13
  folded `level.go`, `work/work.go`, `client/aws`, `go.work`, and CHANGELOG changes into
  feature commits. Commits hold 465 to 1820 lines, and several hold two or three modules.
  CLAUDE.md asks for one commit per green cycle.
- `go.work.sum` has 367 uncommitted added lines from workspace builds.
- `tools/cover-known-low.txt` lists `internal/httpdrain`, a package that no longer exists.

## Tooling findings (older than phase 13, but they hid phase 13 problems)

### T-1 · medium · tools/release · The dry run prints only the first line of each apidiff report
- Where: `tools/cmd/release/main.go:473-486` (`oneLine`)
- What: for the root module, the first line is "Ignoring internal package ...", so the plan never
  shows the `CurrentLevel` addition or any incompatible change. For `examples`, it prints
  "Compatible changes:" with nothing after it. `sort.SliceStable` with a false comparator does
  nothing.
- Fix: print the whole report, or skip the "Ignoring" lines and print every change line.

### T-2 · medium · tools/floor · The floor gate trusts the go line and the library versions of each go.mod
- What: `floor` tests at the go line and library versions that each `go.mod` declares. It never
  compares them with the floors in the specs. So S-2 and S-3 pass. `-libs` runs `go get -u` without
  `-t`, which leaves test-only modules old.
- Fix: read the floor from the spec tables, or keep a floor list, and fail on a mismatch. Add
  `-t` to the upgrade step.

### T-3 · low · repo · Seven earlier modules sit at Go 1.26
- `cmd/wlog`, `examples`, `log/logrus`, `log/zerolog`, `middleware/echo`, `middleware/echo5`,
  and `middleware/gin` moved to `go 1.26.0` in d313c3d (phase 11). `errors/herr` is at 1.26.1.
  This is outside phase 13, and it has the same cause as S-3.

### T-4 · medium · work · `work.Ticker` gives `fn` the outer context
- Where: `work/ticker.go:41-46`, from 4f2b793 (phase 11)
- What: `fn` gets the context without the tick event, so every `wlog.Set` inside a tick fails
  with `WLOG_NO_EVENT`. The job-cron spec row sends users to `Ticker`.
- Fix: pass the context that `Start` returns, and test a field set inside `fn`.

---

## Kafka: queue/kafkago, queue/sarama, queue/franz, queue/confluent

### K-1 · high · sarama, confluent · The next success commits a failed message, so it never comes back
- Where: `queue/sarama/consume.go:50-56`, `queue/confluent/consume.go:56-64`
- What: after a handler error, both loops continue and then mark or commit the next message.
  Kafka keeps one offset per partition, so that commit covers the failed message. A probe with
  offset 7 failing and 8 succeeding gives `marked=[8]` and `committed=[8]`. Both docs promise
  redelivery. confluent also has librdkafka auto commit on by default, and the doc does not say so.
  The tests fail only the last message, which hides the bug.
- Fix: stop the partition after an error (return from `ConsumeClaim`, or `ResetOffset` or `Seek`
  back). Tell confluent users to set `enable.auto.commit=false`. Add a fail-then-succeed test.

### K-2 · high · all four · A panic is recovered (see X-1)

### K-3 · high · kafkago · Criterion 2 is false for a real reader, and its test is a tautology
- Where: `queue/kafkago/consume_test.go:97-120`, `queue/kafkago/consume.go:33-38`
- What: kafka-go moves the fetch position on every fetch (`reader.go:846`), commit or not. The
  next `Consume` on the same reader gets offset 8, and its commit covers 7. The test loads the
  fake with the same message twice, so it passes whatever `Consume` does.
- Fix: document that after an error the caller must open a new reader or exit. Rewrite the fake
  to move ahead on each fetch, and assert that a new reader sees offset 7.

### K-4 · high · sarama · AsyncProducer grows without bound under the default configuration
- Where: `queue/sarama/produce.go:107-165`
- What: `Send` stores one pending entry per message. It removes an entry only on a report from
  `Successes()` or `Errors()`. sarama has `Return.Successes` off by default, so every delivered
  message stays in the map with its event, and its call never ends. This breaks gate G4.
- Fix: take the `*sarama.Config`. If the returns are off, refuse or skip tracking. Cap the map
  and report the overflow.

### K-5 · high · confluent · Produce blocks forever with a nil delivery channel and ignores its context
- Where: `queue/confluent/produce.go:35-51`
- What: the wrapper waits on `<-deliveryChan` with no `select`. A nil channel is normal confluent
  use (the report goes to `Events()`), and a receive from nil blocks forever. A producer with
  `go.delivery.reports=false` also blocks forever. Probes show both, and a cancelled context
  does not stop the wait. This breaks gate G3.
- Fix: use a private channel of size 1, forward the report to the caller, and wait with a
  `select` on `ctx.Done()`.

### K-7 · medium · kafkago · A write across partitions ends its call on the first batch
- Where: `queue/kafkago/produce.go:56-66,110-120`
- What: kafka-go calls `Completion` once per partition batch, and `sync.Once` keeps only the
  first result. A probe with one refused partition records `status:ok` while `WriteMessages`
  returns an error.
- Fix: count the batches and end the call after the last one, or end sync writes on the
  `WriteMessages` result.

### K-8 · medium · kafkago · `Writer(w)` replaces the user's `Completion` and each `WriterData`
- Where: `queue/kafkago/produce.go:26-30,75`
- Fix: keep the old `Completion` and call it, and restore the caller's `WriterData`.

### K-9 · medium · kafkago · The Factory drain gets no acks, never closes its writer, and waits 1 s per flush
- Where: `queue/kafkago/drain.go:24-40,59`
- What: the writer uses `RequireNone`, so a broker refusal never reaches the pipeline retry. The
  sender has no `Close`, so the writer goroutines outlive `log.Close`. The default 1 s
  `BatchTimeout` delays every flush.
- Fix: set `RequiredAcks: kafka.RequireAll`, a short `BatchTimeout`, and the pipeline batch size,
  and give the Factory sender a `Close`.

### K-10 · medium · franz · The doc promises redelivery, but franz autocommit commits the record anyway
- Where: `queue/franz/consume.go:15-18`, `queue/franz/doc.go:11-22`
- Fix: add `kgo.DisableAutoCommit()` to the example, and state the K-1 rule for later records.

### K-11 · medium · confluent · Produce reads the report from the caller's channel
- What: goroutines that share one channel get each other's reports, and the async `Produce`
  becomes blocking. Fix it with the private channel from K-5.

### K-12 · low · franz · Producing the same record again records no call
- Where: `queue/franz/hooks.go:29-33`. franz keeps the first `r.Context`, so the second produce
  sees an open call of the same kind. This comes from the source code and was not run.

### K-13 · low · kafkago, sarama, confluent · Tests miss required scenarios
- The Kafka drain has no test for drain scenarios 5 and 7. The factory test covers only the
  no-environment case. The sarama async error path and the confluent `errNoReport` path have no test.

## Queues: queue/watermill, queue/sqs, queue/nats, queue/amqp

### Q-2 · high · sqs · Receive never asks for the trace attributes
- Where: `queue/sqs/consume.go:63-76`, `queue/sqs/fake_test.go:28-38`
- What: `withAttributes` sets only `MessageSystemAttributeNames`. Real SQS returns a user
  attribute that `MessageAttributeNames` names, and no other. So a consumer never joins the
  producer trace. The fake returns every attribute, so the test passes.
- Fix: add `traceparent`, `tracestate`, and `X-Request-ID` to a copy of `MessageAttributeNames`,
  and make the fake honor the request.

### Q-3 · high · watermill · A successful or nacked message records `dead_letter` and level error
- Where: `queue/watermill/middleware.go:34-39`
- What: the middleware reads `reason_poisoned` after the handler, on whatever the message holds.
  A message that watermill's requeuer replays keeps that metadata, so a later success records
  `dead_letter`. A message whose poison publish failed is nacked, not moved, but it records
  `dead_letter` too. Probes show both.
- Fix: read the metadata before the handler, and record `dead_letter` only on a new value and a
  nil handler error.

### Q-4 · high · watermill · The documented publisher setup publishes after the event emits
- Where: `queue/watermill/doc.go:10-11`, `queue/watermill/publisher.go:30-44`
- What: the router publishes produced messages after all middleware returns. So each message
  raises `WLOG_LATE_WRITE`, no call is recorded, and the traceparent names a span in no event.
- Fix: for an ended event, inject with the unit span and skip `StartCall`, or record the call on a
  `Detach` child. Document `SetContext(msg.Context())`.

### Q-5 · high · nats · The drain never flushes or closes its connection
- Where: `queue/nats/drain.go:19-41,59-63`
- What: `PublishMsg` only fills the client buffer, and the sender has no `Close`, so
  `log.Close` does not flush. `Factory` opens a connection it never closes, so its goroutines
  leak. If the process exits, events can stay in the buffer. This breaks drain scenario 6.
- Fix: call `FlushWithContext` after each batch, and give the Factory sender a `Close`.

### Q-7 · medium · sqs · SendMessage and Publish write into the caller's attribute map
- Where: `queue/sqs/produce.go:31,43,62-94`
- What: the race detector flags shared maps. Each send adds 2 or 3 attributes, so 9 caller
  attributes become 11, over the SQS limit of 10.
- Fix: clone the map and the input struct, and write only `traceparent` and `tracestate`.

### Q-8 · medium · sqs · With client-aws installed, one send records two calls
- What: the spec says the producer goes "through client-aws". The code records its own `queue`
  call, and client-aws adds an `rpc` call. Decide in the spec who owns the record.

### Q-9 · medium · amqp · The `Consume` doc states the opposite requeue rule
- Where: `queue/amqp/consume.go:33-35` against `:26-28` and the code. The commit message repeats
  the wrong rule. Adding one `RequeueOn` error switches every other error from requeue to drop.

### Q-11 · medium · nats · The tests use fakes where the spec asks for an embedded server
- The fake hides Q-5. The factory test never builds a drain from the two variables. See
  decision 5.

### Q-13 to Q-19 · low
- Q-13 watermill: `systemOf` maps `sqs.Subscriber` and `jetstream.Subscriber` to `watermill`.
  The `aws` key never matches, and no test covers the table.
- Q-14 nats: `messaging.nats.subscription` is a field the spec does not list (decision 6).
- Q-15 sqs: the delete uses the loop context, and a delete error drops the rest of the batch.
- Q-16 nats, amqp: `Ack`, `Nak`, `Term`, and `Nack` errors are discarded after the event emits.
- Q-17 nats: a failed publish inside a batch sends the earlier events twice on retry, and one
  oversized event drops the whole batch.
- Q-18 nats: `PublishMsg` replaces the `Header` field of the caller's message struct.
- Q-19 amqp: the doc does not say that the deliveries need `autoAck` false.

## Jobs and events: queue/pubsub, queue/cloudevents, job/asynq, job/river, job/temporal, job/cron

### J-3 · high · cloudevents · A receiver that returns an ACK result records level error
- Where: `queue/cloudevents/cloudevents.go:47-53`
- What: the hook passes `handle.End` straight to the SDK. `protocol.ResultACK` and an HTTP 202
  result are non-nil errors, so every good event logs as an error. Only the send side uses
  `protocol.IsACK` (line 176). A probe shows `level=error outcome=error` for `ResultACK`.
- Fix: map an ACK result to nil before `End`, and test `ResultACK`.

### J-5 · medium · cloudevents · The sent traceparent names the unit span, not the call span
- What: the SDK runs defaulters before `RecordSendingEvent`, so `EventDefaulter` injects before
  the call starts. A probe with a real client shows `match=false`. The test calls the two in the
  opposite order.
- Fix: inject inside `RecordSendingEvent` and `RecordRequestEvent`, and drive a real `client.Send`.

### J-6 · medium · cloudevents · The documented setup loses the event of a panicking function
- What: the doc calls one line "the whole setup", but without `Recover(fn)` the SDK skips the
  callback and the event never ends. `Recover` covers one of the 14 receiver shapes.
- Fix: show `Recover(fn)` and `EventDefaulter()` in the setup, and fix the spec row (X-6).

### J-7 · medium · pubsub · Publish records a cancelled context as a failed publish
- Where: `queue/pubsub/produce.go:33-36`. A result after the event ends is only a late write.
- Fix: wait on `result.Ready()`, then call `Get(context.Background())`.

### J-8 · medium · pubsub · The ack rule and the Publish call have no test that fails without them
- What: swapping `Ack` and `Nack` and removing `StartCall` still passes all 15 tests.
  `pstest` ships inside the pubsub module, so a real test needs no new dependency.

### J-11 · medium · cron · Wrap plus Job gives two events per run, and the recipe does exactly that
- Where: `job/cron/cron.go:18-36`, `examples/cron-job/main.go:30-34`
- What: both run `work.Run`. A `WithChain` wrapper also names every job in the scheduler with one
  name. The spec row shows `cron.WithChain(Wrap(name, spec))`.
- Fix: document `Wrap` as a per-entry wrapper, never combine it with `Job`, and fix the spec
  row and the recipe.

### J-12 · medium · temporal · Criterion 6 is half proven
- There is no replay test and no second-attempt test. The "workflow emits nothing" test passes
  with no interceptor installed.

### J-14 · low · asynq · The exhausted-retry `discard` rule has no test (removing it passes 21 of 21)
### J-16 · low · temporal · `lag_ms` is now minus `ScheduledTime`, but the spec says `StartedTime - ScheduledTime`
### J-18 · low · asynq, river · A panicking job records no `job.result`
### J-19 · low · pubsub · The subscription goes into `messaging.destination`, and the spec does not decide this
### J-20 · low · cloudevents · `X-Request-ID` is dropped, because it is not a valid extension name
### J-21 · low · river · No compile-time assertion proves the types satisfy the River interfaces (they compile today)
### J-22 · low · cron · A panic is raised again without a flush

## Functions, commands, cmd/wlog, and recipes

### F-1 · high · kong · `--help` never emits the event
- Where: `command/kong/kong.go:54-61`
- What: the `kong.Exit` function records and flushes while the event is still open inside
  `work.Run`, then calls `os.Exit`. The event never ends. A subprocess probe with `--help` wrote
  an empty drain file. The adapter's `kong.Exit` also replaces a caller's own exit function.
- Fix: use `work.Start`, and in the exit function call `handle.End`, flush, then exit.

### F-2 · high · cobra, kong, cmd/wlog · A panicking command skips the flush
- Where: `command/cobra/cobra.go:30-37`, `command/kong/kong.go:30-48`, `cmd/wlog/main.go:50-63`
- What: `flush(log)` comes after `work.Run`, and `work.Run` panics again, so the flush never
  runs. Probes with a pipeline drain deliver 0 events. urfave and lambda defer the flush.
- Fix: `defer flush(log)` at the top of each entry.

### F-4 · medium · cmd/wlog · The self event records exit code 1 as level info and outcome success
- Where: `cmd/wlog/main.go:53-60`. The closure always returns nil. Exit code 2 also records
  outcome success.

### F-5 · medium · urfave · An exit-coder error from a subcommand records the root path
- A probe with `app sync` records `path:app`. A flag value before the subcommand also stops the walk.

### F-7 · medium · lambda · API Gateway v2, ALB, SNS, and EventBridge handling has no test
- Removing those cases still passes. A probe shows the code itself works.

### F-8 · medium · urfave, kong · Criterion 9 "flush before exit" is not proven
- Moving the urfave flush after the exit still passes, and kong has no exit-path test.

### F-9 · medium · cobra, urfave, kong · A Measurer sees an empty operation for every command event
- The unit starts with no fields, and `wlog.Set(ctx, "operation", ...)` changes only the output
  map. `Detach` children also get an empty `parent_operation`.

### F-10 · medium · kafka-consumer recipe · The recipe exits on the first failed message and loses its event
- `log.Fatal` runs `os.Exit` without a flush, and one bad message stops the consumer.

### F-11 · medium · lambda · The X-Ray parent becomes a `parent_span_id` that exists nowhere
- Where: `faas/lambda/wrap.go:200-209`. `carrierOf` extracts, which makes a new span id, then
  injects it as the parent.

### F-12 · medium · cobra · A `RunE` error whose text starts with a usage prefix exits 2
- A `RunE` error "requires a running database" changes the real process exit code to 2. The
  `flag.ErrHelp` branch never runs, because cobra uses pflag.

### F-13 to F-27 · low
- F-13 urfave: a panicking command records `exit_code` 0.
- F-14 lambda recipe: the setup handler takes `map[string]any`, so it cannot produce the http
  event the recipe shows.
- F-15 cron-job recipe: `wlog query --where rows` exits 2. It needs `--where 'rows?'`. Some
  backend cells are prose, not queries.
- F-16 lambda: inside `Wrap`, the function event has no `batch_failures`.
- F-17 cmd/wlog: the self event stores any first argument in `cli.path`, which breaks rule 7
  for non-subcommand words.
- F-21 kafka-consumer recipe: it captures the message key (decision 7).
- F-22 gcf: the Cloud trace always says sampled, and it replaces a valid W3C traceparent.
- F-23 gcf, lambda: the gcf test leaks a loopback server, and some timing windows are narrow.
- F-24 lambda: the 2 s flush does not respect the invocation deadline.
- F-27 tests: some `C1` test names cover other rules, such as cold start and flag names.

---

## Correct as built

- No package-level mutable state in any new module (`tools pkgstate` passes). No adapter sets a
  library package variable (rule 8).
- Library floors match the spec for all 19 modules. Go floors match for 17 of them (S-3 names
  the two that do not). Each module passes at Go 1.21 or its floor.
- No message key, body, or non-trace attribute is captured by any adapter (rule 7). The
  kafka-consumer recipe does capture a key in user code (F-21).
- Group fields go where rule 10 says: `messaging.kafka`, `messaging.nats`, `messaging.rabbitmq`,
  `messaging.watermill`, `job.temporal`, and `messaging.cloudevents`.
- asynq SkipRetry gives `discard` and RevokeTask gives `cancel` with warn. River snooze gives
  info, cancel gives warn, and the last failed attempt gives `discard` (criterion 5).
- Temporal workflow code emits nothing, and `ErrResultPending` is not a failure.
- `ProcessSQS` returns the failed record id (criterion 8). Lambda events survive three warm
  invocations and a panic behind `pipeline.Wrap` (criterion 7). The cold-start flag lives inside
  `Wrap`.
- `queue/confluent` builds with and without cgo (criterion 10).
- The Kafka and NATS factories and the SPEC-setup var table rows exist (condition 5).
- `cmd/wlog` builds no Logger without `WLOG_DRAINS`, and with it prints nothing extra.
- `work.Handle.End` keeps an explicit level, and `TestWork_ExplicitLevelWins` fails without the
  change. `CurrentLevel` holds the event lock, so it has no data race.
- The three recipes have the four parts. Their setup blocks compile, and every `wlog explain` id
  resolves.
- No test makes a real outside network call.

---

## Fixed, 2026-09-22

Every finding is closed except the two ceilings named under X-2. The work is in the commits
after 84da85b. The decisions in "Decisions for you" were answered as follows.

1. Panic policy: the real entries record the panic and raise it again, as rule 3 says. The
   recovering helper lives in the test file.
2. `wlog.CurrentLevel`: kept, and recorded in SPEC-core-v2.
3. The `client/aws` floor: restored to v0.7.0, with the two SDK modules pinned forward in
   `go.work`.
4. The package name: `queue/kafkago` is `wlogkafkago`, and the recipe keeps the import alias
   that the other recipes use.
5. The NATS test dependency: approved. `queue/nats` takes `nats-server/v2 v2.10.22` as a
   test-only dependency, and a test drives the drain against an embedded server.
6. `messaging.nats.subscription`: kept, and recorded under rule 10 in SPEC-track-d.
7. The kafka-consumer recipe: it records the payload size, not the message key.

Two ceilings stay, and the work suite doc names them:

- `queue-franz`: `Record` returns an end func from a per-message helper, so a panic in the
  handler emits no event. The suite cannot run its panic scenario through that shape.
- `queue-watermill`: the topic lives on a message context that the library exports no setter
  for, so the suite cannot check the operation through the middleware.

Seventeen of the nineteen factories now drive their real entry with a fake client, so a
change that guts one of those entries fails the suite. The suite takes a declaration from a
factory: the kinds it produces, the messaging system it writes, whether its library reports a
delivery count, and whether the factory can set a job attempt.
