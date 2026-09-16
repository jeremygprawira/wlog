# Implementation Plan: wlog v1 + v1.1

> Source of truth: [SPEC.md](../docs/SPEC.md), [CAPABILITIES.md](../docs/CAPABILITIES.md), [SPEC-redact.md](../docs/SPEC-redact.md).
> Checklist index: [todo.md](todo.md). Task ids are stable. `/build` refers to them.
> Every task also clears the project-wide Definition of Done: tests pass under `-race`, no
> regressions, behavior verified, docs/spec updated.

## Overview

wlog is built as ~25 Go modules in 6 phases. Only `redact` has a detailed module spec today, so
**each phase starts with a spec task** for the modules it introduces. Implementation of a module
never starts before its `SPEC-<id>.md` is approved. Inside each module, work is sliced
vertically: the first task of a module produces a thin, working end-to-end path, and later tasks
widen it.

The riskiest integration (core ↔ redact ↔ net/http middleware ↔ drain) is proven by the end of
**Checkpoint 2A**, before any adapters, drains or CLI get built on top.

## Architecture Decisions (from SPEC.md, affecting task order)

- **Root module stdlib-only, Go 1.23.** Every root-module task is verified with `make compat`.
  Third-party code lives in sub-modules joined by `go.work`.
- **Fixed per-event order.** Keep/sample → enrich → redact → rename → sinks. Core owns the
  order (C7), so `sample`, `enrich`, `audit` and drains only implement interfaces.
- **Redactor is immutable + atomically swapped.** `redact` ships first with no dependency on core.
  core adds `SetRedactor` (C8).
- **One HTTP middleware (`http-std`), thin framework adapters.** A shared conformance suite in
  `internal/conformance` (importable by sub-modules under the same path prefix) makes every
  adapter prove identical output.
- **gorilla/mux without importing it in root.** `wlogstd.WithRouteFunc(func(*http.Request) string)`
  plus a documented `mux.CurrentRoute` snippet and an example app.
- **Audit chain is computed at emit, after redaction, under one mutex** so concurrent audit events
  get a strict order and the stored (redacted) bytes are what is hashed.
- **`cli-map` is last in v1** because its rules detect the public APIs built in phases 1–3.

## Dependency Graph

```
T0 scaffold ─┬─ SPEC-core ─┐
             └─ redact R1..R8 ─┴─ core C1..C15
                                     │
      ┌───────────────┬──────────────┼───────────────┬─────────────┬──────────────┐
  pipeline P1-4    sample SA1-2   http-std H1-5   drain-memory M1-2  enrich E1-3  errors-herr EH1
      │               │              │                 │
      │               └──────┬───────┤             wlogtest W1
    audit A1-3 ◄─────────────┘       │
      │                  ┌───────────┼───────────┬──────────────┐
      │              http-echo    http-echo5   http-gin     log-slog / zap / zerolog / logrus, trace-otel
      │                  └───────────┴───────────┴──── examples EX1
      │
  drains (axiom, loki, file, webhook, otlp) ◄── pipeline P4 (httpdrain helper)
      │
  cli-map MP1..MP6 ◄── core, http adapters, audit, examples
      │
  v1 release ── v1.1 drains (sentry, clickhouse, datadog)
```

## Task Format

Each task: **Description · Acceptance · Verify · Deps · Files · Size** (XS 1 file, S 1–2, M 3–5).
No task is L/XL. "Files" lists test files explicitly. `example_test.go` counts toward the limit.

---

## Phase 0 — Foundation

### T0.1 Repository scaffold
**Description:** Create the root module and tooling so every later task has working `make` targets.
**Acceptance:**
- `go.mod` = `module github.com/jeremygprawira/wlog`, `go 1.23`. `go.work` lists `.`.
- `Makefile` has `test race fuzz bench lint tidy cover compat map`. Module list is derived from `go.work`, so new modules need no Makefile edit.
- MIT `LICENSE`, `.gitignore` (coverage/, bin/, *.test, fuzz cache).
**Verify:** `make test && make lint` succeed on the empty module. `make compat` runs the Go 1.23 toolchain.
**Deps:** None · **Files:** `go.mod`, `go.work`, `Makefile`, `LICENSE`, `.gitignore` · **Size:** M

### T0.2 Agent guidance + README stub
**Description:** Encode golden rules so every session follows the spec.
**Acceptance:**
- `CLAUDE.md`: read order (CAPABILITIES → SPEC → module spec → tasks), TDD rule, gates G1–G6, Always/Ask/Never boundaries, commands, and the Simple English writing rule for all prose.
- `README.md`: one-paragraph pitch, status "pre-v0", link to specs.
**Verify:** Manual review. Every boundary in SPEC.md appears in CLAUDE.md.
**Deps:** T0.1 · **Files:** `CLAUDE.md`, `README.md` · **Size:** S

### T0.3 CI workflow
**Description:** Run the gates on every push/PR (the workflow file only, since creating the GitHub remote is ask-first).
**Acceptance:**
- `.github/workflows/ci.yml` jobs: `test`, `race`, `lint`, `compat` (Go 1.23 + 1.26), `fuzz` (30s), `bench` (report only).
- Workspace-aware: runs targets for every module in `go.work`.
**Verify:** `act -j test` locally, or run each job's commands by hand and prove they pass.
**Deps:** T0.1 · **Files:** `.github/workflows/ci.yml` · **Size:** S

### T0.4 Write SPEC-core.md
**Description:** Specify core's real API before any core code: reserved keys + default namespaced layout, `Start`/`Detach`/`Set`/`SetGroup`/`Append`/`SetLevel`/`Error`/`Audit` hook point, `Key[T]`, `StrictKeys`, interfaces (`Drain`, `DrainFunc`, `ErrorExtractor`, `ErrorInfo`, `Enricher`, `Keeper`, plugin hooks), stage order, field presets, caps + counters, env vars, level rules, sinks, `OnError`, budget.
**Acceptance:**
- All six spec areas + Success Criteria with executable examples. No open questions left unanswered.
- Every SPEC.md decision that touches core is traceable to a section.
**Verify:** Human approval recorded in CAPABILITIES.md specs table.
**Deps:** SPEC.md + SPEC-redact.md approved · **Files:** `SPEC-core.md`, `CAPABILITIES.md` · **Size:** S

### Checkpoint 0
- [ ] `make test lint compat` green on the scaffold
- [ ] SPEC.md, SPEC-redact.md, SPEC-core.md approved
- [ ] First commit on `main`

---

## Phase 1A — `redact`

### R1 Key-token matching, end to end
**Description:** Thinnest working redactor: `New()` with default key denylist. `Apply` masks any matching key at any depth with `"[REDACTED]"`.
**Acceptance:**
- Tokenizer handles `_ - . space`, camelCase and acronyms (`HTTPAuthToken` → `http auth token`).
- Defaults mask `authHeader`, `X-Auth-Token`, `user_password`, `login_pin`. Do not mask `author`, `concert`, `tokenizer_version`, `spin_count`.
- Nested maps and `[]any` walked. Whole subtree replaced on match.
**Verify:** `go test -race -run 'TestRedact_Key|TestTokenize' ./redact`
**Deps:** T0.1 · **Files:** `redact/redact.go`, `redact/tokenize.go`, `redact/defaults.go`, `redact/redact_test.go`, `redact/tokenize_test.go` · **Size:** M

### R2 Paths, globs, arrays
**Description:** Dotted path entries and `*` globs per segment. Array elements inherit parent path.
**Acceptance:**
- `http.request.headers.cookie` matches only from root. `user.*` masks every child of `user`.
- `*_pin`, `x-*-secret` glob within one segment.
- `items.card_number` masks every element's `card_number`.
**Verify:** `go test -race -run TestRedact_Path ./redact`
**Deps:** R1 · **Files:** `redact/match.go`, `redact/match_test.go` · **Size:** S

### R3 Add / remove / replace keys, With, introspection
**Description:** The user-facing denylist controls and derived redactors.
**Acceptance:**
- `AddKeys`, `RemoveKeys`, `ReplaceKeys` behave per spec. `RemoveKeys("session")` leaves `session_id` intact.
- `New`/`With` return errors (never panic) for invalid globs and unknown removals. `With` never mutates the parent.
- `Keys()` sorted. `Fingerprint()` is order-independent and changes after any add/remove.
**Verify:** `go test -race -run 'TestOptions_Keys|TestWith|TestFingerprint' ./redact`
**Deps:** R2 · **Files:** `redact/options.go`, `redact/fingerprint.go`, `redact/options_test.go` · **Size:** S

