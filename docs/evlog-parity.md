# evlog parity

evlog is the TypeScript logger wlog follows: one wide event per unit of work, enriched as
the work runs, redacted once, and sent to any backend. This page maps every page in the
evlog docs index to a wlog module, a planned module, a documented decision, or a gap.

Source: `https://www.evlog.dev/sitemap.xml` (106 pages) and the full docs text, checked
2026-09-16.

## Status key

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
| structured-errors | partial | `ErrorInfo` with code, message, kind, status, cause, stack, why, fix, link, attrs, plus a pretty error block. Missing `data` and `internal` split, `throwError`, `parseError`, `captureException` |
| lifecycle | built | fixed stage order and plugin hooks |
| sampling | built | `sample.New` with `Rate`, `KeepStatus`, `KeepDuration`, `KeepPath`, `KeepFunc` |
| redaction | partial | `redact` with add/remove keys and patterns, builtins toggle, replacement, transform. Missing a replacement function of the matched value |
| typed-fields | built | `Key[T]` and `StrictKeys` |
| catalogs | gap | `hern`-style error codes exist in `errors/herr`; no catalog registry |

## cli

| evlog page | Status | wlog |
|---|---|---|
| overview | built | `cmd/wlog` |
| init | gap | no scaffolding command |
| map | partial | `wlog map` finds handlers, scores them, writes `wlog.map.json` |
| rules | partial | 6 rules vs evlog's 6 requirements and 4 suggestions |
| scoring | partial | weighted 0-100 score and top-3 fixes. Missing grades, per-entry weighting, entry classes |
| ci | partial | `--min-score` and `--baseline` compare the score only; evlog also fails on per-requirement regression |
| observability-score | partial | same source as scoring |
| doctor | gap | no setup diagnosis command |
| agents | gap | no shipped agent skills or `AGENTS.md` writer |
| telemetry | not adopted | wlog sends no telemetry |

## integrate/adapters

| evlog adapter | Status | wlog |
|---|---|---|
| overview | built | `Drain`, `DrainFunc`, `pipeline.Wrap`, env-first constructors |
| Axiom | built | `drain/axiom` |
| Loki | built | `drain/loki` |
| ClickHouse | built | `drain/clickhouse` with `DDL` |
| OTLP | built | `drain/otlp` |
| Datadog | built | `drain/datadog` |
| Sentry | partial | `drain/sentry` sends issues and logs; evlog's page focuses on its log adapter |
| File system | partial | `drain/file` writes NDJSON with rotation. Missing a reader and tailer |
| Memory | partial | `drain/memory` ring buffer, `Snapshot`, `Subscribe`, `SSEHandler`. Missing named stores, filter queries, and a `clear` call |
| PostHog | gap | no drain |
| Better Stack | gap | no drain |
| HyperDX | gap | no drain |
| NuxtHub | not adopted | Cloudflare and TypeScript specific |

## integrate/frameworks

| evlog framework | Status | wlog |
|---|---|---|
| overview | built | `middleware/nethttp`, `middleware/echo`, `middleware/echo5`, `middleware/gin` |
| Nuxt, Next.js, SvelteKit, Nitro, TanStack Start, NestJS, Express, Hono, Fastify, Elysia, React Router, Cloudflare Workers, Astro, oRPC | not adopted | TypeScript frameworks |
| Standalone TypeScript | built | `wlog.Start` works in any Go program, job, or worker |
| AWS Lambda | gap | `wlog.Start` works in a handler, but there is no Lambda example or request helper |

## extend

| evlog page | Status | wlog |
|---|---|---|
| overview | built | `Drain`, `Enricher`, `Keeper`, plugins |
| custom-drains | built | `wlog.DrainFunc`, `pipeline.Sender` |
| custom-enrichers | built | `wlog.EnricherFunc` |
| custom-framework | partial | `middleware/nethttp` is the base to wrap; no adapter guide |
| drain-pipeline | built | `pipeline.Wrap` with batching, retry, bounded buffer, `FanOut`, `Close` |
| identity-headers | built | `internal/httpdrain` sends `User-Agent: wlog/<version>` and `X-Wlog-Source` |
| stream | built | `drain/memory` with `SSEHandler` |
| plugins | built | `Plugin` plus optional `Setup`, `Enricher`, `Keeper`, `Drain`, `RequestStarter`, `RequestFinisher` |
| tail-sampling | built | `sample` keep rules |
| fs-reader | gap | no `readFsLogs` or `tailFsLogs` |
| diagnostics-channel | not adopted | Node runtime specific; the Go analogue is the slog input handler, which is built |
| consumer-recipes | partial | `docs/customization.md` has drain recipes, not the full set |

