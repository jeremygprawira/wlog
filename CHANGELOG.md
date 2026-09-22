# Changelog

Every release names its changes. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the version numbers follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

The v1.0.0 release freezes the public API. A later change to that API waits for v2.

## [0.8.0] - 2026-09-22

### Added

- Track D gives one event per unit of async work. The queues `queue/kafkago`,
  `queue/sarama`, `queue/franz`, `queue/confluent`, `queue/watermill`, `queue/sqs`,
  `queue/nats`, `queue/amqp`, `queue/pubsub`, and `queue/cloudevents`. The jobs
  `job/asynq`, `job/river`, `job/temporal`, and `job/cron`. The functions `faas/lambda`
  and `faas/gcf`. The commands `command/cobra`, `command/urfave`, and `command/kong`.
  Every consumer passes the `work` suite, and every producer passes the `calls` suite.
- `wlog.CurrentLevel` returns the level of the current event and reports whether `SetLevel`
  named it, so a finisher keeps a level an earlier stage chose.
- `wlogcloudevents.Unit` maps one CloudEvent onto the unit of work of the receiver, so a
  receiver outside that module emits the same fields.
- With `WLOG_DRAINS` set, `cmd/wlog` records its own runs through `work`. It prints nothing
  extra.
- `docs/recipes/` gains `kafka-consumer.md`, `cron-job.md`, and `lambda.md`. `examples/`
  gains the three runnable examples, each with a golden event.

### Changed

- `wlog rules`, `wlog schema`, `wlog version`, and `wlog env` pass the subcommand name to the
  explain package, so the four commands work. Before, each one printed its usage and exited 2.
- The middleware budget is 55µs p50, and the measured p50 is about 52µs on an Apple M4
  Pro. The audit set 50µs before the realistic benchmark existed, and no run met it
  without weakening redaction.

### Fixed

- `work.Handle.End` keeps a level the handler named. Before, the level of the error
  replaced it, so a job that cancels or snoozes records the wrong level.
- `work.Start` reads the carrier after it starts the event, so a consumed message joins the
  trace of its producer. Before, the fresh trace of the event replaced the extracted trace.
- `work.Ticker` gives its function the context of the tick event, so a field inside the
  function lands on the event.
- A handler panic reaches the caller of every Track D consumer entry, as track rule 3 says.
  The queue loops stop after a failed message, so a later commit never covers it.
- The NATS and Kafka drains flush each batch and close their clients, and the Kafka writer
  waits for the broker acks.

### Removed

- The tagged `examples/lambda` module. The lambda recipe and its example replace it.

## [0.7.0] - 2026-09-21

v0.7.0 lands the everyday stack, search, and agents. Every HTTP router, RPC layer, outbound
client, store, logger bridge, error library, and flag library emits the same event shape.
The query commands and the MCP server read it back.

## [0.6.0] - 2026-09-19

v0.6.0 lands event shape v2 and the five foundations. Every HTTP adapter and every drain
passes one shared conformance suite, so a backend emits the same event shape on every
framework. A deploy picks its drains and its service identity through environment
variables alone.

### Added

- `wlog.SetDefault` and `wlog.Default` give a package function a Logger for a context that
  carries none. `Default` builds one on first use, so `wlog.Info` works with no setup.
- `wlog.Log(ctx, level, msg, kv...)` writes a plain line at any level, including one below
  the minimum level of the Logger.
- A unit of work records the calls it makes. `wlog.StartCall` returns the context of one
  call plus the end func that appends a record to `calls` and folds the numbers into
  `call_stats`. `wlog.CallFromContext` and `wlog.CallSpanID` read the open call, which
  lets a client or store adapter join a call the outer adapter already records.
- `propagate` carries the trace context of one unit of work across a process boundary.
  `Extract` reads `traceparent`, `tracestate`, and `X-Request-ID`, and `Inject` writes
  them. `HeaderCarrier`, `MapCarrier`, and `BytesCarrier` cover HTTP headers, plain maps,
  and byte headers, and `WithB3` and `WithXRay` add the two read-only fallbacks.
