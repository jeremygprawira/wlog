# Spec: wlog (project-wide, v4)

> The shared contract for every module in [CAPABILITIES.md](CAPABILITIES.md). A module spec
> (`SPEC-<id>.md`) adds its own objective and criteria, and it must not contradict this file.
> v4 (2026-09-16) replaces v3. It adds the decisions from the [gap audit](../tasks/audit-2026-09-16.md)
> and the v0.5 to v1.0 capability map. Audit ids such as CORE-1 or SPEC-G3 point at that audit.

## Objective

**wlog** ("wide log") is a Go library for wide-event logging. Each unit of work produces one
rich, structured event. A unit of work is an HTTP request, an RPC, a message, a job, a command,
or a function call. Code adds to the event through `context.Context` while the work runs. wlog
samples, enriches, redacts, and emits that event once, at the end.

The goal of this initiative is one event that is easy to add and easy to search. It looks the
same whatever framework, logger, store, or backend the app uses. People, agents, and code can
all read it.

**Users:**

- Go teams on any HTTP router, RPC layer, queue, job runner, CLI framework, logger, or backend.
- AI agents that add wlog to code, or that read events to answer a question.
- Tools that consume events: backends, dashboards, `wlog query`, and the `wlog mcp` server.

### Design pillars (in priority order)

The order decides a conflict. For example, safe capture defaults beat richer zero-config events.

1. **Safe by construction.** Redaction runs before every sink and drain, in every environment.
   It fails closed. A value wlog cannot inspect is masked, not passed through.
2. **Never hurts the app.** A logging failure, a slow backend, or a full buffer never blocks,
   panics, or changes a response. wlog never changes the app's own data.
3. **Useful with zero config.** `wlog.New()` and one `Setup` line give a complete, redacted,
   searchable event per unit of work.
4. **One searchable shape.** Every adapter emits the same core fields, with the same names and
   the same meaning. The shape is versioned and published as a JSON Schema.
5. **Agnostic.** Core knows no framework, logger, error library, or vendor. Each integration is
   a thin adapter over a shared foundation. An adapter that imports a third party lives in its own module.
6. **Customizable everywhere.** Every default is an option, and every edge is an interface with
   a function adapter.

### Target usage (illustrative, module specs own the real API)

<!-- snippet:sketch -->
```go
func main() {
	log := wlog.New(setup.FromEnv()) // WLOG_SERVICE, WLOG_DRAINS=axiom, AXIOM_TOKEN, ...
	wlog.SetDefault(log)
	defer log.Close(context.Background())

	r := chi.NewRouter()
	wlogchi.Setup(r) // one event per request, panic recovery, route names

	db := sql.OpenDB(wlogsql.Wrap(connector))                       // each query becomes a call
	http.DefaultTransport = wlogclient.Transport(http.DefaultTransport) // outbound calls + traceparent

	r.Post("/orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		wlog.Set(ctx, "order_id", chi.URLParam(r, "id"))
		if err := charge(ctx); err != nil {
			wlog.Error(ctx, err) // why, fix, and link come from the catalog
			http.Error(w, "payment failed", http.StatusBadGateway)
			return
		}
		audit.Do(ctx, audit.Record{Action: "order.pay", Outcome: "success"})
	})
	http.ListenAndServe(":8080", r)
}
```

## Decisions

Settled 2026-09-14 (v3) and 2026-09-16 (v4). Do not reopen them without the user. The last
column names the audit ids a decision answers.

### Packaging and releases

| Topic | Decision | Answers |
|---|---|---|
| Packaging | One repo with category folders. The root module stays stdlib-only. A package with a third-party import gets its own `go.mod`, joined by `go.work`. Every sub-module requires each sibling module at the release version, with a relative `replace` for builds inside the repo | REL-2 |
| Go versions | Each module's `go` line is the lowest version its code and its dependencies need. Nobody raises it for convenience. A feature from a newer Go lives in a `//go:build go1.N` file. The root floor is Go 1.21. CI tests each module at its floor and on the two newest Go releases | REL-3, SPEC-G16 |
| API policy | Tags stay on v0.x until v1.0.0. Breaking changes are allowed before v1.0.0. Each break gets a CHANGELOG line and a migration note. v1.0.0 freezes the API with an apidiff baseline | |
| Tags | `tools/release` tags the root module first, then every other module in dependency order. Examples are never tagged | REL-7 |

