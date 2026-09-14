# Implementation Plan: wlog v1 + v1.1

> Source of truth: [SPEC.md](../SPEC.md), [CAPABILITIES.md](../CAPABILITIES.md), [SPEC-redact.md](../SPEC-redact.md).
> Checklist index: [todo.md](todo.md). Task ids are stable; `/build` refers to them.
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
- **Fixed per-event order** — keep/sample → enrich → redact → rename → sinks. Core owns the
  order (C7), so `sample`, `enrich`, `audit` and drains only implement interfaces.
- **Redactor is immutable + atomically swapped.** `redact` ships first with no dependency on core;
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
No task is L/XL. "Files" lists test files explicitly; `example_test.go` counts toward the limit.

---

## Phase 0 — Foundation

### T0.1 Repository scaffold
**Description:** Create the root module and tooling so every later task has working `make` targets.
**Acceptance:**
- `go.mod` = `module github.com/jeremygprawira/wlog`, `go 1.23`; `go.work` lists `.`.
- `Makefile` has `test race fuzz bench lint tidy cover compat map`; module list is derived from `go.work`, so new modules need no Makefile edit.
- MIT `LICENSE`, `.gitignore` (coverage/, bin/, *.test, fuzz cache).
**Verify:** `make test && make lint` succeed on the empty module; `make compat` runs the Go 1.23 toolchain.
**Deps:** None · **Files:** `go.mod`, `go.work`, `Makefile`, `LICENSE`, `.gitignore` · **Size:** M

### T0.2 Agent guidance + README stub
**Description:** Encode golden rules so every session follows the spec.
**Acceptance:**
- `CLAUDE.md`: read order (CAPABILITIES → SPEC → module spec → tasks), TDD rule, gates G1–G6, Always/Ask/Never boundaries, commands, and the Simple English writing rule for all prose.
- `README.md`: one-paragraph pitch, status "pre-v0", link to specs.
**Verify:** Manual review; every boundary in SPEC.md appears in CLAUDE.md.
**Deps:** T0.1 · **Files:** `CLAUDE.md`, `README.md` · **Size:** S

### T0.3 CI workflow
**Description:** Run the gates on every push/PR (the workflow file only; creating the GitHub remote is ask-first).
**Acceptance:**
- `.github/workflows/ci.yml` jobs: `test`, `race`, `lint`, `compat` (Go 1.23 + 1.26), `fuzz` (30s), `bench` (report only).
- Workspace-aware: runs targets for every module in `go.work`.
**Verify:** `act -j test` locally, or run each job's commands by hand and confirm they pass.
**Deps:** T0.1 · **Files:** `.github/workflows/ci.yml` · **Size:** S

### T0.4 Write SPEC-core.md
**Description:** Specify core's real API before any core code: reserved keys + default namespaced layout, `Start`/`Detach`/`Set`/`SetGroup`/`Append`/`SetLevel`/`Error`/`Audit` hook point, `Key[T]`, `StrictKeys`, interfaces (`Drain`, `DrainFunc`, `ErrorExtractor`, `ErrorInfo`, `Enricher`, `Keeper`, plugin hooks), stage order, field presets, caps + counters, env vars, level rules, sinks, `OnError`, budget.
**Acceptance:**
- All six spec areas + Success Criteria with executable examples; no open questions left unanswered.
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
**Description:** Thinnest working redactor: `New()` with default key denylist; `Apply` masks any matching key at any depth with `"[REDACTED]"`.
**Acceptance:**
- Tokenizer handles `_ - . space`, camelCase and acronyms (`HTTPAuthToken` → `http auth token`).
- Defaults mask `authHeader`, `X-Auth-Token`, `user_password`, `login_pin`; do not mask `author`, `concert`, `tokenizer_version`, `spin_count`.
- Nested maps and `[]any` walked; whole subtree replaced on match.
**Verify:** `go test -race -run 'TestRedact_Key|TestTokenize' ./redact`
**Deps:** T0.1 · **Files:** `redact/redact.go`, `redact/tokenize.go`, `redact/defaults.go`, `redact/redact_test.go`, `redact/tokenize_test.go` · **Size:** M

### R2 Paths, globs, arrays
**Description:** Dotted path entries and `*` globs per segment; array elements inherit parent path.
**Acceptance:**
- `http.request.headers.cookie` matches only from root; `user.*` masks every child of `user`.
- `*_pin`, `x-*-secret` glob within one segment.
- `items.card_number` masks every element's `card_number`.
**Verify:** `go test -race -run TestRedact_Path ./redact`
**Deps:** R1 · **Files:** `redact/match.go`, `redact/match_test.go` · **Size:** S

### R3 Add / remove / replace keys, With, introspection
**Description:** The user-facing denylist controls and derived redactors.
**Acceptance:**
- `AddKeys`, `RemoveKeys`, `ReplaceKeys` behave per spec; `RemoveKeys("session")` leaves `session_id` intact.
- `New`/`With` return errors (never panic) for invalid globs and unknown removals; `With` never mutates the parent.
- `Keys()` sorted; `Fingerprint()` is order-independent and changes after any add/remove.
**Verify:** `go test -race -run 'TestOptions_Keys|TestWith|TestFingerprint' ./redact`
**Deps:** R2 · **Files:** `redact/options.go`, `redact/fingerprint.go`, `redact/options_test.go` · **Size:** S

### R4 Built-in value patterns A: credit_card, email, jwt, bearer
**Description:** Partial masking for the four highest-value patterns.
**Acceptance:**
- Outputs exactly match the spec table (`****1111`, `a***@***.com`, `eyJ***.***`, `Bearer ***`).
- Credit cards are Luhn-validated; a 16-digit non-Luhn id is untouched.
- Patterns run only on values not already masked by key rules.
**Verify:** `go test -race -run TestBuiltin_A ./redact`
**Deps:** R1 · **Files:** `redact/patterns.go`, `redact/patterns_test.go` · **Size:** S