- `schema` embeds the JSON Schemas of the event shape and of the map report, and
  `tools/cmd/schema` checks every golden document against the schema of its shape, so a
  drift fails the build.
- `work` opens one unit of work of a kind and writes the group of that kind.
  `work.Start` returns a handle, `Handle.Set` and `Handle.Status` write fields, `Handle.End`
  records the outcome and emits once, and `work.Run` wraps a handler function. A panic is
  recorded with its stack and reaches the caller, and `work.RecoverPanics()` returns it as
  an error. `work.ClassOf` maps a status code to a client class, a server class, or
  neither.
- `work.BatchEvent` opens one parent event for a batch, with `batch_size` and
  `batch_failures`, and every message event links to it with `trace.parent_event_id`.
  `work.Ticker` emits one job event per tick and records a skipped tick. `work.Flush()`
  makes `Run` flush the Logger before it returns. A `Unit` with `StartedAt` in the past
  writes `lag_ms`.
- `tools/cmd/pkgstate` fails on a package-level variable that some code writes. The one
  allowed exception is the default Logger pointer.
- `wlog.OutputPreset` and `wlog.WithOutput` let a JSON writer print the dialect of a
  backend. `preset.Flat` writes every nested object as dotted keys. `preset.OTel` writes
  the OTel log data model with semantic conventions v1.43.0. `preset.ECS` writes the
  Elastic Common Schema, and `preset.Datadog` writes the Datadog log attributes.
  `preset.GCP` writes the Cloud Logging fields, and `preset.EMF` writes CloudWatch
  Embedded Metric Format. `preset.ByName` resolves a name, and `preset.Rename` renames a
  top-level key. A preset changes the printed line only, and every drain still receives
  the canonical event.
- `integrations/search/collectors/otel-filelog.yaml` holds a tested OTel Collector config.
  A filelog receiver reads the `otel` preset output, and a transform processor moves the
  resource and the attributes onto the log record.
- `wlog.IsReservedKey` tells a reserved event key from a user key, which a preset and a
  drain need to apply the collision rule.
- `middleware/httpcore` holds the framework-neutral HTTP core that every wlog HTTP
  adapter drives. One `Core` owns the operation name, the level, the route rules, the
  capture policy, and the emit point. An adapter passes what its framework knows.
  Safe defaults keep the allow-listed headers and the query and cookie names. `CaptureAll`
  adds the values and the bodies, and a local, dev, or development environment turns it on.
  A JSON body of any shape parses into the event, and a body cut at `MaxBody` becomes a
  marker with no text. A panic still emits its event, and `http.ErrAbortHandler` reaches
  net/http. The response writer forwards `Unwrap`, `Flush`, `Hijack`, and `ReadFrom`, so a
  deadline and a large download still work. `WriteProblem` writes an RFC 9457 document
  with no internal field, and `ParseProblem` reads it back.
- `wlog.Logger.ServiceEnv` returns the service environment the Logger resolved, which the
  HTTP capture policy reads to decide whether to capture everything.

### Changed

- The `llm` group renames three keys for shape v2. `llm.model` becomes `llm.request_model`,
  `llm.cached_input_tokens` becomes `llm.cache_read_input_tokens`, and `llm.finish_reason`
  becomes `llm.finish_reasons`, an array with one entry per model call.
- A write with no event on the context reports `WLOG_NO_EVENT` with the key name, so a
  silent drop becomes visible. `wlog.Error` with no event writes one `log` event at level
  `error`, and it carries the `ErrorInfo`.

- The `Keeper` hook reads the enriched, read-only event:
  `Keep(ctx context.Context, event wlog.Event) bool`. `KeeperFunc` takes the same shape. A
  keeper reads a field through `event.Get("path")` and not by indexing a map. The tail keep
  runs after the enrichers, so a keeper sees every enriched field.
