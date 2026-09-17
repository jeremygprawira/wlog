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
| `audit` | `audit.Do(ctx, …)` for actor, action, target, outcome, and reason. Never sampled. Hash chain, append-only journal drain, `Verify` | core, catalog, pipeline | root | 2 |
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


## v0.5 to v1.0 modules (approved 2026-09-16)

This initiative fixes every finding in [the gap audit](../tasks/audit-2026-09-16.md) and connects wlog to the rest of the Go ecosystem. It ends with the v1.0.0 tag. The "Closes" column names audit ids (CORE-1, SPEC-G3, PAR-12, BET-2, and so on).

### Goal

One wide event per unit of work that is easy to add and easy to search. That includes an HTTP request, an RPC, a message, a job, a command, and a function call. The event looks the same whatever framework, logger, store, or backend the app uses. People, agents, and code can all read it.

### Decisions recorded 2026-09-16

| Topic | Decision |
|---|---|
| HTTP capture default | Safe by default: method, route, path, status, durations, sizes, ids, user agent, client ip, allow-listed headers, and query and cookie names only. `CaptureAll()` opts into bodies and values. Env `local`, `dev`, and `development` capture all by default |
| Default logger | Add `wlog.SetDefault`, like `slog.SetDefault`. CLAUDE.md gains one named exception for it. The enable switch moves onto the Logger |
| API changes | Break freely before v1.0. Stay on v0.x tags. Every break gets a CHANGELOG line and a migration note. Freeze the API at v1.0.0 |
| Event shape | A: operation is the matched route, level follows status, timestamp is the start, float `duration_ms`, a `summary` sentence, lowercase header names, no empty values. B: `event_id`, a generated trace id, `schema_version`, a published JSON Schema. C: wlog metadata nested under `wlog`. D: fixed key order for stdout and file output |
| Go versions | Old Go keeps working, and new Go gets more. Each module's `go` line is the lowest version its code and dependencies need, never raised for convenience. Newer Go features live in `//go:build go1.N` files. Root drops from 1.23 to 1.21. CI tests each module at its floor and at the two newest Go releases |
| Repo layout | One repo with category folders. Entry points go in `middleware/`, `rpc/`, `queue/`, `job/`, `faas/`, and `command/`. Work inside a unit goes in `client/`, `store/`, `log/`, `errors/`, and `flag/`. Destinations go in `trace/`, `metrics/`, `drain/`, and `ai/` |
| Setup | All four: env-driven setup, one-line `Setup(app)` per adapter, use-case recipes, and a smarter `wlog init` |
| Coverage | Every bundle offered. Entry points: more HTTP routers, RPC, jobs and queues with Kafka clients by name, serverless, and CLI. Inside the work: outbound calls, data stores, logger bridges, error and flag libraries. Destinations: OpenTelemetry, more backends, search tooling, agent and AI tooling |

### How agnostic stays cheap

About 60 integrations share five foundations. An adapter maps its framework onto one of them and passes one shared conformance suite, so every integration emits the same fields.

| Foundation | Shared by |
|---|---|
| `http-core` | every HTTP router and HTTP-based RPC layer |
| `work` | every message, job, RPC, command, and function adapter |
| `core-calls` | every outbound client and data store |
| `log-slog` | every logger bridge that can reach slog, plus direct hooks for the rest |
| `pipeline` | every backend drain |

### Phase 10, ships v0.5: honest build and safety fixes

Existing modules keep their ids. This phase fixes the audit items that the phase 11 rewrites do not replace.

