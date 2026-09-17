# Changelog

Every release names its changes. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the version numbers follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

The v1.0.0 release freezes the public API. A later change to that API waits for v2.

## [Unreleased]

### Added

- Nothing yet. The v0.5.0 tasks run now, and this section fills as they land.

### Changed

- The event's `audit` field is now an array of up to 20 records, so a second `audit.Do`
  adds a record instead of replacing the first. A `Do` after the event ended emits its own
  event. Migration: read `event["audit"].([]any)` and take the last element, or the whole
  list, where the code read a single object.
- `audit.Journal` owns the hash chain and hashes the exact bytes it writes, so one
  changed byte fails `Verify`. The `audit.Chain` drain is gone, and signing is now
  `audit.Journal(path, audit.WithKey(key))` instead of the `audit.Sign` drain. Migration:
  register `audit.Journal(path)` alone, and pass the key through `WithKey`.
- `audit.Journal` takes one writer per file, moves a half-written last line to
  `<path>.partial`, writes a marker line every 100 records and on `Close`, and accepts
  `audit.WithOnError`. `audit.VerifyHead` catches a journal cut at the end, and
  `audit.VerifySigned` requires a signature on every line.
- `audit.Diff` returns a nested tree and an error, instead of a flat dotted map.
- A failed `audit.Wrap` records the outcome `failure` instead of `error`. Migration: a query
  that filters on `outcome:error` moves to `outcome:failure`.
- `audit.Record` gains `correlation_id`, `causation_id`, and `changes`, and `audit.Do` fills
  the first two from the event's trace group, plus a stable `idempotency_key`. `audit.Actor`
  gains `model`, `tools`, and `prompt_id` for the agent actor.
- `drain/memory` deep-copies an event for every subscriber and every snapshot entry, counts a
  subscriber's drops in `Dropped()`, returns the newest N matches for `Query` with `Limit`, and
  subscribes before it writes the SSE replay so an event cannot fall between the two.
  `memory.Remove(name)` unregisters a named store.
- `wlogtest.New` writes nothing at all, and `RequireField` reads a dotted path and compares a
  slice, a map, or a number of another width by value.
- `enrich.Geo` names one provider (`enrich.Geo("cloudfront")`) instead of trying several, so a
  header a client could have sent itself is no longer trusted; its doc states the spoofing
  risk. An enricher never replaces a non-map value at its group key, `UserAgent` and `User`
  take `Overwrite`, geo latitude and longitude are floats, a percent-encoded city is decoded,
  and the user-agent table holds real clients with bots and tools classed apart.
- `sample.New` returns an error and `sample.MustNew` joins it. `sample.Rate` takes a float
  percentage, a `**` segment crosses path segments, a head decision follows
  `trace.trace_id`, a kept event records `wlog.sample_rate`, and `KeepErrorsAndSlow` keeps
  warn and 5xx instead of sampling a warn away. Migration: use `sample.MustNew` where the old
  call returned a keeper, and pass `0` as `0.0` if a typed float is in hand.
- `llm.Record` gains `CacheWriteInputTokens`, a subset of `InputTokens`, so a cache write is
  billed above the input rate. `llm.Cost` splits into `InputMicros` (uncached only),
  `CacheReadMicros`, `CacheWriteMicros`, and `OutputMicros`, and `llm.Price` gains
  `CacheWritePerMillion`. `DefaultPrices` is rebuilt from the vendor pages, prices a dated
  snapshot by the longest matching row name, and drops the rows that were wrong.
- `llm.calls` and `llm.tool_calls` stop at 200 entries, with the extras counted in
  `wlog.dropped_fields`, and `llm.cost_usd` is gone: money stays in whole micros. The price
  enricher never replaces a cost the caller set, and `wlog.CountDropped` lets an adapter
  count a cap it applied itself. Event size accounting now charges the difference when a
  write replaces a value, so a growing array is not charged again on every write.
- `catalog.Audit` gains `Description`, `RequiresChanges`, and `RedactPaths`. A record that
  breaks a policy rule now carries `violations` naming each rule, and `RedactPaths` masks the
  value of a matching change operation.
- `catalog.Entry` gains `Data` and `Internal` defaults, which the extractor merges under the
  values a per-request extractor filled, and `catalog.Extractor` writes `error.attrs.domain`.
  `Get` and `CodedError.Entry` now return deep copies, a template renders in one pass, and
  the extractor no longer copies a raw template into `ErrorInfo.Message`.
- `catalog.Extractor` returns an error, so `MustExtractor` joins it, and it matches full
  codes only: `Registry.AllowShortCodes()` opts one registry in. Migration: use
  `catalog.MustExtractor(...)` where the old call discarded nothing, and declare a
  domain-qualified herr class code so both paths agree. A code that already carries the
  registry's prefix keeps it, and `errors.Is` matches an entry from its own registry only.
- `audit.Patch` returns RFC 6902 operations with a denied path masked, and `audit.OnlyDrain`
  forwards the audit fact alone. `redact.Redactor` gains `DeniesPath` and `Replacement` for
  an adapter that must mask a value itself.
- `audit.Wrap` records the code the Logger's own extractor produced, so
  `audit.error_code` agrees with `error.code`. A 401 or 403 records the outcome `denied`.
- `audit.Mock` is silent and ignores `WLOG_LEVEL`.
- `pipeline.MinLevel` filters an event by level before the buffer, while an event with
  audit records always passes.
- The shared HTTP drain helper moved from `internal/httpdrain` to the public package
  `pipeline/httpdrain`, and it takes `WithHTTPClient`, `WithIdentityHeaders`, and
  `WithUserAgent`.
- Better Stack reads `BETTERSTACK_INGESTING_HOST`; `BETTERSTACK_HOST` stays an alias.
- The PostHog, Better Stack, and HyperDX drains return a sender from `NewSender` and an
  async drain from `New`, with `MustNew` in place of `Must`.
- The Sentry drain's constructors are `New` (an async drain), `NewSender` (the raw
  sender), `MustNew`, and `WithPipeline`, matching the v1 drains. A batch now sends one
  envelope per error event, as Sentry requires.

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
