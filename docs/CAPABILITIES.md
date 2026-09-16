# Capability Map: wlog

Approved 2026-09-14 (rev 3: evlog parity additions: audit, plugins, stream, geo, identity headers, typed fields, `wlog map`).
Module ids are stable: specs, plans and tasks refer to them by id.

Packaging rule: when a package imports nothing outside the standard library, it lives in the
root module (`github.com/jeremygprawira/wlog`). A package with a third-party import gets its own
`go.mod` instead, joined through `go.work`.

## v1 modules

| Module id | Responsibility | Depends on | Go module | Phase |
|---|---|---|---|---|
| `redact` | Denylist masking: keys, paths, and value patterns. Keys can be added or removed. Values stay immutable | none | root | 1 |
| `core` | Wide event and ctx API. `Start`/`Detach`, `SetLevel`, errors (`error` and `errors[]`), typed keys (`Key[T]`), plugins, field-name presets, plain log lines. `Drain`/`ErrorExtractor`/`Enricher`/`Keeper` interfaces, JSON and pretty console sinks, env configuration, atomic redactor swap, fixed pipeline order | redact | root | 1 |
| `pipeline` | Async batching, retry/backoff, fan-out, bounded buffer (drop oldest + `OnDropped`), flush on `Close`, shared HTTP drain helper with identity headers | core | root | 2 |
| `sample` | Head (rate per level) and tail (status, duration, path, predicate) sampling. Default keeps 100%, plus presets | core | root | 2 |
| `enrich` | Built-in enrichers: host/pod/region, deploy version, user agent, **geo** (CDN headers), user-id lookup | core | root | 2 |
| `drain-memory` | Ring buffer, snapshot, live subscriptions (`Subscribe`), optional SSE `http.Handler` | core | root | 2 |
| `wlogtest` | In-memory recorder + assertion helpers for users' tests (built on `drain-memory`) | drain-memory | root | 2 |
| `audit` | `wlog.Audit(ctx, …)` for actor, action, target, outcome, and reason. Never sampled. Hash chain, append-only journal drain, `Verify` | core, pipeline | root | 2 |
| `errors-herr` | `ErrorExtractor` for herr | core | own (`errors/herr`) | 2 |
| `http-std` | net/http + gorilla/mux middleware: capture (default everything), request id, W3C traceparent, panic recovery, plugin request hooks, emit | core | root | 2 |
| `http-echo` | Echo v4 adapter over `http-std` | http-std | own (`middleware/echo`) | 3 |
| `http-echo5` | Echo v5 adapter over `http-std` | http-std | own (`middleware/echo5`) | 3 |
| `http-gin` | Gin adapter over `http-std` | http-std | own (`middleware/gin`) | 3 |
| `log-slog` | slog output + slog input (folds slog calls into the event) | core | root | 3 |
| `log-zap` · `log-zerolog` · `log-logrus` | Output adapters | core | own (`log/*`) | 3 |
| `trace-otel` | Trace/span id from an active OpenTelemetry span | core | own (`trace/otel`) | 3 |
| `drain-axiom` · `drain-loki` · `drain-file` · `drain-webhook` · `drain-otlp` | Backend drains | pipeline | root | 4 |
| `cli-map` | `wlog map`: static analysis (`go/analysis`) of handlers. Gives a deterministic 0-100 observability score, `wlog.map.json`, `--min-score`, `--baseline`. Also works as an analyzer from `go vet` or golangci-lint | core, http-*, audit (rules know their APIs) | own (`cmd/wlog`) | 5 |
| `drain-sentry` · `drain-clickhouse` · `drain-datadog` | Backend drains | pipeline | root | 6 |

## Build order

```
Phase 1  redact → core
Phase 2  pipeline, sample, enrich, drain-memory, errors-herr, http-std → wlogtest, audit
Phase 3  http-echo, http-echo5, http-gin, log-slog, log-zap, log-zerolog, log-logrus, trace-otel
Phase 4  drain-axiom, drain-loki, drain-file, drain-webhook, drain-otlp
Phase 5  cli-map                                                         → v1 release
Phase 6  drain-sentry, drain-clickhouse, drain-datadog                   → v1.1
```