| Module id | Responsibility in this phase | Closes | Depends on | Go module |
|---|---|---|---|---|
| `repo-ci` | A `tools` module and CI. Build and tidy each module with `GOWORK=off`. Test `require` versions, and tag modules in order. Test each module at its Go floor. Compile doc snippets and run the Simple English lint. Add a coverage gate, all fuzz targets, and `govulncheck`. Compare benchmarks with benchstat. Run integration tests with health probes. Test only the modules a change touches | REL-1 to REL-8, DOC-1, DOC-7, RED-11 (CI part), PIPE-9 (CI part), PIPE-23, CLI-2, CLI-18, SPEC-G16, SPEC-G17, SPEC-G20 | none | own (`tools`) |
| `core` | Value fidelity, data ownership, panic isolation, size caps, Detach context, audit bypass of the level filter, drain map contract | CORE-1 to CORE-11, CORE-21, CORE-23 to CORE-26, CORE-28 to CORE-33, SPEC-G1, SPEC-G6, SPEC-G7, SPEC-G19 | redact | root |
| `redact` | Fail closed, joined-token keys, linear-time matching, full configuration in `With`, URL and DSN patterns, fewer false positives, a bounded cache, a full fingerprint | RED-1 to RED-10, RED-12, SPEC-G2 | none | root |
| `pipeline` | Panic recovery, and `OnDropped` outside the lock. `Flush`, and a close bound by ctx. A capped `Retry-After`, batches of `BatchSize`, and clamped options. A safe `FanOut`, a `Dropped` counter, and URL errors with no secrets | PIPE-1 to PIPE-6, PIPE-10, PIPE-11, PIPE-19, PIPE-22, PIPE-24, SPEC-G5 | core | root |
| `audit` | Chain state inside `Journal`. A signed head and marker lines. A hash over the written bytes. Several records per event. Crash recovery, one writer per file, and key ids. A nested `Diff` tree | AUD-1 to AUD-14, SPEC-G4 | core, catalog | root |
| `drain-sentry` · `drain-clickhouse` · `drain-file` · `drain-loki` · `drain-otlp` · `drain-datadog` · `drain-posthog` · `drain-betterstack` · `drain-hyperdx` | The wire format and behavior fixes for each drain. The v1.4 drains return a Sender and `MustNew`, like the v1 drains | PIPE-7, PIPE-8, PIPE-9, PIPE-12 to PIPE-18, PIPE-20, PIPE-21 | pipeline | root |
| `catalog` · `llm` · `errors-herr` | Full-code matching, cross-registry duplicates, copied entries, single-pass templates. Capped `calls[]`, provider-neutral token fields, a corrected price table | CAT-1 to CAT-11 | core | root, own |
| `sample` · `enrich` · `drain-memory` · `wlogtest` | `**` paths, 5xx and warn kept, fractional rates, trace-consistent sampling, `sample_rate` on kept events. A trusted geo provider. Deep-cloned memory events and a drop counter. A silent wlogtest with path-aware assertions | SMP-1 to SMP-10, SPEC-G13 | core | root |
| `cli-map` · `cli-init` · `cli-doctor` · `cli-agents` | Handler discovery across packages, and no score for zero handlers. No baseline overwrite, and an n/a status. Group prefixes. Correct init and doctor, and safe fences. Status lines on stderr. A golangci-lint plugin | CLI-1, CLI-3 to CLI-17, CLI-19 to CLI-22 | core, http adapters | own (`cmd/wlog`) |
| `docs` | Correct stale statuses, README, parity page, event shape, specs, and plan. Record TDD proof rules | DOC-2 to DOC-6, DOC-8, SPEC-G20, SPEC-G21 | none | none |

### Phase 11, ships v0.6: event shape v2 and the five foundations

