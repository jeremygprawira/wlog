# evlog parity

evlog is the TypeScript logger wlog follows: one wide event per unit of work, enriched as
the work runs, redacted once, and sent to any backend. This page maps every page in the
evlog docs index to a wlog module, a planned module, a documented decision, or a gap.

Source: `https://www.evlog.dev/sitemap.xml` (106 pages) and the full docs text, checked
2026-09-16.

## Status key

This page was rebuilt against v0.5.0: every row below was checked against the code and its tests.
A test proves every row that says built. A row that says partial names what is missing.

| Status | Meaning |
|---|---|
| built | wlog ships it with a test |
| partial | wlog ships part of it, or a smaller shape |
| designed for | named in CAPABILITIES.md, not built yet |
| gap | not TypeScript-specific, and not built |
| not adopted | TypeScript, browser, or vendor-specific |

## start

| evlog page | Status | wlog |
|---|---|---|
| introduction, why-evlog, installation, quick-start | built | README quick start, `examples/`, `go doc` per package |
| typescript-logging-library | not adopted | TypeScript comparison |

## learn

| evlog page | Status | wlog |
|---|---|---|
| overview | built | README and `docs/event-shape.md` |
| simple-logging | built | `wlog.Info`, `wlog.Warn`, `wlog.Debug` |
| wide-events | built | `Start`, `Set`, `SetGroup`, `Append`, `Detach` |
| structured-errors | built | `ErrorInfo` with code, message, kind, status, cause, stack, why, fix, link, attrs, plus the `Data` and `Internal` split, `Errorf`, and `DefaultExtractor` |
| lifecycle | built | fixed stage order and plugin hooks |
| sampling | built | `sample.MustNew` with `Rate`, `KeepStatus`, `KeepDuration`, `KeepPath`, `KeepFunc`. A `**` segment crosses path segments, a head decision follows `trace.trace_id`, and a kept event records `wlog.sample_rate` |
| redaction | built | `redact` with add/remove keys, add/remove patterns, a builtins toggle, a fixed replacement, a transform, and `Replace` for a replacement of the matched value. It also offers `DeniesPath` and `Replacement` |
| typed-fields | built | `Key[T]` and `StrictKeys` |
| catalogs | built | `catalog` registry with prefixes, templates, status, guidance, audit policy, and an extractor decorator |

## cli

| evlog page | Status | wlog |
|---|---|---|
| overview | built | `cmd/wlog` |
| init | built | `wlog init` detects the framework and writes a compiling setup |
| map | built | finds handlers across packages, scores them, writes `wlog.map.json`, and prints `--all`, `--entry`, `--json`, or `--format sarif`. A run prints `N handlers found`, and zero handlers exits 2 |
| rules | built | 8 requirements and 2 suggestions, with a class per entry point |
| scoring | built | weighted 0-100 score, A to F grade, read/write/sensitive classes, and top-3 fixes |
| ci | built | `--min-score`, `--baseline` (a path or `git:<ref>`), and `--strict` per handler and per rule. `--no-write`, `//wlog:ignore <rule> -- <reason>`, and a golangci-lint v2 module plugin too |
| observability-score | built | same source as scoring |
| doctor | built | `wlog doctor` loads from `--dir` and runs eight checks. Each check carries a `WLOG_DOCTOR_*` code, a why, and a fix, and `--json` prints one object |
| agents | built | `wlog agents` writes a fenced `AGENTS.md` block and three skills. It refuses an unpaired fence with its line number, and it never overwrites a skill without the wlog marker |
| telemetry | not adopted | wlog sends no telemetry |

## integrate/adapters

| evlog adapter | Status | wlog |
|---|---|---|
| overview | built | `Drain`, `DrainFunc`, `pipeline.Wrap`, and the env-first constructors `New`, `NewSender`, `MustNew`, and `WithPipeline` |
| Axiom | built | `drain/axiom` |
| Loki | built | `drain/loki` |
| ClickHouse | built | `drain/clickhouse` with `DDL` |
| OTLP | built | `drain/otlp` |
| Datadog | built | `drain/datadog` |
| Sentry | partial | `drain/sentry` sends issues and logs. Evlog's page focuses on its log adapter |
| File system | built | `drain/file` writes NDJSON with rotation, and `file.Read`/`file.Tail` read and follow it |
| Memory | built | ring buffer, `Snapshot`, `Subscribe`, `SSEHandler`, plus `Named`, `Stores`, `Query`, `Clear`, and `Remove`. Every subscriber and snapshot entry gets its own deep copy, and `Dropped()` counts a slow subscriber's losses |
| PostHog | built | `drain/posthog` |
| Better Stack | built | `drain/betterstack` |
| HyperDX | built | `drain/hyperdx` |
| NuxtHub | not adopted | Cloudflare and TypeScript specific |

## integrate/frameworks

| evlog framework | Status | wlog |
|---|---|---|
| overview | built | `middleware/nethttp`, `middleware/echo`, `middleware/echo5`, `middleware/gin` |
| Nuxt, Next.js, SvelteKit, Nitro, TanStack Start, NestJS, Express, Hono, Fastify, Elysia, React Router, Cloudflare Workers, Astro, oRPC | not adopted | TypeScript frameworks |
| Standalone TypeScript | built | `wlog.Start` works in any Go program, job, or worker |
| AWS Lambda | built | `examples/lambda` with a handler wrapper and a flush before return |

## extend