### R5 Built-in value patterns B: ipv4, phone, iban, nik
**Description:** Remaining built-ins, including the Indonesia-specific ones.
**Acceptance:**
- `ipv4` skips `127.0.0.1`/`0.0.0.0`; `http.client_ip` is exempt unless `MaskClientIP()`.
- `phone` masks `+62 812-3456-7890` and `081234567890` to `+62 ****7890` form; `iban` per table.
- `nik` is off by default and on with `EnablePatterns("nik")`.
**Verify:** `go test -race -run TestBuiltin_B ./redact`
**Deps:** R4 · **Files:** `redact/patterns_id.go`, `redact/patterns_b_test.go` · **Size:** S

### R6 Pattern options + custom patterns
**Description:** Add/reduce patterns and user-defined replacements.
**Acceptance:**
- `AddPatterns`, `RemovePatterns` (built-ins by name), `EnablePatterns`, `NoBuiltinPatterns`.
- Errors for invalid regex, duplicate name, unknown name.
- `Pattern.Replace` receives `Match{Path, Key, Value, Groups}`; a panic yields `"[REDACTED]"` and doesn't propagate.
**Verify:** `go test -race -run 'TestOptions_Patterns|TestCustomPattern' ./redact`
**Deps:** R3, R5 · **Files:** `redact/options.go`, `redact/custom.go`, `redact/custom_test.go` · **Size:** S

### R7 Transforms, limits, Default/Disabled
**Description:** Remaining behaviour options.
**Acceptance:**
- `Transform` runs before key rules; a panicking transform doesn't stop redaction of the rest.
- `MaxDepth` (default 16 → `"[REDACTED:DEPTH]"`), `MaxStringScan` (default 64KB → `"[REDACTED:TOO_LARGE]"`), `Replacement`.
- `Default()` equals `MustNew()`; `Disabled().Apply` is a no-op.
**Verify:** `go test -race -run 'TestTransform|TestLimits|TestDefaultDisabled' ./redact`
**Deps:** R6 · **Files:** `redact/options.go`, `redact/limits_test.go` · **Size:** S

### R8 Gates G1/G2, benchmark, examples
**Description:** Prove the security and performance invariants.
**Acceptance:**
- `FuzzRedact_NeverLeaks`: 30s clean; seeds cover every default key and built-in pattern.
- 64-goroutine `Apply` test passes `-race`.
- Benchmark (50 fields, 3 levels, 10 strings) recorded in `redact/BENCH.md`; target ≤ 30µs/op, ≤ 10 allocs/op, or a spec update with the measured number approved.
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
- `SetGroup` merges into one slot; `Append` builds arrays; structs normalized via JSON tags.
- Key cap, array cap and group cap enforced; overflow increments `wlog.dropped_fields` in the event.
- Concurrent writers from 32 goroutines pass `-race`.
**Verify:** `go test -race -run 'TestCore_Group|TestCore_Append|TestCore_Caps' ./`
**Deps:** C1 · **Files:** `event.go`, `normalize.go`, `event_test.go` · **Size:** S

### C3 Levels, SetLevel, outcome
**Description:** Level inference and override.
**Acceptance:**
- Default level rules from SPEC-core (errors → error, 4xx → warn, else info); `SetLevel` wins over inference.
- `outcome` is `success`/`error` per spec.
- Minimum level (`WithLevel`, `WLOG_LEVEL`) filters events.
**Verify:** `go test -race -run TestCore_Level ./`
**Deps:** C1 · **Files:** `level.go`, `level_test.go` · **Size:** S

### C4 Errors: extractor, error + errors[]
**Description:** Pluggable error capture.
**Acceptance:**
- `ErrorExtractor` interface + std fallback (message, cause chain, type).
- `ErrorInfo` fields code/message/kind/status/cause/stack/why/fix/link/attrs serialize under `error`.
- The last reported error is `error`; earlier ones go to `errors[]` (cap 10, overflow counted).
**Verify:** `go test -race -run TestCore_Error ./`
**Deps:** C3 · **Files:** `errors.go`, `errors_test.go` · **Size:** S

### C5 Detach + sealed events
**Description:** Background work support.
**Acceptance:**
- `wlog.Detach(ctx, name)` creates a child event with parent `trace.request_id`/`trace.trace_id` and `parent_operation`; emitted on its own `end()`.
- Writes to an emitted event are ignored and counted (`wlog.late_writes`).
- Parent emit racing with child writes passes `-race`.
**Verify:** `go test -race -run 'TestCore_Detach|TestCore_Sealed' ./`
**Deps:** C2 · **Files:** `detach.go`, `detach_test.go` · **Size:** S

### C6 Drains, OnError, Close (G3)
**Description:** Output extension point with failure isolation.
**Acceptance:**
- `Drain` interface + `DrainFunc`; `WithDrains` fans out a redacted snapshot to each.
- A panicking or erroring drain calls `OnError`, never affects other drains or the caller.
- `log.Close(ctx)` calls optional `Close` on drains, respecting the ctx deadline.
**Verify:** `go test -race -run 'TestCore_Drain|TestCore_Close' ./`
**Deps:** C1 · **Files:** `drain.go`, `drain_test.go` · **Size:** S

### C7 Stage order: keep/sample → enrich → redact → rename → sinks
**Description:** Lock the per-event pipeline order so later modules just plug in.
**Acceptance:**
- `Keeper`/sampler and `Enricher` interfaces with panic isolation.
- Order test: a dropped event never reaches an enricher; a field added by an enricher is redacted; renaming happens after redaction.
- Audit hook point reserved (events flagged `audit` bypass sampling).
**Verify:** `go test -race -run TestCore_StageOrder ./`
**Deps:** C4, C6 · **Files:** `stages.go`, `enrich.go`, `stages_test.go` · **Size:** M