| Module id | Responsibility | Closes | Depends on | Go module |
|---|---|---|---|---|
| `core-shape` | Event shape v2: route as operation, start timestamp, float durations, `summary`, `event_id`, `schema_version`, a nested `wlog` object, and no empty values. A JSON writer with the fixed key order. A pretty console v2 that renders a tree and writes once per event. A writer option for stdout, stderr, or any `io.Writer` | CORE-14, CORE-16, CORE-20, CORE-22, CORE-27, CORE-34, BET-5, BET-13, BET-15, BET-17, PAR-7, PAR-9, SPEC-G8 | core | root |
| `core-default` | `wlog.SetDefault` and a Logger-scoped enable switch. Package functions fall back to the default Logger. A write with no event is reported | CORE-18, CORE-19, SPEC-G14, PAR-5 | core | root |
| `core-problems` | wlog's own problem codes, each with code, why, fix, and link. `OnProblem` receives each `Problem`. A rate-limited stderr default. `WLOG_DEBUG` explains a missing event. `Logger.Stats()` and a JSON `DebugHandler` | CORE-17, BET-4, BET-10, BET-19, PAR-3, SPEC-G15 | core | root |
| `core-calls` | Timed sub-operations inside one event: `wlog.StartCall(ctx, Call)` returns a context and an end func. A capped `calls[]` list, per-kind totals, and error counts. The contract every client and store adapter follows | BET-26 (part) | core | root |
| `event-schema` | JSON Schema files for the event and for `wlog.map.json`, embedded with `go:embed`. A test proves that every golden event and drain body matches its schema | BET-7, SPEC-G18, CLI-19 (part) | core-shape | root |
| `output-presets` | Output formats for stdout and file output only: default, flat, OTel semantic conventions, ECS, Google Cloud Logging, Datadog, and CloudWatch EMF. Drains always get the canonical event | CORE-12, CORE-13, BET-14, SPEC-G11 | core-shape | root |
| `propagate` | W3C `traceparent`, `tracestate`, and `baggage`, plus `X-Request-ID`. Extract and inject over headers, message headers, and plain maps. Generate ids that are missing | HTTP-19 (part), BET-3 (part) | core, core-calls | root |
| `work` | The unit-of-work kit: kinds (message, job, rpc, command, function, request) and their standard field groups. `Run` and `Start` helpers. Level and outcome from status or error. Attempt, lag, and redelivery fields. `Flush` for short-lived runtimes | SPEC-G9, SPEC-G19 (part) | core, propagate | root |
| `http-core` | Framework-neutral HTTP capture. A request and response view that any router implements. The safe default policy and presets, and per-route rules. Header allow-lists, trusted proxies, and request id rules. Body capture for any JSON value. Level from status and operation from route. The `Setup(app)` contract | HTTP-1, HTTP-6, HTTP-7, HTTP-13 to HTTP-17, HTTP-19, HTTP-23, CORE-14, CORE-15, SPEC-G3, SPEC-G10, SPEC-G12, PAR-12, PAR-13, PAR-15 | core-shape, work, propagate | root (`middleware/httpcore`) |
| `conformance` | Shared suites: `http`, `work`, `calls`, `log`, and `drain`. Every adapter passes its suite. The suites cover the gaps listed in HTTP-22 and PIPE-25 | HTTP-22, PIPE-25, SPEC-G20 | http-core, work, core-calls | root (`internal/conformance`) |
| `setup` | `setup.FromEnv()` builds drains from `WLOG_DRAINS` and each drain's env vars. It accepts evlog env var names as aliases. A missing credential disables that drain and reports a problem. Third-party drains join through explicit factories, with no global registry | BET-16, PAR-1, PAR-19, PAR-20 | pipeline, the phase 10 root drains, core-problems | root |
| `http-std` · `http-echo` · `http-echo5` · `http-gin` | Rebuilt on `http-core`, with all adapter fixes, `Unwrap`, and `Setup(app)` | HTTP-2 to HTTP-5, HTTP-8 to HTTP-12 | http-core, conformance | root, own |

### Phase 12, ships v0.7: everyday stack, search, and agents

Four tracks run in parallel once phase 11 is done. Priority P1 ships first inside a track. If the phase runs long, P3 items can move to "designed for".

#### Track A: HTTP routers and RPC

| Module id | Integrates | Priority | Depends on | Go module |
|---|---|---|---|---|
| `http-chi` | go-chi/chi v5, route from the chi route pattern | P1 | http-core | own (`middleware/chi`) |
| `http-fiber` | gofiber/fiber v2 (fasthttp) | P1 | http-core, http-fasthttp | own (`middleware/fiber`) |
| `http-fiber3` | gofiber/fiber v3 | P1 | http-core, http-fasthttp | own (`middleware/fiber3`) |
| `http-httprouter` | julienschmidt/httprouter, matched route path | P2 | http-core | own (`middleware/httprouter`) |
| `http-gozero` | zeromicro/go-zero rest | P2 | http-core | own (`middleware/gozero`) |
| `http-hertz` | cloudwego/hertz | P3 | http-core | own (`middleware/hertz`) |
| `http-kratos` | go-kratos/kratos HTTP transport. A Kratos gRPC server uses the `rpc-grpc` interceptors | P3 | http-core, rpc-grpc | own (`middleware/kratos`) |
| `http-huma` | danielgtaylor/huma v2, operation id as operation | P3 | http-core | own (`middleware/huma`) |
| `http-fasthttp` | valyala/fasthttp handlers, and the request and response views the Fiber modules share | P1 | http-core | own (`middleware/fasthttp`) |
| `rpc-grpc` | google.golang.org/grpc server and client interceptors. gRPC status to level. Status details (ErrorInfo, Help, LocalizedMessage) to why, fix, and link | P1 | work, core-calls, propagate | own (`rpc/grpc`) |
| `rpc-connect` | connectrpc.com/connect interceptors, server and client | P2 | work, core-calls, propagate | own (`rpc/connect`) |
| `rpc-gqlgen` | 99designs/gqlgen handler extension: operation name, complexity, resolver errors | P2 | http-core, work | own (`rpc/gqlgen`) |
| `rpc-twirp` | twitchtv/twirp server and client hooks | P3 | work, core-calls | own (`rpc/twirp`) |