### The event

| Topic | Decision | Answers |
|---|---|---|
| Units of work | Eight kinds: request, rpc, message, job, command, function, work, and log. Each kind has one operation format and at most one field group, defined in `SPEC-work.md` and `SPEC-http-core.md`. Library fields outside a group's table go under `<group>.<system>` | SPEC-G9, SPEC-G10 |
| Shape v2 | Core keys come first: `timestamp`, `level`, `summary`, `operation`, `kind`, `outcome`, `duration_ms`, `message`, `error`, `event_id`, `service`, and `trace`. Then the kind group, domain groups, and user keys. The arrays `audit`, `errors`, `logs`, and `calls` follow. Then `call_stats`, `feature_flags`, and `wlog`. SPEC-core-v2.md holds the table. Empty values are left out. Header names are lowercase. A JSON Schema is published for each `schema_version` | CORE-34, BET-7 |
| Key order | Stdout and file output write keys in the order above. Drains receive maps, where order has no meaning | BET-13 |
| wlog metadata | `wlog` holds `schema_version`, `redact_fingerprint`, `sample_rate`, the drop counters, `late_writes`, `unknown_keys`, and `truncated` | CORE-34 |
| Ids | At its start, an event gets `trace.trace_id` and `trace.span_id` from the incoming trace context, or new ids. A `Detach` child keeps its parent's trace id. A recording OTel span wins, through the trace-otel `Starter`. `trace.request_id` comes from a valid `X-Request-ID`, or equals the trace id. Finalize sets `event_id`, a UUIDv7 | BET-3, SPEC-G13 |
| Level | An explicit `SetLevel` wins. Otherwise a recorded error gives `error`, with two cases that give `warn`: its `ErrorInfo.Status` is from 400 to 499, or the unit's status class is client error. Otherwise the status class decides: a server error gives `error`, and a client error gives `warn`. Otherwise `info` | CORE-15, HTTP-11, SPEC-G9 |
| Outcome | If the level is `error`, the outcome is `error`. Every other event has the outcome `success` | |
| Time | `timestamp` is the start of the work, in RFC 3339 UTC with nanoseconds. `duration_ms` is a float with microsecond precision. An event holds one duration only | CORE-16, CORE-27, SPEC-G8 |
| Summary | One sentence built after redaction from a template per kind. It never holds a value the redactor masked | BET-5 |
| Values | wlog copies each value into its own tree at the write call. Integers keep every digit. NaN and infinity become strings. An error becomes its message. A `time.Duration` becomes float milliseconds. A value that cannot be encoded becomes a type marker | CORE-3 to CORE-6, CORE-26, SPEC-G1, SPEC-G6 |
| Size | The total event cap is 256 KiB after redaction. Over the cap, wlog drops the largest user fields first and lists their names in `wlog.truncated` | CORE-25, SPEC-G7 |
| Caps | Top-level keys 200, fields per group 50, array items 200, and nesting depth 16. Arrays: `logs` 50, `errors` 10, `calls` 50 plus totals per kind, `audit` 20, and `feature_flags` 50 | G4 |

### Stages and delivery

| Topic | Decision | Answers |
|---|---|---|
| Stage order v2 | 1 head sample, from level and trace id only. 2 enrich. 3 tail keep, which sees the enriched event. 4 redact. 5 finalize: summary, `event_id`, schema version, size cap. 6 drains get the canonical map. 7 output presets. 8 writers for stdout and files. A dropped event skips every later stage | CORE-12, SPEC-G11, PAR-14 |
| Audit bypass | An event with audit records skips head sampling, tail keep, and the level filter. A disabled Logger still drops it, and reports `WLOG_AUDIT_DISABLED` | CORE-9, AUD-5 |
| Drains | A drain gets a read-only map. A drain that must change the event copies it first. Every built-in network drain is async by default and accepts pipeline options. A `NewSender` form stays for custom composition | CORE-11, CORE-21, PIPE-17 |
| Delivery | Best effort per drain. A retry after an unknown result can send an event twice. A drain that splits a batch sends only the part that failed. `Logger.Flush(ctx)` delivers pending events and keeps drains running. `Logger.Close(ctx)` flushes and stops. After `Close`, an emit reports `WLOG_LOGGER_CLOSED` | PIPE-4, PIPE-15, SPEC-G5, SPEC-G19 |
| Overflow | A bounded buffer drops the oldest event. `OnDropped` runs outside the lock, and a counter records every drop. There is no disk spill | PIPE-3 |