### C8 Atomic redactor swap
**Description:** Runtime denylist changes (hybrid C).
**Acceptance:**
- `WithRedactor`, `log.SetRedactor(next)` via `atomic.Pointer`; nil → `redact.Default()`.
- `redact.fingerprint` present in every event (disable option).
- 1000 emits concurrent with 100 swaps: `-race` clean, every event fully redacted by exactly one config.
**Verify:** `go test -race -run TestCore_SetRedactor ./`
**Deps:** C7 · **Files:** `wlog.go`, `redactor_test.go` · **Size:** S

### C9 Field-name presets + renaming
**Description:** Customizable event shape.
**Acceptance:**
- Default namespaced layout matches SPEC-core golden file.
- `FieldsOTel()`, `FieldsFlat()` presets match their golden files; `WithFieldNames(map)` renames individual keys.
- Denylist entries still match canonical names after renaming.
**Verify:** `go test -race -run TestCore_Fields ./` (golden files under `testdata/`)
**Deps:** C7 · **Files:** `fields.go`, `fields_test.go`, `testdata/fields_*.golden.json` · **Size:** M

### C10 Plugins
**Description:** One struct, many hooks.
**Acceptance:**
- `WithPlugins`; detection of optional interfaces (`Setup`, `Enricher`, `Keeper`, `Drain`, `RequestStarter`, `RequestFinisher`); request hooks exposed for http-std.
- Each hook is panic-isolated and reports to `OnError` with the plugin name.
- A struct implementing three hooks is invoked at the right stages.
**Verify:** `go test -race -run TestCore_Plugin ./`
**Deps:** C7 · **Files:** `plugin.go`, `plugin_test.go` · **Size:** S

### C11 Typed keys + StrictKeys
**Description:** Compile-time key safety, opt-in.
**Acceptance:**
- `wlog.NewKey[T](name)`, `Key[T].Set(ctx, T)`; a compile-fail case is documented in a `go vet`-checked example (not a failing test).
- `StrictKeys(keys...)`: unregistered keys flagged in `wlog.unknown_keys` when env is local/dev; untouched in prod.
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
- Tree layout per SPEC-core (level, method/path/status/duration header; error why/fix/link block).
- Auto-selected when env is `local`/`dev` and `WLOG_FORMAT` is unset; JSON otherwise; `NO_COLOR` respected.
- Output is redacted (G1 sink test covers it).
**Verify:** `go test -race ./sink/...` (golden files)
**Deps:** C9 · **Files:** `sink/pretty.go`, `sink/pretty_test.go`, `sink/testdata/pretty_*.golden` · **Size:** S

### C14 Env configuration
**Description:** Env vars as defaults, code wins.
**Acceptance:**
- `WLOG_ENV`, `WLOG_LEVEL`, `WLOG_FORMAT`, `WLOG_SERVICE`, `WLOG_VERSION` read once in `New`.
- An explicit option overrides the matching env var; invalid env values → `OnError` + default, never panic.
- `internal/env` helper reusable by drains.
**Verify:** `go test -race -run TestCore_Env ./ ./internal/env`
**Deps:** C3 · **Files:** `config.go`, `internal/env/env.go`, `config_test.go` · **Size:** S

### C15 Core gates, benchmark, examples
**Description:** Close out core.
**Acceptance:**
- G1 sink test runs JSON + pretty sinks with fuzzed secrets; G2/G3/G4 tests exist for core paths.
- Benchmark Start→Set×10→emit (no drain) recorded; ≤ 20µs p50 target (half of the 50µs request budget).
- Example tests for every exported function; coverage ≥ 85%; `make compat` green.
**Verify:** `make race fuzz compat cover && go test -run=xxx -bench=. -benchmem ./`
**Deps:** C5, C8–C14 · **Files:** `gates_test.go`, `bench_test.go`, `example_test.go` · **Size:** S

### Checkpoint 1B
- [ ] SPEC-core success criteria met; root module still stdlib-only
- [ ] `make race fuzz compat` green
- [ ] Human review of the emitted event shape (golden files) — **last cheap moment to change it**

---

## Phase 2 — Pipeline, HTTP, error adapter, test tooling, audit

### S2.1 Specs: pipeline, sample, drain-memory, wlogtest
**Description:** Module specs for the delivery and test-tooling modules.
**Acceptance:** Four `SPEC-<id>.md` files with six areas + success criteria; approved.
**Verify:** Human approval recorded in CAPABILITIES.md.
**Deps:** Checkpoint 1B · **Files:** `SPEC-pipeline.md`, `SPEC-sample.md`, `SPEC-drain-memory.md`, `SPEC-wlogtest.md`, `CAPABILITIES.md` · **Size:** M

### S2.2 Specs: http-std, enrich, errors-herr, audit
**Description:** Module specs for request capture, enrichment, herr and audit.
**Acceptance:** Four specs approved; http-std spec defines the conformance suite contract; audit spec defines canonical JSON + hash format.
**Verify:** Human approval recorded in CAPABILITIES.md.
**Deps:** Checkpoint 1B · **Files:** `SPEC-http-std.md`, `SPEC-enrich.md`, `SPEC-errors-herr.md`, `SPEC-audit.md`, `CAPABILITIES.md` · **Size:** M