- `WithSampler` is replaced by `WithKeepers`, which takes `Keeper` values. A head decision
  uses `WithHeadSampler` and the new `HeadSampler` interface. `sample.New` implements both,
  so an existing sampler still works as a `Keeper`.
- The event shape is v2. The metadata is one nested `wlog` object, `duration_ms` is a float
  with microsecond precision, and every event carries an `event_id`. A reader changes
  `event["wlog.schema_version"]` to `event["wlog"].(map[string]any)["schema_version"]` and
  reads `duration_ms` as a float.
- The failure of a stage reports through `OnProblem`, so a problem hook sees every dropped
  field, disabled drain, and hook panic in one place.

### Removed

- The package-level `wlog.SetEnabled` and `wlog.Enabled` are gone. Each Logger has
  `SetEnabled` and `Enabled`, so one Logger that is off leaves every other Logger on.
- `wlog.FieldsFlat`, `wlog.FieldsOTel`, `wlog.FieldNames`, and `wlog.WithFieldNames` are
  gone. `wlog.WithOutput` with a preset replaces them.
- `memory.Named`, `memory.Remove`, and `memory.Stores` are gone. They held a second
  package-level registry, and a caller passes the store it wants instead.

- `WithSampler` is gone. `WithKeepers` and `WithHeadSampler` replace it, because a head
  decision and a tail keep are different questions.

### Fixed

- The nightly integration stack runs to the end. It had never done so. The collector
  pin named a tag the registry had deleted. The collector ran as a user with no permission
  to write the output mount. The ClickHouse image refused its default user from outside
  localhost. The collector also holds its output file open, so the otlp test removed
  the file that the collector was still writing to.
- `tools release` reads every require line back before it tags, and it reads the change
  line the plan writes. It used to skip that line, edit nothing, and tag the release
  anyway.

## [0.5.0] - 2026-09-17

v0.5.0 lands the phase 10 fixes: an honest build and the safety findings the later rewrites do not
replace. The section below names every audit id this release closes.

### Added

- `wlog map` gains the `keys.strict` rule. A literal key that is a near miss of a declared typed
  key is reported at the `Set` call, such as `Set(ctx, "orderID", v)` beside
  `NewKey[string]("order_id")`.
- `audit.Patch` returns RFC 6902 operations, and a denied path carries the replacement text.
  `audit.OnlyDrain` forwards the audit fact alone. `redact.Redactor` gains `DeniesPath` and
  `Replacement`, so an adapter can mask a value itself.
- `catalog.Audit` gains `Description`, `RequiresChanges`, and `RedactPaths`. A record that breaks a
  policy rule carries `violations`, and `RedactPaths` masks the value of a matching change.
  `catalog.Entry` gains `Data` and `Internal` defaults, which merge under the call-site values.
- The map rules run as a golangci-lint v2 module plugin (`cmd/wlog/golangci`).
- `wlog.CountDropped` lets an adapter count a cap it applied itself.
- `memory.Remove(name)` unregisters a named store.

### Changed

- `wlog doctor` loads the package under `--dir`. When the package does not load, the run fails.
  Every finding carries a `WLOG_DOCTOR_*` code, a why, and a fix. `--json` prints one object.
- `wlog agents` refuses an unpaired fence and names its line. It never overwrites a skill without
  the wlog marker, and it keeps the file's line endings. Every template snippet compiles and runs.
- `wlog init` rewrites with `go/ast`. It wraps a nil handler around the default mux. It wraps the
  Handler field of an `http.Server` literal. `.env.example` merges instead of replacing.
  `--dry-run` prints a unified diff. Every write goes through a temporary file and a rename. An
  unknown `--drain` exits 2.
- `wlog map` honors `//wlog:ignore <rule> -- <reason>`. A directive with no reason is a finding
  under `ignore.reason`. `--baseline git:<ref>` reads the map at a revision. `--no-write` skips the
  file. `--format sarif` writes SARIF for GitHub code scanning.