#### Track B: outbound calls and data stores

| Module id | Integrates | Priority | Depends on | Go module |
|---|---|---|---|---|
| `sqlshape` | SQL statement shaper that removes every literal, for the SQL stores | P1 | none | root (`store/sqlshape`) |
| `client-http` | `http.RoundTripper` that records calls and sends trace headers. Covers resty and any `http.Client` user | P1 | core-calls, propagate | root (`client/http`) |
| `store-sql` | `database/sql` driver wrapper: statement shape with literals removed, rows, duration, and error. Covers sqlx, ent, and sqlc | P1 | core-calls | root (`store/sql`) |
| `store-pgx` | jackc/pgx v5 query, batch, and copy tracers | P1 | core-calls | own (`store/pgx`) |
| `store-gorm` | gorm.io/gorm plugin | P1 | core-calls | own (`store/gorm`) |
| `store-redis` | redis/go-redis v9 hook | P1 | core-calls | own (`store/redis`) |
| `store-mongo` | mongo-driver v2 command monitor | P2 | core-calls | own (`store/mongo`) |
| `client-aws` | aws-sdk-go-v2 middleware: one call per AWS API call, such as S3, DynamoDB, or SQS | P2 | core-calls, propagate | own (`client/aws`) |
| `store-bun` | uptrace/bun query hook | P3 | core-calls | own (`store/bun`) |

#### Track C: logger bridges, error libraries, and flags

Changed after API research on 2026-09-16. The core default extractor reads pkg/errors stacks
through reflection, so `errors-pkg` is gone. charm log already works through the `log-slog`
bridge in both directions, so `log-charm` becomes a recipe.

| Module id | Integrates | Priority | Depends on | Go module |
|---|---|---|---|---|
| `log-slog` | Fix handler semantics to pass `testing/slogtest`. slog is the hub for any logger with a slog handler | P1 | core | root |
| `log-logr` | go-logr/logr sink in both directions. Covers klog v2 and controller-runtime | P1 | log-slog | own (`log/logr`) |
| `log-zap` | Input `zapcore.Core` that folds into the event, plus the existing output with key collision fixes | P1 | log-slog | own |
| `log-zerolog` | Input hook through the context, plus the existing output with key collision fixes | P1 | log-slog | own |
| `log-logrus` | Input hook through `entry.Context`, plus the existing output | P2 | log-slog | own |
| `log-std` | stdlib `log` writer that turns lines into plain events | P2 | core-default | root (`log/std`) |
| `log-hclog` | hashicorp/go-hclog logger | P3 | log-slog | own (`log/hclog`) |
| `errors-validator` | go-playground/validator field errors into `error.data` | P1 | core | own (`errors/validator`) |
| `errors-oops` | samber/oops code, hint, public message, domain, and tags into ErrorInfo | P2 | core | own (`errors/oops`) |
| `errors-cockroach` | cockroachdb/errors safe details and stacks | P3 | core | own (`errors/cockroach`) |
| `flag-openfeature` | open-feature/go-sdk hook that records flag evaluations in a `feature_flags` group | P2 | core | own (`flag/openfeature`) |

#### Track F: search and agents

