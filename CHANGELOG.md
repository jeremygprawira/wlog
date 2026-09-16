# Changelog

Every release names its changes. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the version numbers follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

The v1.0.0 release freezes the public API. A later change to that API waits for v2.

## [Unreleased]

### Added

- Nothing yet. The v0.5.0 tasks run now, and this section fills as they land.

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