- The analyzer takes `-suggest` and `-rules`, skips test files, and reports at the offending call.
  Loading reads dependencies as export data and only the app's own packages for real. A two-file
  scan fell from 354 MB to 4 MB of heap. This adds the
  `github.com/golangci/plugin-module-register` dependency to `cmd/wlog` alone.
- The map JSON is version 2. It carries `tool_version`, `rules_version`, and a `summary` with the
  projected score. It names a framework with a short id and a file with a module-relative path.
  `top_fixes` holds objects with a fix and a docs link. `evidence` holds `file:line` for each rule
  result. The text report lists each failing handler with its file:line, rule, fix, and docs link.
  It then prints `FIX FIRST` with the projected score. `--strict` needs `--baseline`, and it
  compares per handler and per rule. `wlog --help` lists every command and the exit codes.
- `wlog map` writes no file on a failed gate. It refuses an `--out` path equal to `--baseline`.
  Config comes only from `wlog.map.yaml`, never from its own `wlog.map.json` output. A flag beats
  the config, including `--min-score 0`. Every human line goes to stderr, and `--json` leaves one
  document on stdout. The text report wraps at `COLUMNS`, and color follows `NO_COLOR`.
- `wlog map` reports `n/a` for a rule with nothing to check. An `n/a` rule adds no points and no
  weight, so the three error rules no longer hand every handler 45 free points. Print logging means
  stdout or the standard logger, so `fmt.Fprintf(w, ...)` is not a finding. `swallowed-error` reads
  the control flow graph, skips a write to the response writer, and needs an error result. A
  sensitive word matches a whole path segment or word, so /authors is not sensitive. A generated
  file is not scored. `keys.no_denylisted` covers a typed key's name.
- `wlog map` keeps the prefix of an Echo or Gin group and of a mux `PathPrefix().Subrouter()`. It
  reads the Echo `Group` and Gin `Group` shapes. A mux chain that names several methods gives one
  entry per method. It reads the handler before Echo's per-route middleware, and it resolves a
  route written as a constant. It credits the middleware rule for a router wrapped in another
  package.
- `wlog map` resolves a handler to the code that runs, across packages: a named handler in another
  package, an `http.HandlerFunc(fn)` conversion, an `echo.WrapHandler` or `gin.WrapH` adapter, a
  closure factory, and a type with `ServeHTTP`. It reads the chained mux route, the Echo `Add` and
  `Match` shapes, and the Gin `Match` shape. It reads a route written as a constant, such as
  `http.MethodPost`. A run prints `N handlers found`, and zero handlers exits 2 instead of
  reporting a perfect score.
- `drain/memory` deep-copies an event for every subscriber and every snapshot entry. It counts a
  subscriber's drops in `Dropped()`. `Query` with `Limit` returns the newest N matches. It
  subscribes before it writes the SSE replay, so an event cannot fall between the two.
- `wlogtest.New` writes nothing at all. `RequireField` reads a dotted path, and it compares a
  slice, a map, or a number of another width by value.
- `enrich.Geo` names one provider, such as `enrich.Geo("cloudfront")`, instead of trying several.
  A header a client sent itself is therefore no longer trusted, and the doc states the spoofing
  risk. An enricher never replaces a non-map value at its group key. `UserAgent` and `User` take
  `Overwrite`. Geo latitude and longitude are floats, and a percent-encoded city is decoded. The
  user-agent table holds real clients, with bots and tools classed apart.
- `sample.New` returns an error, and `sample.MustNew` joins it. `sample.Rate` takes a float
  percentage. A `**` segment crosses path segments. A head decision follows `trace.trace_id`, and
  a kept event records `wlog.sample_rate`. `KeepErrorsAndSlow` keeps warn and 5xx instead of
  sampling a warn away. Migration: where the old call returned a keeper, use
  `sample.MustNew`. A typed float reads `0` as `0.0`.