| Module id | Responsibility | Priority | Closes | Depends on | Go module |
|---|---|---|---|---|---|
| `cli-query` | `wlog query` and `wlog tail` over files, folders, stdin, and a live app. Filters by level, time, status, code, request id, and any field path. Newest N first, group and count. Output as JSON, pretty text, or summary lines | P1 | BET-2, PAR-22 (part) | core-shape, drain-file | own (`cmd/wlog`) |
| `cli-explain` | `wlog explain <id>` for rules, problem codes, doctor codes, reserved fields, catalog codes, and env vars. `wlog rules --json`, `wlog schema`, `wlog version` | P1 | BET-8 | core-problems, event-schema | own (`cmd/wlog`) |
| `agent-docs` | `llms.txt` and `llms-full.txt` built by `make docs`. Skills in `<name>/SKILL.md` layout with a skills index. A CLAUDE.md that imports AGENTS.md. A test that compiles and runs every snippet | P1 | BET-3, BET-11, PAR-35, PAR-36 | cli-agents | own (`cmd/wlog`), docs |
| `search-recipes` | lnav format file, jq recipes, Grafana dashboards for Loki and ClickHouse, ClickHouse views, Axiom and Datadog saved queries. A test loads each file | P1 | BET-21 (part) | core-shape, event-schema | none (`integrations/search`) |
| `drain-memory` | A query HTTP handler with newest-N limits and level lists. SSE v2 frames with hello, ping, `since`, and a token | P2 | BET-20, PAR-22, PAR-23 | core-shape | root |
| `query` | The one filter language for `wlog query`, the memory endpoint, and `wlog mcp`. Stdlib only | P1 | BET-2 (part) | core-shape | root |
| `cli-mcp` | `wlog mcp` stdio server with tools: `events_query`, `events_by_request_id`, `events_by_trace_id`, `map_entry`, `explain`, `redact_check`, `schema_event`. SPEC-track-g.md defines it | P1 | BET-12 | query, cli-explain | own (`cmd/wlog`) |
| `cli-doctor` (additions) | Checks for a global logger call inside a handler, the OTel middleware order, and each new adapter's setup | P2 | PAR-34 (part), HTTP-21 (part) | query | own (`cmd/wlog`) |
| `recipes` | `docs/recipes/` with a tested example and search queries each. Phase 12: REST API, gRPC service, CLI tool. Phase 13: Kafka consumer, cron job, Lambda. Phase 14: LLM agent, MCP server | P1 | BET-1 (part) | the modules each recipe uses | none (`examples/`) |

### Phase 13, ships v0.8: messages, jobs, functions, and commands

#### Track D: async work

Every consumer emits one event per message or job through `work`. Every producer records a call through `core-calls` and sends trace headers through `propagate`. A Kafka or NATS module also gives a drain that ships events to a topic or subject.

| Module id | Integrates | Priority | Depends on | Go module |
|---|---|---|---|---|
| `queue-kafkago` | segmentio/kafka-go reader and writer, plus a Kafka drain | P1 | work, core-calls, pipeline | own (`queue/kafkago`) |
| `queue-sarama` | IBM/sarama consumer group handler and producer interceptors | P1 | work, core-calls | own (`queue/sarama`) |
| `queue-franz` | twmb/franz-go hooks and a per-record helper | P1 | work, core-calls | own (`queue/franz`) |
| `queue-confluent` | confluentinc/confluent-kafka-go. It needs cgo, so it builds only with cgo on | P2 | work, core-calls | own (`queue/confluent`) |
| `queue-watermill` | ThreeDotsLabs/watermill router middleware and publisher decorator. One module covers Kafka, NATS, AMQP, SQS, Pub/Sub, and Redis Streams | P1 | work, core-calls | own (`queue/watermill`) |
| `queue-sqs` | aws-sdk-go-v2 SQS receive loop helper, SNS and SQS publish calls | P1 | work, client-aws | own (`queue/sqs`) |
| `queue-nats` | nats-io/nats.go and JetStream handlers, plus a NATS drain | P2 | work, core-calls, pipeline | own (`queue/nats`) |
| `queue-amqp` | rabbitmq/amqp091-go deliveries and publishes | P2 | work, core-calls | own (`queue/amqp`) |
| `queue-pubsub` | cloud.google.com/go/pubsub receive and publish | P2 | work, core-calls | own (`queue/pubsub`) |
| `queue-cloudevents` | cloudevents/sdk-go receivers and senders | P3 | work, core-calls | own (`queue/cloudevents`) |
| `job-asynq` | hibiken/asynq middleware | P2 | work | own (`job/asynq`) |
| `job-river` | riverqueue/river worker middleware | P2 | work | own (`job/river`) |
| `job-temporal` | go.temporal.io/sdk activity interceptors. A workflow gets replay-safe fields only | P2 | work | own (`job/temporal`) |
| `job-cron` | robfig/cron v3 job wrapper, plus a stdlib ticker helper in `work` | P2 | work | own (`job/cron`) |
| `faas-lambda` | aws/aws-lambda-go handler wrapper for API Gateway v1 and v2, ALB, SQS batches, SNS, EventBridge, Kinesis, and DynamoDB streams. It calls `Flush` before each return. It replaces `examples/lambda` | P1 | work, http-core | own (`faas/lambda`) |
| `faas-gcf` | GoogleCloudPlatform/functions-framework-go, HTTP and CloudEvent functions | P3 | work, http-core, queue-cloudevents | own (`faas/gcf`) |
| `command-cobra` | spf13/cobra: one event per command run, with the exit code. If `WLOG_DRAINS` is set, `cmd/wlog` also records its own runs through `work` | P1 | work | own (`command/cobra`) |
| `command-urfave` | urfave/cli v3 | P2 | work | own (`command/urfave`) |
| `command-kong` | alecthomas/kong | P3 | work | own (`command/kong`) |