### Safety

| Topic | Decision | Answers |
|---|---|---|
| Redaction default | On in every environment. `redact.Disabled()` is the explicit opt-out | |
| Failure model | Fail closed. wlog masks four cases: a type the redactor cannot walk, and a panicking transform or replace function. It also masks a body that fails to parse or was cut, and a value that fails to encode | CORE-1 to CORE-3, RED-9, HTTP-1, SPEC-G2 |
| Denylist policy | A `*redact.Redactor` is immutable, and the Logger swaps it atomically. `With` starts from the full resolved config | RED-2 |
| HTTP capture default | Method, route, path, status, durations, sizes, ids, user agent, client IP, allow-listed headers, and query and cookie names. `CaptureAll()` adds bodies and values. Env `local`, `dev`, and `development` use `CaptureAll()` by default | HTTP-14, SPEC-G12 |
| Untrusted input | Key names are cut to 256 bytes before matching. `X-Forwarded-For` counts only from a trusted proxy. `X-Request-ID` counts only at 128 characters or fewer from `[A-Za-z0-9._:-]`. `traceparent` follows W3C rules. CDN geo headers count only from the configured provider | RED-1, HTTP-13, SMP-5, SPEC-G3 |
| Privacy defaults | wlog never captures SQL parameter values, message bodies, GraphQL variables, LLM prompts and completions, or validator field values unless the user opts in | |
| Panic isolation | Every user-supplied hook runs under recover. That covers extractors, enrichers, keepers, drains, senders, plugins, `Starter`, `Finisher`, and `Measurer` hooks, output presets, `OnProblem`, `OnDropped`, transforms, and replace functions | CORE-7, CORE-8, PIPE-1 to PIPE-3, HTTP-8 |

### Setup and API

| Topic | Decision | Answers |
|---|---|---|
| Default logger | `wlog.SetDefault(l)` sets the process default. A package function uses the Logger on the context, or else the default. CLAUDE.md names this atomic pointer as the one allowed piece of package-level state. The enable switch lives on the Logger | CORE-18, CORE-19, SPEC-G14 |
| Config | Code options win over env vars, in any order. Core `New` reads the logger and service identity vars. `setup.FromEnv` reads `WLOG_DRAINS` and `WLOG_OUTPUT`. Each drain reads its own vars in `New`. `WLOG_*` vars come first, then aliases such as `OTEL_SERVICE_NAME`. Without a service name, wlog uses the main module path from build info. Without a version, it uses `vcs.revision` | BET-16, PAR-1 |
| Env-driven setup | `setup.FromEnv()` builds drains from `WLOG_DRAINS` and each drain's env vars, and it accepts evlog's var names as aliases. A missing credential disables that drain and reports a problem | PAR-19, PAR-20 |
| Framework setup | Every entry-point adapter, which starts events, has a constructor that takes a `*wlog.Logger`. It also has a one-line setup form that uses the default Logger. A call adapter, a bridge, or an adapter that adds to an open event reads the Logger from the context | |
| Problems | wlog reports its own failures as a `wlog.Problem` with a code, source, why, fix, link, and error. `OnProblem` receives each one. The default prints one stderr line per code per minute. `WLOG_DEBUG=1` reports every dropped event with its reason | CORE-17, BET-4, BET-10, SPEC-G15 |
| Output presets | `default`, `flat`, `otel`, `ecs`, `gcp`, `datadog`, and `emf`. A preset changes only stdout and file output | CORE-12, CORE-13 |
| Pretty console | A tree with the error block first, written once per event. It turns on for a terminal, or for env `local`, `dev`, or `development`. `NO_COLOR` turns colors off | CORE-22, PAR-9 |