`cli-map` comes last in v1 because its rules detect the public APIs of core, the HTTP adapters,
and audit. Building it earlier means rewriting rules every time those APIs move.

## Designed for, not built in v1

`core` must support these without API changes. Each becomes its own module later.

| Future module id | Responsibility |
|---|---|
| `log-zap-in`, `log-zerolog-in`, `log-logrus-in` | Fold zap/zerolog/logrus calls into the event |
| `job` | Helpers for cron/scripts/workers (the `wlog.Start` primitive already ships in core) |
| `queue-kafka`, `queue-nats`, `queue-rabbitmq`, `queue-sqs` | One event per consumed message |
| `grpc` | Unary + stream server interceptors |
| `http-client` | `http.RoundTripper` recording outbound calls into the parent event |
| `enrich-llm` | Token usage / model / cost fields for LLM calls (evlog AI SDK equivalent) |

Not adopted from evlog (TypeScript/browser-specific): client/browser logging, Vite plugin, NuxtHub,
Better Auth integration, CLI telemetry.

Out of scope for this initiative: migrating `go-echo-boilerplate` to wlog (separate spec).

## v1.2 to v1.4 modules (proposed 2026-09-16, awaiting approval)

These close the 15 gaps the evlog audit found. See [evlog parity](evlog-parity.md) for the
audit itself. The gap number in each row points back to that page's ranked table.

Three phases, one release each. The API phase lands first because the CLI phase reads
those APIs, the same reason `cli-map` came last in v1.

### Phase 7, ships v1.2: public API

| Module id | Responsibility | Gap | Depends on | Go module |
|---|---|---|---|---|
| `core` (additions) | `ErrorInfo` gains a `data` and `internal` split. New `wlog.Enabled`, `wlog.Silent`, and a raw-object mode | 12, 15 | none | root |
| `catalog` | Error-library-agnostic registry. One domain per registry, with prefix, code, status, message template, and audit metadata (target type, severity, reason required) | 2 | core | root |
| `llm` | Typed `llm.Record` for model, provider, tokens, tool calls, stream timings, and cost. Pricing table and cost calculator. Enricher that writes the record onto the event | 1, 13 | core | root |
| `audit` (additions) | `version`, `idempotency_key`, `context`, `deny`, `Wrap`, `Diff`, `Only`, HMAC signing, a test mock, and audit entries read from `catalog` | 3 | core, catalog | root |
| `drain-memory` (additions) | Named stores, filtered reads, and `Clear` | 5 | core | root |
| `drain-file` (additions) | `Read` and `Tail` with level, time, and custom filters | 4 | core | root |
| `redact` (addition) | A replacement function of the matched value, next to the fixed replacement string | partial | none | root |
| `errors/herr` (addition) | Optional bridge that maps a herr `Class` to a `catalog` entry. herr stays optional, never required | 2 | catalog | own |

### Phase 8, ships v1.3: CLI

| Module id | Responsibility | Gap | Depends on | Go module |
|---|---|---|---|---|
| `cli-init` | `wlog init` scaffolds wlog into a Go project and writes the starting configuration | 6 | cli-map | own (`cmd/wlog`) |
| `cli-doctor` | `wlog doctor` reports on the module, middleware, drains, and env vars | 7 | cli-map | own (`cmd/wlog`) |
| `cli-agents` | `wlog agents` writes an `AGENTS.md` block and installs 3 portable markdown skills | 8 | cli-map | own (`cmd/wlog`) |
| `cli-map` (rules) | New rules for `why`/`fix` on errors and swallowed errors. New suggestions for catalog use and audit coverage | 9 | catalog, audit | own (`cmd/wlog`) |
| `cli-map` (report) | `--all` matrix, single-entry view, `--json`, entry classes, grades, per-entry weighting, and per-rule baseline regression | 10 | cli-map | own (`cmd/wlog`) |

### Phase 9, ships v1.4: drains, docs, example