### Phase 14, ships v0.9: destinations, OpenTelemetry, and AI

#### Track E: destinations

| Module id | Integrates | Priority | Depends on | Go module |
|---|---|---|---|---|
| `trace-otel` | Copies the redacted event onto the active span, with span status from outcome. Records OTel metrics from every event, before sampling. Reports wlog `Stats` as OTel counters. Go 1.21 | P1 | core-shape, core-problems, output-presets | own |
| `trace-otellog` | OTel Logs Bridge output that uses the app's LoggerProvider. A separate module, because `otel/log` is v0 and needs Go 1.25 | P1 | output-presets | own (`trace/otellog`) |
| `pipeline` (addition) | `PartialError`, so a drain retries only the events a backend asked for again | P1 | pipeline | root |
| `metrics-prometheus` | prometheus/client_golang: rate, errors, and duration by kind and operation, from every event before sampling. A `Stats` collector | P1 | core-shape, core-problems | own (`metrics/prometheus`) |
| `drain-honeycomb` | Honeycomb batch events API | P1 | pipeline | root |
| `drain-elastic` | Elasticsearch and OpenSearch bulk API, with the ECS preset and an index template | P1 | pipeline, output-presets | root |
| `drain-splunk` | Splunk HTTP Event Collector | P2 | pipeline | root |
| `drain-victorialogs` | VictoriaLogs JSON lines ingest | P2 | pipeline | root |
| `drain-syslog` | RFC 5424 over TCP, UDP, or TLS | P2 | pipeline | root |
| `drain-cloudwatch` | aws-sdk-go-v2 CloudWatch Logs, with a client that the app builds. The EMF stdout preset covers Lambda with no dependency | P2 | pipeline | own (`drain/cloudwatch`) |
| `drain-newrelic` | New Relic Log API | P3 | pipeline | root |

#### Track G: AI and agents

Every LLM module fills `llm.Record` from the SDK response, so users stop copying token counts by hand. No module stores prompt or completion text unless the user opts in.

| Module id | Integrates | Priority | Depends on | Go module |
|---|---|---|---|---|
| `ai-anthropic` | anthropics/anthropic-sdk-go | P1 | llm | own (`ai/anthropic`) |
| `ai-openai` | openai/openai-go | P1 | llm | own (`ai/openai`) |
| `ai-mcpsdk` | modelcontextprotocol/go-sdk server middleware: one event per tool call, resource read, or prompt | P1 | work, llm | own (`ai/mcpsdk`) |
| `ai-mcpgo` | mark3labs/mcp-go server hooks, same fields as `ai-mcpsdk` | P1 | work, llm | own (`ai/mcpgo`) |
| `ai-genai` | googleapis/go-genai | P2 | llm | own (`ai/genai`) |
| `ai-langchaingo` | tmc/langchaingo callbacks handler | P2 | llm | own (`ai/langchaingo`) |
| `ai-goopenai` | sashabaranov/go-openai | P2 | llm | own (`ai/goopenai`) |
| `ai-eino` | cloudwego/eino callbacks | P3 | llm | own (`ai/eino`) |
| `llm` | Add cache-write tokens, output tokens per second, response id, error, steps, and embeddings through the `embeddings` operation. Money stays in integer micros | P1 | core | root |
| `cli-init` | Detect every framework, queue, store, and logger in go.mod, and wire the matching adapters. Adds `--yes` and `--json`, and runs doctor at the end | P1 | every track | own (`cmd/wlog`) |

### Phase 15, ships v1.0.0

No new modules. Freeze the public API with an apidiff baseline. Rewrite the parity page against evlog. Add comparison pages against slog, zap, zerolog, the OpenTelemetry logs bridge, and evlog (PAR-37, BET-25). Close every remaining audit id or record a decision for it. Tagging v1.0.0 is ask-first.