### H1 net/http middleware, thin slice (high risk → first)
**Description:** One event per request with core HTTP fields.
**Acceptance:**
- `wlogstd.Middleware(log)(handler)` sets `http.method/route/path/status/duration_ms/bytes_out`, `trace.request_id` (reuse `X-Request-ID` or generate; echoed on the response).
- Route uses Go 1.22+ `r.Pattern` when present; `WithRouteFunc` overrides.
- Handlers can call `wlog.Set(r.Context(), …)` and see it in the event.
**Verify:** `go test -race -run TestStd_Basic ./middleware/nethttp`
**Deps:** S2.2, C15 · **Files:** `middleware/nethttp/middleware.go`, `middleware/nethttp/writer.go`, `middleware/nethttp/middleware_test.go` · **Size:** S

### H2 Header, query, param, cookie capture + skip rules
**Description:** Default-everything metadata capture with toggles.
**Acceptance:**
- Headers, query, cookies captured by default; each toggleable globally and per route; `SkipPaths("/health")` emits nothing.
- `Authorization`/`Cookie` values are redacted in output (via core, no special-casing).
**Verify:** `go test -race -run TestStd_Capture ./middleware/nethttp`
**Deps:** H1 · **Files:** `middleware/nethttp/capture.go`, `middleware/nethttp/options.go`, `middleware/nethttp/capture_test.go` · **Size:** S

### H3 Body capture
**Description:** Request + response bodies, bounded.
**Acceptance:**
- Both bodies captured by default, 10KB cap each, JSON parsed else `raw` string; request body restored for the handler.
- Content-type filters and per-route disable; binary types skipped by default.
- Writer wrapper preserves `http.Flusher`, `http.Hijacker`, `io.ReaderFrom`; streaming responses don't buffer past cap (G4).
**Verify:** `go test -race -run TestStd_Body ./middleware/nethttp`
**Deps:** H2 · **Files:** `middleware/nethttp/body.go`, `middleware/nethttp/writer.go`, `middleware/nethttp/body_test.go` · **Size:** S

### H4 Panics, traceparent, user id, plugin request hooks
**Description:** Robustness and correlation.
**Acceptance:**
- Panic → 500 response, event has `error` with stack, level error; process keeps serving.
- W3C `traceparent` parsed into `trace.trace_id/span_id`; invalid header ignored.
- `WithUserFunc(func(*http.Request) string)`; `RequestStarter`/`RequestFinisher` plugin hooks invoked.
**Verify:** `go test -race -run 'TestStd_Panic|TestStd_Trace|TestStd_Hooks' ./middleware/nethttp`
**Deps:** H3, C10 · **Files:** `middleware/nethttp/recover.go`, `middleware/nethttp/trace.go`, `middleware/nethttp/hooks_test.go` · **Size:** S

### H5 Conformance suite + gorilla/mux proof
**Description:** The contract every HTTP adapter must pass.
**Acceptance:**
- `internal/conformance.Run(t, Adapter)` covers route, status, bodies, headers, panic, request id, traceparent, Set from handler, skip paths; compares normalized events.
- net/http adapter passes it; a mux example (in `examples/`, own module) passes it using `WithRouteFunc(mux.CurrentRoute…)`.
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
- Exhausted retries call `OnDropped(events, err)`; tests use a fake clock (no sleeps > 10ms).
**Verify:** `go test -race -run TestPipeline_Retry ./pipeline`
**Deps:** P1 · **Files:** `pipeline/retry.go`, `pipeline/retry_test.go` · **Size:** S

### P3 Bounded buffer, drop-oldest, fan-out isolation
**Description:** Overload behaviour (G3, G4).
**Acceptance:**
- `MaxBuffer` (default 1000); overflow drops oldest, calls `OnDropped`, increments `Dropped()`.
- A hanging drain never blocks emits (test: 10k emits against a drain that never returns finish < 1s).
- `pipeline.FanOut(d1, d2)`: one failing destination doesn't delay or fail the other.
**Verify:** `go test -race -run 'TestPipeline_Overflow|TestPipeline_FanOut' ./pipeline`
**Deps:** P2 · **Files:** `pipeline/buffer.go`, `pipeline/fanout.go`, `pipeline/overflow_test.go` · **Size:** S

### P4 Shared HTTP drain helper + identity headers
**Description:** Common code for every HTTP-based drain.
**Acceptance:**
- `internal/httpdrain`: POST with timeout, optional gzip, status classification (retryable 429/5xx vs permanent 4xx), `Retry-After` honoured.
- Sends `User-Agent: wlog/<version>` and `X-Wlog-Source: <name>`; overridable/disable-able.
- `internal/httpfake` test server records requests for drain tests.
**Verify:** `go test -race ./internal/httpdrain ./internal/httpfake`
**Deps:** P2 · **Files:** `internal/httpdrain/httpdrain.go`, `internal/httpdrain/httpdrain_test.go`, `internal/httpfake/httpfake.go`, `internal/version/version.go` · **Size:** M

### SA1 Head + tail sampling rules
**Description:** The sampler as a core `Keeper`.
**Acceptance:**
- Head: rate per level (0–100), unset levels 100, error 100 unless explicitly set.
- Tail keep: `Status(>=n)`, `Duration(>=d)`, `Path(glob)`, `Func(pred)`; OR logic; tail-kept events skip head sampling.
- Deterministic tests via injectable random source.
**Verify:** `go test -race -run TestSample ./sample`
**Deps:** S2.1, C7 · **Files:** `sample/sample.go`, `sample/rules.go`, `sample/sample_test.go` · **Size:** S

### SA2 Presets + defaults
**Description:** Ready-made rules, default keep 100%.
**Acceptance:**
- No sampler configured → every event kept.
- `sample.KeepErrorsAndSlow(slow, healthyRate)` preset; audit-flagged events always kept.
**Verify:** `go test -race -run TestSamplePresets ./sample && go test -race -run TestCore_StageOrder ./`
**Deps:** SA1 · **Files:** `sample/presets.go`, `sample/presets_test.go` · **Size:** S