### R4 Built-in value patterns A: credit_card, email, jwt, bearer
**Description:** Partial masking for the four highest-value patterns.
**Acceptance:**
- Outputs exactly match the spec table (`****1111`, `a***@***.com`, `eyJ***.***`, `Bearer ***`).
- Credit cards are Luhn-verified. A 16-digit non-Luhn id is untouched.
- Patterns run only on values not already masked by key rules.
**Verify:** `go test -race -run TestBuiltin_A ./redact`
**Deps:** R1 · **Files:** `redact/patterns.go`, `redact/patterns_test.go` · **Size:** S

### R5 Built-in value patterns B: ipv4, phone, iban, nik
**Description:** Remaining built-ins, including the Indonesia-specific ones.
**Acceptance:**
- `ipv4` skips `127.0.0.1`/`0.0.0.0`. `http.client_ip` is exempt unless `MaskClientIP()`.
- `phone` masks `+62 812-3456-7890` and `081234567890` to `+62 ****7890` form. `iban` per table.
- `nik` is off by default and on with `EnablePatterns("nik")`.
**Verify:** `go test -race -run TestBuiltin_B ./redact`
**Deps:** R4 · **Files:** `redact/patterns_id.go`, `redact/patterns_b_test.go` · **Size:** S

### R6 Pattern options + custom patterns
**Description:** Add/reduce patterns and user-defined replacements.
**Acceptance:**
- `AddPatterns`, `RemovePatterns` (built-ins by name), `EnablePatterns`, `NoBuiltinPatterns`.
- Errors for invalid regex, duplicate name, unknown name.
- `Pattern.Replace` receives `Match{Path, Key, Value, Groups}`. A panic yields `"[REDACTED]"` and does not propagate.
**Verify:** `go test -race -run 'TestOptions_Patterns|TestCustomPattern' ./redact`
**Deps:** R3, R5 · **Files:** `redact/options.go`, `redact/custom.go`, `redact/custom_test.go` · **Size:** S

### R7 Transforms, limits, Default/Disabled
**Description:** Remaining behaviour options.
**Acceptance:**
- `Transform` runs before key rules. A panicking transform does not stop redaction of the rest.
- `MaxDepth` (default 16 → `"[REDACTED:DEPTH]"`), `MaxStringScan` (default 64KB → `"[REDACTED:TOO_LARGE]"`), `Replacement`.
- `Default()` equals `MustNew()`. `Disabled().Apply` is a no-op.
**Verify:** `go test -race -run 'TestTransform|TestLimits|TestDefaultDisabled' ./redact`
**Deps:** R6 · **Files:** `redact/options.go`, `redact/limits_test.go` · **Size:** S

### R8 Gates G1/G2, benchmark, examples
**Description:** Prove the security and performance invariants.
**Acceptance:**
- `FuzzRedact_NeverLeaks`: 30s clean. Seeds cover every default key and built-in pattern.
- 64-goroutine `Apply` test passes `-race`.
- Benchmark (50 fields, 3 levels, 10 strings) recorded in `redact/BENCH.md`. Target ≤ 30µs/op, ≤ 10 allocs/op, or a spec update with the measured number approved.
- `ExampleNew`, `ExampleRedactor_With`, `ExamplePattern_replace`.
**Verify:** `make fuzz && go test -race ./redact && go test -run=xxx -bench=. -benchmem ./redact && make compat`
**Deps:** R7 · **Files:** `redact/fuzz_test.go`, `redact/bench_test.go`, `redact/example_test.go`, `redact/BENCH.md` · **Size:** M

### Checkpoint 1A
- [ ] `redact` meets all 10 SPEC-redact success criteria
- [ ] Zero non-stdlib imports (`go list -deps ./redact | grep -v '^[a-z]*$'` shows only stdlib/self)
- [ ] Human review of masking outputs before core consumes them

---

## Phase 1B — `core` (exact names follow SPEC-core.md)

### C1 Thin wide event: Start → Set → emit JSON
**Description:** First end-to-end slice: `wlog.New()`, `log.Start(ctx, name)` returns `(ctx, end)`, `wlog.Set`, `end()` redacts with `redact.Default()` and writes one JSON line to stdout.
**Acceptance:**
- Event contains `timestamp`, `level`, `duration_ms`, `service.*`, `operation`, user keys.
- A `password` key set via `Set` appears as `"[REDACTED]"` in output.
- `Set` on a ctx without an event is a silent no-op.
**Verify:** `go test -race -run TestCore_StartSetEmit ./ ./sink/...`
**Deps:** Checkpoint 1A, T0.4 · **Files:** `wlog.go`, `event.go`, `sink/json.go`, `wlog_test.go`, `sink/json_test.go` · **Size:** M

### C2 SetGroup, Append, normalization, caps (G4)
**Description:** Richer enrichment API with bounded memory.
**Acceptance:**
- `SetGroup` merges into one slot. `Append` builds arrays. Structs normalized via JSON tags.
- Key cap, array cap and group cap enforced. Overflow increments `wlog.dropped_fields` in the event.
- Concurrent writers from 32 goroutines pass `-race`.
**Verify:** `go test -race -run 'TestCore_Group|TestCore_Append|TestCore_Caps' ./`
**Deps:** C1 · **Files:** `event.go`, `normalize.go`, `event_test.go` · **Size:** S

### C3 Levels, SetLevel, outcome
**Description:** Level inference and override.
**Acceptance:**
- Default level rules from SPEC-core (errors → error, 4xx → warn, else info). `SetLevel` wins over inference.
- `outcome` is `success`/`error` per spec.
- Minimum level (`WithLevel`, `WLOG_LEVEL`) filters events.
**Verify:** `go test -race -run TestCore_Level ./`
**Deps:** C1 · **Files:** `level.go`, `level_test.go` · **Size:** S

### C4 Errors: extractor, error + errors[]
**Description:** Pluggable error capture.
**Acceptance:**
- `ErrorExtractor` interface + std fallback (message, cause chain, type).
- `ErrorInfo` fields code/message/kind/status/cause/stack/why/fix/link/attrs serialize under `error`.
- The last reported error is `error`. Earlier ones go to `errors[]` (cap 10, overflow counted).
**Verify:** `go test -race -run TestCore_Error ./`
**Deps:** C3 · **Files:** `errors.go`, `errors_test.go` · **Size:** S

### C5 Detach + sealed events
**Description:** Background work support.
**Acceptance:**
- `wlog.Detach(ctx, name)` creates a child event with parent `trace.request_id`/`trace.trace_id` and `parent_operation`. Emitted on its own `end()`.
- Writes to an emitted event are ignored and counted (`wlog.late_writes`).
- Parent emit racing with child writes passes `-race`.
**Verify:** `go test -race -run 'TestCore_Detach|TestCore_Sealed' ./`
**Deps:** C2 · **Files:** `detach.go`, `detach_test.go` · **Size:** S

### C6 Drains, OnError, Close (G3)
**Description:** Output extension point with failure isolation.
**Acceptance:**
- `Drain` interface + `DrainFunc`. `WithDrains` fans out a redacted snapshot to each.
- A panicking or erroring drain calls `OnError`, never affects other drains or the caller.
- `log.Close(ctx)` calls optional `Close` on drains, respecting the ctx deadline.
**Verify:** `go test -race -run 'TestCore_Drain|TestCore_Close' ./`
**Deps:** C1 · **Files:** `drain.go`, `drain_test.go` · **Size:** S

### C7 Stage order: keep/sample → enrich → redact → rename → sinks
**Description:** Lock the per-event pipeline order so later modules just plug in.
**Acceptance:**
- `Keeper`/sampler and `Enricher` interfaces with panic isolation.
- Order test: a dropped event never reaches an enricher. A field added by an enricher is redacted. Renaming happens after redaction.
- Audit hook point reserved (events flagged `audit` bypass sampling).
**Verify:** `go test -race -run TestCore_StageOrder ./`
**Deps:** C4, C6 · **Files:** `stages.go`, `enrich.go`, `stages_test.go` · **Size:** M

### C8 Atomic redactor swap
**Description:** Runtime denylist changes (hybrid C).
**Acceptance:**
- `WithRedactor`, `log.SetRedactor(next)` via `atomic.Pointer`. Nil → `redact.Default()`.
- `redact.fingerprint` present in every event (disable option).
- 1000 emits concurrent with 100 swaps: `-race` clean, every event fully redacted by exactly one config.
**Verify:** `go test -race -run TestCore_SetRedactor ./`
**Deps:** C7 · **Files:** `wlog.go`, `redactor_test.go` · **Size:** S

### C9 Field-name presets + renaming
**Description:** Customizable event shape.
**Acceptance:**
- Default namespaced layout matches SPEC-core golden file.
- `FieldsOTel()`, `FieldsFlat()` presets match their golden files. `WithFieldNames(map)` renames individual keys.
- Denylist entries still match canonical names after renaming.
**Verify:** `go test -race -run TestCore_Fields ./` (golden files under `testdata/`)
**Deps:** C7 · **Files:** `fields.go`, `fields_test.go`, `testdata/fields_*.golden.json` · **Size:** M