### Build order

```
Phase 10  repo-ci → core, redact → pipeline → audit, drains, catalog/llm/herr,
          sample/enrich/memory/wlogtest, cli-map/init/doctor/agents, docs        → v0.5
Phase 11  core-problems → core-shape → core-default, core-calls → propagate
          → work, event-schema, output-presets → http-core → conformance
          → setup, http-std/echo/echo5/gin rebuilt                              → v0.6
Phase 12  Track A (HTTP, RPC) ‖ Track B (calls, stores) ‖ Track C (logs, errors,
          flags) ‖ Track F (search, agents)                                     → v0.7
Phase 13  Track D (queues, jobs, functions, commands)                           → v0.8
Phase 14  pipeline PartialError → Track E (destinations, OTel) ‖ Track G (AI, MCP)
          → cli-init v2                                                          → v0.9
Phase 15  API freeze, parity rewrite, docs close-out                            → v1.0.0
```

`repo-ci` comes first, so every later change runs on a build that tells the truth. Phase 10 skips any fix that a phase 11 rewrite replaces, so no code gets fixed twice. `conformance` lands before any new adapter, because every adapter proves itself against it. Track F ships in phase 12, because search is the goal the user named first.

### Designed for, not built in this initiative

Core keeps the extension points, so each of these can arrive later with no core change. Each one waits for a real user request.

| Future module id | Integrates |
|---|---|
| `http-iris` · `http-beego` · `http-gokit` · `http-goa` | Less common routers and toolkits |
| `rpc-grpcgateway` | Specific grpc-gateway fields. `http-core` and `rpc-grpc` already cover it |
| `job-gocron` · `job-machinery` | Other schedulers and job queues |
| `drain-azure` · `drain-gcplogging` | Azure Monitor and Google Cloud Logging APIs. The stdout presets cover AKS, GKE, and Cloud Run |
| `drain-s3` · `drain-parquet` | Archive and local columnar search |
| `ai-genkit` | Firebase Genkit for Go |
| `flag-launchdarkly` | Direct SDK hooks. OpenFeature covers it today |
| `ws-events` | One event per websocket message |

### Open questions for the module specs

None block this map. Each one is answered in its module spec, then reviewed there.

1. The exact `summary` template per kind: request, message, job, rpc, command, function.
2. The `calls[]` cap and the aggregate field names.
3. The fixed allow-list of captured headers.
4. The problem code list and its names.
5. The field group names for `work` kinds. Draft: `messaging`, `job`, `rpc`, `cli`, `faas`, aligned with OTel semantic conventions where one exists.
6. Which upstream version of each integrated library is the floor, and its Go floor.

## Specs