### M1 Memory drain: ring buffer + subscribe
**Description:** In-process event store and live feed.
**Acceptance:**
- `memory.New(size)` (default 1000) ring buffer; `Snapshot()` copies; `Subscribe(ctx) <-chan Event` with a per-subscriber buffer; slow subscribers drop (counted), never block (G3/G4).
**Verify:** `go test -race ./drain/memory`
**Deps:** S2.1, C6 · **Files:** `drain/memory/memory.go`, `drain/memory/memory_test.go` · **Size:** S

### M2 SSE handler
**Description:** Watch events live over HTTP.
**Acceptance:**
- `SSEHandler()` streams events as `text/event-stream`; replays last N on connect; closes on client disconnect without leaking goroutines (goroutine count check).
**Verify:** `go test -race -run TestSSE ./drain/memory`
**Deps:** M1 · **Files:** `drain/memory/sse.go`, `drain/memory/sse_test.go` · **Size:** S

### W1 wlogtest recorder
**Description:** Assertions for users' tests.
**Acceptance:**
- `wlogtest.New(t, opts...)` returns a logger + recorder backed by the memory drain.
- Helpers: `Events()`, `Last()`, `RequireField(t, key, want)`, `RequireErrorCode(t, code)`, `RequireCount(t, n)`; failure messages show the event diff.
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
- Detects Chrome, Edge, Firefox, Safari, curl/Go-http-client, bots; iOS/Android/Windows/macOS/Linux; mobile/desktop/bot.
- Table test with ≥ 30 real UA strings; unknown → `raw` only.
**Verify:** `go test -race -run TestUserAgent ./enrich`
**Deps:** E1 · **Files:** `enrich/useragent.go`, `enrich/useragent_test.go` · **Size:** S

### E3 Geo + user-id enrichers
**Description:** CDN geo headers and user lookup.
**Acceptance:**
- `enrich.Geo()` maps Cloudflare, CloudFront, Vercel headers into `geo.country/region/city/lat/lon`; `GeoHeaders(map)` for custom CDNs.
- `enrich.User(func(ctx) (id string))`; panics isolated by core.
**Verify:** `go test -race -run 'TestGeo|TestUser' ./enrich`
**Deps:** E1 · **Files:** `enrich/geo.go`, `enrich/user.go`, `enrich/geo_test.go` · **Size:** S

### EH1 herr error extractor
**Description:** First non-std `ErrorExtractor`, in its own module.
**Acceptance:**
- `errors/herr/go.mod` requires herr v0.1.0 (`go 1.26.1`) and wlog via workspace.
- `wlogherr.Extractor()` maps herr `Record` → `ErrorInfo` (code, kind, status, internal → message, fields → attrs, cause, stack); non-herr errors fall back to std.
- herr public message is **not** copied into logs unless `WithPublicMessage()`.
**Verify:** `cd errors/herr && go test -race ./...`
**Deps:** S2.2, C4 · **Files:** `errors/herr/go.mod`, `errors/herr/herr.go`, `errors/herr/herr_test.go`, `go.work` · **Size:** S

### A1 Audit records
**Description:** `wlog.Audit` inside and outside a request.
**Acceptance:**
- `audit.Record{Actor{Type,ID,Email}, Action, Target{Type,ID}, Outcome, Reason}` sets reserved `audit` field on the current event, or emits a standalone audit event without one.
- Audit events bypass sampling (0% sampler test) — G5 part 1.
- Actor email is still redacted by default patterns.
**Verify:** `go test -race -run TestAudit_Record ./audit ./`
**Deps:** S2.2, SA2 · **Files:** `audit/audit.go`, `audit/audit_test.go`, `wlog.go` · **Size:** S

### A2 Hash chain
**Description:** Tamper-evidence at emit time.
**Acceptance:**
- After redaction, canonical JSON (sorted keys) of the audit event is hashed: `audit.hash = sha256(prev_hash || canonical)`, `audit.prev_hash` set; chain state is per logger under one mutex.
- 100 concurrent audit emits produce a single valid chain (`-race`).
**Verify:** `go test -race -run TestAudit_Chain ./audit`
**Deps:** A1, C8 · **Files:** `audit/chain.go`, `audit/canonical.go`, `audit/chain_test.go` · **Size:** S

### A3 Journal drain + Verify (G5)
**Description:** Append-only local proof.
**Acceptance:**
- `audit.Journal(path)` appends NDJSON with fsync per batch, `O_APPEND`, file mode 0600; resumes the chain from the last line on restart.
- `audit.Verify(path)` reports the first broken line; fixtures with an edited, reordered and deleted line all fail.
- Journal is fed through `pipeline` like any drain; the main drain receives the same event.
**Verify:** `go test -race -run 'TestAudit_Journal|TestAudit_Verify' ./audit`
**Deps:** A2, P3 · **Files:** `audit/journal.go`, `audit/verify.go`, `audit/journal_test.go`, `audit/testdata/tampered_*.ndjson` · **Size:** M

### Checkpoint 2B
- [ ] Every phase-2 module meets its spec; G1–G5 green
- [ ] Refund-handler scenario (SPEC.md criterion 11) passes as an integration test
- [ ] Human review

---

## Phase 3 — Framework and logger adapters, trace, examples

### S3 Specs: http-echo, http-echo5, http-gin, log-slog, log-zap/zerolog/logrus, trace-otel
**Description:** Short adapter specs (they inherit http-std / core contracts). Confirm Echo v5's released API surface at spec time.
**Acceptance:** Specs approved; each names the exact upstream versions pinned.
**Verify:** Human approval recorded in CAPABILITIES.md.
**Deps:** Checkpoint 2B · **Files:** `SPEC-http-adapters.md`, `SPEC-log-adapters.md`, `SPEC-trace-otel.md`, `CAPABILITIES.md` · **Size:** S