- `llm.Record` gains `CacheWriteInputTokens`, a subset of `InputTokens`, so a cache write is billed
  above the input rate. `llm.Cost` splits into `InputMicros` (uncached only), `CacheReadMicros`,
  `CacheWriteMicros`, and `OutputMicros`. `llm.Price` gains `CacheWritePerMillion`.
  `DefaultPrices` is rebuilt from the vendor pages, and it prices a dated snapshot by the longest
  matching row name. The rows that were wrong are gone.
- `llm.calls` and `llm.tool_calls` stop at 200 entries, with the extras counted in
  `wlog.dropped_fields`. `llm.cost_usd` is gone, so money stays in whole micros. The price enricher
  never replaces a cost the caller set. When a write replaces a value, event size accounting now charges
  the difference. A growing array is therefore not charged again on every write.
- The event's `audit` field is an array of up to 20 records, so a second `audit.Do` adds a record
  instead of replacing the first. A `Do` after the event ended emits its own event. Migration: read
  `event["audit"].([]any)` where the code read a single object.
- `audit.Journal` owns the hash chain and hashes the exact bytes it writes, so one changed byte
  fails `Verify`. The `audit.Chain` drain is gone. Signing is
  `audit.Journal(path, audit.WithKey(key))` instead of the `audit.Sign` drain. Migration: register
  `audit.Journal(path)` alone, and pass the key through `WithKey`.
- `audit.Journal` takes one writer per file. It moves a half-written last line to `<path>.partial`,
  writes a marker line every 100 records and on `Close`, and accepts `audit.WithOnError`.
  `audit.VerifyHead` catches a journal cut at the end, and `audit.VerifySigned` requires a
  signature on every line.
- `audit.Diff` returns a nested tree and an error, instead of a flat dotted map.
- `audit.Wrap` records the code the Logger's own extractor produced, so `audit.error_code` agrees
  with `error.code`. A 401 or 403 records the outcome `denied`.
- `audit.Mock` is silent and ignores `WLOG_LEVEL`.
- `catalog.Extractor` returns an error, so `MustExtractor` joins it. It matches full codes only,
  and `Registry.AllowShortCodes()` opts one registry in. A code that already carries the
  registry's prefix keeps it, and `errors.Is` matches an entry from its own registry only.
  Migration: use `catalog.MustExtractor(...)`, and declare a domain-qualified herr class code.
- `catalog.Extractor` writes `error.attrs.domain`, and it merges `Data` and `Internal` defaults
  under the call-site values. `Get` and `CodedError.Entry` return deep copies. A template renders
  in one pass, and the extractor no longer copies a raw template into `ErrorInfo.Message`.
- `pipeline.MinLevel` filters an event by level before the buffer, while an event with audit
  records always passes.
- The shared HTTP drain helper moved from `internal/httpdrain` to the public package
  `pipeline/httpdrain`. It takes `WithHTTPClient`, `WithIdentityHeaders`, and `WithUserAgent`.
- The Sentry drain's constructors are `New` (an async drain), `NewSender` (the raw sender),
  `MustNew`, and `WithPipeline`, matching the v1 drains. A batch now sends one envelope per error
  event, as Sentry requires.
- Better Stack reads `BETTERSTACK_INGESTING_HOST`, and `BETTERSTACK_HOST` stays an alias.
- The PostHog, Better Stack, and HyperDX drains return a sender from `NewSender` and an async drain
  from `New`, with `MustNew` in place of `Must`.
- A failed `audit.Wrap` records the outcome `failure` instead of `error`. Migration: a query that
  filters on `outcome:error` moves to `outcome:failure`.
- `audit.Record` gains `correlation_id`, `causation_id`, and `changes`. `audit.Do` fills the first
  two from the event's trace group, plus a stable `idempotency_key`. `audit.Actor` gains `model`,
  `tools`, and `prompt_id` for the agent actor.