| Module id | Spec | Status |
|---|---|---|
| (project-wide) | [SPEC.md](SPEC.md) | v3 approved 2026-09-15. v4 drafted 2026-09-16, awaiting approval |
| `redact` | [SPEC-redact.md](SPEC-redact.md) | approved 2026-09-15 (v2) |
| (plan) | [tasks/plan.md](../tasks/plan.md) | v2 drafted 2026-09-16, awaiting approval. v1, approved 2026-09-15, is in [tasks/archive/plan-v1.md](../tasks/archive/plan-v1.md) |
| `core` | [SPEC-core.md](SPEC-core.md) | approved 2026-09-15. SPEC-core-v2.md replaces its event shape, sink, field name, and configuration sections |
| `pipeline` | [SPEC-pipeline.md](SPEC-pipeline.md) | approved 2026-09-15 |
| `sample` | [SPEC-sample.md](SPEC-sample.md) | approved 2026-09-15 |
| `drain-memory` | [SPEC-drain-memory.md](SPEC-drain-memory.md) | approved 2026-09-15 |
| `wlogtest` | [SPEC-wlogtest.md](SPEC-wlogtest.md) | approved 2026-09-15 |
| `http-std` | [SPEC-http-std.md](SPEC-http-std.md) | approved 2026-09-15. Replaced by SPEC-http-core.md in phase 11 |
| `enrich` | [SPEC-enrich.md](SPEC-enrich.md) | approved 2026-09-15 |
| `errors-herr` | [SPEC-errors-herr.md](SPEC-errors-herr.md) | approved 2026-09-15 |
| `audit` | [SPEC-audit.md](SPEC-audit.md) | approved 2026-09-15 |
| `http-echo`, `http-echo5`, `http-gin` | [SPEC-http-adapters.md](SPEC-http-adapters.md) | approved 2026-09-16. Replaced by SPEC-http-core.md in phase 11 |
| `log-slog`, `log-zap`, `log-zerolog`, `log-logrus` | [SPEC-log-adapters.md](SPEC-log-adapters.md) | approved 2026-09-16. SPEC-track-c.md replaces its rules in phase 12 |
| `trace-otel` | [SPEC-trace-otel.md](SPEC-trace-otel.md) | approved 2026-09-16. SPEC-track-e.md extends it in phase 14 |
| `drain-axiom` · `drain-loki` · `drain-file` · `drain-webhook` · `drain-otlp` | [SPEC-drains-v1.md](SPEC-drains-v1.md) | approved 2026-09-16 |
| `cli-map` | [SPEC-cli-map.md](SPEC-cli-map.md) | approved 2026-09-16 |
| `drain-sentry` · `drain-clickhouse` · `drain-datadog` | [SPEC-drains-v1.1.md](SPEC-drains-v1.1.md) | approved 2026-09-16 |
| `catalog` | [SPEC-catalog.md](SPEC-catalog.md) | drafted 2026-09-16, awaiting approval |
| `llm` | [SPEC-llm.md](SPEC-llm.md) | drafted 2026-09-16, awaiting approval |
| v1.2 additions to core, audit, drain-memory, drain-file, redact, errors-herr | [SPEC-v1.2-additions.md](SPEC-v1.2-additions.md) | drafted 2026-09-16, awaiting approval |
| `cli-init` · `cli-doctor` · `cli-agents` · cli-map additions | [SPEC-cli-v1.3.md](SPEC-cli-v1.3.md) | drafted 2026-09-16, awaiting approval |
| `drain-posthog` · `drain-betterstack` · `drain-hyperdx` | [SPEC-drains-v1.4.md](SPEC-drains-v1.4.md) | drafted 2026-09-16, awaiting approval |
| `repo-ci` | [SPEC-repo-ci.md](SPEC-repo-ci.md) | drafted 2026-09-16, awaiting approval |
| Phase 10 fixes to existing modules | [SPEC-hardening.md](SPEC-hardening.md) | drafted 2026-09-16, awaiting approval |
| `core-shape` · `core-default` · `core-problems` · `core-calls` · `event-schema` · `output-presets` | [SPEC-core-v2.md](SPEC-core-v2.md) | drafted 2026-09-16, awaiting approval |
| `propagate` · `work` | [SPEC-work.md](SPEC-work.md) | drafted 2026-09-16, awaiting approval |
| `http-core`, and `http-std` · `http-echo` · `http-echo5` · `http-gin` rebuilt | [SPEC-http-core.md](SPEC-http-core.md) | drafted 2026-09-16, awaiting approval |
| `conformance` | [SPEC-conformance.md](SPEC-conformance.md) | drafted 2026-09-16, awaiting approval |
| `setup` | [SPEC-setup.md](SPEC-setup.md) | drafted 2026-09-16, awaiting approval |
| Track A: HTTP routers and RPC | [SPEC-track-a.md](SPEC-track-a.md) | drafted 2026-09-16, awaiting approval |
| Track B: outbound calls and data stores | [SPEC-track-b.md](SPEC-track-b.md) | drafted 2026-09-16, awaiting approval |
| Track C: logger bridges, error libraries, and flags | [SPEC-track-c.md](SPEC-track-c.md) | drafted 2026-09-16, awaiting approval |
| Track D: messages, jobs, functions, and commands | [SPEC-track-d.md](SPEC-track-d.md) | drafted 2026-09-16, awaiting approval |
| Track E: destinations and OpenTelemetry | [SPEC-track-e.md](SPEC-track-e.md) | drafted 2026-09-16, awaiting approval |
| Track F: search and agents, and the `query` package | [SPEC-track-f.md](SPEC-track-f.md) | drafted 2026-09-16, awaiting approval |
| Track G: AI and agents, `llm` additions, `cli-mcp`, `cli-init` v2 | [SPEC-track-g.md](SPEC-track-g.md) | drafted 2026-09-16, awaiting approval |
| (research) | [tasks/research/README.md](../tasks/research/README.md) | done 2026-09-16 |
| others | SPEC-<id>.md | not started |