### HE4 Echo v4 adapter
**Acceptance:**
- `wlogecho.Middleware(log)` wraps http-std; route from `c.Path()`; errors returned from handlers reach `wlog.Error` and still flow to Echo's `HTTPErrorHandler`.
- Passes `internal/conformance`.
**Verify:** `cd middleware/echo && go test -race ./...`
**Deps:** S3, H5 · **Files:** `middleware/echo/go.mod`, `middleware/echo/echo.go`, `middleware/echo/echo_test.go`, `go.work` · **Size:** S

### HE5 Echo v5 adapter
**Acceptance:** Same as HE4 against `github.com/labstack/echo/v5`; passes conformance.
**Verify:** `cd middleware/echo5 && go test -race ./...`
**Deps:** S3, H5 · **Files:** `middleware/echo5/go.mod`, `middleware/echo5/echo.go`, `middleware/echo5/echo_test.go`, `go.work` · **Size:** S

### HG Gin adapter
**Acceptance:**
- `wloggin.Middleware(log)`; route from `c.FullPath()`; `c.Errors` reported via `wlog.Error`; body capture works with Gin's writer.
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
- `wlogslog.Handler(next slog.Handler)`: records with a ctx carrying an event are appended to `logs[]` (level, msg, attrs; cap per SPEC-core); others pass to `next`.
- `slog.SetDefault(slog.New(wlogslog.Handler(...)))` works with third-party libraries logging via slog.
**Verify:** `go test -race -run TestSlogInput ./log/slog`
**Deps:** LS1 · **Files:** `log/slog/input.go`, `log/slog/input_test.go` · **Size:** S

### LZ zap output
**Acceptance:** `wlogzap.Drain(*zap.Logger)`; nested fields via `zap.Object`/`zap.Any`; level mapped.
**Verify:** `cd log/zap && go test -race ./...`
**Deps:** S3, C6 · **Files:** `log/zap/go.mod`, `log/zap/zap.go`, `log/zap/zap_test.go`, `go.work` · **Size:** S

### LZR zerolog output
**Acceptance:** `wlogzerolog.Drain(zerolog.Logger)`; nested dicts; level mapped.
**Verify:** `cd log/zerolog && go test -race ./...`
**Deps:** S3, C6 · **Files:** `log/zerolog/go.mod`, `log/zerolog/zerolog.go`, `log/zerolog/zerolog_test.go`, `go.work` · **Size:** S

### LL logrus output
**Acceptance:** `wloglogrus.Drain(*logrus.Logger)`; fields via `WithFields`; level mapped.
**Verify:** `cd log/logrus && go test -race ./...`
**Deps:** S3, C6 · **Files:** `log/logrus/go.mod`, `log/logrus/logrus.go`, `log/logrus/logrus_test.go`, `go.work` · **Size:** S

### TO OpenTelemetry span link
**Acceptance:** `wlogotel.Enricher()` sets `trace.trace_id/span_id` from an active span in ctx; overrides traceparent-derived values only when valid.
**Verify:** `cd trace/otel && go test -race ./...`
**Deps:** S3, C7 · **Files:** `trace/otel/go.mod`, `trace/otel/otel.go`, `trace/otel/otel_test.go`, `go.work` · **Size:** S

### EX1 Example apps (HTTP)
**Acceptance:**
- `examples/` apps: `nethttp`, `echo`, `echo5`, `gin` (mux exists from H5), each ≤ 5 lines of wlog setup (SPEC.md criterion 2), each with a test asserting identical core fields.
**Verify:** `cd examples && go test -race ./...`
**Deps:** HE4, HE5, HG · **Files:** `examples/nethttp/main.go`, `examples/echo/main.go`, `examples/echo5/main.go`, `examples/gin/main.go`, `examples/http_parity_test.go` · **Size:** M

### EX2 Example apps (extension points)
**Acceptance:**
- `detach`, `custom-drain`, `custom-extractor`, `plugin`, `audit-refund`, `typed-keys`, `logger-output` examples; each has a test; README table links them.
**Verify:** `cd examples && go test -race ./...`
**Deps:** EX1, A3, LS2, LZ · **Files:** 5 small example dirs (`main.go` + `main_test.go` each, split across two commits if > 5 files) · **Size:** M

### Checkpoint 3
- [ ] Five HTTP stacks produce identical core fields (criterion 2); logger swap is one option (criterion 4)
- [ ] `make race compat` green across all modules
- [ ] Human review

---

## Phase 4 — v1 drains

### S4 Specs: axiom, loki, file, webhook, otlp
**Acceptance:** Wire formats, env vars, Loki label set (default `service`, `env`, `level`), file rotation policy and OTLP JSON attribute mapping specified and approved.
**Verify:** Human approval recorded in CAPABILITIES.md.
**Deps:** Checkpoint 3 · **Files:** `SPEC-drains-v1.md`, `CAPABILITIES.md` · **Size:** S

### DA Axiom drain
**Acceptance:** `axiom.New(opts)` posts NDJSON batches to the ingest API; `AXIOM_TOKEN`, `AXIOM_DATASET`, `AXIOM_URL` env; 401/403 non-retryable; verified against `httpfake`.
**Verify:** `go test -race ./drain/axiom`
**Deps:** S4, P4 · **Files:** `drain/axiom/axiom.go`, `drain/axiom/axiom_test.go` · **Size:** S

### DL Loki drain
**Acceptance:** Push API JSON streams grouped by label set; labels configurable, high-cardinality keys rejected at construction; `LOKI_URL`, `LOKI_USERNAME`, `LOKI_PASSWORD`, `LOKI_TENANT_ID`; nanosecond timestamps.
**Verify:** `go test -race ./drain/loki`
**Deps:** S4, P4 · **Files:** `drain/loki/loki.go`, `drain/loki/loki_test.go` · **Size:** S