| Module id | Responsibility | Gap | Depends on | Go module |
|---|---|---|---|---|
| `drain-posthog` | PostHog drain | 11 | pipeline | root |
| `drain-betterstack` | Better Stack drain | 11 | pipeline | root |
| `drain-hyperdx` | HyperDX drain | 11 | pipeline | root |
| `examples/lambda` | AWS Lambda example and request helper | 14 | core | own |
| (docs) | Best-practice guide. Cost guide ships with `llm` in phase 7 | 13 | none | none |

### Build order

```
Phase 7  core additions → catalog → llm → audit additions
                        → drain-memory, drain-file, redact, errors/herr bridge
Phase 8  cli-map rules → cli-map report → cli-init, cli-doctor, cli-agents
Phase 9  drain-posthog, drain-betterstack, drain-hyperdx, lambda example, docs
```

### Agnostic rule, restated

`catalog` imports nothing outside the standard library and knows about no error library.
A herr user gets a bridge in `errors/herr`. A user of standard `errors` gets the same
registry with no bridge at all.


## Specs

| Module id | Spec | Status |
|---|---|---|
| (project-wide) | [SPEC.md](SPEC.md) | approved 2026-09-15 (v3) |
| `redact` | [SPEC-redact.md](SPEC-redact.md) | approved 2026-09-15 (v2) |
| (plan) | [tasks/plan.md](../tasks/plan.md) | approved 2026-09-15 |
| `core` | [SPEC-core.md](SPEC-core.md) | approved 2026-09-15 |
| `pipeline` | [SPEC-pipeline.md](SPEC-pipeline.md) | approved 2026-09-15 |
| `sample` | [SPEC-sample.md](SPEC-sample.md) | approved 2026-09-15 |
| `drain-memory` | [SPEC-drain-memory.md](SPEC-drain-memory.md) | approved 2026-09-15 |
| `wlogtest` | [SPEC-wlogtest.md](SPEC-wlogtest.md) | approved 2026-09-15 |
| `http-std` | [SPEC-http-std.md](SPEC-http-std.md) | approved 2026-09-15 |
| `enrich` | [SPEC-enrich.md](SPEC-enrich.md) | approved 2026-09-15 |
| `errors-herr` | [SPEC-errors-herr.md](SPEC-errors-herr.md) | approved 2026-09-15 |
| `audit` | [SPEC-audit.md](SPEC-audit.md) | approved 2026-09-15 |
| `http-echo`, `http-echo5`, `http-gin` | [SPEC-http-adapters.md](SPEC-http-adapters.md) | approved 2026-09-16 |
| `log-slog`, `log-zap`, `log-zerolog`, `log-logrus` | [SPEC-log-adapters.md](SPEC-log-adapters.md) | approved 2026-09-16 |
| `trace-otel` | [SPEC-trace-otel.md](SPEC-trace-otel.md) | approved 2026-09-16 |
| `drain-axiom` · `drain-loki` · `drain-file` · `drain-webhook` · `drain-otlp` | [SPEC-drains-v1.md](SPEC-drains-v1.md) | approved 2026-09-16 |
| `cli-map` | [SPEC-cli-map.md](SPEC-cli-map.md) | approved 2026-09-16 |
| `drain-sentry` · `drain-clickhouse` · `drain-datadog` | [SPEC-drains-v1.1.md](SPEC-drains-v1.1.md) | approved 2026-09-16 |
| `catalog` | [SPEC-catalog.md](SPEC-catalog.md) | drafted 2026-09-16, awaiting approval |
| `llm` | [SPEC-llm.md](SPEC-llm.md) | drafted 2026-09-16, awaiting approval |
| v1.2 additions to core, audit, drain-memory, drain-file, redact, errors-herr | [SPEC-v1.2-additions.md](SPEC-v1.2-additions.md) | drafted 2026-09-16, awaiting approval |
| `cli-init` · `cli-doctor` · `cli-agents` · cli-map additions | [SPEC-cli-v1.3.md](SPEC-cli-v1.3.md) | drafted 2026-09-16, awaiting approval |
| `drain-posthog` · `drain-betterstack` · `drain-hyperdx` | [SPEC-drains-v1.4.md](SPEC-drains-v1.4.md) | drafted 2026-09-16, awaiting approval |
| others | SPEC-<id>.md | not started |