### Closed audit ids

Phase 10 closes these findings from the 2026-09-16 gap audit. Each one names a test in the module
that holds it.

- `repo-ci`: REL-1 to REL-8, CLI-2, CLI-18, DOC-1, DOC-7, PIPE-23, RED-11 (the CI part), SPEC-G16,
  SPEC-G17, SPEC-G20.
- `core`: CORE-2 to CORE-11, CORE-21, CORE-23 to CORE-26, CORE-28 to CORE-33, SPEC-G1, SPEC-G6,
  SPEC-G7, SPEC-G19.
- `redact`: RED-1 to RED-10, RED-12, SPEC-G2.
- `pipeline`: PIPE-1 to PIPE-6, PIPE-10, PIPE-11, PIPE-19, PIPE-22, PIPE-24, PAR-16 to PAR-18,
  SPEC-G5.
- `audit`: AUD-1 to AUD-14, PAR-26 (the schema part), PAR-11.
- `drains`: PIPE-7 to PIPE-9, PIPE-12 to PIPE-18, PIPE-20, PIPE-21, PIPE-25, PAR-21.
- `catalog` and `llm`: CAT-1 to CAT-11, PAR-10.
- `sample`, `enrich`, `drain-memory`, `wlogtest`: SMP-1 to SMP-10, BET-18, SPEC-G13.
- `cli`: CLI-1, CLI-3 to CLI-17, CLI-19 to CLI-22, PAR-6, PAR-28 to PAR-32, PAR-34, BET-9, BET-22.
- `docs`: DOC-2 to DOC-6, DOC-8, SPEC-G21.

### Docs

- Every document passes the Simple English lint, and the skip list is gone. Every Go block either
  compiles and runs, or carries a `<!-- snippet:sketch -->` marker that says it shows an API
  shape. The snippet skip list is gone too.
- `docs/evlog-parity.md` was rebuilt against v0.5.0: every row was checked against the code and
  its tests.
- `CLAUDE.md` gains the proof rule and names the `wlog.SetDefault` pointer as the one allowed
  piece of package-level state.

### Note on the v1.2 to v1.4 approval gap

The v1.2, v1.3, and v1.4 specs in `docs/CAPABILITIES.md` still say "awaiting approval". Their code
shipped in v0.2.0, v0.3.0, and v0.4.0 before the specs were reviewed. v0.5.0 fixes the findings
against that code. The specs need a review pass, and `docs/CAPABILITIES.md` names them.

## [0.4.0] - 2026-09-16

### Added

- The `map` command scores a service against the wlog rules, and it reports the
  entry class, the grade, and the weight of each finding.
- The `wlog init` command writes a setup that compiles, and it covers net/http,
  echo, gin, and gorilla/mux.
- The `wlog doctor` command runs seven checks over a service.
- The `wlog agents` command writes an instruction block and three skills.
- The PostHog, Better Stack, and HyperDX drains.
- An AWS Lambda example with `faas` fields and a flush on a deadline.

## [0.3.0] - 2026-09-16

### Added

- The catalog registry, the extractor, and the `herr` bridge.
- The LLM record, the price table, and the cost enricher.
- The audit record extras, `audit.Diff`, and the test mock.
- HMAC signing and a catalog-driven audit policy.
- Named memory stores, queries, and `Clear`.
- The file drain reader and tailer, and the redaction replacement function.

## [0.2.0] - 2026-09-16

### Added

- The Sentry, ClickHouse, and Datadog drains.
- The `wlogtest` package and the conformance suite over every adapter.
- The batched pipeline with retry, a bounded queue, and drop accounting.

## [0.1.0] - 2026-09-15

### Added

- The core: the event shape, the redactor, the drain interface, the HTTP
  middleware, and the error extractor.
- The adapters for echo, gin, zap, zerolog, logrus, and OpenTelemetry.
- The Loki, OTLP, file, webhook, Axiom, and memory drains.