### Integrations

| Topic | Decision | Answers |
|---|---|---|
| Foundations | `http-core`, `work`, `core-calls`, `log-slog`, and `pipeline`. Each adapter maps its framework onto one of them | |
| Conformance | Every adapter passes the shared suite for its kind: `http`, `work`, `calls`, `log`, or `drain`. If its suite is not green, a new adapter does not merge | HTTP-22, PIPE-25, SPEC-G20 |
| Upstream versions | Each integration supports the latest major version of its library. It also supports the previous major while upstream still ships fixes for it. Each module spec pins the floor | |
| Calls | Clients and stores record calls, capped at 50 per event, plus totals per kind. Where the protocol has headers, they send `traceparent` and `tracestate` | BET-26 |
| Logger bridges | If a log record's context holds an event, the input side folds the record into `logs`. Other records pass through unchanged. The output side writes a finished event through the app's logger | HTTP-18 |
| Errors | Main `error` and earlier `errors`, capped at 10. `ErrorInfo` holds code, message, kind, type, status, cause, causes, stack, caller, why, fix, link, attrs, data, and internal. The default extractor reads `Code()`, `Stack()`, and `Unwrap() []error` through `errors.As` | CORE-24, BET-17 |

### Search, agents, and existing modules

| Topic | Decision | Answers |
|---|---|---|
| Search | `wlog query` and `wlog tail` read files, folders, stdin, and a live app. `wlog mcp` exposes the same queries to agents. Each search backend ships saved queries or a dashboard: Loki, ClickHouse, Axiom, Datadog, Honeycomb, and Elastic | BET-2, BET-12, BET-21 |
| Agent docs | Every problem code, rule id, and reserved field has `wlog explain`. `make docs` builds `llms.txt` and `llms-full.txt`. Skills use the `<name>/SKILL.md` layout, and a test compiles and runs every snippet | BET-3, BET-8, BET-11 |
| Background work | `wlog.Detach(ctx, name)` starts a linked child event. Its context keeps the parent's values and drops the parent's cancellation | CORE-10 |
| Sampling | Head rates are floats from 0 to 100 per level. The head decision hashes the trace id, so a request, its children, and other services agree. Tail keep rules combine with OR. Errors and server errors are always kept. A kept event records `wlog.sample_rate`. Globs support `**` | SMP-2 to SMP-4, SPEC-G13 |
| Audit | `audit.Journal` holds the chain state and one write lock. The hash covers the exact bytes written. A signed marker line lands every N lines and on `Close`. `Verify` takes an expected head, and it catches edits, inserts, deletes, cuts, and reorders. One writer per file, enforced by a file lock. An event holds up to 20 audit records | AUD-1 to AUD-6, SPEC-G4 |
| `wlog map` | A failed gate writes nothing. Config comes from `wlog.map.yaml` only. A rule that does not apply reports `n/a`. Zero handlers fails the gate | CLI-1, CLI-5 to CLI-7 |
| Kept from v3 | Typed keys and `StrictKeys`. Plugins with optional hooks, where several keepers combine with OR. W3C `traceparent` in core, and an optional OTel module. `wlogtest`. The memory drain with SSE. Identity headers on built-in drains, now overridable. Geo from CDN headers, now from a trusted provider | CORE-31, PIPE-24, SMP-5 |

## Tech stack

- Root module: Go 1.21 or later, standard library only. Files for newer Go use `//go:build go1.N`.
- Sub-modules: each one lists its single integrated library in its module spec, with the
  supported version range and its Go floor.
- `tools` module: release, lint, snippet, schema, coverage, and benchmark tooling. It can use
  third-party tools such as `golang.org/x/exp/apidiff`, `golang.org/x/perf/cmd/benchstat`, and
  `golang.org/x/vuln/cmd/govulncheck`.
- `cmd/wlog`: `golang.org/x/tools` for analysis, and the MCP library its spec names.