## use-cases

| evlog page | Status | wlog |
|---|---|---|
| overview | built | README and `examples/` |
| enrichers | built | `enrich.Host`, `Deployment`, `UserAgent`, `Geo`, `User` |
| structured-logging-nodejs | built | README quick start covers the Go shape |
| audit: overview, schema, recording, pipeline, compliance, recipes | partial | `audit.Do` with actor, action, target, outcome, reason, hash chain, journal, and `Verify`. Missing `version`, `idempotencyKey`, `context`, `deny`, `withAudit`, `auditDiff`, `auditOnly`, HMAC signing, `mockAudit`, audit catalogs, and the compliance guidance |
| client-logging | not adopted | browser specific |
| better-auth: overview, middleware, identify-user, client-sync, performance | not adopted | TypeScript auth library; the concept maps to `enrich.User` and `WithUserFunc` |
| AI SDK: overview, usage, options, metadata, telemetry | designed for | `enrich-llm` in CAPABILITIES.md |
| eve | not adopted | Vercel agent framework; the concept is covered by the `enrich-llm` plan |
| telemetry: overview, setup, ingest, reference | not adopted | product telemetry for CLI authors |

## reference

| evlog page | Status | wlog |
|---|---|---|
| overview | built | this docs set |
| configuration | partial | options, env vars, code wins over env. Missing a global enable switch, a silent switch, and a raw-object stringify mode |
| performance | built | benchmark budget, `make bench` |
| cost | gap | no cost calculator or cost docs |
| best-practices | gap | no Go best-practice page |
| vite-plugin | not adopted | Vite and TypeScript specific |
| vs-other-loggers | not adopted | TypeScript library comparison |
| agent-skills | gap | wlog ships no agent skills |

## Gaps that are not TypeScript-specific

Ranked by value, with the evlog page that motivates each one.

| # | Gap | Source | Effort |
|---|---|---|---|
| 1 | AI and LLM observability: token usage, tool calls, streaming metrics, and cost on the event | `use-cases/ai-sdk/*` | M, `enrich-llm` |
| 2 | Error catalogs: one registry per domain with prefix, status, message template, and audit action metadata | `learn/catalogs` | M |
| 3 | Audit extras: `version`, `idempotencyKey`, `context`, `deny`, `withAudit`, `auditDiff`, `auditOnly`, HMAC signing, `mockAudit`, audit catalogs | `use-cases/audit/*` | M |
| 4 | File reader: `readFsLogs` and `tailFsLogs` with level, time, and custom filters | `extend/fs-reader` | S |
| 5 | Memory queries: named stores, filtered reads, and a clear call | `integrate/adapters/self-hosted/memory` | S |
| 6 | `wlog init`: scaffold wlog into a Go project and write the config | `cli/init` | M |
| 7 | `wlog doctor`: check that the module, middleware, drains, and env vars are wired | `cli/doctor` | S |
| 8 | `wlog agents`: ship logging, audit, and log-analysis skills plus an `AGENTS.md` block | `cli/agents`, `reference/agent-skills` | S |
| 9 | Map rules for structured errors and swallowed errors, plus catalog and audit-coverage suggestions | `cli/rules` | M |
| 10 | Map report extras: `--all`, single-entry-point view, `--json`, classes, grades, and per-entry weighting | `cli/map`, `cli/scoring` | M |
| 11 | Three more drains: PostHog, Better Stack, HyperDX | `integrate/adapters/*` | M each |
| 12 | Structured error helpers: `data` and `internal` split, `throwError`, `parseError` | `learn/structured-errors` | S |
| 13 | Cost and best-practice docs | `reference/cost`, `reference/best-practices` | S |
| 14 | AWS Lambda example | `integrate/frameworks/aws-lambda` | XS |
| 15 | Global enable, silent, and raw-object modes | `reference/configuration` | XS |

## Not adopted, and why

| evlog surface | Why wlog does not ship it |
|---|---|
| Browser and client logging | JavaScript in the browser, not the language wlog serves |
| Vite plugin | Vite is a JavaScript build tool |
| NuxtHub storage | Cloudflare Workers and TypeScript |
| Better Auth | A TypeScript auth library; wlog covers the user field with `enrich.User` |
| eve | A TypeScript agent framework; the lasting part is LLM observability, which is gap 1 |
| CLI and product telemetry | wlog sends no telemetry |
| TypeScript framework adapters | Go has its own frameworks, all covered |