### C10 Plugins
**Description:** One struct, many hooks.
**Acceptance:**
- `WithPlugins`. Detection of optional interfaces (`Setup`, `Enricher`, `Keeper`, `Drain`, `RequestStarter`, `RequestFinisher`). Request hooks exposed for http-std.
- Each hook is panic-isolated and reports to `OnError` with the plugin name.
- A struct implementing three hooks is invoked at the right stages.
**Verify:** `go test -race -run TestCore_Plugin ./`
**Deps:** C7 · **Files:** `plugin.go`, `plugin_test.go` · **Size:** S

### C11 Typed keys + StrictKeys
**Description:** Compile-time key safety, opt-in.
**Acceptance:**
- `wlog.NewKey[T](name)`, `Key[T].Set(ctx, T)`. A compile-fail case is documented in a `go vet`-checked example (not a failing test).
- `StrictKeys(keys...)`: In a local or dev env, unregistered keys are flagged in `wlog.unknown_keys`. Untouched in prod.
- Works on Go 1.23 (`make compat`).
**Verify:** `go test -race -run TestCore_Key ./ && make compat`
**Deps:** C2 · **Files:** `key.go`, `key_test.go` · **Size:** S

### C12 Plain one-off log lines
**Description:** `log.Info/Warn/Error/Debug(ctx, msg, kv...)` outside wide events.
**Acceptance:**
- Emits a single-line event (`kind: "log"`) through the same sample → enrich → redact → drain order.
- Inside a request ctx, the line is still standalone (folding into the event is `log-slog`'s job).
**Verify:** `go test -race -run TestCore_PlainLog ./`
**Deps:** C7 · **Files:** `plain.go`, `plain_test.go` · **Size:** S

### C13 Pretty console sink
**Description:** Readable dev output.
**Acceptance:**
- Tree layout per SPEC-core (level, a method/path/status/duration header, and an error why/fix/link block).
- In a `local` or `dev` env with `WLOG_FORMAT` unset, this format is auto-selected. JSON otherwise. `NO_COLOR` respected.
- Output is redacted (G1 sink test covers it).
**Verify:** `go test -race ./sink/...` (golden files)
**Deps:** C9 · **Files:** `sink/pretty.go`, `sink/pretty_test.go`, `sink/testdata/pretty_*.golden` · **Size:** S

### C14 Env configuration
**Description:** Env vars as defaults, code wins.
**Acceptance:**
- `WLOG_ENV`, `WLOG_LEVEL`, `WLOG_FORMAT`, `WLOG_SERVICE`, `WLOG_VERSION` read once in `New`.
- An explicit option overrides the matching env var. Invalid env values → `OnError` + default, never panic.
- `internal/env` helper reusable by drains.
**Verify:** `go test -race -run TestCore_Env ./ ./internal/env`
**Deps:** C3 · **Files:** `config.go`, `internal/env/env.go`, `config_test.go` · **Size:** S

### C15 Core gates, benchmark, examples
**Description:** Close out core.
**Acceptance:**
- G1 sink test runs JSON + pretty sinks with fuzzed secrets. G2/G3/G4 tests exist for core paths.
- Benchmark Start→Set×10→emit (no drain) recorded. ≤ 20µs p50 target (half of the 50µs request budget).
- Example tests for every exported function. Coverage ≥ 85%. `make compat` green.
**Verify:** `make race fuzz compat cover && go test -run=xxx -bench=. -benchmem ./`
**Deps:** C5, C8–C14 · **Files:** `gates_test.go`, `bench_test.go`, `example_test.go` · **Size:** S

### Checkpoint 1B
- [ ] SPEC-core success criteria met. Root module still stdlib-only
- [ ] `make race fuzz compat` green
- [ ] Human review of the emitted event shape (golden files). This is the **last cheap moment to change it**

---

## Phase 2 — Pipeline, HTTP, error adapter, test tooling, audit

### S2.1 Specs: pipeline, sample, drain-memory, wlogtest
**Description:** Module specs for the delivery and test-tooling modules.
**Acceptance:** Four `SPEC-<id>.md` files with six areas + success criteria. Approved.
**Verify:** Human approval recorded in CAPABILITIES.md.
**Deps:** Checkpoint 1B · **Files:** `SPEC-pipeline.md`, `SPEC-sample.md`, `SPEC-drain-memory.md`, `SPEC-wlogtest.md`, `CAPABILITIES.md` · **Size:** M

### S2.2 Specs: http-std, enrich, errors-herr, audit
**Description:** Module specs for request capture, enrichment, herr and audit.
**Acceptance:** Four specs approved. Http-std spec defines the conformance suite contract. Audit spec defines canonical JSON + hash format.
**Verify:** Human approval recorded in CAPABILITIES.md.
**Deps:** Checkpoint 1B · **Files:** `SPEC-http-std.md`, `SPEC-enrich.md`, `SPEC-errors-herr.md`, `SPEC-audit.md`, `CAPABILITIES.md` · **Size:** M

### H1 net/http middleware, thin slice (high risk → first)
**Description:** One event per request with core HTTP fields.
**Acceptance:**
- `wlogstd.Middleware(log)(handler)` sets `http.method/route/path/status/duration_ms/bytes_out`, `trace.request_id` (reuse `X-Request-ID` or generate one, then echo it on the response).
- A present Go 1.22+ `r.Pattern` supplies the route. `WithRouteFunc` overrides it.
- Handlers can call `wlog.Set(r.Context(), …)` and see it in the event.
**Verify:** `go test -race -run TestStd_Basic ./middleware/nethttp`
**Deps:** S2.2, C15 · **Files:** `middleware/nethttp/middleware.go`, `middleware/nethttp/writer.go`, `middleware/nethttp/middleware_test.go` · **Size:** S

### H2 Header, query, param, cookie capture + skip rules
**Description:** Default-everything metadata capture with toggles.
**Acceptance:**
- Headers, query, cookies captured by default. Each toggleable globally and per route. `SkipPaths("/health")` emits nothing.
- `Authorization`/`Cookie` values are redacted in output (via core, no special-casing).
**Verify:** `go test -race -run TestStd_Capture ./middleware/nethttp`
**Deps:** H1 · **Files:** `middleware/nethttp/capture.go`, `middleware/nethttp/options.go`, `middleware/nethttp/capture_test.go` · **Size:** S

### H3 Body capture
**Description:** Request + response bodies, bounded.
**Acceptance:**
- Both bodies captured by default, 10KB cap each, JSON parsed else `raw` string. Request body restored for the handler.
- Content-type filters and per-route disable. Binary types skipped by default.
- Writer wrapper preserves `http.Flusher`, `http.Hijacker`, `io.ReaderFrom`. Streaming responses do not buffer past cap (G4).
**Verify:** `go test -race -run TestStd_Body ./middleware/nethttp`
**Deps:** H2 · **Files:** `middleware/nethttp/body.go`, `middleware/nethttp/writer.go`, `middleware/nethttp/body_test.go` · **Size:** S

### H4 Panics, traceparent, user id, plugin request hooks
**Description:** Strength and correlation.
**Acceptance:**
- Panic → 500 response, event has `error` with stack, level error. Process keeps serving.
- W3C `traceparent` parsed into `trace.trace_id/span_id`. Invalid header ignored.
- `WithUserFunc(func(*http.Request) string)`. `RequestStarter`/`RequestFinisher` plugin hooks invoked.
**Verify:** `go test -race -run 'TestStd_Panic|TestStd_Trace|TestStd_Hooks' ./middleware/nethttp`
**Deps:** H3, C10 · **Files:** `middleware/nethttp/recover.go`, `middleware/nethttp/trace.go`, `middleware/nethttp/hooks_test.go` · **Size:** S

### H5 Conformance suite + gorilla/mux proof
**Description:** The contract every HTTP adapter must pass.
**Acceptance:**
- `internal/conformance.Run(t, Adapter)` covers route, status, bodies, headers, panic, request id, traceparent, Set from handler, skip paths. Compares normalized events.
- net/http adapter passes it. A mux example (in `examples/`, own module) passes it using `WithRouteFunc(mux.CurrentRoute…)`.
**Verify:** `go test -race ./middleware/nethttp ./internal/conformance && (cd examples && go test -race ./mux/...)`
**Deps:** H4 · **Files:** `internal/conformance/suite.go`, `middleware/nethttp/conformance_test.go`, `examples/go.mod`, `examples/mux/main.go`, `examples/mux/main_test.go` · **Size:** M

### Checkpoint 2A — first real end-to-end
- [ ] net/http + mux emit complete, redacted events through a `DrainFunc` test drain
- [ ] Middleware + core overhead benchmark ≤ 50µs p50 with default capture + 1KB JSON body (SPEC.md criterion 9)
- [ ] Human looks at a real request's event before adapters multiply it

### P1 Batching + flush
**Description:** `pipeline.Wrap(drain, opts)` buffers and flushes.
**Acceptance:**
- Flush on `BatchSize` (default 50) or `Interval` (default 5s), whichever first.
- `Close(ctx)` flushes everything pending within the deadline.
- Emit path never waits on the wrapped drain (G3).
**Verify:** `go test -race -run TestPipeline_Batch ./pipeline`
**Deps:** S2.1, C6 · **Files:** `pipeline/pipeline.go`, `pipeline/options.go`, `pipeline/pipeline_test.go` · **Size:** S

### P2 Retry + backoff
**Description:** Transient failure handling.
**Acceptance:**
- `MaxAttempts` (default 3), backoff `exponential|linear|fixed`, `InitialDelay` 1s, `MaxDelay` 30s, with jitter.
- Exhausted retries call `OnDropped(events, err)`. Tests use a fake clock (no sleeps > 10ms).
**Verify:** `go test -race -run TestPipeline_Retry ./pipeline`
**Deps:** P1 · **Files:** `pipeline/retry.go`, `pipeline/retry_test.go` · **Size:** S

### P3 Bounded buffer, drop-oldest, fan-out isolation
**Description:** Overload behaviour (G3, G4).
**Acceptance:**
- `MaxBuffer` (default 1000). Overflow drops oldest, calls `OnDropped`, increments `Dropped()`.
- A hanging drain never blocks emits (test: 10k emits against a drain that never returns finish < 1s).
- `pipeline.FanOut(d1, d2)`: one failing destination does not delay or fail the other.
**Verify:** `go test -race -run 'TestPipeline_Overflow|TestPipeline_FanOut' ./pipeline`
**Deps:** P2 · **Files:** `pipeline/buffer.go`, `pipeline/fanout.go`, `pipeline/overflow_test.go` · **Size:** S

### P4 Shared HTTP drain helper + identity headers
**Description:** Common code for every HTTP-based drain.
**Acceptance:**
- `internal/httpdrain`: POST with timeout, optional gzip, status classification (retryable 429/5xx vs permanent 4xx), `Retry-After` honoured.
- Sends `User-Agent: wlog/<version>` and `X-Wlog-Source: <name>`. Overridable/disable-able.
- `internal/httpfake` test server records requests for drain tests.
**Verify:** `go test -race ./internal/httpdrain ./internal/httpfake`
**Deps:** P2 · **Files:** `internal/httpdrain/httpdrain.go`, `internal/httpdrain/httpdrain_test.go`, `internal/httpfake/httpfake.go`, `internal/version/version.go` · **Size:** M

### SA1 Head + tail sampling rules
**Description:** The sampler as a core `Keeper`.
**Acceptance:**
- Head: rate per level (0–100), unset levels 100, error 100 unless explicitly set.
- Tail keep: `Status(>=n)`, `Duration(>=d)`, `Path(glob)`, `Func(pred)`. OR logic. Tail-kept events skip head sampling.
- Deterministic tests via injectable random source.
**Verify:** `go test -race -run TestSample ./sample`
**Deps:** S2.1, C7 · **Files:** `sample/sample.go`, `sample/rules.go`, `sample/sample_test.go` · **Size:** S

### SA2 Presets + defaults
**Description:** Ready-made rules, default keep 100%.
**Acceptance:**
- No sampler configured → every event kept.
- `sample.KeepErrorsAndSlow(slow, healthyRate)` preset. Audit-flagged events always kept.
**Verify:** `go test -race -run TestSamplePresets ./sample && go test -race -run TestCore_StageOrder ./`
**Deps:** SA1 · **Files:** `sample/presets.go`, `sample/presets_test.go` · **Size:** S

### M1 Memory drain: ring buffer + subscribe
**Description:** In-process event store and live feed.
**Acceptance:**
- `memory.New(size)` (default 1000) ring buffer. `Snapshot()` copies. `Subscribe(ctx) <-chan Event` with a per-subscriber buffer. Slow subscribers drop (counted), never block (G3/G4).
**Verify:** `go test -race ./drain/memory`
**Deps:** S2.1, C6 · **Files:** `drain/memory/memory.go`, `drain/memory/memory_test.go` · **Size:** S

### M2 SSE handler
**Description:** Watch events live over HTTP.
**Acceptance:**
- `SSEHandler()` streams events as `text/event-stream`. Replays last N on connect. Closes on client disconnect without leaking goroutines (goroutine count check).
**Verify:** `go test -race -run TestSSE ./drain/memory`
**Deps:** M1 · **Files:** `drain/memory/sse.go`, `drain/memory/sse_test.go` · **Size:** S

### W1 wlogtest recorder
**Description:** Assertions for users' tests.
**Acceptance:**
- `wlogtest.New(t, opts...)` returns a logger + recorder backed by the memory drain.
- Helpers: `Events()`, `Last()`, `RequireField(t, key, want)`, `RequireErrorCode(t, code)`, `RequireCount(t, n)`. Failure messages show the event diff.
- Used by at least one existing test (H5 or C15) to prove ergonomics.
**Verify:** `go test -race ./wlogtest`
**Deps:** M1 · **Files:** `wlogtest/wlogtest.go`, `wlogtest/wlogtest_test.go`, `wlogtest/example_test.go` · **Size:** S

### E1 Host + deployment enrichers
**Description:** Environment context on every event.
**Acceptance:**
- `enrich.Host()` (hostname, pid, pod/namespace/node from k8s downward-API env vars), `enrich.Deployment()` (region, commit, version from env), all with `Overwrite(false)` default.
**Verify:** `go test -race -run 'TestHost|TestDeployment' ./enrich`
**Deps:** S2.2, C7 · **Files:** `enrich/host.go`, `enrich/enrich_test.go` · **Size:** S

### E2 User agent enricher
**Description:** Parse UA into browser/os/device with stdlib only.
**Acceptance:**
- Detects Chrome, Edge, Firefox, Safari, curl/Go-http-client, bots. IOS/Android/Windows/macOS/Linux. Mobile/desktop/bot.
- Table test with ≥ 30 real UA strings. Unknown → `raw` only.
**Verify:** `go test -race -run TestUserAgent ./enrich`
**Deps:** E1 · **Files:** `enrich/useragent.go`, `enrich/useragent_test.go` · **Size:** S

### E3 Geo + user-id enrichers
**Description:** CDN geo headers and user lookup.
**Acceptance:**
- `enrich.Geo()` maps Cloudflare, CloudFront, Vercel headers into `geo.country/region/city/lat/lon`. `GeoHeaders(map)` for custom CDNs.
- `enrich.User(func(ctx) (id string))`. Panics isolated by core.
**Verify:** `go test -race -run 'TestGeo|TestUser' ./enrich`
**Deps:** E1 · **Files:** `enrich/geo.go`, `enrich/user.go`, `enrich/geo_test.go` · **Size:** S

### EH1 herr error extractor
**Description:** First non-std `ErrorExtractor`, in its own module.
**Acceptance:**
- `errors/herr/go.mod` requires herr v0.1.0 (`go 1.26.1`) and wlog via workspace.
- `wlogherr.Extractor()` maps herr `Record` → `ErrorInfo` (code, kind, status, internal → message, fields → attrs, cause, stack). Non-herr errors fall back to std.
- herr public message is **not** copied into logs unless `WithPublicMessage()`.
**Verify:** `cd errors/herr && go test -race ./...`
**Deps:** S2.2, C4 · **Files:** `errors/herr/go.mod`, `errors/herr/herr.go`, `errors/herr/herr_test.go`, `go.work` · **Size:** S

### A1 Audit records
**Description:** `wlog.Audit` inside and outside a request.
**Acceptance:**
- `audit.Record{Actor{Type,ID,Email}, Action, Target{Type,ID}, Outcome, Reason}` sets reserved `audit` field on the current event, or emits a standalone audit event without one.
- Audit events bypass sampling (0% sampler test), gate G5 part 1.
- Actor email is still redacted by default patterns.
**Verify:** `go test -race -run TestAudit_Record ./audit ./`
**Deps:** S2.2, SA2 · **Files:** `audit/audit.go`, `audit/audit_test.go`, `wlog.go` · **Size:** S

### A2 Hash chain
**Description:** Tamper-evidence at emit time.
**Acceptance:**
- After redaction, canonical JSON (sorted keys) of the audit event is hashed: `audit.hash = sha256(prev_hash || canonical)`, `audit.prev_hash` set. Chain state is per logger under one mutex.
- 100 concurrent audit emits produce a single valid chain (`-race`).
**Verify:** `go test -race -run TestAudit_Chain ./audit`
**Deps:** A1, C8 · **Files:** `audit/chain.go`, `audit/canonical.go`, `audit/chain_test.go` · **Size:** S

### A3 Journal drain + Verify (G5)
**Description:** Append-only local proof.
**Acceptance:**
- `audit.Journal(path)` appends NDJSON with fsync per batch, `O_APPEND`, file mode 0600. Resumes the chain from the last line on restart.
- `audit.Verify(path)` reports the first broken line. Fixtures with an edited, reordered and deleted line all fail.
- Journal is fed through `pipeline` like any drain. The main drain receives the same event.
**Verify:** `go test -race -run 'TestAudit_Journal|TestAudit_Verify' ./audit`
**Deps:** A2, P3 · **Files:** `audit/journal.go`, `audit/verify.go`, `audit/journal_test.go`, `audit/testdata/tampered_*.ndjson` · **Size:** M

### Checkpoint 2B
- [ ] Every phase-2 module meets its spec. G1–G5 green
- [ ] Refund-handler scenario (SPEC.md criterion 11) passes as an integration test
- [ ] Human review

---

## Phase 3 — Framework and logger adapters, trace, examples

### S3 Specs: http-echo, http-echo5, http-gin, log-slog, log-zap/zerolog/logrus, trace-otel
**Description:** Short adapter specs (they inherit http-std / core contracts). Verify Echo v5's released API surface at spec time.
**Acceptance:** Specs approved. Each names the exact upstream versions pinned.
**Verify:** Human approval recorded in CAPABILITIES.md.
**Deps:** Checkpoint 2B · **Files:** `SPEC-http-adapters.md`, `SPEC-log-adapters.md`, `SPEC-trace-otel.md`, `CAPABILITIES.md` · **Size:** S

### HE4 Echo v4 adapter
**Acceptance:**
- `wlogecho.Middleware(log)` wraps http-std. Route from `c.Path()`. Errors returned from handlers reach `wlog.Error` and still flow to Echo's `HTTPErrorHandler`.
- Passes `internal/conformance`.
**Verify:** `cd middleware/echo && go test -race ./...`
**Deps:** S3, H5 · **Files:** `middleware/echo/go.mod`, `middleware/echo/echo.go`, `middleware/echo/echo_test.go`, `go.work` · **Size:** S

### HE5 Echo v5 adapter
**Acceptance:** Same as HE4 against `github.com/labstack/echo/v5`. Passes conformance.
**Verify:** `cd middleware/echo5 && go test -race ./...`
**Deps:** S3, H5 · **Files:** `middleware/echo5/go.mod`, `middleware/echo5/echo.go`, `middleware/echo5/echo_test.go`, `go.work` · **Size:** S

### HG Gin adapter
**Acceptance:**
- `wloggin.Middleware(log)`. Route from `c.FullPath()`. `c.Errors` reported via `wlog.Error`. Body capture works with Gin's writer.
- Passes conformance.
**Verify:** `cd middleware/gin && go test -race ./...`
**Deps:** S3, H5 · **Files:** `middleware/gin/go.mod`, `middleware/gin/gin.go`, `middleware/gin/gin_test.go`, `go.work` · **Size:** S

### LS1 slog output
**Acceptance:**
- `wlogslog.Drain(handler slog.Handler)` writes each event as one slog record (nested groups preserved).
- Stdlib only (root module).
**Verify:** `go test -race -run TestSlogOutput ./log/slog`
**Deps:** S3, C6 · **Files:** `log/slog/output.go`, `log/slog/output_test.go` · **Size:** S

### LS2 slog input
**Acceptance:**
- `wlogslog.Handler(next slog.Handler)`: records with a ctx carrying an event are appended to `logs[]` (level, msg, and attrs, capped per SPEC-core). Others pass to `next`.
- `slog.SetDefault(slog.New(wlogslog.Handler(...)))` works with third-party libraries logging via slog.
**Verify:** `go test -race -run TestSlogInput ./log/slog`
**Deps:** LS1 · **Files:** `log/slog/input.go`, `log/slog/input_test.go` · **Size:** S

### LZ zap output
**Acceptance:** `wlogzap.Drain(*zap.Logger)`. Nested fields via `zap.Object`/`zap.Any`. Level mapped.
**Verify:** `cd log/zap && go test -race ./...`
**Deps:** S3, C6 · **Files:** `log/zap/go.mod`, `log/zap/zap.go`, `log/zap/zap_test.go`, `go.work` · **Size:** S

### LZR zerolog output
**Acceptance:** `wlogzerolog.Drain(zerolog.Logger)`. Nested dicts. Level mapped.
**Verify:** `cd log/zerolog && go test -race ./...`
**Deps:** S3, C6 · **Files:** `log/zerolog/go.mod`, `log/zerolog/zerolog.go`, `log/zerolog/zerolog_test.go`, `go.work` · **Size:** S

### LL logrus output
**Acceptance:** `wloglogrus.Drain(*logrus.Logger)`. Fields via `WithFields`. Level mapped.
**Verify:** `cd log/logrus && go test -race ./...`
**Deps:** S3, C6 · **Files:** `log/logrus/go.mod`, `log/logrus/logrus.go`, `log/logrus/logrus_test.go`, `go.work` · **Size:** S

### TO OpenTelemetry span link
**Acceptance:** `wlogotel.Enricher()` sets `trace.trace_id/span_id` from an active span in ctx. A valid span context is the only thing that overrides a traceparent-derived value.
**Verify:** `cd trace/otel && go test -race ./...`
**Deps:** S3, C7 · **Files:** `trace/otel/go.mod`, `trace/otel/otel.go`, `trace/otel/otel_test.go`, `go.work` · **Size:** S

### EX1 Example apps (HTTP)
**Acceptance:**
- `examples/` apps: `nethttp`, `echo`, `echo5`, `gin` (mux exists from H5), each ≤ 5 lines of wlog setup (SPEC.md criterion 2), each with a test asserting identical core fields.
**Verify:** `cd examples && go test -race ./...`
**Deps:** HE4, HE5, HG · **Files:** `examples/nethttp/main.go`, `examples/echo/main.go`, `examples/echo5/main.go`, `examples/gin/main.go`, `examples/http_parity_test.go` · **Size:** M

### EX2 Example apps (extension points)
**Acceptance:**
- `detach`, `custom-drain`, `custom-extractor`, `plugin`, `audit-refund`, `typed-keys`, `logger-output` examples. Each has a test. README table links them.
**Verify:** `cd examples && go test -race ./...`
**Deps:** EX1, A3, LS2, LZ · **Files:** 5 small example dirs, each with a `main.go` and a `main_test.go`, split across two commits past 5 files · **Size:** M

### Checkpoint 3
- [ ] Five HTTP stacks produce identical core fields (criterion 2). Logger swap is one option (criterion 4)
- [ ] `make race compat` green across all modules
- [ ] Human review

---

## Phase 4 — v1 drains

### S4 Specs: axiom, loki, file, webhook, otlp
**Acceptance:** Wire formats, env vars, Loki label set (default `service`, `env`, `level`), file rotation policy and OTLP JSON attribute mapping specified and approved.
**Verify:** Human approval recorded in CAPABILITIES.md.
**Deps:** Checkpoint 3 · **Files:** `SPEC-drains-v1.md`, `CAPABILITIES.md` · **Size:** S

### DA Axiom drain
**Acceptance:** `axiom.New(opts)` posts NDJSON batches to the ingest API. `AXIOM_TOKEN`, `AXIOM_DATASET`, `AXIOM_URL` env. 401/403 non-retryable. Verified against `httpfake`.
**Verify:** `go test -race ./drain/axiom`
**Deps:** S4, P4 · **Files:** `drain/axiom/axiom.go`, `drain/axiom/axiom_test.go` · **Size:** S

### DL Loki drain
**Acceptance:** Push API JSON streams grouped by label set. Labels configurable, high-cardinality keys rejected at construction. `LOKI_URL`, `LOKI_USERNAME`, `LOKI_PASSWORD`, `LOKI_TENANT_ID`. Nanosecond timestamps.
**Verify:** `go test -race ./drain/loki`
**Deps:** S4, P4 · **Files:** `drain/loki/loki.go`, `drain/loki/loki_test.go` · **Size:** S

### DF File drain
**Acceptance:** NDJSON append. Rotation by size and age with max backups. Mode 0600. Safe under concurrent batches. `WLOG_FILE_PATH`.
**Verify:** `go test -race ./drain/file`
**Deps:** S4, P1 · **Files:** `drain/file/file.go`, `drain/file/rotate.go`, `drain/file/file_test.go` · **Size:** S

### DW Webhook drain
**Acceptance:** POST JSON array (or NDJSON option) to any URL with custom headers and optional HMAC signature header. `WLOG_WEBHOOK_URL`.
**Verify:** `go test -race ./drain/webhook`
**Deps:** S4, P4 · **Files:** `drain/webhook/webhook.go`, `drain/webhook/webhook_test.go` · **Size:** S

### DO OTLP drain
**Acceptance:** OTLP/HTTP JSON `ExportLogsServiceRequest`. Resource attrs from `service.*`. Body/attributes mapping per spec. Severity number mapping. `OTEL_EXPORTER_OTLP_ENDPOINT`/`_HEADERS` honoured. Payload proven against a golden file captured from a real collector.
**Verify:** `go test -race ./drain/otlp`
**Deps:** S4, P4 · **Files:** `drain/otlp/otlp.go`, `drain/otlp/mapping.go`, `drain/otlp/otlp_test.go`, `drain/otlp/testdata/export.golden.json` · **Size:** M

### DI Optional docker integration tests
**Acceptance:** `//go:build integration` tests + `docker-compose.integration.yml` for Loki and an OTel collector. `make integration` target. Not part of default `make test`.
**Verify:** `make integration` locally with docker running.
**Deps:** DL, DO · **Files:** `docker-compose.integration.yml`, `drain/loki/integration_test.go`, `drain/otlp/integration_test.go`, `Makefile` · **Size:** S

### Checkpoint 4
- [ ] Every v1 drain works from env vars alone (criterion 5). G1 drain test covers all of them
- [ ] Human review

---

## Phase 5 — `cli-map`, then v1 release

### S5 Spec: cli-map
**Acceptance:** Rule ids + weights, entry-point detection per framework, sensitive-route patterns + config file, score formula, `wlog.map.json` schema, baseline comparison semantics. Approved.
**Verify:** Human approval recorded in CAPABILITIES.md.
**Deps:** Checkpoint 4 · **Files:** `SPEC-cli-map.md`, `CAPABILITIES.md` · **Size:** S

### MP1 CLI skeleton + net/http/mux entry points
**Acceptance:** `cmd/wlog` module (`golang.org/x/tools`). `wlog map ./...` loads packages and lists net/http + mux handlers found in fixture apps under `testdata/`.
**Verify:** `cd cmd/wlog && go test -race ./...`
**Deps:** S5, EX1 · **Files:** `cmd/wlog/go.mod`, `cmd/wlog/main.go`, `cmd/wlog/entry/nethttp.go`, `cmd/wlog/entry/entry_test.go`, `go.work` · **Size:** M

### MP2 Echo v4/v5 + Gin entry points
**Acceptance:** Handlers registered via Echo v4, v5 and Gin routers detected in fixtures, with route strings.
**Verify:** `cd cmd/wlog && go test -race ./entry/...`
**Deps:** MP1 · **Files:** `cmd/wlog/entry/echo.go`, `cmd/wlog/entry/gin.go`, `cmd/wlog/entry/frameworks_test.go` · **Size:** S

### MP3 Rules: middleware coverage, context set, errors reach wlog
**Acceptance:** Three rules report per-handler pass/fail with positions. Fixtures for pass and fail of each.
**Verify:** `cd cmd/wlog && go test -race ./rules/...`
**Deps:** MP2 · **Files:** `cmd/wlog/rules/coverage.go`, `cmd/wlog/rules/context.go`, `cmd/wlog/rules/errors.go`, `cmd/wlog/rules/rules_a_test.go` · **Size:** M

### MP4 Rules: sensitive-route audit, no print logging, no denylisted keys
**Acceptance:** Sensitive routes (default patterns + `wlog.map.yaml`/JSON config) get 2× weight and require `Audit`. `fmt.Print*`/`log.Print*` in handlers flagged. `Set` with a literal key matching `redact.Default()` flagged.
**Verify:** `cd cmd/wlog && go test -race ./rules/...`
**Deps:** MP3 · **Files:** `cmd/wlog/rules/audit.go`, `cmd/wlog/rules/print.go`, `cmd/wlog/rules/keys.go`, `cmd/wlog/rules/rules_b_test.go` · **Size:** M

### MP5 Score, report, CI gates (G6)
**Acceptance:** Deterministic 0–100 score. Top-3 fixes. `wlog.map.json` (sorted, no timestamps). `--min-score n` and `--baseline file` set non-zero exit. Golden-file tests prove byte-identical output across runs.
**Verify:** `cd cmd/wlog && go test -race -run 'TestScore|TestReport|TestGolden' ./...`
**Deps:** MP4 · **Files:** `cmd/wlog/score/score.go`, `cmd/wlog/report/report.go`, `cmd/wlog/score/score_test.go`, `cmd/wlog/testdata/golden/*.json` · **Size:** M

### MP6 go vet analyzer + dogfooding
**Acceptance:** `cmd/wlog/analyzer` exports an `*analysis.Analyzer` runnable via `go vet -vettool` and documented for golangci-lint. `make map` passes with `--min-score 80` on `examples/`.
**Verify:** `make map && go vet -vettool=$(go env GOPATH)/bin/wlogvet ./examples/...`
**Deps:** MP5, EX2 · **Files:** `cmd/wlog/analyzer/analyzer.go`, `cmd/wlog/cmd/wlogvet/main.go`, `cmd/wlog/analyzer/analyzer_test.go` · **Size:** S

### REL1 v1 documentation + parity audit
**Acceptance:**
- README: quick start, every module with install line, customization guide, event shape, gates.
- Per-module `doc.go` package docs. Evlog parity table (criterion 15) with a module or "designed for" entry for every non-TypeScript-specific evlog doc page.
- All 15 SPEC.md success criteria ticked with evidence links (test names).
**Verify:** Manual walkthrough of README quick start in a fresh module. `go doc` renders every package.
**Deps:** MP6, Checkpoint 4 · **Files:** `README.md`, `docs/customization.md`, `docs/evlog-parity.md`, `docs/event-shape.md` · **Size:** M

### Checkpoint 5 — v1 release gate
- [ ] SPEC.md success criteria 1–9 and 11–15 met. Criterion 10 met for the v1 drains
- [ ] `make test race fuzz lint compat map` green across every module
- [ ] **Ask first:** create GitHub remote, push, tag `v0.1.0` (root + each sub-module path tag)

---

## Phase 6 — v1.1 drains

### S6 Specs: sentry, clickhouse, datadog
**Acceptance:** Sentry envelope format + grouping by error code. ClickHouse DDL helper + JSONEachRow mapping. Datadog intake format + site selection. Approved.
**Verify:** Human approval recorded in CAPABILITIES.md.
**Deps:** Checkpoint 5 · **Files:** `SPEC-drains-v1.1.md`, `CAPABILITIES.md` · **Size:** S

### DS Sentry drain
**Acceptance:** Error events → envelope API issues, fingerprint = error code (fallback type), wide event attached as context. `AllEvents()` opt-in sends to Sentry Logs. `SENTRY_DSN`.
**Verify:** `go test -race ./drain/sentry`
**Deps:** S6, P4 · **Files:** `drain/sentry/sentry.go`, `drain/sentry/envelope.go`, `drain/sentry/sentry_test.go` · **Size:** S

### DC ClickHouse drain
**Acceptance:** HTTP interface `INSERT … FORMAT JSONEachRow`. `clickhouse.DDL(table)` returns the recommended schema. Never auto-creates. `CLICKHOUSE_URL/USER/PASSWORD/DATABASE/TABLE`.
**Verify:** `go test -race ./drain/clickhouse`
**Deps:** S6, P4 · **Files:** `drain/clickhouse/clickhouse.go`, `drain/clickhouse/ddl.go`, `drain/clickhouse/clickhouse_test.go` · **Size:** S

### DD Datadog drain
**Acceptance:** Logs intake v2 JSON array. `DD_API_KEY`, `DD_SITE`. `ddsource=wlog`, `service`, `ddtags` from env/service fields. 413 splits the batch.
**Verify:** `go test -race ./drain/datadog`
**Deps:** S6, P4 · **Files:** `drain/datadog/datadog.go`, `drain/datadog/datadog_test.go` · **Size:** S

### Checkpoint 6 — v1.1
- [ ] Criterion 10 fully met. ClickHouse integration test added to `make integration`
- [ ] **Ask first:** tag `v0.2.0`

---

## Phase 7 — v1.2, public API (closes gaps 1, 2, 3, 4, 5, 12, 15)

Specs: [SPEC-catalog.md](../docs/SPEC-catalog.md), [SPEC-llm.md](../docs/SPEC-llm.md),
[SPEC-v1.2-additions.md](../docs/SPEC-v1.2-additions.md).

### D1 Plain-English pass over the older specs
**Description:** The seven specs written before the lint hook carry 111 style violations.
**Acceptance:**
- `SPEC-core.md`, `SPEC-audit.md`, `SPEC-drain-memory.md`, `SPEC-redact.md`, `SPEC-errors-herr.md`, `SPEC-cli-map.md`, and `SPEC-drains-v1.md` each lint clean.
- Wording only. No fact, name, path, or code block changes.
- Each amended spec gains a link to its v1.2 addition section.
**Verify:** `python3 evals/ste_lint.py docs/SPEC-*.md` reports 0 for every file.
**Deps:** none · **Files:** the 7 specs · **Size:** S

### CE1 ErrorInfo data and internal split
**Acceptance:**
- `ErrorInfo.Data` and `ErrorInfo.Internal` reach the event. `Attrs` still works untouched.
- `wlog.ErrorData(ctx)` returns the current error's `Data`, or nil.
- `wlog.Errorf(ctx, format, a...)` records and returns the same error value.
**Verify:** `go test -race -run 'TestCore_Error(Data|f)_' .`
**Deps:** D1 · **Files:** `errors.go`, `errors_test.go`, `docs/SPEC-v1.2-additions.md` · **Size:** S

### CE2 Global modes
**Acceptance:**
- `SetEnabled(false)` makes `Start` return a no-op end func. A recorder sees no events.
- `WithSilent()` writes nothing to stdout and still reaches every drain.
- `WithRawValues()` skips `normalize` for a value already in tree form. Redaction still walks it.
**Verify:** `go test -race -run 'TestCore_(Enabled|Silent|RawValues)' .`
**Deps:** CE1 · **Files:** `wlog.go`, `config.go`, `event.go`, `modes_test.go` · **Size:** S

### CT1 Catalog registry
**Acceptance:**
- `New(prefix, entries...)` builds full codes, panics on a duplicate or empty code.
- `Get` resolves a short and a full code to one entry. `Codes` returns them sorted.
- `Err(code, params...)` renders the template, supports `errors.Is` and `errors.As`, and unwraps a `%w` cause.
- A missing param leaves its placeholder in place, and never panics.
**Verify:** `go test -race ./catalog`
**Deps:** CE1 · **Files:** `catalog/catalog.go`, `catalog/entry.go`, `catalog/error.go`, `catalog/catalog_test.go` · **Size:** M

### CT2 Catalog extractor, agnostic proof
**Acceptance:**
- `Extractor(next, regs...)` fills `Kind`, `Status`, `Why`, `Fix`, and `Link` from the matching entry, and never replaces a field `next` filled.
- An unmatched code passes through untouched.
- One registry gives the same `ErrorInfo` through the herr extractor and the default extractor.
**Verify:** `go test -race -run 'TestCatalog_Extractor' ./catalog ./errors/herr`
**Deps:** CT1 · **Files:** `catalog/extractor.go`, `catalog/extractor_test.go` · **Size:** S

### CT3 herr catalog bridge
**Acceptance:**
- `wlogherr.Catalog(classes...)` maps a herr `Class` to a `catalog.Entry` with the same code, kind, and status.
- `catalog` still imports nothing outside the standard library plus `core`.
**Verify:** `cd errors/herr && go test -race ./...`
**Deps:** CT2 · **Files:** `errors/herr/catalog.go`, `errors/herr/catalog_test.go` · **Size:** S

### LM1 LLM record and event fields
**Acceptance:**
- `llm.Set(ctx, Record{...})` writes every non-zero field under `llm`, and leaves zero fields off.
- `llm.Add` folds totals and appends to `llm.calls[]`, capped by core's array limit.
- Both are no-ops outside a `wlog.Start`, and never panic.
- No field ever holds prompt or completion text. A test asserts it.
**Verify:** `go test -race -run 'TestLLM_(Set|Add)' ./llm`
**Deps:** CE1 · **Files:** `llm/record.go`, `llm/set.go`, `llm/record_test.go` · **Size:** M

### LM2 Pricing, cost, enricher
**Acceptance:**
- `Cost` prices a record from token counts, and prices a cached input token at the cached rate.
- Money stays in whole micros. A test prices a value with no float representation.
- An unknown model reports false, and the enricher sets `llm.cost_unknown`.
- `With` returns a new `Prices` and leaves the original unchanged, under `-race`.
**Verify:** `go test -race ./llm`
**Deps:** LM1 · **Files:** `llm/price.go`, `llm/enricher.go`, `llm/price_test.go`, `docs/cost.md` · **Size:** M

### AX1 Audit record extras
**Acceptance:**
- `Version`, `IdempotencyKey`, and `Context` reach the event under `audit`.
- `Deny` records outcome "denied". `Only` emits an audit record with no other event fields.
- `Wrap(ctx, r, fn)` records "success" on a nil error and "error" otherwise, keeping the error's code in `audit.error_code`.
**Verify:** `go test -race -run 'TestAudit_(Deny|Only|Wrap|Extras)' ./audit`
**Deps:** CE1 · **Files:** `audit/audit.go`, `audit/wrap.go`, `audit/extras_test.go` · **Size:** M

### AX2 Diff and the test mock
**Acceptance:**
- `Diff(before, after)` returns only changed fields as `{"field": {"from": x, "to": y}}`, and an empty map for two equal values. It walks a struct through its JSON tags.
- `Mock(t)` returns a logger and a recorder with `RequireAction`, `RequireOutcome`, `RequireActor`, and `RequireNoAudit`.
**Verify:** `go test -race -run 'TestAudit_(Diff|Mock)' ./audit`
**Deps:** AX1 · **Files:** `audit/diff.go`, `audit/mock.go`, `audit/diff_test.go`, `audit/mock_test.go` · **Size:** M

### AX3 HMAC signing and catalog-driven audit
**Acceptance:**
- `Sign(key)` adds `audit.signature`, an HMAC-SHA256 over the chain hash. `Verify` with the wrong key fails, and with the right key passes.
- `audit.Catalog(reg)` reads audit metadata from a catalog entry.
- An entry with `ReasonRequired` and an empty reason records `audit.reason_missing` and still reaches the journal.
**Verify:** `go test -race -run 'TestAudit_(Sign|Catalog|Reason)' ./audit`
**Deps:** AX2, CT2 · **Files:** `audit/sign.go`, `audit/catalog.go`, `audit/verify.go`, `audit/sign_test.go` · **Size:** M

### MQ1 Memory named stores and queries
**Acceptance:**
- `Named(name, size)` returns the same store for the same name, under `-race`.
- `Query(Filter)` filters by level, time range, contained pair, and custom function, and honors `Limit`.
- `Clear` empties a store and keeps its size.
**Verify:** `go test -race ./drain/memory`
**Deps:** CE1 · **Files:** `drain/memory/named.go`, `drain/memory/query.go`, `drain/memory/query_test.go` · **Size:** M

### FR1 File reader and tailer
**Acceptance:**
- `Read(path, filter)` returns every matching line, skips a malformed line, and counts skips in a `ParseErrors` value.
- `Tail(ctx, path, filter)` delivers a line appended after it started, survives a rotation, and closes its channel once `ctx` is done.
- Both reuse `memory.Filter`.
**Verify:** `go test -race -run 'TestFile_(Read|Tail)' ./drain/file`
**Deps:** MQ1 · **Files:** `drain/file/read.go`, `drain/file/tail.go`, `drain/file/read_test.go` · **Size:** M

### RF1 Redaction replacement function
**Acceptance:**
- `ReplaceFunc(fn)` masks with the function's result.
- A panicking function falls back to the fixed replacement string, with no raw value in the output.
- The G1 fuzz test covers the panic path.
**Verify:** `make fuzz && go test -race -run 'TestRedact_ReplaceFunc' ./redact`
**Deps:** none · **Files:** `redact/custom.go`, `redact/replace_test.go`, `redact/fuzz_test.go` · **Size:** S

### Checkpoint 7 — v1.2
- [ ] Every phase 7 criterion met. G1 through G5 still green. `make compat` passes
- [ ] The root module still imports nothing outside the standard library
- [ ] `docs/cost.md` ships, and README links it
- [ ] Human review
- [ ] **Ask first:** tag `v0.3.0` and its module tags

---

## Phase 8 — v1.3, CLI (closes gaps 6, 7, 8, 9, 10)

Spec: [SPEC-cli-v1.3.md](../docs/SPEC-cli-v1.3.md).

### MR1 Rule: error guidance
**Acceptance:** `error-guidance` fires on an `ErrorInfo` reaching the event with no `why` and no `fix`, and stays quiet on the clean twin fixture.
**Verify:** `cd cmd/wlog && go test -race -run 'TestRules_ErrorGuidance' ./...`
**Deps:** Checkpoint 7 · **Files:** `cmd/wlog/rules/error_guidance.go`, its test, `cmd/wlog/testdata/errguidance/` · **Size:** M

### MR2 Rule: swallowed error
**Acceptance:**
- `swallowed-error` walks the control flow graph and reports a path with a live error and no `wlog.Error`, no `Errorf`, and no return to a logging caller.
- It stays quiet on any path it cannot fully resolve, proven by an unresolvable fixture.
**Verify:** `cd cmd/wlog && go test -race -run 'TestRules_SwallowedError' ./...`
**Deps:** MR1 · **Files:** `cmd/wlog/rules/swallowed.go`, its test, two fixtures · **Size:** M

### MR3 Suggestions: catalog use and audit coverage
**Acceptance:**
- `use-catalog` stays quiet until a registry holds the literal code, and fires then.
- `audit-coverage` fires on a write-route handler that records no audit.
**Verify:** `cd cmd/wlog && go test -race -run 'TestRules_(UseCatalog|AuditCoverage)' ./...`
**Deps:** MR2 · **Files:** `cmd/wlog/rules/catalog.go`, `cmd/wlog/rules/audit_coverage.go`, tests, fixtures · **Size:** M

### MS1 Entry classes, grades, per-entry weighting
**Acceptance:**
- Each entry point classifies as read, write, or sensitive, from its HTTP method and route words.
- A sensitive entry scores lower than a read entry for the same miss.
- Grades A to F derive from the score. Gate G6 holds with classes and grades included.
**Verify:** `cd cmd/wlog && go test -race -run 'TestScore_|TestGolden' ./...`
**Deps:** MR3 · **Files:** `cmd/wlog/score/class.go`, `cmd/wlog/score/grade.go`, tests, golden files · **Size:** M

### MS2 Report forms and strict baseline
**Acceptance:**
- `--all`, `--entry <name>`, and `--json` each print deterministic bytes, proven by golden tests.
- `--strict` fails on a per-rule regression whose total score did not drop.
**Verify:** `cd cmd/wlog && go test -race -run 'TestReport_|TestGates' ./...`
**Deps:** MS1 · **Files:** `cmd/wlog/report/*.go`, `cmd/wlog/main.go`, tests, golden files · **Size:** M

### CI1 wlog init
**Acceptance:**
- `init` writes a compiling setup for each of the five frameworks. The test builds each generated tree.
- `--dry-run` writes nothing. An existing `wlog.go` makes it exit 1.
- Nothing is written until the whole plan succeeds.
**Verify:** `cd cmd/wlog && go test -race -run 'TestInit_' ./...`
**Deps:** MS2 · **Files:** `cmd/wlog/cmd/init/*.go`, tests, 5 fixture trees · **Size:** L

### CI2 wlog doctor
**Acceptance:**
- Seven checks run, each printing pass, warn, or fail.
- A fail appears for a missing middleware, a missing drain variable, and a disabled redactor.
- It passes on the repository's own `examples`, and exits 0 with warns and no fails.
- `--json` prints one object per check.
**Verify:** `cd cmd/wlog && go test -race -run 'TestDoctor_' ./...`
**Deps:** CI1 · **Files:** `cmd/wlog/cmd/doctor/*.go`, tests, fixtures · **Size:** M

### CI3 wlog agents
**Acceptance:**
- The `AGENTS.md` block is written between its fences, and a rerun replaces only that block.
- Three markdown skills are written with front matter, matching a golden file.
- Skill content is generated from the repository's docs.
**Verify:** `cd cmd/wlog && go test -race -run 'TestAgents_' ./...`
**Deps:** CI2 · **Files:** `cmd/wlog/cmd/agents/*.go`, 3 skill templates, tests, golden files · **Size:** M

### Checkpoint 8 — v1.3
- [ ] Gate G6 holds for every new printed form
- [ ] `wlog doctor` passes on `examples/`, and `wlog init` output builds for all 5 frameworks
- [ ] Human review
- [ ] **Ask first:** tag `v0.4.0`

---

## Phase 9 — v1.4, drains, example, docs (closes gaps 11, 13, 14)

Spec: [SPEC-drains-v1.4.md](../docs/SPEC-drains-v1.4.md).

### DP PostHog drain
**Acceptance:** Builds from `POSTHOG_API_KEY` alone. Posts a `/batch/` body with a flattened `properties` map. `distinct_id` comes from `user.id`, then `trace.request_id`. G1 leak test passes.
**Verify:** `go test -race ./drain/posthog`
**Deps:** Checkpoint 8 · **Files:** `drain/posthog/posthog.go`, its test · **Size:** S

### DB Better Stack drain
**Acceptance:** Builds from `BETTERSTACK_SOURCE_TOKEN` alone. Posts a JSON array with a Bearer header, `dt`, `level`, and `message` mapped. G1 leak test passes.
**Verify:** `go test -race ./drain/betterstack`
**Deps:** Checkpoint 8 · **Files:** `drain/betterstack/betterstack.go`, its test · **Size:** S

### DH HyperDX drain
**Acceptance:** Builds from `HYPERDX_API_KEY` alone. Reuses the `drain/otlp` encoder, and produces the same body apart from endpoint, header, and service attribute. G1 leak test passes.
**Verify:** `go test -race ./drain/hyperdx`
**Deps:** DP · **Files:** `drain/hyperdx/hyperdx.go`, its test, a small export from `drain/otlp` · **Size:** M

### EXL AWS Lambda example
**Acceptance:**
- The helper starts one event per invocation and sets `faas.request_id`, `faas.cold_start`, `faas.remaining_ms`, and the function name.
- It flushes before returning, proven by a drain that records its `Close` call.
- Its own module builds and tests.
**Verify:** `cd examples/lambda && go test -race ./...`
**Deps:** DH · **Files:** `examples/lambda/go.mod`, `main.go`, `handler.go`, `handler_test.go`, `go.work` · **Size:** M

### REL2 Best-practice guide and parity close-out
**Acceptance:**
- `docs/best-practices.md` covers the five topics, and every rule points at a test.
- `docs/evlog-parity.md` shows no open gap, or a documented decision for each one left.
- README links both.
**Verify:** Human review, plus `make map` still passing on `examples/`.
**Deps:** EXL · **Files:** `docs/best-practices.md`, `docs/evlog-parity.md`, `README.md` · **Size:** M

### Checkpoint 9 — v1.4
- [ ] Gates G1 through G6 green. Every drain has a leak test
- [ ] `docs/evlog-parity.md` lists no open gap
- [ ] Human review
- [ ] **Ask first:** tag `v1.0.0`, the first stable API promise

---

## Parallelization

| Can run in parallel | Must be sequential |
|---|---|
| R4/R5 alongside R2/R3 (both only need R1) | R1 → R2 → R3 and R6 → R7 → R8 |
| After C7: C8, C9, C10, C12 | C1 → C7 (shape of the event and stage order) |
| After Checkpoint 1B: P*, SA*, M*, E*, EH1, H* are independent tracks | H1 → H5 (conformance suite defines adapter contract) |
| After H5: HE4, HE5, HG, LS*, LZ, LZR, LL, TO | A1 → A3 (chain depends on record, journal on chain and pipeline) |
| Phase 4 drains (after P4) | MP1 → MP6 |
| Spec tasks S2.1 and S2.2 | Any task before its module spec is approved |

## Risks and Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| 50µs request budget missed with default "capture everything" | High | Benchmark at Checkpoint 2A, before adapters. If missed, move bodies to lazy capture or make costly items opt-in (spec change, ask first) |
| Redaction perf/false positives on real payloads | High | R8 benchmark + fuzz. Run against the boilerplate's `server.log` samples as a fixture. Tokenization instead of substring |
| Event shape churn after drains/CLI depend on it | High | Golden files + human review at Checkpoint 1B. Reserved keys are ask-first after that |
| Echo v5 API not stable/released as assumed | Med | Verify at S3. If unavailable, HE5 moves to "designed for" (ask first) |
| Go 1.23 floor vs herr's 1.26.1 in one workspace | Med | Only `errors/herr` declares 1.26.1. `make compat` builds root with `GOTOOLCHAIN=go1.23.0` outside the workspace (`GOWORK=off`) |
| Audit chain ordering under concurrency / restarts | Med | Single mutex at emit. Journal resumes from last line. G5 tamper fixtures |
| `cli-map` static analysis gives false results on real code | Med | Fixture apps per framework + dogfood on `examples/` and (later, read-only) on go-echo-boilerplate |
| Multi-module release tagging mistakes | Low | Checkpoint 5 is ask-first. Document `module/path/vX.Y.Z` tag convention in REL1 |
| Scope: ~70 tasks before v1 | Med | Phases ship independently useful slices. A cut v1 after Checkpoint 4 moves only `cli-map` |

## Open Questions

None. Resolved 2026-09-15:
1. SPEC.md (v3), SPEC-redact.md, CAPABILITIES.md and this plan are approved.
2. CI provider is GitHub Actions.
3. R8 can copy sanitized request samples from `go-echo-boilerplate/server.log` into `redact/testdata`. The boilerplate repo stays read-only.