## Commands

```bash
make test         # go test ./... in every module listed in go.work
make race         # go test -race ./... in every module                      (G2)
make fuzz         # every Fuzz target for FUZZTIME (default 30s each)          (G1)
make bench        # benchmarks, compared with benchstat against bench/baseline  (budget)
make lint         # go vet and golangci-lint v2 in every module
make ste          # Simple English lint over docs, specs, and doc comments
make snippets     # compile and run every Go snippet in README, docs, and skills (G8)
make schema       # validate golden events and drain bodies against schema/     (G7, from phase 11)
make floor        # test each module with GOWORK=off at its Go and library floors (G8)
make tidy         # go mod tidy in every module with GOWORK=off, then go work sync
make cover        # coverage per package, fails under 85% in root packages
make vuln         # govulncheck on an upgraded copy of each module's build list
make map          # wlog map --min-score 80 over examples and fixture apps
make docs         # build llms.txt and llms-full.txt
make integration  # docker compose up with health probes, then integration tests
make release-check # dry run: require versions, tidy diff, apidiff, tag order
```

Single test: `go test -race -run TestName ./package`

CI runs lint, test, floor, tidy, snippets, cover, fuzz, and map on each push, and bench on pull
requests. It runs vuln, the long fuzz, and integration nightly. SPEC-repo-ci.md holds the schedule.
It builds only the modules a change touches, plus every module that depends on them.

## Project structure

```
wlog/
├── go.mod go.work Makefile README.md CHANGELOG.md LICENSE llms.txt
├── CLAUDE.md AGENTS.md                → agent guidance
├── docs/                              → SPEC.md, CAPABILITIES.md, SPEC-<id>.md, guides, recipes/
├── tasks/                             → plan.md, todo.md, audit and archive
├── schema/                            → event and map JSON Schemas (go:embed)
├── *.go                               → core (package wlog)
├── redact/ sample/ enrich/ pipeline/ audit/ catalog/ llm/ setup/ propagate/ work/ wlogtest/
├── preset/ query/ pipeline/httpdrain/  → root module
├── internal/conformance/{http,work,calls,log,drain}/ → shared adapter suites
├── internal/version/
├── middleware/{nethttp,httpcore}/     → root module
├── middleware/{echo,echo5,gin,chi,fiber,fiber3,httprouter,gozero,hertz,kratos,huma,fasthttp}/ (own go.mod)
├── rpc/{grpc,connect,gqlgen,twirp}/   (own go.mod)
├── queue/{kafkago,sarama,franz,confluent,watermill,sqs,nats,amqp,pubsub,cloudevents}/ (own)
├── job/{asynq,river,temporal,cron}/ faas/{lambda,gcf}/ command/{cobra,urfave,kong}/ (own)
├── client/http/ store/sql/ store/sqlshape/ → root module
├── client/aws/ store/{pgx,gorm,redis,mongo,bun}/ (own)
├── log/{slog,std}/                    → root module
├── log/{zap,zerolog,logrus,logr,hclog}/ errors/{herr,validator,oops,cockroach}/ flag/openfeature/ (own)
├── drain/{memory,file,axiom,loki,webhook,otlp,sentry,clickhouse,datadog,posthog,betterstack,hyperdx,honeycomb,elastic,splunk,victorialogs,syslog,newrelic}/ → root
├── drain/cloudwatch/ trace/{otel,otellog}/ metrics/prometheus/ ai/{anthropic,openai,genai,langchaingo,goopenai,eino,mcpsdk,mcpgo}/ (own)
├── integrations/search/               → lnav format, jq recipes, dashboards, saved queries
├── cmd/wlog/ (own)                    → map, init, doctor, agents, query, tail, explain, schema, mcp
├── tools/ (own)                       → release, snippets, ste, schema, cover, bench tooling
└── examples/ (own, never tagged)      → one tested program per recipe
```

## Code style

Follows herr and v3. A doc comment on every package, type, and exported function explains the
flow. Functional options. Unexported concrete types behind small interfaces. Fail loud at
construction, fail safe at runtime. Every extension point is an interface with a function
adapter.