### DF File drain
**Acceptance:** NDJSON append; rotation by size and age with max backups; mode 0600; safe under concurrent batches; `WLOG_FILE_PATH`.
**Verify:** `go test -race ./drain/file`
**Deps:** S4, P1 · **Files:** `drain/file/file.go`, `drain/file/rotate.go`, `drain/file/file_test.go` · **Size:** S

### DW Webhook drain
**Acceptance:** POST JSON array (or NDJSON option) to any URL with custom headers and optional HMAC signature header; `WLOG_WEBHOOK_URL`.
**Verify:** `go test -race ./drain/webhook`
**Deps:** S4, P4 · **Files:** `drain/webhook/webhook.go`, `drain/webhook/webhook_test.go` · **Size:** S

### DO OTLP drain
**Acceptance:** OTLP/HTTP JSON `ExportLogsServiceRequest`; resource attrs from `service.*`; body/attributes mapping per spec; severity number mapping; `OTEL_EXPORTER_OTLP_ENDPOINT`/`_HEADERS` honoured; payload validated against a golden file captured from a real collector.
**Verify:** `go test -race ./drain/otlp`
**Deps:** S4, P4 · **Files:** `drain/otlp/otlp.go`, `drain/otlp/mapping.go`, `drain/otlp/otlp_test.go`, `drain/otlp/testdata/export.golden.json` · **Size:** M

### DI Optional docker integration tests
**Acceptance:** `//go:build integration` tests + `docker-compose.integration.yml` for Loki and an OTel collector; `make integration` target; not part of default `make test`.
**Verify:** `make integration` locally with docker running.
**Deps:** DL, DO · **Files:** `docker-compose.integration.yml`, `drain/loki/integration_test.go`, `drain/otlp/integration_test.go`, `Makefile` · **Size:** S

### Checkpoint 4
- [ ] Every v1 drain works from env vars alone (criterion 5); G1 drain test covers all of them
- [ ] Human review

---

## Phase 5 — `cli-map`, then v1 release

### S5 Spec: cli-map
**Acceptance:** Rule ids + weights, entry-point detection per framework, sensitive-route patterns + config file, score formula, `wlog.map.json` schema, baseline comparison semantics; approved.
**Verify:** Human approval recorded in CAPABILITIES.md.
**Deps:** Checkpoint 4 · **Files:** `SPEC-cli-map.md`, `CAPABILITIES.md` · **Size:** S

### MP1 CLI skeleton + net/http/mux entry points
**Acceptance:** `cmd/wlog` module (`golang.org/x/tools`); `wlog map ./...` loads packages and lists net/http + mux handlers found in fixture apps under `testdata/`.
**Verify:** `cd cmd/wlog && go test -race ./...`
**Deps:** S5, EX1 · **Files:** `cmd/wlog/go.mod`, `cmd/wlog/main.go`, `cmd/wlog/entry/nethttp.go`, `cmd/wlog/entry/entry_test.go`, `go.work` · **Size:** M

### MP2 Echo v4/v5 + Gin entry points
**Acceptance:** Handlers registered via Echo v4, v5 and Gin routers detected in fixtures, with route strings.
**Verify:** `cd cmd/wlog && go test -race ./entry/...`
**Deps:** MP1 · **Files:** `cmd/wlog/entry/echo.go`, `cmd/wlog/entry/gin.go`, `cmd/wlog/entry/frameworks_test.go` · **Size:** S

### MP3 Rules: middleware coverage, context set, errors reach wlog
**Acceptance:** Three rules report per-handler pass/fail with positions; fixtures for pass and fail of each.
**Verify:** `cd cmd/wlog && go test -race ./rules/...`
**Deps:** MP2 · **Files:** `cmd/wlog/rules/coverage.go`, `cmd/wlog/rules/context.go`, `cmd/wlog/rules/errors.go`, `cmd/wlog/rules/rules_a_test.go` · **Size:** M

### MP4 Rules: sensitive-route audit, no print logging, no denylisted keys
**Acceptance:** Sensitive routes (default patterns + `wlog.map.yaml`/JSON config) get 2× weight and require `Audit`; `fmt.Print*`/`log.Print*` in handlers flagged; `Set` with a literal key matching `redact.Default()` flagged.
**Verify:** `cd cmd/wlog && go test -race ./rules/...`
**Deps:** MP3 · **Files:** `cmd/wlog/rules/audit.go`, `cmd/wlog/rules/print.go`, `cmd/wlog/rules/keys.go`, `cmd/wlog/rules/rules_b_test.go` · **Size:** M

### MP5 Score, report, CI gates (G6)
**Acceptance:** Deterministic 0–100 score; top-3 fixes; `wlog.map.json` (sorted, no timestamps); `--min-score n` and `--baseline file` set non-zero exit; golden-file tests prove byte-identical output across runs.
**Verify:** `cd cmd/wlog && go test -race -run 'TestScore|TestReport|TestGolden' ./...`
**Deps:** MP4 · **Files:** `cmd/wlog/score/score.go`, `cmd/wlog/report/report.go`, `cmd/wlog/score/score_test.go`, `cmd/wlog/testdata/golden/*.json` · **Size:** M

### MP6 go vet analyzer + dogfooding
**Acceptance:** `cmd/wlog/analyzer` exports an `*analysis.Analyzer` runnable via `go vet -vettool` and documented for golangci-lint; `make map` passes with `--min-score 80` on `examples/`.
**Verify:** `make map && go vet -vettool=$(go env GOPATH)/bin/wlogvet ./examples/...`
**Deps:** MP5, EX2 · **Files:** `cmd/wlog/analyzer/analyzer.go`, `cmd/wlog/cmd/wlogvet/main.go`, `cmd/wlog/analyzer/analyzer_test.go` · **Size:** S