| evlog page | Status | wlog |
|---|---|---|
| overview | built | `Drain`, `Enricher`, `Keeper`, `Measurer`, plugins |
| custom-drains | built | `wlog.DrainFunc`, `pipeline.Sender` |
| custom-enrichers | built | `wlog.EnricherFunc` |
| custom-framework | partial | `middleware/nethttp` is the base to wrap. No adapter guide |
| drain-pipeline | built | `pipeline.Wrap` with batching, retry, bounded buffer, `FanOut`, `Close` |
| identity-headers | built | `pipeline/httpdrain` sends `User-Agent: wlog/<version>` and `X-Wlog-Source` |
| stream | built | `drain/memory` with `SSEHandler` |
| plugins | built | `Plugin` plus optional `Setup`, `Starter`, `Finisher`, `Enricher`, `Keeper`, `Measurer`, `Drain` |
| tail-sampling | built | `sample` keep rules |
| fs-reader | built | `file.Read` and `file.Tail` |
| diagnostics-channel | not adopted | Node runtime specific. The Go analogue is the slog input handler, which is built |
| consumer-recipes | partial | `docs/customization.md` has drain recipes, not the full set |

## use-cases

| evlog page | Status | wlog |
|---|---|---|
| overview | built | README and `examples/` |
| enrichers | built | `enrich.Host`, `Deployment`, `UserAgent`, `Geo`, `User` |
| structured-logging-nodejs | built | README quick start covers the Go shape |
| audit: overview, schema, recording, pipeline, compliance, recipes | built | `audit.Do` plus version, idempotency key, context, `Deny`, `Only`, `Wrap`, `Diff`, `Sign`, `Mock`, and catalog-driven policy. The compliance guidance is not written yet |
| client-logging | not adopted | browser specific |
| better-auth: overview, middleware, identify-user, client-sync, performance | not adopted | TypeScript auth library. The concept maps to `enrich.User` and `WithUserFunc` |
| AI SDK: overview, usage, options, metadata, telemetry | built | `llm` records tokens, tools, stream timing, and exact cost. It is not an SDK wrapper, so a caller fills the record |
| eve | not adopted | Vercel agent framework. The concept is covered by the `enrich-llm` plan |
| telemetry: overview, setup, ingest, reference | not adopted | product telemetry for CLI authors |

## reference

| evlog page | Status | wlog |
|---|---|---|
| overview | built | this docs set |
| configuration | built | options, env vars, code wins over env, plus `SetEnabled`, `WithSilent`, and `WithRawValues` |
| performance | built | benchmark budget, `make bench` |
| cost | partial | `docs/cost.md` prices calls and charts spend. No interactive calculator |
| best-practices | built | `docs/best-practices.md`, every rule tied to a test |
| vite-plugin | not adopted | Vite and TypeScript specific |
| vs-other-loggers | not adopted | TypeScript library comparison |
| agent-skills | built | `wlog agents` writes three skills and an `AGENTS.md` block |

## Gaps that are not TypeScript-specific

Ranked by value, with the evlog page that motivates each one.

| # | Gap | Source | Effort |
|---|---|---|---|
| 1 | ~~AI and LLM observability~~ done in v1.2 as `llm` | `use-cases/ai-sdk/*` | done |
| 2 | ~~Error catalogs~~ done in v1.2 as `catalog` | `learn/catalogs` | done |
| 3 | ~~Audit extras~~ done in v1.2, except the compliance guide | `use-cases/audit/*` | mostly done |
| 4 | ~~File reader~~ done in v1.2 as `file.Read` and `file.Tail` | `extend/fs-reader` | done |
| 5 | ~~Memory queries~~ done in v1.2 as `Named`, `Query`, and `Clear` | `integrate/adapters/self-hosted/memory` | done |
| 6 | ~~`wlog init`~~ done in v1.3 | `cli/init` | done |
| 7 | ~~`wlog doctor`~~ done in v1.3 | `cli/doctor` | done |
| 8 | ~~`wlog agents`~~ done in v1.3 | `cli/agents`, `reference/agent-skills` | done |
| 9 | ~~Map rules~~ done in v1.3 | `cli/rules` | done |
| 10 | ~~Map report extras~~ done in v1.3 | `cli/map`, `cli/scoring` | done |
| 11 | ~~Three more drains~~ done in v1.4 | `integrate/adapters/*` | done |
| 12 | ~~Structured error helpers~~ done in v1.2 as `Data`/`Internal`, `Errorf`, and `DefaultExtractor` | `learn/structured-errors` | done |
| 13 | ~~Cost and best-practice docs~~ done as `docs/cost.md` and `docs/best-practices.md` | `reference/cost`, `reference/best-practices` | done |
| 14 | ~~AWS Lambda example~~ done in v1.4 | `integrate/frameworks/aws-lambda` | done |
| 15 | ~~Global enable, silent, and raw modes~~ done in v1.2 | `reference/configuration` | done |

## Not adopted, and why

| evlog surface | Why wlog does not ship it |
|---|---|
| Browser and client logging | JavaScript in the browser, not the language wlog serves |
| Vite plugin | Vite is a JavaScript build tool |
| NuxtHub storage | Cloudflare Workers and TypeScript |
| Better Auth | A TypeScript auth library. Wlog covers the user field with `enrich.User` |
| eve | A TypeScript agent framework. The lasting part is LLM observability, which is gap 1 |
| CLI and product telemetry | wlog sends no telemetry |
| TypeScript framework adapters | Go has its own frameworks, all covered |