<!-- snippet:sketch -->
```go
// Package wlogchi is wlog's go-chi/chi adapter: one event per request, built on httpcore.
//
// Read top to bottom: Middleware wraps a handler with httpcore.Handle. chiView reports the
// matched route from chi's RouteContext after next runs, because chi fills it while routing.
// Setup installs Middleware with the default Logger on a chi.Router.
package wlogchi

// Middleware returns chi middleware that emits one wide event per request through log.
func Middleware(log *wlog.Logger, opts ...httpcore.Option) func(http.Handler) http.Handler {
	return httpcore.NetHTTP(log, append(opts, httpcore.RouteFunc(route))...)
}
```

- Adapter packages are named `wlog<name>` (`wlogchi`, `wloggrpc`, `wlogkafkago`).
- Constructors return errors, with `Must…` variants for `main`. Runtime paths never return a
  logging error into app code. They report a `wlog.Problem`.
- Event keys use `snake_case`. Core-owned keys are reserved and listed in `schema/event.v1.json`.
- Use `any`, not `interface{}`. `gofmt`, `go vet`, and `golangci-lint` v2 clean.
- Prose follows [AminBlg/SimpleEnglish](https://github.com/AminBlg/SimpleEnglish): active voice,
  20 words per instruction, 25 per description, no semicolons or em-dashes. `make ste` checks it.

## Testing strategy

- **Strict TDD, vertical slices.** Write one failing test, then the minimum code, then refactor.
  Commit once per green cycle, so the history shows each test failing before its code.
- **Proof rule.** Every acceptance criterion names a test. The test fails once the behavior is
  removed. A plan `Verify` command that runs zero tests fails. A golden file is captured from a
  real system or written by hand, never produced by the code under test.
- Root module tests use the standard library `testing` package only. Sub-modules can use testify.
- Black-box tests in `package <name>_test`. White-box tests only for unexported algorithms.
- Levels:
  - **Unit:** every package, table-driven.
  - **Conformance:** every adapter runs its kind's suite from `internal/conformance`. A suite
    runs the same scenarios against every adapter and compares normalized events.
  - **Fuzz:** redaction over whole event shapes (keys, nesting, types, plain lines, enrichers).
    Also the JSON writer, SQL statement shaping, header parsing, and `Verify`. Crash inputs are
    committed under `testdata/fuzz`.
  - **Race:** every package under `-race`, with concurrent writes, emits, swaps, and closes.
  - **Floor:** each module builds and tests with `GOWORK=off` at its own Go floor.
  - **Drains:** against `httptest.Server` fakes with golden bodies checked against vendor docs.
    Optional `//go:build integration` tests run against real services in Docker.
  - **Schema:** every golden event and every drain body is valid against `schema/`.
  - **Snippets:** every Go block in README, docs, recipes, and skills compiles, and marked
    blocks run.
  - **Benchmarks:** each hot path has a budget in its module spec. A regression over 20% fails CI.
- Coverage is 85% or more per root package, and CI enforces it.

### Safety gates (must never regress)

| Gate | Invariant | Guarded by |
|---|---|---|
| **G1 no leak** | A value the active redactor denies never appears in any sink, drain, call record, summary, problem message, or CLI output | Event-shape fuzzing, and the leak scenario in the `drain`, `http`, `work`, and `calls` suites |
| **G2 race-free** | No shared mutable state without synchronization | `make race` over concurrent write, emit, swap, flush, and close tests |
| **G3 never hurts the app** | A failing, slow, or panicking hook never blocks, panics, or changes a response, a status, or the app's own data | Hanging, panicking, and mutating fakes in every suite |
| **G4 bounded memory** | Keys, groups, arrays, depth, event bytes, logs, errors, calls, audit records, buffers, caches, goroutines, and subscriber channels are all capped | A test that exceeds each cap and asserts the drop and its counter |
| **G5 audit integrity** | No audit record is lost without a problem report. `Verify` catches an edited, inserted, deleted, reordered, or cut line, and it survives restarts and crashes | Tamper fixtures, restart and crash tests, sampler and level tests |
| **G6 map determinism** | The same source and CLI version give byte-identical `wlog.map.json`. Leftover files never change a result | Golden tests over fixture apps, run twice |
| **G7 one shape** | Every adapter emits the same core fields, and every event is valid against its schema | The conformance suites and `make schema` |
| **G8 honest build** | Every module builds and passes its tests with `GOWORK=off` at its Go floor and on the newest Go. Doc snippets compile. CI is green before merge | `make floor`, `make snippets`, required CI checks |

## Boundaries

- **Always:**
  - Write the failing test first. Commit per green cycle with a Co-Authored-By trailer.
  - If a change touches redaction, event storage, emit, or capture, run `make race` and `make fuzz`.
  - Keep the root `go.mod` free of third-party requirements.
  - Update the module's `SPEC-<id>.md` before changing behavior it specifies.
  - Add every new adapter to its conformance suite, and every new drain to the `drain` suite.
  - Add a CHANGELOG line and a migration note for every breaking change.
- **Ask first:**
  - Adding any dependency not named in a module spec.
  - Changing the default denylist, built-in patterns, capture defaults, caps, or reserved keys.
  - Changing the event schema version.
  - Changing public API after v1.0.0.
  - A push, a release, or a tag for any module.
- **Never:**
  - Let a sink or drain receive an event that skipped the redactor.
  - Add package-level mutable state, except the `wlog.SetDefault` pointer.
  - Block, panic, or return a logging error into app code.
  - Change the app's data, response, status, or control flow.
  - Capture SQL parameters, message bodies, GraphQL variables, or LLM prompts without an opt-in.
  - Make real network calls in default `go test`.
  - Edit `~/Documents/herr` or `~/Documents/go-echo-boilerplate` as part of this initiative.

## Success criteria (initiative-level, for v1.0.0)

1. **Zero config:** `wlog.New()` plus one `Setup` line gives, per request, an event that is valid
   against `schema/event.v1.json`, uses safe capture, and has a `summary`.
2. **Agnostic:** every P1 adapter in CAPABILITIES.md passes its conformance suite. Six events
   carry the same core keys with the same meaning: requests through chi, Gin, and Fiber, a gRPC
   call, a kafka-go message, and a cobra command. A Lambda invocation matches them too.
3. **Searchable:** `wlog query --status '>=500' --since 1h ./logs` returns exactly the matching
   events from a fixture. `wlog tail` shows a line appended after it started. The `wlog mcp`
   tool `events_query` returns the same events as `wlog query`.
4. **Human-readable:** the pretty console and the fixed key order match golden files. Every
   event has one summary sentence that holds no masked value.
5. **Agent-readable:** every problem code, rule id, and reserved field has a `wlog explain` page.
   `make docs` builds `llms.txt`. Every skill snippet compiles and runs in a test.
6. **Safe:** gates G1 to G8 pass in CI. A 10 minute nightly fuzz run over event shapes finds no
   leak.
7. **Fast:** middleware plus core overhead is 55µs p50 or less on an M-series Mac. The request
   has 13 headers, a cookie, a query, and a 1KB JSON body, with safe default capture and JSON
   to stdout. The measured p50 is about 52µs on an Apple M4 Pro. No benchmark regresses by
   more than 20% in CI.
8. **Old Go works:** the root module builds and passes its tests on Go 1.21. Each sub-module
   passes at its own floor and on the newest Go release.
9. **Audit holds:** the refund scenario survives a 0% sampler, `WLOG_LEVEL=error`, a restart,
   and a crash that leaves half a line. `Verify` catches an edit, an insert, a delete, a
   reorder, and a cut journal.
10. **Easy setup:** `wlog init --yes` on each recipe app gives code that builds and serves.
    Its events pass the conformance suite for that app's kind.
11. **Audit closed:** each of the 167 findings in the gap audit maps to a passing named test or
    a recorded decision.
12. **Parity:** each PAR row in the audit is closed or has a recorded decision. Each P1 BET item
    ships.

## Open questions

None block this spec. Detail choices belong to each module spec and get reviewed there. The
capability map lists them under "Open questions for the module specs".