### REL1 v1 documentation + parity audit
**Acceptance:**
- README: quick start, every module with install line, customization guide, event shape, gates.
- Per-module `doc.go` package docs; evlog parity table (criterion 15) with a module or "designed for" entry for every non-TypeScript-specific evlog doc page.
- All 15 SPEC.md success criteria ticked with evidence links (test names).
**Verify:** Manual walkthrough of README quick start in a fresh module; `go doc` renders every package.
**Deps:** MP6, Checkpoint 4 · **Files:** `README.md`, `docs/customization.md`, `docs/evlog-parity.md`, `docs/event-shape.md` · **Size:** M

### Checkpoint 5 — v1 release gate
- [ ] SPEC.md success criteria 1–9 and 11–15 met; criterion 10 met for the v1 drains
- [ ] `make test race fuzz lint compat map` green across every module
- [ ] **Ask first:** create GitHub remote, push, tag `v0.1.0` (root + each sub-module path tag)

---

## Phase 6 — v1.1 drains

### S6 Specs: sentry, clickhouse, datadog
**Acceptance:** Sentry envelope format + grouping by error code; ClickHouse DDL helper + JSONEachRow mapping; Datadog intake format + site selection; approved.
**Verify:** Human approval recorded in CAPABILITIES.md.
**Deps:** Checkpoint 5 · **Files:** `SPEC-drains-v1.1.md`, `CAPABILITIES.md` · **Size:** S

### DS Sentry drain
**Acceptance:** Error events → envelope API issues, fingerprint = error code (fallback type), wide event attached as context; `AllEvents()` opt-in sends to Sentry Logs; `SENTRY_DSN`.
**Verify:** `go test -race ./drain/sentry`
**Deps:** S6, P4 · **Files:** `drain/sentry/sentry.go`, `drain/sentry/envelope.go`, `drain/sentry/sentry_test.go` · **Size:** S

### DC ClickHouse drain
**Acceptance:** HTTP interface `INSERT … FORMAT JSONEachRow`; `clickhouse.DDL(table)` returns the recommended schema; never auto-creates; `CLICKHOUSE_URL/USER/PASSWORD/DATABASE/TABLE`.
**Verify:** `go test -race ./drain/clickhouse`
**Deps:** S6, P4 · **Files:** `drain/clickhouse/clickhouse.go`, `drain/clickhouse/ddl.go`, `drain/clickhouse/clickhouse_test.go` · **Size:** S

### DD Datadog drain
**Acceptance:** Logs intake v2 JSON array; `DD_API_KEY`, `DD_SITE`; `ddsource=wlog`, `service`, `ddtags` from env/service fields; 413 splits the batch.
**Verify:** `go test -race ./drain/datadog`
**Deps:** S6, P4 · **Files:** `drain/datadog/datadog.go`, `drain/datadog/datadog_test.go` · **Size:** S

### Checkpoint 6 — v1.1
- [ ] Criterion 10 fully met; ClickHouse integration test added to `make integration`
- [ ] **Ask first:** tag `v0.2.0`

---

## Parallelization

| Can run in parallel | Must be sequential |
|---|---|
| R4/R5 alongside R2/R3 (both only need R1) | R1 → R2 → R3 and R6 → R7 → R8 |
| After C7: C8, C9, C10, C12 | C1 → C7 (shape of the event and stage order) |
| After Checkpoint 1B: P*, SA*, M*, E*, EH1, H* are independent tracks | H1 → H5 (conformance suite defines adapter contract) |
| After H5: HE4, HE5, HG, LS*, LZ, LZR, LL, TO | A1 → A3 (chain depends on record; journal on chain + pipeline) |
| Phase 4 drains (after P4) | MP1 → MP6 |
| Spec tasks S2.1 and S2.2 | Any task before its module spec is approved |

## Risks and Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| 50µs request budget missed with default "capture everything" | High | Benchmark at Checkpoint 2A, before adapters; if missed, move bodies to lazy capture or make costly items opt-in (spec change, ask first) |
| Redaction perf/false positives on real payloads | High | R8 benchmark + fuzz; run against the boilerplate's `server.log` samples as a fixture; tokenization instead of substring |
| Event shape churn after drains/CLI depend on it | High | Golden files + human review at Checkpoint 1B; reserved keys are ask-first after that |
| Echo v5 API not stable/released as assumed | Med | Verify at S3; if unavailable, HE5 moves to "designed for" (ask first) |
| Go 1.23 floor vs herr's 1.26.1 in one workspace | Med | Only `errors/herr` declares 1.26.1; `make compat` builds root with `GOTOOLCHAIN=go1.23.0` outside the workspace (`GOWORK=off`) |
| Audit chain ordering under concurrency / restarts | Med | Single mutex at emit; journal resumes from last line; G5 tamper fixtures |
| `cli-map` static analysis gives false results on real code | Med | Fixture apps per framework + dogfood on `examples/` and (later, read-only) on go-echo-boilerplate |
| Multi-module release tagging mistakes | Low | Checkpoint 5 is ask-first; document `module/path/vX.Y.Z` tag convention in REL1 |
| Scope: ~70 tasks before v1 | Med | Phases ship independently useful slices; v1 can be cut after Checkpoint 4 if needed (only `cli-map` moves) |

## Open Questions

None. Resolved 2026-09-15:
1. SPEC.md (v3), SPEC-redact.md, CAPABILITIES.md and this plan are approved.
2. CI provider is GitHub Actions.
3. R8 can copy sanitized request samples from `go-echo-boilerplate/server.log` into `redact/testdata`. The boilerplate repo stays read-only.
