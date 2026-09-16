# Implementation Plan: wlog v0.5 to v1.0

> Source of truth: [SPEC.md](../docs/SPEC.md) v4, the approved map in
> [CAPABILITIES.md](../docs/CAPABILITIES.md) ("v0.5 to v1.0 modules"), and the module specs named
> in each phase. Findings: [the gap audit](audit-2026-09-16.md). Task list: [todo.md](todo.md).
> The v1 to v1.4 plan lives in [archive/plan-v1.md](archive/plan-v1.md). Its pending
> tag approvals (v0.2.0 to v1.0.0) are replaced by the tags in this plan.

## Overview

Six phases take wlog from the audited v0.1.0 state to a v1.0.0 API freeze. Phase 10 makes the
build honest and fixes the audit findings that later rewrites do not replace. Phase 11 ships event
shape v2 and the five foundations every adapter shares. Phases 12 to 14 add integrations in
parallel tracks. Phase 15 freezes the API.

Phases 10 and 11 use the full task form below. Phases 12 to 14 use a short form, because each
module spec holds the rules and numbered criteria. Library and vendor facts come from
[the research reports](research/README.md).

Every task clears the Definition of Done. The named tests fail before the change and pass after
it. `-race` is clean, and no other test regresses. The spec says what the code does.

## Task format

Each task has an id, a description, acceptance criteria, a `Verify` command, dependencies, files,
and a size. Ids read `<phase>-<area>-<n>`, such as `10-CORE-3`. A regression test for an audit id
is named `Test<Module>_<AuditID>_<Behavior>`, so the `Verify` pattern finds it. `tools verifyplan`
fails a `Verify` command that runs zero tests.

Sizes: XS is 1 file, S is 1 to 2, M is 3 to 5, L is 6 to 8. A task larger than M is split.

## Architecture decisions that set the order

- `repo-ci` goes first, so every later task runs on a CI that tells the truth.
- Phase 10 skips each finding that a phase 11 rewrite replaces, so no code is fixed twice.
- `core-shape` comes before every other foundation, because the event shape is the contract
  they all emit.
- `conformance` lands before any new adapter. If an adapter's suite fails, the adapter is not done.
- Tracks inside a phase share no files, so separate sessions can build them at the same time.
- Each phase ends with a review point and an ask-first tag.

---

## Phase 10, v0.5: honest build and safety fixes

Specs: [SPEC-repo-ci.md](../docs/SPEC-repo-ci.md), [SPEC-hardening.md](../docs/SPEC-hardening.md).

### Track: repo-ci (sequential, first)

#### 10-CI-1 tools module with `modules` and `affected`
**Acceptance:**
- `tools/go.mod` exists, and the root module does not depend on it.
- `go run ./tools/cmd/modules` prints every `go.work` module with its path, Go floor, and dependents.
- `go run ./tools/cmd/affected -base HEAD~1` lists touched modules plus their dependents.

**Verify:** `cd tools && go test -race -run 'TestModules_|TestAffected_' ./...`.
**Deps:** none. **Size:** M.
**Files:**

- `tools/go.mod`
- `tools/cmd/modules/main.go`
- `tools/cmd/affected/main.go`
- `tools/internal/workspace/workspace.go`
- `tools/internal/workspace/workspace_test.go`

#### 10-CI-2 `requires` and `tidy -check`, then fix every sub-module go.mod
**Acceptance:**
- Both commands fail on the current tree and name each module from REL-2.
- Each sub-module requires its siblings at the release version in `tools/version.txt`, with a relative `replace`, and no `// indirect` on a direct import.
- `GOWORK=off go build ./...` passes in every module.

**Verify:** `go test -race -run 'TestRequires_|TestTidy_' ./tools/... && go run ./tools/cmd/requires && go run ./tools/cmd/tidy -check`.
**Deps:** 10-CI-1. **Size:** M. **Closes:** REL-2, CLI-2, CAT-10.
**Files:**

- `tools/cmd/requires/main.go`
- `tools/cmd/tidy/main.go`
- their tests
- all sub-module `go.mod` files (one mechanical commit)

#### 10-CI-3 Go floor per module
**Acceptance:**
- Root `go.mod` says `go 1.21`. `middleware/nethttp/pattern_go123.go` reads `r.Pattern` behind `//go:build go1.23`, with a pre-1.23 fallback file.
- Each sub-module's `go` line is the lowest its code and dependencies allow.
- `tools floor` passes every module at its floor, and a fixture using a newer Go feature makes it fail. Module directories given as arguments limit the run to those modules.

**Verify:** `go test -race -run 'TestFloor_' ./tools/... && go run ./tools/cmd/floor`.
**Deps:** 10-CI-2. **Size:** M. **Closes:** REL-3.
**Files:**

- `go.mod`
- `middleware/nethttp/pattern_go123.go`
- `middleware/nethttp/pattern_old.go`
- `tools/cmd/floor/main.go`
- its test

#### 10-CI-4 CI workflow rewrite
**Acceptance:**
- Jobs from SPEC-repo-ci exist: `lint`, `test`, `floor`, `tidy`, `snippets`, `cover`, `fuzz`, `map`, plus nightly jobs.
- The lint job uses golangci-lint v2 built for the newest Go, and passes.
- The `map` job runs `make map` and `go vet -vettool` with `wlogvet`.

**Verify:** a branch push shows every required job green in `gh run view`. `tools verifyplan` passes on this task.
**Deps:** 10-CI-3. **Size:** M. **Closes:** REL-1, REL-4, CLI-18.
**Files:**

- `.github/workflows/ci.yml`
- `.github/workflows/nightly.yml`
- `Makefile`
- `.golangci.yml`

#### 10-CI-5 `snippets`, `ste`, and `verifyplan`
**Acceptance:**
- `tools snippets` compiles every Go block in README, docs, examples, and skill templates. A `snippets-known-bad.txt` list holds today's failures, and later docs tasks empty it.
- `tools ste` ports the Simple English lint to Go and reproduces the Python linter's result on 5 fixture files. A `ste-known-bad.txt` list holds the older specs that fail today: SPEC-drains-v1.1, SPEC-enrich, SPEC-http-std, SPEC-llm, SPEC-pipeline, SPEC-sample, SPEC-v1.2-additions, and SPEC-wlogtest.
- `tools verifyplan` fails on a `Verify` command that runs zero tests.

**Verify:** `go test -race -run 'TestSnippets_|TestSte_|TestVerifyPlan_' ./tools/...`.
**Deps:** 10-CI-1. **Size:** M. **Closes:** DOC-1 (tool part), DOC-7, REL-6.
**Files:**

- `tools/cmd/snippets/main.go`
- `tools/cmd/ste/main.go`
- `tools/cmd/verifyplan/main.go`
- `tools/internal/ste/ste.go`
- tests

#### 10-CI-6 `cover`, `bench`, `vuln`, all fuzz targets
**Acceptance:**
- `tools cover -min 85` reports each root package, and a list of packages below 85 is recorded for later tasks.
- `tools bench` compares against `bench/baseline.txt` with benchstat, and fails on a 25% slowdown fixture.
- `make fuzz` runs every `Fuzz` target, and `testdata/fuzz/` is tracked in git.

**Verify:** `go test -race -run 'TestCover_|TestBench_' ./tools/... && make fuzz FUZZTIME=5s`.
**Deps:** 10-CI-1. **Size:** M. **Closes:** REL-4 (gates), REL-5 (gate), RED-11 (CI part).
**Files:**

- `tools/cmd/cover/main.go`
- `tools/cmd/bench/main.go`
- `Makefile`
- `.gitignore`
- `bench/baseline.txt`

#### 10-CI-7 Release hygiene
**Acceptance:**
- `internal/version.Version` comes from ldflags with `"dev"` as fallback, and no test compares a version literal.
- `examples/mux/mux` is untracked, and `.gitignore` covers module binaries.
- `CHANGELOG.md`, `SECURITY.md`, and `CONTRIBUTING.md` exist. `tools release -dry-run` prints the tag order and apidiff results.

**Verify:** `go test -race -run 'TestRelease_' ./tools/... && go test -race ./internal/... ./internal/httpdrain`.
**Deps:** 10-CI-2. **Size:** M. **Closes:** REL-7, REL-8, PIPE-23.
**Files:**

- `internal/version/version.go`
- `tools/cmd/release/main.go`
- `CHANGELOG.md`
- `SECURITY.md`
- `CONTRIBUTING.md`

#### Review point 10-CI
- [ ] Every required CI job is green on `main`.
- [ ] `GOWORK=off` builds pass in every module at its floor.
- [ ] Human review.

### Track: core (after 10-CI-4)

#### 10-CORE-1 Value copy tree
**Acceptance:**
- Write calls copy values into the owned tree per SPEC-hardening core rules 1 to 4, before taking the event lock.
- Tests: `TestCore_CORE4_CallerMapUnchanged`, `TestCore_CORE6_Int64Exact`, `TestCore_CORE5_NaNIsString`, `TestCore_CORE3_JSONDashNeverLeaks`, `TestCore_CORE26_ErrorAndDuration`, `TestCore_CORE2_NestedMapStringStringRedacted`.
- A `MarshalJSON` that logs through wlog on the same event finishes.

**Verify:** `go test -race -run 'TestCore_CORE(2|3|4|5|6|26)_' .`.
**Deps:** 10-CI-4. **Size:** M. **Closes:** CORE-2, CORE-3, CORE-4, CORE-5, CORE-6, CORE-26.
**Files:**

- `value.go`
- `value_test.go`
- `event.go`

#### 10-CORE-2 Event-shape fuzz test
**Acceptance:**
- `FuzzCore_EventShapeNeverLeaks` fuzzes key names, nesting, value types, plain lines, and enricher values against every sink and a recorder drain.
- Run against the commit before 10-CORE-1, it finds a leak within 60 seconds. After 10-CORE-3, it runs 10 minutes clean.

**Verify:** `go test -run=xxx -fuzz=FuzzCore_EventShapeNeverLeaks -fuzztime=60s .`.
**Deps:** 10-CORE-1. **Size:** S. **Closes:** RED-11 (test part).
**Files:**

- `gates_test.go`
- `testdata/fuzz/FuzzCore_EventShapeNeverLeaks/`

#### 10-CORE-3 Plain lines, enricher values, drain contract
**Acceptance:**
- `Info`, `Warn`, `Debug`, and `AppendLog` copy key-value pairs. After enrich, core copies values an enricher added.
- The `Drain` doc says a drain must not change the map, and core never changes it after the drain stage.
- Tests: `TestCore_CORE1_PlainLineStructRedacted`, `TestCore_CORE2_EnricherStructRedacted`, `TestCore_CORE11_CoreNeverMutatesAfterDrains`.

**Verify:** `go test -race -run 'TestCore_CORE(1|2|11)_' .`.
**Deps:** 10-CORE-1. **Size:** M. **Closes:** CORE-1, CORE-11 (core part).
**Files:**

- `plain.go`
- `stages.go`
- `drain.go`
- `plain_test.go`
- `stages_test.go`

#### 10-CORE-4 Hook isolation
**Acceptance:**
- A panicking extractor, a typed-nil error, and a panicking `OnError` never reach the caller.
- Tests: `TestCore_CORE7_ExtractorPanicIsolated`, `TestCore_CORE7_TypedNilError`, `TestCore_CORE8_OnErrorPanicIsolated`.

**Verify:** `go test -race -run 'TestCore_CORE(7|8)_' .`.
**Deps:** 10-CI-4. **Size:** S. **Closes:** CORE-7, CORE-8.
**Files:**

- `errors.go`
- `drain.go`
- `errors_test.go`

#### 10-CORE-5 Audit level bypass, Detach context
**Acceptance:**
- An audit event survives `WithLevel(LevelError)` and `WLOG_LEVEL=error`.
- A Detach child's context is not canceled with the parent, keeps parent values, and applies StrictKeys.
- Tests: `TestCore_CORE9_AuditSkipsLevelFilter`, `TestCore_CORE10_DetachSurvivesParentCancel`, `TestCore_CORE28_DetachAppliesStrictKeys`.

**Verify:** `go test -race -run 'TestCore_CORE(9|10|28)_' .`.
**Deps:** 10-CI-4. **Size:** S. **Closes:** CORE-9, CORE-10, CORE-28.
**Files:**

- `event.go`
- `detach.go`
- `detach_test.go`
- `level_test.go`

#### 10-CORE-6 Option validation and keepers
**Acceptance:**
- Bad levels are rejected and reported. Empty `WithService` arguments keep env values. `WithRedactor(nil)` stores `Default()` once.
- All keepers combine with OR, and `Plugins()` returns a copy.
- Tests: `TestCore_CORE29_ServiceKeepsEnv`, `TestCore_CORE30_InvalidLevelRejected`, `TestCore_CORE31_KeepersCombineOr`, `TestCore_CORE33_NilRedactorBuiltOnce`.

**Verify:** `go test -race -run 'TestCore_CORE(29|30|31|33)_' .`.
**Deps:** 10-CI-4. **Size:** M. **Closes:** CORE-29, CORE-30, CORE-31, CORE-33.
**Files:**

- `wlog.go`
- `level.go`
- `plugin.go`
- `redactor.go`
- `config_test.go`

#### 10-CORE-7 Flush and Close
**Acceptance:**
- `Logger.Flush(ctx)` flushes drains and keeps them. `Close(ctx)` closes all drains at once, bounded by ctx. An emit after `Close` reports and sends nothing.
- Tests: `TestCore_CORE32_CloseConcurrentAndBounded`, `TestCore_CORE32_EmitAfterCloseReported`, `TestCore_Flush_KeepsDrains`.

**Verify:** `go test -race -run 'TestCore_CORE32_|TestCore_Flush_' .`.
**Deps:** 10-CI-4. **Size:** S. **Closes:** CORE-32.
**Files:**

- `drain.go`
- `drain_test.go`

#### 10-CORE-8 Default extractor
**Acceptance:**
- `errors.As` finds `Code() string`, `Code() any`, and `Stack()` through wrapping. Joined errors list `causes`. A pkg/errors stack renders through reflection. `type` is set.
- Tests: `TestCore_CORE24_WrappedCatalogCode`, `TestCore_CORE24_JoinCauses`, `TestCore_CORE24_PkgErrorsStack`.

**Verify:** `go test -race -run 'TestCore_CORE24_' .`.
**Deps:** 10-CORE-4. **Size:** S. **Closes:** CORE-24.
**Files:**

- `errors.go`
- `errors_test.go`

#### 10-CORE-9 Size counting, group merge, drain docs
**Acceptance:**
- Writes past 256 KiB are dropped and counted. `SetGroup` merges nested maps the same way at every depth. `Set` and `WithDrains` docs match the behavior.
- Tests: `TestCore_CORE25_SizeCapCounts`, `TestCore_CORE23_SetGroupMergesNested`.

**Verify:** `go test -race -run 'TestCore_CORE(23|25)_' .`.
**Deps:** 10-CORE-1. **Size:** S. **Closes:** CORE-21 (docs), CORE-23, CORE-25, PAR-2.
**Files:**

- `event.go`
- `event_test.go`

### Track: redact (after 10-CI-4, parallel with core)

#### 10-RED-1 Linear key matching and a bounded cache
**Acceptance:**
- Keys are cut to 256 bytes. Runs compare token slices up to the longest entry. The cache holds 4096 keys.
- Tests: `TestRedact_RED1_LongKeyUnderOneMs` (4000-byte key under 1ms), `TestRedact_RED4_CacheBounded`.
- `BenchmarkRedact` does not regress by more than 20%.

**Verify:** `go test -race -run 'TestRedact_RED(1|4)_' ./redact && go test -run=xxx -bench=BenchmarkRedact ./redact`.
**Deps:** 10-CI-4. **Size:** S. **Closes:** RED-1, RED-4.
**Files:**

- `redact/redact.go`
- `redact/tokenize.go`
- `redact/limits_test.go`

#### 10-RED-2 `With` keeps the full configuration
**Acceptance:**
- `With` starts from every resolved setting. `Disabled().With(...)` returns an error.
- Test: `TestRedact_RED2_WithKeepsPatternsTransformsReplaceFunc`.

**Verify:** `go test -race -run 'TestRedact_RED2_' ./redact`.
**Deps:** 10-RED-1. **Size:** S. **Closes:** RED-2.
**Files:**

- `redact/redact.go`
- `redact/options.go`
- `redact/options_test.go`

#### 10-RED-3 Joined tokens and the default list
**Acceptance:**
- `api_key` matches `apikey`, and `session_id` matches `sessionid`. The RED-3 list entries are added.
- Test: `TestRedact_RED3_OneWordKeysMasked` covers every key named in RED-3.

**Verify:** `go test -race -run 'TestRedact_RED3_' ./redact && make fuzz FUZZTIME=30s`.
**Deps:** 10-RED-1. **Size:** S. **Closes:** RED-3.
**Files:**

- `redact/redact.go`
- `redact/defaults.go`
- `redact/redact_test.go`

#### 10-RED-4 Pattern names and new patterns
**Acceptance:**
- Unknown or duplicate pattern names return an error.
- `url_credentials`, `url_query_secret`, `basic_auth`, and `api_key_prefix` mask their examples from RED-6.
- Tests: `TestRedact_RED5_UnknownPatternName`, `TestRedact_RED6_NewPatterns`.

**Verify:** `go test -race -run 'TestRedact_RED(5|6)_' ./redact`.
**Deps:** 10-RED-1. **Size:** S. **Closes:** RED-5, RED-6.
**Files:**

- `redact/custom.go`
- `redact/patterns.go`
- `redact/patterns_test.go`

#### 10-RED-5 Fewer false positives
**Acceptance:**
- A Chrome user agent, a 13-digit millisecond timestamp, a 19-digit snowflake id, `0000012345`, and `ID12 ORDER STATUS OK` stay unchanged.
- Real cards with separators, and cards under card key names, stay masked.
- Test: `TestRedact_RED7_NoFalsePositives`.

**Verify:** `go test -race -run 'TestRedact_RED7_' ./redact && make fuzz FUZZTIME=30s`.
**Deps:** 10-RED-4. **Size:** S. **Closes:** RED-7.
**Files:**

- `redact/patterns.go`
- `redact/patterns_id.go`
- `redact/patterns_b_test.go`

#### 10-RED-6 Fail closed and a full fingerprint
**Acceptance:**
- A panicking transform or `ReplaceFunc` masks its whole value. An unknown type is masked. A 13-digit or longer integer is scanned by the card pattern.
- `Fingerprint` differs for any configuration difference.
- Tests: `TestRedact_RED8_FingerprintCoversConfig`, `TestRedact_RED9_TransformPanicFailsClosed`, `TestRedact_SPECG2_UnknownTypeMasked`.

**Verify:** `go test -race -run 'TestRedact_(RED8|RED9|SPECG2)_' ./redact`.
**Deps:** 10-RED-2. **Size:** M. **Closes:** RED-8, RED-9, SPEC-G2.
**Files:**

- `redact/redact.go`
- `redact/fingerprint.go`
- `redact/custom_test.go`

#### 10-RED-7 Paths, globs, and docs
**Acceptance:**
- Docs, specs, and tests use `http.request_headers`. A path that can never match a reserved key reports at `New`.
- `*` crosses `/` inside one segment. `RemoveKeys` works by token form. Bad limits and empty-match regexes return errors. The BENCH.md numbers and the regexp comment are correct.
- Tests: `TestRedact_RED10_DeadPathReported`, `TestRedact_RED12_GlobsAndRemoveKeys`.

**Verify:** `go test -race -run 'TestRedact_RED(10|12)_' ./redact && go run ./tools/cmd/snippets`.
**Deps:** 10-RED-6. **Size:** M. **Closes:** RED-10, RED-12.
**Files:**

- `redact/match.go`
- `redact/options.go`
- `redact/match_test.go`
- `docs/SPEC-redact.md`
- `redact/BENCH.md`

### Track: pipeline (after 10-CORE-7)

#### 10-PIPE-1 Recover and unlock
**Acceptance:**
- A panicking `SendBatch` or `OnDropped` never kills the process. `OnDropped` runs after the lock is released.
- Tests: `TestPipeline_PIPE1_SenderPanicSurvives`, `TestPipeline_PIPE3_OnDroppedPanicDoesNotLock`, `TestPipeline_PIPE3_OnDroppedReentrant`.

**Verify:** `go test -race -run 'TestPipeline_PIPE(1|3)_' ./pipeline`.
**Deps:** 10-CORE-7. **Size:** S. **Closes:** PIPE-1, PIPE-3.
**Files:**

- `pipeline/pipeline.go`
- `pipeline/overflow_test.go`

#### 10-PIPE-2 Flush, Close, and sends after close
**Acceptance:**
- `Flush(ctx)` sends and keeps the worker. `Close(ctx)` cancels in-flight sends at the ctx deadline. A `Send` after `Close` calls `OnDropped`.
- Tests: `TestPipeline_PIPE4_FlushKeepsWorker`, `TestPipeline_PIPE4_SendAfterCloseDropped`, `TestPipeline_PIPE6_CloseCancelsSend`.

**Verify:** `go test -race -run 'TestPipeline_PIPE(4|6)_' ./pipeline`.
**Deps:** 10-PIPE-1. **Size:** S. **Closes:** PIPE-4, PIPE-6.
**Files:**

- `pipeline/pipeline.go`
- `pipeline/pipeline_test.go`

#### 10-PIPE-3 Retry timing
**Acceptance:**
- `Retry-After` is capped at `MaxDelay`. Bad values fall back to the backoff. Waits select on the worker context. Backoff never overflows, and jitter stays under `MaxDelay`.
- Tests: `TestPipeline_PIPE5_RetryAfterCapped`, `TestPipeline_PIPE5_RetryAfterOverflow`, `TestPipeline_PIPE22_BackoffBounded` with an injected clock.

**Verify:** `go test -race -run 'TestPipeline_PIPE(5|22)_' ./pipeline`.
**Deps:** 10-PIPE-1. **Size:** M. **Closes:** PIPE-5, PIPE-22.
**Files:**

- `pipeline/pipeline.go`
- `pipeline/retry.go`
- `internal/httpdrain/httpdrain.go`
- `pipeline/retry_test.go`

#### 10-PIPE-4 Batches, clamps, timer, counters
**Acceptance:**
- A batch holds at most `BatchSize`. Retry waits count toward `MaxBuffer`. Zero options clamp. The worker sleeps on a timer. `Dropped()` and `Stats()` exist.
- Tests: `TestPipeline_PIPE10_BatchSizeCap`, `TestPipeline_PIPE11_OptionClamps`, `TestPipeline_PIPE24_StatsAndTimer`.

**Verify:** `go test -race -run 'TestPipeline_PIPE(10|11|24)_' ./pipeline`.
**Deps:** 10-PIPE-2. **Size:** M. **Closes:** PIPE-10, PIPE-11, PIPE-24 (pipeline part).
**Files:**

- `pipeline/pipeline.go`
- `pipeline/config.go`
- `pipeline/stats.go`
- `pipeline/buffer_test.go`

#### 10-PIPE-5 FanOut rewrite
**Acceptance:**
- One bounded queue and one goroutine per drain, with recover. `Flush` and `Close` reach every drain.
- Tests: `TestPipeline_PIPE2_FanOutPanicSurvives`, `TestPipeline_PIPE2_FanOutGoroutinesBounded`, `TestPipeline_PIPE2_FanOutCloses`.

**Verify:** `go test -race -run 'TestPipeline_PIPE2_' ./pipeline`.
**Deps:** 10-PIPE-2. **Size:** S. **Closes:** PIPE-2.
**Files:**

- `pipeline/fanout.go`
- `pipeline/fanout_test.go`

#### 10-PIPE-6 HTTP drain helper
**Acceptance:**
- Errors never hold a query string or user info. Each drain can take `WithHTTPClient`, `WithTimeout`, and `WithUserAgent`.
- Tests: `TestHTTPDrain_PIPE19_ErrorHasNoSecrets`, `TestHTTPDrain_PIPE24_ClientOptions`.

**Verify:** `go test -race -run 'TestHTTPDrain_PIPE(19|24)_' ./internal/httpdrain`.
**Deps:** 10-PIPE-1. **Size:** S. **Closes:** PIPE-19, PIPE-24 (drain part).
**Files:**

- `internal/httpdrain/httpdrain.go`
- `internal/httpdrain/options.go`
- `internal/httpdrain/httpdrain_test.go`

### Track: audit (after 10-CORE-5)

#### 10-AUD-1 Journal owns the chain
**Acceptance:**
- `Journal` hashes the exact bytes it writes under one lock. `Chain` is removed.
- Tests: `TestAudit_AUD2_ConcurrentJournalVerifies` (100 goroutines, 20 runs), `TestAudit_AUD3_RestartVerifies`, `TestAudit_AUD4_ByteEditsFail` (every edit listed in AUD-4).

**Verify:** `go test -race -count=20 -run 'TestAudit_AUD(2|3|4)_' ./audit`.
**Deps:** 10-CORE-5. **Size:** M. **Closes:** AUD-2, AUD-3, AUD-4.
**Files:**

- `audit/journal.go`
- `audit/chain.go` (deleted)
- `audit/verify.go`
- `audit/journal_test.go`

#### 10-AUD-2 Marker lines and verification rules
**Acceptance:**
- A marker line lands every 100 lines and on `Close`. With a key, the marker is signed and holds a key id.
- `Verify` fails on an empty file, a cut journal, and a count or head mismatch. `VerifyHead` and `VerifySigned` exist, and signatures compare with `hmac.Equal`.
- Tests: `TestAudit_AUD1_TruncationFails`, `TestAudit_AUD1_EmptyFileFails`, `TestAudit_AUD10_SignedNeedsKey`.

**Verify:** `go test -race -run 'TestAudit_AUD(1|10)_' ./audit`.
**Deps:** 10-AUD-1. **Size:** M. **Closes:** AUD-1, AUD-10.
**Files:**

- `audit/journal.go`
- `audit/verify.go`
- `audit/sign.go`
- `audit/verify_test.go`

#### 10-AUD-3 File lock and crash recovery
**Acceptance:**
- A second writer on the same path fails to open. A partial last line moves to `<path>.partial`, and the journal resumes. A 2 MB line verifies. `Close` syncs the file and its directory.
- Tests: `TestAudit_AUD9_SecondWriterRefused`, `TestAudit_AUD9_PartialLineRecovered`, `TestAudit_AUD9_LongLine`.

**Verify:** `go test -race -run 'TestAudit_AUD9_' ./audit`.
**Deps:** 10-AUD-2. **Size:** M. **Closes:** AUD-9.
**Files:**

- `audit/journal.go`
- `audit/lock_unix.go`
- `audit/lock_windows.go`
- `audit/recover_test.go`

#### 10-AUD-4 Several records and no silent loss
**Acceptance:**
- An event holds up to 20 records in `audit`. `Do` after emit, or with no event, emits a standalone event. The key cap never drops a record. Every journal failure reports. NaN never breaks a line.
- Tests: `TestAudit_AUD5_TwoRecordsKept`, `TestAudit_AUD5_AfterEndStandalone`, `TestAudit_AUD5_KeyCapKeepsAudit`, `TestAudit_AUD6_FailuresReported`.

**Verify:** `go test -race -run 'TestAudit_AUD(5|6)_' ./audit .`.
**Deps:** 10-AUD-1, 10-CORE-1. **Size:** M. **Closes:** AUD-5, AUD-6.
**Files:**

- `audit/audit.go`
- `audit/journal.go`
- `event.go`
- `audit/audit_test.go`

#### 10-AUD-5 Diff, Wrap, Mock, tests, and docs
**Acceptance:**
- `Diff` returns a nested tree and errors on non-object input. `Wrap` uses the ctx extractor and maps a denial to `denied`. `Mock` is silent and ignores `WLOG_LEVEL`.
- `examples/audit-refund` uses `Journal` and `Verify`. SPEC-audit and SPEC.md match the code.
- Tests: `TestAudit_AUD7_DiffPathRedacted`, `TestAudit_AUD8_WrapUsesLoggerExtractor`, `TestAudit_AUD11_DiffEdgeCases`, `TestAudit_AUD12_MockIgnoresEnv`.

**Verify:** `go test -race -run 'TestAudit_AUD(7|8|11|12)_' ./audit && cd examples && go test -race ./audit-refund`.
**Deps:** 10-AUD-4. **Size:** M. **Closes:** AUD-7, AUD-8, AUD-11, AUD-12, AUD-13, AUD-14.
**Files:**

- `audit/diff.go`
- `audit/wrap.go`
- `audit/mock.go`
- `examples/audit-refund/main.go`
- `docs/SPEC-audit.md`


#### 10-PIPE-7 MinLevel, a public HTTP drain helper, and identity headers
**Acceptance:**
- `pipeline.MinLevel(level)` drops lower events before the buffer, and an event with `audit` always passes.
- `internal/httpdrain` moves to `pipeline/httpdrain`, and every drain imports the new path. A third-party test drain in `pipeline/httpdrain/example_test.go` uses it.
- `WithUserAgent("")` and `WithIdentityHeaders(false)` send no such header.
- Tests: `TestPipeline_PAR16_MinLevelKeepsAudit`, `TestHTTPDrain_PAR17_ThirdPartyDrain`, `TestHTTPDrain_PAR18_HeadersOff`.

**Verify:** `go test -race -run 'TestPipeline_PAR16_|TestHTTPDrain_PAR1(7|8)_' ./pipeline/...`.
**Deps:** 10-PIPE-6. **Size:** M. **Closes:** PAR-16, PAR-17, PAR-18.
**Files:**

- `pipeline/config.go`
- `pipeline/httpdrain/` (moved from `internal/httpdrain/`)
- `pipeline/httpdrain/example_test.go`
- one import line in each drain (mechanical)

#### 10-AUD-6 Actor types, outcomes, and correlation ids
**Acceptance:**
- Actor types `user`, `service`, `system`, and `agent` exist, and an agent holds `model`, `tools`, and `prompt_id`. Outcome `failure` exists.
- A record gets `correlation_id` and `causation_id` defaults from the trace group. The default `idempotency_key` is stable for the same request id.
- Tests: `TestAudit_PAR26_AgentActor`, `TestAudit_PAR26_CorrelationDefaults`, `TestAudit_PAR26_IdempotencyKeyStable`.

**Verify:** `go test -race -run 'TestAudit_PAR26_' ./audit`.
**Deps:** 10-AUD-5. **Size:** M. **Closes:** PAR-26 (schema part).
**Files:**

- `audit/audit.go`
- `audit/actor.go`
- `audit/audit_test.go`
- `docs/SPEC-audit.md`

#### 10-AUD-7 JSON Patch and audit-only routing
**Acceptance:**
- `audit.Patch(before, after)` returns RFC 6902 operations. A path denylist entry masks the `value` of a matching operation.
- `audit.OnlyDrain(d)` forwards only events with `audit`, holding exactly `timestamp`, `event_id`, `service`, `trace`, and `audit`.
- Tests: `TestAudit_PAR26_PatchOps`, `TestAudit_PAR26_PatchRedacted`, `TestAudit_PAR26_OnlyDrainShape`.

**Verify:** `go test -race -run 'TestAudit_PAR26_(Patch|OnlyDrain)' ./audit`.
**Deps:** 10-AUD-6. **Size:** M. **Closes:** PAR-26 (routing part).
**Files:**

- `audit/patch.go`
- `audit/only.go`
- `audit/patch_test.go`

### Track: drains (after 10-PIPE-7)

Each drain task also moves the drain to `New`, `NewSender`, `MustNew`, and `WithPipeline` (PIPE-17, CORE-21). Until phase 11, each one adds a leak test and an env-alone test. It also adds a status table test for 2xx, 400, 401, 403, 413, 429, and 5xx (PIPE-25). The tracks of different drains share no files, so they can run at the same time.

#### 10-DRN-1 drain-sentry
**Acceptance:**
- One envelope per error event, with header `event_id` equal to the item id. Log items use typed flat attributes and a trace id, and error events are not sent as log items. DSNs with a path prefix or no scheme parse.
- Tests: `TestSentry_PIPE7_EnvelopeEventID`, `TestSentry_PIPE8_ErrorsNotLogs`, `TestSentry_PIPE21_DSNForms`, plus the three interim tests.

**Verify:** `go test -race -run 'TestSentry_' ./drain/sentry`.
**Deps:** 10-PIPE-7. **Size:** M. **Closes:** PIPE-7, PIPE-8, PIPE-21.
**Files:**

- `drain/sentry/sentry.go`
- `drain/sentry/envelope.go`
- `drain/sentry/sentry_test.go`
- `drain/sentry/testdata/`

#### 10-DRN-2 drain-clickhouse and the integration stack
**Acceptance:**
- Timestamps use `2006-01-02 15:04:05.000000000` in UTC. `DDL` uses a `String` column before server 25.3. Identifiers must match the rule. `CLICKHOUSE_URL` with a query string parses.
- Compose pins `clickhouse/clickhouse-server` 25.3 or newer, the OTel collector binds `0.0.0.0:4318`, and every service has a health probe.
- Tests: `TestClickHouse_PIPE9_TimestampFormat`, `TestClickHouse_PIPE20_IdentifierRule`, `TestClickHouse_PIPE19_URLWithQuery`, plus the interim tests.

**Verify:** `go test -race -run 'TestClickHouse_' ./drain/clickhouse && make integration`.
**Deps:** 10-PIPE-7. **Size:** M. **Closes:** PIPE-9, PIPE-19 (clickhouse part), PIPE-20.
**Files:**

- `drain/clickhouse/clickhouse.go`
- `drain/clickhouse/ddl.go`
- `drain/clickhouse/clickhouse_test.go`
- `docker-compose.integration.yml`
- `testdata/otel-collector.yaml`

#### 10-DRN-3 drain-file
**Acceptance:**
- `Read` skips and counts a line over 1 MiB. `Tail` detects rotation with `os.SameFile`, drains the old file first, and caps a pending line. Age rotation uses the open time. A write after `Close` returns an error.
- With no path, the drain writes `.wlog/logs/<YYYY-MM-DD>.jsonl`, creates `.wlog/.gitignore`, and `MaxFiles` keeps the newest 7 files.
- Tests: `TestFile_PIPE12_LongLineSkipped`, `TestFile_PIPE12_TailRotationSameSize`, `TestFile_PAR21_DefaultFolder`, `TestFile_PAR21_MaxFiles`.

**Verify:** `go test -race -run 'TestFile_' ./drain/file`.
**Deps:** 10-PIPE-7. **Size:** M. **Closes:** PIPE-12, PAR-21.
**Files:**

- `drain/file/file.go`
- `drain/file/read.go`
- `drain/file/tail.go`
- `drain/file/default.go`
- `drain/file/file_test.go`

#### 10-DRN-4 drain-loki and drain-otlp
**Acceptance:**
- Loki label names match the label rule, dotted paths resolve, and the cardinality denylist covers the path and its last part. Basic auth needs a user and a password.
- OTLP header values are percent-decoded, and the logs endpoint var is used as is. A map inside an array becomes `kvlistValue`. The environment key is `deployment.environment.name`.
- The OTLP golden body is written from the OTLP JSON spec, not from the encoder. The integration test sends it to the collector.
- Tests: `TestLoki_PIPE13_DottedLabels`, `TestLoki_PIPE13_BasicAuthPair`, `TestOTLP_PIPE14_HeaderDecode`, `TestOTLP_PIPE14_KvlistInArray`.

**Verify:** `go test -race -run 'TestLoki_|TestOTLP_' ./drain/loki ./drain/otlp`.
**Deps:** 10-PIPE-7. **Size:** M. **Closes:** PIPE-13, PIPE-14, PIPE-25 (OTLP golden).
**Files:**

- `drain/loki/loki.go`
- `drain/loki/loki_test.go`
- `drain/otlp/otlp.go`
- `drain/otlp/mapping.go`
- `drain/otlp/otlp_test.go`

#### 10-DRN-5 drain-datadog, drain-axiom, and drain-webhook
**Acceptance:**
- Datadog batches split at 1000 entries or 5 MB. After a 413, only the failed half is retried. `DD_SERVICE` and `DD_ENV` are read once in `New`. The env test sets no `WithURL`.
- Axiom and webhook move to the new constructors and pass the interim tests.
- Tests: `TestDatadog_PIPE15_SplitBatches`, `TestDatadog_PIPE15_Retry413Half`, `TestDatadog_PIPE25_EnvAlone`.

**Verify:** `go test -race -run 'TestDatadog_|TestAxiom_|TestWebhook_' ./drain/datadog ./drain/axiom ./drain/webhook`.
**Deps:** 10-PIPE-7. **Size:** M. **Closes:** PIPE-15, PIPE-17 (these drains).
**Files:**

- `drain/datadog/datadog.go`
- `drain/datadog/datadog_test.go`
- `drain/axiom/axiom.go`
- `drain/webhook/webhook.go`
- their tests

#### 10-DRN-6 drain-betterstack, drain-hyperdx, and drain-posthog
**Acceptance:**
- Better Stack `New` requires a scheme and host, adds `https://` to a bare host, and reads `BETTERSTACK_INGESTING_HOST`. HyperDX adds `/v1/logs` to an empty path. PostHog sets `$process_person_profile` to false without `user.id`.
- PostHog and Better Stack get golden bodies written from vendor docs. The HyperDX test no longer compares the encoder with itself.
- Tests: `TestBetterStack_PIPE16_BareHost`, `TestHyperDX_PIPE16_EmptyPath`, `TestPostHog_PIPE18_AnonymousEvent`.

**Verify:** `go test -race -run 'TestBetterStack_|TestHyperDX_|TestPostHog_' ./drain/betterstack ./drain/hyperdx ./drain/posthog`.
**Deps:** 10-PIPE-7. **Size:** M. **Closes:** PIPE-16, PIPE-18, PIPE-25 (goldens).
**Files:**

- `drain/betterstack/betterstack.go`
- `drain/hyperdx/hyperdx.go`
- `drain/posthog/posthog.go`
- their tests and `testdata/`

### Track: catalog and llm (after 10-CORE-8)

#### 10-CAT-1 Code matching and registries
**Acceptance:**
- `catalog.Extractor` matches full codes, and `AllowShortCodes()` opts in per registry. A code that two registries define returns an error, and `MustExtractor` panics on it. A nil registry is skipped. `errors.Is` matches only inside one registry.
- Tests: `TestCatalog_CAT6_FullCodeOnly`, `TestCatalog_CAT6_DuplicateReturnsError`, `TestCatalog_CAT7_IsSameRegistry`.

**Verify:** `go test -race -run 'TestCatalog_CAT(6|7)_' ./catalog`.
**Deps:** 10-CORE-8. **Size:** S. **Closes:** CAT-6, CAT-7.
**Files:**

- `catalog/extractor.go`
- `catalog/registry.go`
- `catalog/extractor_test.go`

#### 10-CAT-2 Copies, templates, domain, and entry defaults
**Acceptance:**
- `Get` and `Entry()` return deep copies. A template renders in one pass, and the extractor leaves `Message` alone.
- The extractor writes `error.attrs.domain`. Entry `Data` and `Internal` defaults merge under call-site values.
- Tests: `TestCatalog_CAT8_DeepCopy`, `TestCatalog_CAT9_SinglePassTemplate`, `TestCatalog_PAR10_DomainAttr`, `TestCatalog_PAR10_EntryDefaults`.

**Verify:** `go test -race -run 'TestCatalog_(CAT8|CAT9|PAR10)_' ./catalog`.
**Deps:** 10-CAT-1. **Size:** M. **Closes:** CAT-5, CAT-8, CAT-9, PAR-10.
**Files:**

- `catalog/entry.go`
- `catalog/template.go`
- `catalog/extractor.go`
- `catalog/entry_test.go`

#### 10-CAT-3 Audit catalog fields
**Acceptance:**
- `catalog.Audit` has `Description`, `RequiresChanges`, and `RedactPaths`. `audit.Do` writes a record that breaks a rule, with `violations`. `RedactPaths` masks inside `changes`.
- Tests: `TestCatalog_PAR11_ViolationsRecorded`, `TestCatalog_PAR11_RedactPaths`.

**Verify:** `go test -race -run 'TestCatalog_PAR11_' ./catalog ./audit`.
**Deps:** 10-CAT-2, 10-AUD-6. **Size:** M. **Closes:** PAR-11.
**Files:**

- `catalog/audit.go`
- `audit/audit.go`
- `catalog/audit_test.go`

#### 10-LLM-1 Caps and cost fields
**Acceptance:**
- `llm.calls` and `llm.tool_calls` hold 200 items each, and extras count in `wlog.dropped_fields`. The enricher keeps a caller cost and reads a record written by `Set`. `llm.cost_usd` is gone.
- Tests: `TestLLM_CAT1_CallsCapped`, `TestLLM_CAT4_KeepsCallerCost`, `TestLLM_CAT11_NoCostUSD`.

**Verify:** `go test -race -run 'TestLLM_CAT(1|4|11)_' ./llm`.
**Deps:** 10-CORE-1. **Size:** S. **Closes:** CAT-1, CAT-4, CAT-11.
**Files:**

- `llm/set.go`
- `llm/price.go`
- `llm/llm_test.go`

#### 10-LLM-2 Token semantics and the price table
**Acceptance:**
- `Record` follows OTel token semantics with both cache write kinds. `Cost` prices each part at its rate.
- `DefaultPrices` holds only rows from the 2026-09-16 pricing pages, with exact and longest-prefix matching. `docs/cost.md` shows the Anthropic sum rule and one example per provider.
- Tests: `TestLLM_CAT2_TokenInvariants`, `TestLLM_CAT2_CostParts`, `TestLLM_CAT3_SnapshotPrefix`.

**Verify:** `go test -race -run 'TestLLM_CAT(2|3)_' ./llm && go run ./tools/cmd/snippets`.
**Deps:** 10-LLM-1. **Size:** M. **Closes:** CAT-2, CAT-3.
**Files:**

- `llm/record.go`
- `llm/price.go`
- `llm/prices.go`
- `llm/price_test.go`
- `docs/cost.md`

### Track: sample, enrich, drain-memory, wlogtest (after 10-CORE-1)

#### 10-SMP-1 Sampling rules
**Acceptance:**
- `KeepPath` supports `**`, and a bad glob returns an error. `Rate` takes a float percent and rejects other values. Head sampling hashes `trace.trace_id`. A kept event records `wlog.sample_rate`. `KeepErrorsAndSlow` keeps warn, error, and 5xx.
- Tests: `TestSample_SMP3_DoubleStarGlob`, `TestSample_SMP4_TraceConsistent`, `TestSample_SMP4_RateRecorded`, `TestSample_SMP2_KeepsWarnAnd5xx`.

**Verify:** `go test -race -run 'TestSample_SMP(2|3|4)_' ./sample`.
**Deps:** 10-CORE-6. **Size:** M. **Closes:** SMP-2, SMP-3, SMP-4, BET-18.
**Files:**

- `sample/sample.go`
- `sample/glob.go`
- `sample/sample_test.go`

#### 10-ENR-1 Enrichers
**Acceptance:**
- `Geo(provider)` reads one named provider, and its doc names the spoofing risk. An enricher never replaces a non-map group. `Overwrite` exists for `UserAgent` and `User`. The user agent table holds 30 real agents.
- Tests: `TestEnrich_SMP5_GeoProvider`, `TestEnrich_SMP6_NoGroupReplace`, `TestEnrich_SMP7_UserAgentTable`, `TestEnrich_SMP10_GeoTypes`.

**Verify:** `go test -race -run 'TestEnrich_SMP(5|6|7|10)_' ./enrich`.
**Deps:** 10-CORE-3. **Size:** M. **Closes:** SMP-5, SMP-6, SMP-7, SMP-10.
**Files:**

- `enrich/geo.go`
- `enrich/useragent.go`
- `enrich/options.go`
- `enrich/enrich_test.go`

#### 10-MEM-1 drain-memory and wlogtest
**Acceptance:**
- drain-memory deep-copies per subscriber and per snapshot entry, and counts subscriber drops. `Query` with `Limit` returns the newest N. SSE takes the replay after subscribing, and each write has a deadline. `Named` stores have `Remove`.
- `wlogtest.New` is silent. `RequireField` uses `reflect.DeepEqual` and dotted paths. Each helper has an example.
- Tests: `TestMemory_SMP1_SubscriberCopy`, `TestMemory_SMP8_NewestN`, `TestMemory_SMP8_ReplayOrder`, `TestWlogtest_SMP9_Silent`, `TestWlogtest_SMP9_DottedPath`.

**Verify:** `go test -race -run 'TestMemory_SMP|TestWlogtest_SMP' ./drain/memory ./wlogtest`.
**Deps:** 10-CORE-3. **Size:** M. **Closes:** SMP-1, SMP-8, SMP-9.
**Files:**

- `drain/memory/memory.go`
- `drain/memory/sse.go`
- `wlogtest/wlogtest.go`
- `drain/memory/memory_test.go`
- `wlogtest/wlogtest_test.go`

### Track: CLI (after 10-CI-4 and the HTTP adapters it reads)

#### 10-MAP-1 Handler discovery
**Acceptance:**
- Discovery resolves handlers through `go/types` across packages, including conversions, closure factories, `ServeHTTP` types, Echo `Add` and `Match`, Gin `Match`, and mux chains.
- The report prints `N handlers found`, and zero handlers exits 2.
- Tests: `TestMap_CLI1_CrossPackageHandlers`, `TestMap_CLI1_ZeroHandlersExit2`.

**Verify:** `cd cmd/wlog && go test -race -run 'TestMap_CLI1_' ./...`.
**Deps:** 10-CI-4. **Size:** M. **Closes:** CLI-1.
**Files:**

- `cmd/wlog/internal/mapper/discover.go`
- `cmd/wlog/internal/mapper/discover_test.go`
- `cmd/wlog/internal/mapper/testdata/crosspkg/`

#### 10-MAP-2 Routes and middleware coverage
**Acceptance:**
- Routes keep Echo, Gin, and mux prefixes. Constant routes resolve. mux `Methods` gives one entry per method. A middleware call covers every handler on the router it wraps.
- Tests: `TestMap_CLI8_GroupPrefixes`, `TestMap_CLI8_MuxMethods`, `TestMap_CLI9_MiddlewareAcrossPackages`.

**Verify:** `cd cmd/wlog && go test -race -run 'TestMap_CLI(8|9)_' ./...`.
**Deps:** 10-MAP-1. **Size:** M. **Closes:** CLI-8, CLI-9.
**Files:**

- `cmd/wlog/internal/mapper/routes.go`
- `cmd/wlog/internal/mapper/coverage.go`
- `cmd/wlog/internal/mapper/routes_test.go`

#### 10-MAP-3 Rule accuracy
**Acceptance:**
- `n/a` adds no points. `error-guidance` treats an unresolved error as guided. `print` and `swallowed-error` follow SPEC-hardening rule 6, and `swallowed-error` walks the CFG. Generated files are skipped. `SPEC-cli-map.md` holds the new formula first.
- Tests: `TestMap_CLI7_NotApplicable`, `TestMap_CLI14_PrintScope`, `TestMap_CLI14_WholeWordSensitive`, `TestMap_CLI20_SwallowedErrorCFG`.

**Verify:** `cd cmd/wlog && go test -race -run 'TestMap_CLI(7|14|20)_' ./...`.
**Deps:** 10-MAP-1. **Size:** M. **Closes:** CLI-7, CLI-14, CLI-20.
**Files:**

- `docs/SPEC-cli-map.md`
- `cmd/wlog/internal/rules/score.go`
- `cmd/wlog/internal/rules/print.go`
- `cmd/wlog/internal/rules/swallowed.go`
- `cmd/wlog/internal/rules/rules_test.go`

#### 10-MAP-4 Gates, configuration, and streams
**Acceptance:**
- A failed gate writes no file, and `--out` equal to `--baseline` exits 2. Configuration comes from `wlog.map.yaml`, and flags win. Status lines go to stderr, and `--json` prints one document. Color follows `NO_COLOR`, and text wraps at `COLUMNS`.
- Tests: `TestMap_CLI5_FailedGateNoWrite`, `TestMap_CLI6_FlagsWin`, `TestMap_CLI12_OneJSONDocument`, `TestMap_PAR28_NoColorColumns`.

**Verify:** `cd cmd/wlog && go test -race -run 'TestMap_(CLI5|CLI6|CLI12|PAR28)_' ./...`.
**Deps:** 10-MAP-3. **Size:** M. **Closes:** CLI-5, CLI-6, CLI-12, PAR-28.
**Files:**

- `cmd/wlog/map.go`
- `cmd/wlog/internal/config/config.go`
- `cmd/wlog/internal/term/term.go`
- `cmd/wlog/map_test.go`

#### 10-MAP-5 Map JSON v2 and the text report
**Acceptance:**
- The JSON has `version: 2`, tool and rule versions, short framework ids, relative paths, object `top_fixes`, `summary`, and `evidence`.
- The text report prints `file:line`, rule id, and fix per failing handler, `FIX FIRST` with the projected score, and a docs link per fix. `--strict` rules follow rule 11.
- Tests: `TestMap_CLI19_JSONv2Golden`, `TestMap_CLI21_TextReportGolden`, `TestMap_PAR29_FixFirst`, `TestMap_PAR32_Evidence`.

**Verify:** `cd cmd/wlog && go test -race -run 'TestMap_(CLI19|CLI21|PAR29|PAR32)_' ./...`.
**Deps:** 10-MAP-4. **Size:** M. **Closes:** CLI-19, CLI-21, PAR-29, PAR-32.
**Files:**

- `cmd/wlog/internal/report/json.go`
- `cmd/wlog/internal/report/text.go`
- `cmd/wlog/internal/report/testdata/`
- `cmd/wlog/internal/report/report_test.go`

#### 10-MAP-6 Analyzer, loading cost, and golangci-lint plugin
**Acceptance:**
- The vet analyzer skips suggestions without `-suggest`, skips tests, reads `wlog.map.yaml`, reports at the call, and has `-rules`. Loading uses export data, and a two-file app peaks under 150 MB. `cmd/wlog/golangci` registers the analyzer.
- Tests: `TestVet_CLI16_Flags`, `TestMap_CLI17_MemoryBound`, `TestGolangci_CLI13_PluginLoads`.

**Verify:** `cd cmd/wlog && go test -race -run 'TestVet_CLI16_|TestMap_CLI17_|TestGolangci_CLI13_' ./...`.
**Deps:** 10-MAP-3. **Size:** M. **Closes:** CLI-13, CLI-16, CLI-17.
**Files:**

- `cmd/wlog/wlogvet/analyzer.go`
- `cmd/wlog/internal/mapper/load.go`
- `cmd/wlog/golangci/plugin.go`
- their tests

#### 10-MAP-7 Ignore comments, git baselines, and SARIF
**Acceptance:**
- `//wlog:ignore <rule> -- <reason>` suppresses a rule and counts it. A comment with no reason is a finding. `--baseline git:<ref>` reads through `git show`. `--no-write` writes nothing. `--format sarif` passes the SARIF 2.1.0 JSON Schema.
- Tests: `TestMap_PAR30_IgnoreNeedsReason`, `TestMap_PAR31_GitBaseline`, `TestMap_BET9_SARIFSchema`.

**Verify:** `cd cmd/wlog && go test -race -run 'TestMap_(PAR30|PAR31|BET9)_' ./...`.
**Deps:** 10-MAP-5. **Size:** M. **Closes:** PAR-30, PAR-31, BET-9.
**Files:**

- `cmd/wlog/internal/rules/ignore.go`
- `cmd/wlog/internal/report/sarif.go`
- `cmd/wlog/internal/baseline/git.go`
- their tests

#### 10-MAP-8 The `keys.strict` rule
**Acceptance:**
- `Set(ctx, "orderID", v)` next to `NewKey("order_id")` is flagged at the `Set` call. An exact match or an unrelated key is not.
- Test: `TestMap_BET22_StrictKeys`.

**Verify:** `cd cmd/wlog && go test -race -run 'TestMap_BET22_' ./...`.
**Deps:** 10-MAP-3. **Size:** S. **Closes:** PAR-6, BET-22.
**Files:**

- `cmd/wlog/internal/rules/strictkeys.go`
- `cmd/wlog/internal/rules/strictkeys_test.go`

#### 10-INIT-1 `wlog init` correctness
**Acceptance:**
- Rewrites use `go/ast`. `ListenAndServe(addr, nil)` and `http.Server{Handler: h}` are wrapped. `.env.example` merges. `--dry-run` prints a unified diff. Writes are atomic. An unknown `--drain` exits 2.
- The init test builds each generated app, serves one request with status 200, and sees one event.
- Tests: `TestInit_CLI4_NilMux`, `TestInit_CLI4_ServerLiteral`, `TestInit_CLI15_AtomicWrites`, `TestInit_CLI4_GeneratedAppServes`.

**Verify:** `cd cmd/wlog && go test -race -run 'TestInit_CLI(4|15)_' ./...`.
**Deps:** 10-CI-4. **Size:** M. **Closes:** CLI-4, CLI-15.
**Files:**

- `cmd/wlog/init.go`
- `cmd/wlog/internal/initgen/rewrite.go`
- `cmd/wlog/internal/initgen/write.go`
- `cmd/wlog/init_test.go`

#### 10-INIT-2 `wlog doctor` and `wlog agents`
**Acceptance:**
- `doctor` loads from `--dir`, fails on a load error, walks every file, and gives each finding a `WLOG_DOCTOR_*` code with why and fix. `--json` prints one object.
- `agents` refuses an unpaired fence with line numbers, never overwrites a skill without its marker, and keeps line endings. Every template snippet compiles and runs.
- Tests: `TestDoctor_CLI10_LoadError`, `TestDoctor_PAR34_CodesJSON`, `TestAgents_CLI11_UnpairedFence`, `TestAgents_CLI3_SnippetsRun`.

**Verify:** `cd cmd/wlog && go test -race -run 'TestDoctor_|TestAgents_' ./...`.
**Deps:** 10-INIT-1. **Size:** M. **Closes:** CLI-3, CLI-10, CLI-11, CLI-22, PAR-34.
**Files:**

- `cmd/wlog/doctor.go`
- `cmd/wlog/agents.go`
- `cmd/wlog/internal/templates/`
- `cmd/wlog/doctor_test.go`
- `cmd/wlog/agents_test.go`

### Track: docs (last in phase 10)

#### 10-DOCS-1 Statuses, README, and CHANGELOG
**Acceptance:**
- CAPABILITIES statuses match the code, and the v1.2 to v1.4 approval gap is in CHANGELOG. README lists every module with its install line and the latest tag. Gate and criteria tables link to passing tests.
- `CHANGELOG.md` lists every audit id closed in phase 10 under v0.5.0.

**Verify:** `go run ./tools/cmd/snippets && go run ./tools/cmd/ste README.md docs/CAPABILITIES.md CHANGELOG.md`.
**Deps:** every other phase 10 task. **Size:** M. **Closes:** DOC-2, DOC-3.
**Files:**

- `README.md`
- `docs/CAPABILITIES.md`
- `CHANGELOG.md`

#### 10-DOCS-2 Parity page and guides
**Acceptance:**
- `docs/evlog-parity.md` is rebuilt from the PAR rows, and no row says built unless the behavior matches.
- The older specs pass `tools ste`, and `ste-known-bad.txt` is empty.
- `docs/event-shape.md`, `docs/customization.md`, and `docs/best-practices.md` state only what the code does. Every Go block compiles, and `snippets-known-bad.txt` is empty.
- CLAUDE.md gains the proof rule and the `SetDefault` exception.

**Verify:** `go run ./tools/cmd/snippets && go run ./tools/cmd/ste docs/*.md CLAUDE.md`.
**Deps:** 10-DOCS-1. **Size:** M. **Closes:** DOC-4, DOC-5, DOC-6, DOC-8.
**Files:**

- `docs/evlog-parity.md`
- `docs/event-shape.md`
- `docs/customization.md`
- `docs/best-practices.md`
- `CLAUDE.md`

### Review point 10, v0.5.0

- [ ] Every test named in phase 10 fails on the commit before its task and passes after it.
- [ ] The event-shape fuzz test runs 10 minutes clean.
- [ ] `make race`, `make floor`, `make snippets`, `make cover`, and the full CI pass.
- [ ] `tools release -dry-run` shows the module tag order.
- [ ] Human review. Tagging v0.5.0 is ask-first.

---

## Phase 11, v0.6: event shape v2 and the five foundations

Specs: [SPEC-core-v2.md](../docs/SPEC-core-v2.md), [SPEC-work.md](../docs/SPEC-work.md),
[SPEC-http-core.md](../docs/SPEC-http-core.md), [SPEC-conformance.md](../docs/SPEC-conformance.md),
[SPEC-setup.md](../docs/SPEC-setup.md).

Order: problems first, then the shape tasks. Default and calls follow in parallel, then propagate.
Then work, schema, and presets. Then http-core, conformance, setup, and the rebuilt adapters. Every
golden file is written by hand from the spec.

### Track: core-shape (first, sequential)

#### 11-SHAPE-1 Time semantics and ErrorInfo v2
**Acceptance:**
- `timestamp` is the start of the work. `duration_ms` is a float with microsecond precision. `http.duration_ms` is gone.
- `ErrorInfo` gains `Type`, `Causes`, and `Caller`. `WithCaller(false)` turns the caller off.
- Tests: `TestShape_CORE16_TimestampIsStart`, `TestShape_CORE27_FloatDuration`, `TestShape_BET17_CallerTypeCauses`, `TestShape_PAR7_PlainErrorHasType`.

**Verify:** `go test -race -run 'TestShape_(CORE16|CORE27|BET17|PAR7)_' .`.
**Deps:** 11-PROB-1. **Size:** M. **Closes:** CORE-16, CORE-27, BET-17, PAR-7, SPEC-G8.
**Files:**

- `event.go`
- `errors.go`
- `shape_test.go`
- `errors_test.go`

#### 11-SHAPE-2 Reserved keys and the ordered JSON writer
**Acceptance:**
- The JSON writer follows the reserved key table order. `kind`, `message`, `event_id` (UUIDv7), and the nested `wlog` object exist. Empty values are left out. Each event is one `Write`, with HTML escaping off.
- One golden per kind in `testdata/shape/` matches.
- `Start` and `Detach` set the trace and span ids, and a child keeps its parent's trace id. Finalize sets `event_id`.
- Tests: `TestShape_BET13_KeyOrderGolden`, `TestShape_CORE34_WlogNested`, `TestShape_EventIDIsUUIDv7`, `TestShape_EmptyValuesOmitted`.

**Verify:** `go test -race -run 'TestShape_(BET13|CORE34|EventID|Empty)' .`.
**Deps:** 11-SHAPE-1. **Size:** M. **Closes:** BET-13, CORE-34.
**Files:**

- `encode.go`
- `uuid.go`
- `event.go`
- `shape_test.go`
- `testdata/shape/`

#### 11-SHAPE-3 Summary
**Acceptance:**
- The finalize stage builds `summary` from the redacted event with the template per kind, the error suffix, and up to two `_id` keys. A masked or long part is left out, and the result is at most 240 characters. `WithSummary` runs under recover.
- The event-shape fuzz test also proves that a masked value never reaches `summary`.
- Tests: `TestShape_BET5_SummaryPerKind`, `TestShape_BET5_CatalogErrorSummary`, `TestShape_BET5_SummaryPanicFallsBack`.

**Verify:** `go test -race -run 'TestShape_BET5_' . && go test -run=xxx -fuzz=FuzzCore_EventShapeNeverLeaks -fuzztime=60s .`.
**Deps:** 11-SHAPE-2. **Size:** M. **Closes:** BET-5.
**Files:**

- `summary.go`
- `summary_test.go`
- `gates_test.go`

#### 11-SHAPE-4 Stage order v2 and the size cap
**Acceptance:**
- Stages run in the SPEC.md order. `HeadSampler` gets level and trace id only. A `Keeper` sees the enriched event and can rescue a head drop. Finalize enforces 256 KiB and sets `wlog.truncated`.
- `sample.New` implements both interfaces.
- Tests: `TestStages_PAR14_KeeperSeesEnriched`, `TestStages_HeadDropRescued`, `TestStages_SizeCapFinalize`.

**Verify:** `go test -race -run 'TestStages_' . ./sample`.
**Deps:** 11-SHAPE-3. **Size:** M. **Closes:** PAR-14, SPEC-G13 (stage part).
**Files:**

- `stages.go`
- `sample/sample.go`
- `stages_test.go`

#### 11-SHAPE-5 Plugin hooks v2
**Acceptance:**
- `Starter` and `Finisher` replace the request hooks and run for every kind. `Finisher` gets the read-only `Event` after finalize.
- `Measurer` runs for every non-log event before head sampling and the level filter, with value patterns applied.
- `New` calls `Setup` on drains, plugins, and presets.
- `BenchmarkEmit_WithMeasurer` joins the bench gate, and one measurer adds at most 1µs p50.
- Tests: `TestHooks_PAR25_FinisherSeesEvent`, `TestHooks_MeasurerBeforeSampling`, `TestHooks_MeasurerValuePatterns`, `TestHooks_SetupOnDrain`.

**Verify:** `go test -race -run 'TestHooks_' .`.
**Deps:** 11-SHAPE-4. **Size:** M. **Closes:** PAR-25, PAR-4 (decision, doc only), PAR-24 (decision, doc only).
**Files:**

- `plugin.go`
- `measure.go`
- `stages.go`
- `plugin_test.go`

### Track: core-problems (first)

#### 11-PROB-1 Problem codes
**Acceptance:**
- `Problem`, `OnProblem`, `Logger.Report`, and `Problems()` exist, and `OnError` is removed. The default handler prints one stderr line per code per minute.
- `docs/problems.md` has a section for each code. A late write reports `WLOG_LATE_WRITE`.
- Tests: `TestProblems_BET4_EveryCodeDocumented`, `TestProblems_DefaultRateLimited`, `TestProblems_PAR3_LateWriteReported`, `TestProblems_CORE17_LateWriteCounted`.

**Verify:** `go test -race -run 'TestProblems_' .`.
**Deps:** Review point 10. **Size:** M. **Closes:** BET-4, CORE-17, PAR-3.
**Files:**

- `problem.go`
- `problem_catalog.go`
- `docs/problems.md`
- `problem_test.go`

#### 11-PROB-2 Debug reasons and Stats
**Acceptance:**
- With `WLOG_DEBUG=1`, each drop reports `WLOG_EVENT_DROPPED` with its reason. `Stats` holds emitted, dropped by reason, writer drops, and one entry per `StatsReporter` drain. `DebugHandler` serves it as JSON. A slow synchronous drain reports `WLOG_DRAIN_SLOW` once.
- Tests: `TestProblems_BET10_EveryDropReason`, `TestStats_BET19_Counts`, `TestStats_DebugHandlerJSON`, `TestProblems_DrainSlowOnce`.

**Verify:** `go test -race -run 'TestProblems_BET10_|TestStats_|TestProblems_DrainSlow' .`.
**Deps:** 11-PROB-1, 11-SHAPE-4. **Size:** M. **Closes:** BET-10, BET-19, SPEC-G15.
**Files:**

- `stats.go`
- `debug.go`
- `stages.go`
- `stats_test.go`

#### 11-SHAPE-6 Writers
**Acceptance:**
- `WithWriter`, `WriterSync`, `WriterBuffer`, `WithFormat`, and `WithSilent` exist. The default writer is async with a bounded queue that drops the oldest line. `Flush` and `Close` drain the queue. `WithSilent` without a drain reports `WLOG_SILENT_NO_DRAIN`.
- Tests: `TestWriter_CORE20_BlockedWriterNeverBlocks` (10,000 emits under 1 second), `TestWriter_DropsReported`, `TestWriter_FlushDrainsQueue`, `TestWriter_SyncOption`.

**Verify:** `go test -race -run 'TestWriter_' .`.
**Deps:** 11-SHAPE-2. **Size:** M. **Closes:** CORE-20.
**Files:**

- `writer.go`
- `wlog.go`
- `writer_test.go`

#### 11-SHAPE-7 Pretty console v2
**Acceptance:**
- The layout matches the spec: the summary line, the error block with why, fix, link, and caller, one tree line per group, and one line per call and log. Each event is one write. Colors turn on only for a terminal, and `NO_COLOR` turns them off.
- Tests: `TestPretty_BET15_Golden`, `TestPretty_CORE22_NoInterleave` (100 concurrent events), `TestPretty_PAR9_ErrorFirst`.

**Verify:** `go test -race -run 'TestPretty_' .`.
**Deps:** 11-SHAPE-6. **Size:** M. **Closes:** CORE-22, BET-15, PAR-9.
**Files:**

- `pretty.go`
- `pretty_test.go`
- `testdata/pretty/`

#### 11-LLM-1 Rename llm event keys
**Acceptance:**
- `model` becomes `request_model`, `cached_input_tokens` becomes `cache_read_input_tokens`, and `finish_reason` becomes `finish_reasons`, an array. `CHANGELOG.md` lists each rename.
- Tests: `TestLLM_KeysV2Golden`.

**Verify:** `go test -race -run 'TestLLM_KeysV2' ./llm`.
**Deps:** 11-SHAPE-2. **Size:** S. **Closes:** none (shape v2 for `llm`).
**Files:**

- `llm/set.go`
- `llm/llm_test.go`
- `CHANGELOG.md`

### Track: core-default (after 11-SHAPE-2)

#### 11-DEF-1 SetDefault and a Logger-scoped switch
**Acceptance:**
- `SetDefault`, `Default`, `Logger.SetEnabled`, `Logger.Enabled`, and `Logger.Flush` exist. Package-level `SetEnabled` and the second state in `memory.Named` are gone. Package functions fall back to `Default()`.
- `tools` gains a `pkgstate` command that fails on a package-level `var` outside an allow-list that holds only the default pointer.
- Tests: `TestDefault_PAR5_InfoWithoutSetup`, `TestDefault_CORE19_EnabledPerLogger`, `TestPkgState_OnlyDefaultPointer`.

**Verify:** `go test -race -run 'TestDefault_' . ./drain/memory && cd tools && go test -race -run 'TestPkgState_' ./... && go run ./cmd/pkgstate`.
**Deps:** 11-SHAPE-2. **Size:** M. **Closes:** CORE-19, PAR-5.
**Files:**

- `default.go`
- `wlog.go`
- `drain/memory/named.go`
- `tools/cmd/pkgstate/main.go`
- `default_test.go`

#### 11-DEF-2 Writes with no event
**Acceptance:**
- `Set`, `SetGroup`, `Append`, `SetLevel`, and `AppendLog` with no event report `WLOG_NO_EVENT`. `Error` with no event emits a `log` event at level `error`. `Log` writes at any level.
- Tests: `TestDefault_CORE18_OrphanSetReported`, `TestDefault_CORE18_OrphanErrorEmits`, `TestDefault_SPECG14_LogAnyLevel`.

**Verify:** `go test -race -run 'TestDefault_(CORE18|SPECG14)_' .`.
**Deps:** 11-DEF-1. **Size:** S. **Closes:** CORE-18, SPEC-G14.
**Files:**

- `plain.go`
- `event.go`
- `default_test.go`

### Track: core-calls (after 11-SHAPE-5)

#### 11-CALL-1 StartCall and call records
**Acceptance:**
- `StartCall` and `CallFromContext` follow every core-calls rule: the cap of 50, `call_stats`, a late end, a nested call of the same kind, a double end, a `Detach` child, and recover. `CallSpanID` returns the open call's span id. A record takes `error.code` from `ErrCode` or the extractor, and `error.message` only from `ErrMessage`.
- Tests: `TestCalls_ErrTextNeverCopied`, `TestCalls_Cap50Stats60`, `TestCalls_NestedSameKindSkips`, `TestCalls_LateEndRecordsNothing`, `TestCalls_EndTwiceOnce`, `TestCalls_AttrsRedacted`.

**Verify:** `go test -race -run 'TestCalls_' .`.
**Deps:** 11-SHAPE-5. **Size:** M. **Closes:** BET-26 (core part).
**Files:**

- `calls.go`
- `event.go`
- `calls_test.go`

### Track: propagate and work (after 11-CALL-1)

#### 11-PROP-1 propagate
**Acceptance:**
- The three carriers, `Extract`, `Inject`, and `FromContext` follow SPEC-work. W3C test vectors pass. `X-Request-ID` follows the length and charset rule. B3 and X-Ray are read only with their option. `Inject` inside a call uses the call's span id. `ContextWith` replaces the ids on the context and on the event.
- Tests: `TestPropagate_ContextWithUpdatesEvent`, `TestPropagate_W3CVectors`, `TestPropagate_RequestIDRule`, `TestPropagate_B3XRayOptions`, `TestPropagate_InjectUsesCallSpan`.

**Verify:** `go test -race -run 'TestPropagate_' ./propagate`.
**Deps:** 11-CALL-1. **Size:** M. **Closes:** HTTP-19 (trace context part).
**Files:**

- `propagate/propagate.go`
- `propagate/carrier.go`
- `propagate/xray.go`
- `propagate/propagate_test.go`

#### 11-WORK-1 Units, kinds, and levels
**Acceptance:**
- `Kind`, `Unit`, `Start`, `Run`, and `Handle` exist. `End` applies the level rule and the status class tables. An error with client error class gives `warn`. Each kind writes its group and `operation`. A panic records a stack and reaches the caller, and `RecoverPanics()` returns an error instead.
- Tests: `TestWork_ClientClassErrorIsWarn`, `TestWork_RunErrorEvent`, `TestWork_StatusClassTables`, `TestWork_PanicReachesCaller`, `TestWork_RecoverPanics`.

**Verify:** `go test -race -run 'TestWork_' ./work`.
**Deps:** 11-PROP-1. **Size:** M. **Closes:** SPEC-G9.
**Files:**

- `work/work.go`
- `work/status.go`
- `work/work_test.go`

#### 11-WORK-2 Lag, batches, and flush
**Acceptance:**
- `StartedAt` gives `lag_ms`, and `timestamp` stays the processing start. `BatchEvent` records batch size and failures. `Ticker` emits one job event per tick and records a skipped tick. `Flush` bounds a short-lived runtime.
- Tests: `TestWork_LagMs`, `TestWork_BatchEvent`, `TestWork_TickerSkipsBusyTick`, `TestWork_FlushBounded`.

**Verify:** `go test -race -run 'TestWork_(Lag|Batch|Ticker|Flush)' ./work`.
**Deps:** 11-WORK-1. **Size:** S. **Closes:** SPEC-G19.
**Files:**

- `work/batch.go`
- `work/flush.go`
- `work/ticker.go`
- `work/batch_test.go`

### Track: event-schema and output-presets (after 11-SHAPE-3)

#### 11-SCH-1 JSON Schemas
**Acceptance:**
- `schema/event.v1.json` covers every reserved key and kind group. `schema/map.v2.json` covers the map file. `schema.EventV1()` and `schema.MapV2()` embed them.
- `tools schema` tests every golden event and drain golden body against its schema. The `tools` module adds `github.com/santhosh-tekuri/jsonschema/v6`, which needs approval first.
- Tests: `TestSchema_BET7_GoldensValid`, `TestSchema_SPECG18_BadEventFails`.

**Verify:** `go test -race ./schema && cd tools && go test -race -run 'TestSchema_' ./... && go run ./cmd/schema`.
**Deps:** 11-SHAPE-3. **Size:** M. **Closes:** BET-7, SPEC-G18, CLI-19 (schema part).
**Files:**

- `schema/event.v1.json`
- `schema/map.v2.json`
- `schema/schema.go`
- `tools/cmd/schema/main.go`

#### 11-PRE-1 Preset contract and flat
**Acceptance:**
- `OutputPreset`, `WithOutput`, `Lead` ordering, `ByName`, and `Rename` exist. The old `FieldsFlat` and `FieldsOTel` are removed. User key collisions move to `wlog.fields`. A panicking preset falls back to canonical.
- Tests: `TestPreset_CORE12_DrainsSeeCanonical`, `TestPreset_FlatGolden`, `TestPreset_CollisionMoves`, `TestPreset_PanicFallsBack`, `TestPreset_RenameBadPairs`.

**Verify:** `go test -race -run 'TestPreset_' ./preset .`.
**Deps:** 11-SHAPE-6. **Size:** M. **Closes:** CORE-12, SPEC-G11.
**Files:**

- `preset.go`
- `preset/preset.go`
- `preset/flat.go`
- `preset/preset_test.go`
- `fields.go` (deleted)

#### 11-PRE-2 OTel preset
**Acceptance:**
- The OTel golden for a request, an error, a log, a message, and an LLM event matches the table. Every attribute name outside `gen_ai.*` and `http.request.header.*` is in `semconv-1.43.0.tsv`.
- `integrations/search/collectors/otel-filelog.yaml` parses the preset. The integration test runs the collector with it and receives one log record per golden line.
- Tests: `TestPreset_BET14_OTelGoldens`, `TestPreset_BET14_SemconvNamesExist`, `TestPreset_BET14_GenAINames`.

**Verify:** `go test -race -run 'TestPreset_BET14_' ./preset && make integration`.
**Deps:** 11-PRE-1. **Size:** M. **Closes:** BET-14, CORE-13.
**Files:**

- `preset/otel.go`
- `preset/otel_test.go`
- `preset/testdata/semconv-1.43.0.tsv`
- `preset/testdata/genai-names.txt`
- `preset/testdata/otel/`
- `integrations/search/collectors/otel-filelog.yaml`

#### 11-PRE-3 ECS and Datadog presets
**Acceptance:**
- ECS goldens start with `@timestamp`, `log.level`, `message`, and `ecs.version`, and put user keys under `wlog.fields`. A non-IP client address goes to `client.address`.
- Datadog goldens never hold `host`, and hold nanosecond `duration`.
- Tests: `TestPreset_ECSGoldens`, `TestPreset_ECSClientIP`, `TestPreset_DatadogGoldens`.

**Verify:** `go test -race -run 'TestPreset_(ECS|Datadog)' ./preset`.
**Deps:** 11-PRE-1. **Size:** M. **Closes:** CORE-13 (other presets).
**Files:**

- `preset/ecs.go`
- `preset/datadog.go`
- `preset/ecs_test.go`
- `preset/testdata/ecs/`
- `preset/testdata/datadog/`

#### 11-PRE-4 GCP and EMF presets
**Acceptance:**
- GCP goldens hold `severity` and `httpRequest`, with string sizes and a seconds latency. They hold the trace resource name with a project, and `@type` on error events only.
- EMF goldens hold a valid `_aws` object for a request, none for a `log` event, and leave out a dimension set with a missing key.
- Tests: `TestPreset_GCPGoldens`, `TestPreset_GCPTraceWithoutProject`, `TestPreset_EMFGoldens`, `TestPreset_EMFMissingDimension`.

**Verify:** `go test -race -run 'TestPreset_(GCP|EMF)' ./preset`.
**Deps:** 11-PRE-1. **Size:** M. **Closes:** SPEC-G11 (backends part).
**Files:**

- `preset/gcp.go`
- `preset/emf.go`
- `preset/gcp_test.go`
- `preset/testdata/gcp/`
- `preset/testdata/emf/`

### Track: http-core (after 11-WORK-1)

#### 11-HTTP-1 Views, Exchange, and route rules
**Acceptance:**
- `Request`, `Response`, `Exchange`, and `NetHTTP` exist. `operation` is `{METHOD} {route}` or `{METHOD} unmatched`, and the wildcard 404 rule blanks the route. Level follows the SPEC.md rule. `http.scheme` and `http.host` follow the proxy rule.
- Tests: `TestHTTPCore_CORE14_OperationIsRoute`, `TestHTTPCore_CORE15_LevelFromStatus`, `TestHTTPCore_HTTP15_UnmatchedRoute`, `TestHTTPCore_SchemeHost`.

**Verify:** `go test -race -run 'TestHTTPCore_(CORE14|CORE15|HTTP15|SchemeHost)' ./middleware/httpcore`.
**Deps:** 11-WORK-1. **Size:** M. **Closes:** CORE-14, CORE-15, HTTP-15, SPEC-G10, PAR-12.
**Files:**

- `middleware/httpcore/exchange.go`
- `middleware/httpcore/view.go`
- `middleware/httpcore/nethttp.go`
- `middleware/httpcore/exchange_test.go`

#### 11-HTTP-2 Capture policy
**Acceptance:**
- Safe defaults capture only the allow-lists and names. `CaptureAll`, env `local` and `dev` defaults, `TrustedProxies`, the request id rules, `SkipPaths` globs, `Skip`, and `ForRoute` follow the spec.
- Tests: `TestHTTPCore_HTTP13_SpoofedForwardedFor`, `TestHTTPCore_HTTP14_SessionValuesNeverKept`, `TestHTTPCore_HTTP17_ForRouteAndGlobs`, `TestHTTPCore_PAR15_SafeDefaults`.

**Verify:** `go test -race -run 'TestHTTPCore_(HTTP13|HTTP14|HTTP17|PAR15)_' ./middleware/httpcore`.
**Deps:** 11-HTTP-1. **Size:** M. **Closes:** HTTP-13, HTTP-14, HTTP-17, SPEC-G12, PAR-13, PAR-15.
**Files:**

- `middleware/httpcore/policy.go`
- `middleware/httpcore/proxy.go`
- `middleware/httpcore/options.go`
- `middleware/httpcore/policy_test.go`

#### 11-HTTP-3 Bodies
**Acceptance:**
- JSON of any shape parses into the tree, a cut or bad JSON body becomes the truncated marker, forms parse, and text is cut. Encoded bodies are skipped. `MaxBody` clamps, and small bodies use the pool.
- Tests: `TestHTTPCore_HTTP1_ArrayBodyRedacted`, `TestHTTPCore_HTTP1_CutBodyNoText`, `TestHTTPCore_HTTP1_FormPassword`, `TestHTTPCore_HTTP16_MaxBodyClamp`.

**Verify:** `go test -race -run 'TestHTTPCore_HTTP(1|16)_' ./middleware/httpcore`.
**Deps:** 11-HTTP-2. **Size:** M. **Closes:** HTTP-1, HTTP-16.
**Files:**

- `middleware/httpcore/body.go`
- `middleware/httpcore/pool.go`
- `middleware/httpcore/body_test.go`

#### 11-HTTP-4 Panics and the writer wrapper
**Acceptance:**
- `Recover500` and `Repanic` follow the spec, and `http.ErrAbortHandler` always panics again. The writer implements `Unwrap`, `Flush`, `Hijack`, and `ReadFrom` with the stated status rules.
- Tests: `TestHTTPCore_HTTP6_AbortHandlerRepanics`, `TestHTTPCore_HTTP6_ResponseControllerDeadline`, `TestHTTPCore_HTTP7_ReadFromAllocations`.

**Verify:** `go test -race -run 'TestHTTPCore_HTTP(6|7)_' ./middleware/httpcore`.
**Deps:** 11-HTTP-1. **Size:** M. **Closes:** HTTP-6, HTTP-7.
**Files:**

- `middleware/httpcore/panic.go`
- `middleware/httpcore/writer.go`
- `middleware/httpcore/writer_test.go`

#### 11-HTTP-5 Problem responses, edge cases, benchmark, and docs
**Acceptance:**
- `WriteProblem` writes RFC 9457 JSON without internal fields, and `ParseProblem` reads it back.
- HEAD, 204, 304, gzip, `application/problem+json`, a sniffed type, and future or missing `traceparent` follow the spec.
- `BenchmarkMiddleware_Realistic` is 50µs p50 or less. The package doc lists capture defaults, the proxy rule, the response changes, the emit point, and the untrusted inputs.
- Tests: `TestHTTPCore_BET6_ProblemJSON`, `TestHTTPCore_HTTP19_EdgeCases`, `BenchmarkMiddleware_Realistic`.

**Verify:** `go test -race -run 'TestHTTPCore_(BET6|HTTP19)_' ./middleware/httpcore && go run ./tools/cmd/bench -pkg ./middleware/httpcore`.
**Deps:** 11-HTTP-3, 11-HTTP-4. **Size:** M. **Closes:** BET-6, PAR-8, HTTP-10, HTTP-19, HTTP-23, SPEC-G3.
**Files:**

- `middleware/httpcore/problem.go`
- `middleware/httpcore/doc.go`
- `middleware/httpcore/edge_test.go`
- `middleware/httpcore/bench_test.go`

### Track: conformance (after 11-HTTP-5 and 11-WORK-2)

#### 11-CONF-1 Shared test helpers and the http suite
**Acceptance:**
- `Recorder`, `Normalize`, and `Diff` exist. `Normalize` removes run-varying values, including the duration text in `summary`. The `http` suite runs all 19 scenarios with a golden, a schema test, and a leak test each.
- A broken fake adapter fails each scenario it breaks with a readable diff. The suite runs in under 20 seconds.
- Tests: `TestConformance_HTTP22_BrokenAdapterFails`, `TestConformance_HTTP22_NetHTTPPasses`.

**Verify:** `go test -race -run 'TestConformance_HTTP22_' ./internal/conformance/...`.
**Deps:** 11-HTTP-5, 11-SCH-1. **Size:** L. **Closes:** HTTP-22, SPEC-G20 (suite part).
**Files:**

- `internal/conformance/harness.go`
- `internal/conformance/http/suite.go`
- `internal/conformance/http/testdata/`
- `internal/conformance/http/suite_test.go`

#### 11-CONF-2 work, calls, and log suites
**Acceptance:**
- Each suite runs its scenarios against a fake adapter of each kind, and a broken fake fails. The `work` suite runs the `Starter` and `Finisher` scenario.
- Tests: `TestConformance_WorkFakes`, `TestConformance_CallsFake`, `TestConformance_LogFake`.

**Verify:** `go test -race -run 'TestConformance_(Work|Calls|Log)' ./internal/conformance/...`.
**Deps:** 11-CONF-1. **Size:** M. **Closes:** SPEC-G20 (suites part).
**Files:**

- `internal/conformance/work/suite.go`
- `internal/conformance/calls/suite.go`
- `internal/conformance/log/suite.go`
- their tests

#### 11-CONF-3 drain suite and drain migration
**Acceptance:**
- The `drain` suite has a core part for every drain and an HTTP part for HTTP drains. It runs against every existing drain. Each drain reads the v2 shape: nested `wlog`, float durations, and `event_id`.
- Tests: `TestConformance_PIPE25_EveryDrain` covers each drain as a subtest.

**Verify:** `go test -race -run 'TestConformance_PIPE25_' ./internal/conformance/... ./drain/...`.
**Deps:** 11-CONF-1. **Size:** L. **Closes:** PIPE-25.
**Files:**

- `internal/conformance/drain/suite.go`
- `internal/conformance/drain/suite_test.go`
- the mapping file of each drain that the suite fails (one commit per drain)

### Track: setup and rebuilt adapters (after conformance)

#### 11-SET-1 setup.FromEnv
**Acceptance:**
- `FromEnv`, `With`, `WithEnv`, and `Resolve` follow SPEC-setup. `FromEnv` reads `WLOG_DRAINS` and `WLOG_OUTPUT`, core `New` reads the logger and identity vars, and code options win in any order. Service identity falls back to build info. A missing credential disables one drain and reports it.
- Tests: `TestSetup_PAR20_MissingCredentialDisables`, `TestSetup_PAR19_AliasOrder`, `TestSetup_BET16_BuildInfoService`, `TestSetup_ResolveHidesSecrets`, `TestSetup_ThirdPartyFactory`.

**Verify:** `go test -race -run 'TestSetup_' ./setup`.
**Deps:** 11-CONF-3, 11-PRE-1, 11-DEF-1. **Size:** M. **Closes:** BET-16, PAR-1, PAR-19, PAR-20.
**Files:**

- `setup/setup.go`
- `setup/identity.go`
- `setup/vars.go`
- `config.go`
- `setup/setup_test.go`

#### 11-ADP-1 http-std rebuilt
**Acceptance:**
- `wlogstd.Middleware` is `httpcore.NetHTTP`. `Setup(mux)` exists. `r.Pattern` comes from a `//go:build go1.23` file with a fallback. gorilla/mux with `router.Use` gives the route template. The `config.go` comment says `r.Pattern` needs Go 1.23.
- `http-std` and the mux example pass the `http` suite.
- Tests: `TestStd_Conformance`, `TestStd_HTTP9_MuxRouteTemplate`.

**Verify:** `go test -race -run 'TestStd_' ./middleware/nethttp && cd examples && go test -race ./mux`.
**Deps:** 11-CONF-1. **Size:** M. **Closes:** HTTP-9.
**Files:**

- `middleware/nethttp/middleware.go`
- `middleware/nethttp/pattern_go123.go`
- `middleware/nethttp/pattern_old.go`
- `examples/mux/main.go`

#### 11-ADP-2 http-echo and http-echo5 rebuilt
**Acceptance:**
- Both follow the rebuilt adapter table, including `PassErrors()` and `WriteProblem(c, err)`. Both pass the `http` suite.
- Tests: `TestEcho_Conformance`, `TestEcho_HTTP11_HTTPError400Warn`, `TestEcho_HTTP12_PanicUpdatesResponse`, `TestEcho5_Conformance`, `TestEcho5_HTTP5_NoSecondBody`.

**Verify:** `cd middleware/echo && go test -race ./... && cd ../echo5 && go test -race ./...`.
**Deps:** 11-ADP-1. **Size:** M. **Closes:** HTTP-5, HTTP-8, HTTP-11, HTTP-12 (Echo part).
**Files:**

- `middleware/echo/echo.go`
- `middleware/echo/echo_test.go`
- `middleware/echo5/echo.go`
- `middleware/echo5/echo_test.go`

#### 11-ADP-3 http-gin rebuilt
**Acceptance:**
- Gin follows the table, with `Handler(engine)` for redirects and `WriteProblem(c, err)`. It passes the `http` suite.
- Tests: `TestGin_Conformance`, `TestGin_HTTP2_PanicStopsChain`, `TestGin_HTTP3_StatusBeforeWrite`, `TestGin_HTTP4_404Status`, `TestGin_HTTP12_ErrorsBeforePanic`.

**Verify:** `cd middleware/gin && go test -race ./...`.
**Deps:** 11-ADP-1. **Size:** M. **Closes:** HTTP-2, HTTP-3, HTTP-4, HTTP-12 (Gin part).
**Files:**

- `middleware/gin/gin.go`
- `middleware/gin/gin_test.go`

#### 11-MIG-1 Migrate log outputs, trace-otel, examples, and the CLI to v2 names
**Acceptance:**
- These parts use `OnProblem`, `Starter`, `Finisher`, and the v2 keys: the zap, zerolog, and logrus outputs, `trace-otel`, the examples, the `wlog init` templates, and the `wlog map` rules.
- `CHANGELOG.md` lists every renamed or removed API under v0.6.0 with a migration note.
- Tests: the existing tests of each module, updated. `tools snippets` passes.

**Verify:** `go work sync && for m in log/zap log/zerolog log/logrus trace/otel examples cmd/wlog; do (cd $m && go test -race ./...); done && go run ./tools/cmd/snippets`.
**Deps:** 11-SET-1. **Size:** L. **Closes:** none (keeps the build green).
**Files:**

- `log/*/`
- `trace/otel/otel.go`
- `examples/`
- `cmd/wlog/internal/templates/`
- `CHANGELOG.md`

### Review point 11, v0.6.0

- [ ] Every adapter and drain passes its conformance suite.
- [ ] Every golden event is valid against `schema/event.v1.json`.
- [ ] `BenchmarkEmit_RequestEvent` and `BenchmarkMiddleware_Realistic` meet their budgets.
- [ ] Human review. Tagging v0.6.0 is ask-first.

---

## Phases 12 to 14: integration tracks

Each integration task below uses a short form. The module spec holds the full rules, and each task
names the spec criteria it proves. Every task in these phases also meets four shared conditions:

1. The module passes the conformance suite for its kind.
2. `go run ./tools/cmd/floor <module dir>` passes at the library floor and Go floor from its spec,
   and the module also builds with the newest release of its library.
3. The package doc shows the one-line setup, and `tools snippets` compiles it.
4. `wlog init` detects the library, and its adapter table row exists. `cli-init` v2 in phase 14
   uses that row.
5. A new drain adds its row to the SPEC-setup var table and its factory to `setup` or its own
   `Factory()`.

A new third-party module needs approval before its first commit, per CLAUDE.md. The approval
message for this plan lists every library and floor, so one approval can cover a whole track.

## Phase 12, v0.7: everyday stack, search, and agents

Tracks A, B, C, and F share no files, so four sessions can build them at the same time. Inside a
track, P1 tasks come first.

### Track A: HTTP routers and RPC ([SPEC-track-a.md](../docs/SPEC-track-a.md))

#### 12-A-1 http-chi
**Acceptance:** chi route patterns become `http.route`. `Setup(r)` installs the middleware. Criteria 1 and 9.
**Verify:** `cd middleware/chi && go test -race ./...`.
**Deps:** Review point 11. **Size:** S. **Files:** `middleware/chi/`.

#### 12-A-2 http-fasthttp
**Acceptance:** The fasthttp middleware follows its router row. `RequestView` and `ResponseView` clone every value from the reused buffers, and the Fiber modules can import them. Criteria 1 and 9.
**Verify:** `cd middleware/fasthttp && go test -race ./...`.
**Deps:** Review point 11. **Size:** M. **Files:** `middleware/fasthttp/`.

#### 12-A-3 http-fiber and http-fiber3
**Acceptance:** Both follow the router table on the shared fasthttp views. A Fiber 404 and a handler error log their final statuses, and response bytes match a run without wlog. Criteria 1, 6, and 9.
**Verify:** `cd middleware/fiber && go test -race ./... && cd ../fiber3 && go test -race ./...`.
**Deps:** 12-A-2. **Size:** M. **Files:** `middleware/fiber/`, `middleware/fiber3/`.

#### 12-A-4 rpc-grpc
**Acceptance:** Server and client interceptors for unary and streams. Status details map to code, message, why, link, fix, and data. Client error codes give `warn` through the status class. `UnknownServiceHandler` records unknown methods. Criteria 2, 3, and 9.
**Verify:** `cd rpc/grpc && go test -race ./...`.
**Deps:** Review point 11. **Size:** M. **Files:** `rpc/grpc/`.

#### 12-A-5 http-httprouter and http-gozero
**Acceptance:** httprouter reads the matched route path. A go-zero route that times out logs 503. Criteria 1, 8, and 9.
**Verify:** `cd middleware/httprouter && go test -race ./... && cd ../gozero && go test -race ./...`.
**Deps:** 12-A-1. **Size:** M. **Files:** `middleware/httprouter/`, `middleware/gozero/`.

#### 12-A-6 rpc-connect
**Acceptance:** Server and client interceptors. `Peer.Protocol` goes to `rpc.protocol`. A malformed body gives one event with status 400 through the HTTP middleware. Criteria 2, 4, and 9.
**Verify:** `cd rpc/connect && go test -race ./...`.
**Deps:** 12-A-4. **Size:** M. **Files:** `rpc/connect/`.

#### 12-A-7 rpc-gqlgen
**Acceptance:** The handler extension records the operation name, `rpc.graphql.type`, complexity, and resolver errors. A password in `variables` or an inline literal never appears. Criteria 5 and 9.
**Verify:** `cd rpc/gqlgen && go test -race ./...`.
**Deps:** 12-A-1. **Size:** M. **Files:** `rpc/gqlgen/`.

#### 12-A-8 http-hertz and http-kratos
**Acceptance:** Each follows its router row. Hertz redirects give one event with status 301. A Kratos gRPC server records through the `rpc-grpc` interceptors. Criteria 1, 7, and 9.
**Verify:** `cd middleware/hertz && go test -race ./... && cd ../kratos && go test -race ./...`.
**Deps:** 12-A-4. **Size:** M. **Files:** `middleware/hertz/`, `middleware/kratos/`.

#### 12-A-9 http-huma and rpc-twirp
**Acceptance:** huma uses the operation id and passes the `http` suite with the chi adapter. twirp server and client hooks pass the `work` and `calls` suites. Criteria 1, 2, and 9.
**Verify:** `cd middleware/huma && go test -race ./... && cd ../../rpc/twirp && go test -race ./...`.
**Deps:** 12-A-1, 12-A-4. **Size:** M. **Files:** `middleware/huma/`, `rpc/twirp/`.

#### 12-A-10 Recipes: rest-api and grpc-service
**Acceptance:** `docs/recipes/rest-api.md` and `docs/recipes/grpc-service.md` each have setup code, a golden event, five questions with query forms, and explain ids. Track F criterion 9.
**Verify:** `cd examples && go test -race ./rest-api ./grpc-service && go run ../tools/cmd/snippets`.
**Deps:** 12-A-1, 12-A-4, 12-F-1. **Size:** M. **Files:** `docs/recipes/`, `examples/rest-api/`, `examples/grpc-service/`.

### Track B: outbound calls and data stores ([SPEC-track-b.md](../docs/SPEC-track-b.md))

#### 12-B-1 sqlshape
**Acceptance:** The statement shaper removes every literal form in the pitfall table, stops on ambiguity, and never returns a secret. The fuzz target runs 5 minutes clean. Criterion 4.
**Verify:** `go test -race ./store/sqlshape && go test -run=xxx -fuzz=FuzzSQLShape -fuzztime=60s ./store/sqlshape`.
**Deps:** Review point 11. **Size:** M. **Files:** `store/sqlshape/`.

#### 12-B-2 client-http
**Acceptance:** The `RoundTripper` records one call per request with host and route, sends trace headers, and never reads a body. It covers resty. Criterion 1.
**Verify:** `go test -race ./client/http`.
**Deps:** Review point 11. **Size:** S. **Files:** `client/http/`.

#### 12-B-3 store-sql
**Acceptance:** `Wrap` and `WrapDriver` follow every database/sql rule on a fake driver in root tests. The untagged module `store/sql/drivertest` runs the same tests with pgx stdlib, MySQL, and modernc SQLite. Criteria 1 and 2.
**Verify:** `go test -race ./store/sql && cd store/sql/drivertest && go test -race ./...`.
**Deps:** 12-B-1. **Size:** M. **Files:** `store/sql/`, `store/sql/drivertest/`.

#### 12-B-4 store-pgx and store-gorm
**Acceptance:** The pgx tracer covers query, batch, and copy. gorm over pgx stdlib with both `store-gorm` and `store-sql` records each query once. Criteria 1, 3, and 7.
**Verify:** `cd store/pgx && go test -race ./... && cd ../gorm && go test -race ./...`.
**Deps:** 12-B-3. **Size:** M. **Files:** `store/pgx/`, `store/gorm/`.

#### 12-B-5 store-redis
**Acceptance:** The hook records commands and pipelines. A `GET` miss has status `miss` and no error. Criteria 1, 5, and 7.
**Verify:** `cd store/redis && go test -race ./...`.
**Deps:** Review point 11. **Size:** S. **Files:** `store/redis/`.

#### 12-B-6 store-mongo and client-aws
**Acceptance:** The mongo monitor wraps an existing monitor and copies no documents by default. The AWS middleware records one call with `attempts` across retries. Criteria 1, 5, 6, and 7.
**Verify:** `cd store/mongo && go test -race ./... && cd ../../client/aws && go test -race ./...`.
**Deps:** Review point 11. **Size:** M. **Files:** `store/mongo/`, `client/aws/`.

#### 12-B-7 store-bun
**Acceptance:** The query hook shapes statements with inlined args. Criteria 1 and 7.
**Verify:** `cd store/bun && go test -race ./...`.
**Deps:** 12-B-1. **Size:** S. **Files:** `store/bun/`.

### Track C: logger bridges, error libraries, and flags ([SPEC-track-c.md](../docs/SPEC-track-c.md))

#### 12-C-1 log-slog
**Acceptance:** The handler passes `testing/slogtest` for folded records and the `log` suite. Criteria 1 and 2. Closes HTTP-18.
**Verify:** `go test -race ./log/slog`.
**Deps:** Review point 11. **Size:** M. **Files:** `log/slog/`.

#### 12-C-2 log-logr and log-zap
**Acceptance:** logr works in both directions, and klog `FromContext(ctx).V(2).Info` folds with the plugin only. The zap input core folds, `zap.Any("ctx", ctx)` leaks nothing, and a zap sampler still writes a line with no event. The zap output never attaches the drain stack. Criteria 2 to 5 and 10. Closes HTTP-20 (zap part).
**Verify:** `cd log/logr && go test -race ./... && cd ../zap && go test -race ./...`.
**Deps:** 12-C-1. **Size:** M. **Files:** `log/logr/`, `log/zap/`.

#### 12-C-3 log-zerolog, log-logrus, and log-hclog
**Acceptance:** `zerolog.Ctx(ctx)` folds. A logrus entry with `WithContext(ctx)` folds, and the wrapped logger writes nothing for it. hclog `FromContext(ctx).Named("db")` folds. Output collisions move to `wlog.fields`. Criteria 2, 3, 6, and 10. Closes HTTP-20.
**Verify:** `for m in zerolog logrus hclog; do (cd log/$m && go test -race ./...); done`.
**Deps:** 12-C-1. **Size:** L. **Files:** `log/zerolog/`, `log/logrus/`, `log/hclog/`.

#### 12-C-4 log-std
**Acceptance:** The stdlib `log` writer turns each line into a plain event through the default Logger. Criterion 2.
**Verify:** `go test -race ./log/std`.
**Deps:** 12-C-1. **Size:** S. **Files:** `log/std/`.

#### 12-C-5 errors-validator and errors-oops
**Acceptance:** A validator error records `field`, `tag`, and `param`, never the value. An oops error with a request leaks no header or body. Criteria 7, 8, and 10.
**Verify:** `cd errors/validator && go test -race ./... && cd ../oops && go test -race ./...`.
**Deps:** Review point 11. **Size:** M. **Files:** `errors/validator/`, `errors/oops/`.

#### 12-C-6 errors-cockroach and flag-openfeature
**Acceptance:** cockroach safe details and stacks map to `ErrorInfo`. An OpenFeature missing flag records `reason` `ERROR` and `error_code` `FLAG_NOT_FOUND`, and a panicking step never reaches the SDK. Criteria 9 and 10.
**Verify:** `cd errors/cockroach && go test -race ./... && cd ../../flag/openfeature && go test -race ./...`.
**Deps:** Review point 11. **Size:** M. **Files:** `errors/cockroach/`, `flag/openfeature/`.

### Track F: search and agents ([SPEC-track-f.md](../docs/SPEC-track-f.md))

#### 12-F-1 `wlog query` filters and output
**Acceptance:** Sources, every filter flag, the four formats, `--limit`, exit codes, and the skipped-line count follow the spec. The filter language lives in the root package `query`, stdlib only. A 1 GB fixture reads in under 10 seconds. Criteria 1 and 10. Closes BET-2 (query part).
**Verify:** `cd cmd/wlog && go test -race -run 'TestQuery_' ./... && cd ../.. && go test -race ./query && go test -run=xxx -bench=BenchmarkQuery1GB ./query`.
**Deps:** Review point 11. **Size:** M. **Files:** `query/`, `cmd/wlog/query.go`, `cmd/wlog/testdata/query/`.

#### 12-F-2 Group, stats, size, and tail
**Acceptance:** `--group-by`, `--count`, `--stats`, and `--size` match hand-computed goldens. `wlog tail` follows appends and rotation. Criteria 2 and 3. Closes BET-2, BET-21.
**Verify:** `cd cmd/wlog && go test -race -run 'TestQuery_(Group|Stats|Size)|TestTail_' ./...`.
**Deps:** 12-F-1. **Size:** M. **Files:** `query/aggregate.go`, `cmd/wlog/size.go`, `cmd/wlog/tail.go`, tests.

#### 12-F-3 drain-memory query endpoint and SSE v2
**Acceptance:** `QueryHandler` uses the root `query` package, returns the newest N, and takes level lists. SSE v2 sends hello, ping, and event frames, honors `since`, and needs a token off loopback. `wlog query --url` returns the same events as the file query. Criterion 4. Closes BET-20, PAR-22, PAR-23.
**Verify:** `go test -race -run 'TestMemory_(Query|SSE)' ./drain/memory && cd cmd/wlog && go test -race -run 'TestQuery_URL' ./...`.
**Deps:** 12-F-1. **Size:** M. **Files:** `drain/memory/query.go`, `drain/memory/sse.go`, tests.

#### 12-F-4 `wlog explain`, `rules`, `schema`, and `version`
**Acceptance:** Every problem code, doctor code, rule id, catalog code, reserved field, and setup var has an entry. A test walks each source to prove it. `--json` works on each command. Criterion 5. Closes BET-8.
**Verify:** `cd cmd/wlog && go test -race -run 'TestExplain_|TestRules_|TestSchemaCmd_|TestVersion_' ./...`.
**Deps:** 12-F-1. **Size:** M. **Files:** `cmd/wlog/explain.go`, `cmd/wlog/internal/explain/`, tests.

#### 12-F-5 Agent docs
**Acceptance:** `make docs` builds `llms.txt` and `llms-full.txt` with identical bytes on a second run. Skills use `<name>/SKILL.md` with an index, and CLAUDE.md imports AGENTS.md. Every skill snippet compiles, and every shell snippet runs against a fixture app. Criteria 6 and 7. Closes BET-3, BET-11, PAR-35, PAR-36.
**Verify:** `make docs && git diff --exit-code llms.txt llms-full.txt && cd cmd/wlog && go test -race -run 'TestAgentDocs_' ./...`.
**Deps:** 12-F-4. **Size:** M. **Files:** `tools/cmd/docs/`, `llms.txt`, `llms-full.txt`, `cmd/wlog/internal/templates/skills/`, `AGENTS.md`.

#### 12-F-6 Search recipes
**Acceptance:** Every file in the search-recipes table exists except the phase 14 Elastic template. The jq cookbook gives golden output under gojq. Dashboards and templates load as JSON or SQL text. `tools` adds `github.com/itchyny/gojq`, which needs approval first. Criterion 8. Closes BET-21 (recipes part).
**Verify:** `cd tools && go test -race -run 'TestSearchRecipes_' ./...`.
**Deps:** 12-F-1. **Size:** M. **Files:** `integrations/search/`, `tools/searchrecipes_test.go`.

#### 12-F-7 `wlog mcp`
**Acceptance:** The stdio server exposes `events_query`, `events_by_request_id`, `events_by_trace_id`, `map_entry`, `explain`, `redact_check`, and `schema_event` through the root `query` package. The go-sdk client calls each tool in a test. `cmd/wlog` adds `github.com/modelcontextprotocol/go-sdk` v1.8.0, which needs approval first. Track G criterion 5. Closes BET-12.
**Verify:** `cd cmd/wlog && go test -race -run 'TestMCP_' ./...`.
**Deps:** 12-F-3, 12-F-4. **Size:** M. **Files:** `cmd/wlog/mcp.go`, `cmd/wlog/internal/mcpserver/`, tests.

#### 12-F-8 Recipe: cli-tool
**Acceptance:** `docs/recipes/cli-tool.md` follows the four recipe parts, with `cmd/wlog` itself as the example until `command-cobra` lands. Criterion 9.
**Verify:** `cd examples && go test -race ./cli-tool && go run ../tools/cmd/snippets`.
**Deps:** 12-F-2. **Size:** S. **Files:** `docs/recipes/cli-tool.md`, `examples/cli-tool/`.

#### 12-F-9 `wlog doctor` additions
**Acceptance:** `doctor` warns about a global logger call inside a handler. It reports each installed adapter from tracks A to C with its setup line. Each finding has a `WLOG_DOCTOR_*` code that `wlog explain` knows.
**Verify:** `cd cmd/wlog && go test -race -run 'TestDoctor_Tracks' ./...`.
**Deps:** 12-F-4. **Size:** M. **Closes:** PAR-34 (tracks part). **Files:** `cmd/wlog/internal/doctor/`, tests.

### Review point 12, v0.7.0

- [ ] Every Track A, B, and C module passes its suite and its floor.
- [ ] `wlog query`, `wlog explain`, and `wlog mcp` answer the five recipe questions on the fixtures.
- [ ] Human review. Tagging v0.7.0 and every new module tag is ask-first.

## Phase 13, v0.8: messages, jobs, functions, and commands ([SPEC-track-d.md](../docs/SPEC-track-d.md))

The tasks share no files. Kafka tasks come first, because the user named Kafka.

#### 13-D-1 queue-kafkago
**Acceptance:** `Consume` uses `FetchMessage` and commits after success. A failed handler does not commit, and the next fetch sees the same offset. The `Writer` wrapper adds headers, and `Drain(w)` ships events. `Factory()` reads `WLOG_KAFKA_BROKERS` and `WLOG_KAFKA_TOPIC`. Criteria 1 and 2.
**Verify:** `cd queue/kafkago && go test -race ./...`.
**Deps:** Review point 12. **Size:** M. **Files:** `queue/kafkago/`.

#### 13-D-2 queue-sarama and queue-franz
**Acceptance:** sarama `Handler(fn)` owns the claim loop and marks after success, and the producer wrappers add headers. franz `Hooks()` records produce calls, and `Record(r)` helps the poll loop. Criterion 1.
**Verify:** `cd queue/sarama && go test -race ./... && cd ../franz && go test -race ./...`.
**Deps:** Review point 12. **Size:** M. **Files:** `queue/sarama/`, `queue/franz/`.

#### 13-D-3 queue-confluent
**Acceptance:** cgo files build and test with cgo, and the cgo-free build passes with only `doc.go`. Criteria 1 and 10.
**Verify:** `cd queue/confluent && CGO_ENABLED=1 go test -race ./... && CGO_ENABLED=0 go build ./...`.
**Deps:** 13-D-1. **Size:** M. **Files:** `queue/confluent/`.

#### 13-D-4 queue-watermill
**Acceptance:** Router middleware and publisher decorator follow the table. A PoisonQueue failure records `result` `dead_letter` and level `error`. Criteria 1 and 3.
**Verify:** `cd queue/watermill && go test -race ./...`.
**Deps:** Review point 12. **Size:** M. **Files:** `queue/watermill/`.

#### 13-D-5 queue-sqs
**Acceptance:** `Receive` requests the receive count and sent time, deletes on success, and leaves a failed message. A third receive records `delivery_count` 3. SNS and SQS publish record calls with a `traceparent` attribute. Criteria 1 and 4.
**Verify:** `cd queue/sqs && go test -race ./...`.
**Deps:** Review point 12. **Size:** M. **Files:** `queue/sqs/`.

#### 13-D-6 queue-nats and queue-amqp
**Acceptance:** NATS core and JetStream handlers ack or nak per the table, and `Drain(nc, subject)` ships events, with `Factory()` for `WLOG_NATS_URL` and `WLOG_NATS_SUBJECT`. JetStream fields go under `messaging.nats`. AMQP acks on success and nacks with `RequeueOn`. Criterion 1.
**Verify:** `cd queue/nats && go test -race ./... && cd ../amqp && go test -race ./...`.
**Deps:** Review point 12. **Size:** M. **Files:** `queue/nats/`, `queue/amqp/`.

#### 13-D-7 queue-pubsub and queue-cloudevents
**Acceptance:** Pub/Sub `Receive` acks and nacks, and `Publish` clones attributes. CloudEvents observability recovers its own panic, and `EventDefaulter` sets `traceparent`. Criterion 1.
**Verify:** `cd queue/pubsub && go test -race ./... && cd ../cloudevents && go test -race ./...`.
**Deps:** Review point 12. **Size:** M. **Files:** `queue/pubsub/`, `queue/cloudevents/`.

#### 13-D-8 job-asynq and job-river
**Acceptance:** asynq middleware and `Enqueue` with task headers follow the table. A River snooze records `result` `snooze` and level `info`. Criteria 1 and 5.
**Verify:** `cd job/asynq && go test -race ./... && cd ../river && go test -race ./...`.
**Deps:** Review point 12. **Size:** M. **Files:** `job/asynq/`, `job/river/`.

#### 13-D-9 job-temporal and job-cron
**Acceptance:** A Temporal workflow replay emits no event, and each activity attempt emits one with `job.attempt` and `job.temporal` fields. The cron wrapper emits one event per run with `job.schedule`. Criteria 1 and 6.
**Verify:** `cd job/temporal && go test -race ./... && cd ../cron && go test -race ./...`.
**Deps:** Review point 12. **Size:** M. **Files:** `job/temporal/`, `job/cron/`.

#### 13-D-10 faas-lambda
**Acceptance:** `Wrap` covers API Gateway v1 and v2, ALB, SNS, EventBridge, and the batch sources. `ProcessSQS` returns the failing record id in `BatchItemFailures`. Events survive three warm invocations and a panic. `examples/lambda` moves to a recipe. Criteria 1, 7, and 8.
**Verify:** `cd faas/lambda && go test -race ./...`.
**Deps:** Review point 12. **Size:** L. **Files:** `faas/lambda/`, `examples/lambda/` (removed), `docs/recipes/lambda.md`.

#### 13-D-11 faas-gcf
**Acceptance:** HTTP and CloudEvent functions emit one event each through `work` and `http-core`. Criterion 1.
**Verify:** `cd faas/gcf && go test -race ./...`.
**Deps:** 13-D-7, 13-D-10. **Size:** S. **Files:** `faas/gcf/`.

#### 13-D-12 command-cobra, command-urfave, and command-kong
**Acceptance:** Each records `cli.path`, flag names, and the exit code, and flushes before exit. A cobra `RunE` error, a urfave exit-coder error, and a kong parse error each give the right code. With `WLOG_DRAINS` set, `cmd/wlog` records its own runs and prints nothing extra. Without it, no Logger is built. Criteria 1 and 9. Closes BET-24, PAR-38.
**Verify:** `for m in cobra urfave kong; do (cd command/$m && go test -race ./...); done && cd cmd/wlog && go test -race -run 'TestSelfEvent_' ./...`.
**Deps:** Review point 12. **Size:** L. **Files:** `command/cobra/`, `command/urfave/`, `command/kong/`, `cmd/wlog/main.go`.

#### 13-D-13 Recipes: kafka-consumer, cron-job, and lambda
**Acceptance:** Each recipe follows the four parts, and its golden event is valid against the schema. Track F criterion 9.
**Verify:** `cd examples && go test -race ./kafka-consumer ./cron-job ./lambda && go run ../tools/cmd/snippets`.
**Deps:** 13-D-1, 13-D-9, 13-D-10. **Size:** M. **Files:** `docs/recipes/`, `examples/kafka-consumer/`, `examples/cron-job/`, `examples/lambda/`.

### Review point 13, v0.8.0

- [ ] Every Track D module passes its suites and its floor. `queue-confluent` passes with and without cgo.
- [ ] Human review. Tagging v0.8.0 and the new module tags is ask-first.

## Phase 14, v0.9: destinations, OpenTelemetry, and AI

Tracks E and G share no files. `cli-init` v2 comes last.

### Track E: destinations ([SPEC-track-e.md](../docs/SPEC-track-e.md))

#### 14-E-1 pipeline.PartialError
**Acceptance:** The worker drops and retries by index, ignores an index outside the batch, and counts an index in both lists as dropped. Criterion 1.
**Verify:** `go test -race -run 'TestPipeline_PartialError' ./pipeline`.
**Deps:** Review point 13. **Size:** S. **Files:** `pipeline/partial.go`, `pipeline/pipeline.go`, `pipeline/partial_test.go`.

#### 14-E-2 trace-otel spans and metrics
**Acceptance:** `Plugin` replaces the unit's trace ids with a recording span's ids through `propagate.ContextWith`. It adds span attributes, status, and one exception event, and records `http.server.request.duration` and `wlog.work.duration` before sampling. The operation cap reports once. Stats counters exist. If the OTel middleware sits inside wlog, `wlog doctor` reports a warning. The go line drops to 1.21 with otel v1.20.0. Criteria 2, 3, and 11. Closes HTTP-21.
**Verify:** `cd trace/otel && go test -race ./... && cd ../.. && go run ./tools/cmd/floor ./trace/otel`.
**Deps:** 14-E-1. **Size:** M. **Files:** `trace/otel/`.

#### 14-E-3 trace-otellog
**Acceptance:** `New` emits one record per event with severity, body, event name, and attributes from the `otel` preset. Without a span on `ctx`, the record still holds the trace id. Criteria 4 and 11.
**Verify:** `cd trace/otellog && go test -race ./...`.
**Deps:** 14-E-2. **Size:** M. **Files:** `trace/otellog/`.

#### 14-E-4 metrics-prometheus
**Acceptance:** The histogram, `Register` reuse, `GetMetricWithLabelValues`, the operation cap, and `StatsCollector` follow the spec. A golden exposition matches. Criteria 5 and 11.
**Verify:** `cd metrics/prometheus && go test -race ./...`.
**Deps:** 14-E-1. **Size:** M. **Files:** `metrics/prometheus/`.

#### 14-E-5 drain-honeycomb and drain-newrelic
**Acceptance:** Both match their golden bodies from vendor docs. Honeycomb maps positional statuses to exact `Retry` and `Dropped` sets and honors `WithSpans`. New Relic moves reserved user keys and keeps 255 attributes. Criteria 6, 7, and 12.
**Verify:** `go test -race ./drain/honeycomb ./drain/newrelic`.
**Deps:** 14-E-1. **Size:** M. **Files:** `drain/honeycomb/`, `drain/newrelic/`.

#### 14-E-6 drain-elastic
**Acceptance:** Bulk bodies use `create` and the ECS preset. Item results map to exact sets, and `error.reason` is never read. `Template` works on Elasticsearch 8 and OpenSearch 2 in the integration test. `integrations/search/elastic/index-template.json` equals `Template(Elasticsearch)`. Criteria 6, 7, 10, and 12.
**Verify:** `go test -race ./drain/elastic && make integration`.
**Deps:** 14-E-1. **Size:** M. **Files:** `drain/elastic/`, `integrations/search/elastic/index-template.json`, `docker-compose.integration.yml`.

#### 14-E-7 drain-splunk and drain-victorialogs
**Acceptance:** Splunk sends the channel header, maps HEC codes, splits on code 6, and reports `WLOG_DRAIN_BACKPRESSURE` for codes 24 and 25. VictoriaLogs rejects high-cardinality stream fields and drops lines over the cap. Both pass integration tests. Criteria 6, 7, 10, and 12.
**Verify:** `go test -race ./drain/splunk ./drain/victorialogs && make integration`.
**Deps:** 14-E-1. **Size:** M. **Files:** `drain/splunk/`, `drain/victorialogs/`, `docker-compose.integration.yml`.

#### 14-E-8 drain-syslog
**Acceptance:** Frames parse with the RFC 5424 ABNF test parser. TLS and TCP use octet counting on loopback listeners. A large UDP event arrives as its summary form. The SD-ID rule rejects a bad id. Criteria 8 and 12.
**Verify:** `go test -race ./drain/syslog`.
**Deps:** 14-E-1. **Size:** M. **Files:** `drain/syslog/`.

#### 14-E-9 drain-cloudwatch
**Acceptance:** Batches sort and split by count, bytes, and span. Rejected ranges map to exact dropped sets. `ResourceNotFoundException` creates the stream once. `Factory(client)` works with setup. Criteria 9, 11, and 12.
**Verify:** `cd drain/cloudwatch && go test -race ./...`.
**Deps:** 14-E-1. **Size:** M. **Files:** `drain/cloudwatch/`.

### Track G: AI and agents ([SPEC-track-g.md](../docs/SPEC-track-g.md))

#### 14-G-1 llm additions
**Acceptance:** `Record` v2 gains `Steps` and `OutputTokensPerSecond`. The snake_case keys for new fields, both cache write prices, and the token invariants per provider follow the spec. The Sonnet 4.6 example prices at 30,150 micros. Criterion 2. Closes BET-23, PAR-27.
**Verify:** `go test -race ./llm`.
**Deps:** Review point 13. **Size:** M. **Files:** `llm/`.

#### 14-G-2 ai-anthropic and ai-openai
**Acceptance:** Typed helpers match hand-written goldens. Stream observers give the same `Record` as full responses, keep no text, and never block the stream. Middleware reads request ids and retry counts. Criteria 1 and 3.
**Verify:** `cd ai/anthropic && go test -race ./... && cd ../openai && go test -race ./...`.
**Deps:** 14-G-1. **Size:** L. **Files:** `ai/anthropic/`, `ai/openai/`.

#### 14-G-3 ai-genai and ai-goopenai
**Acceptance:** Same as 14-G-2 for these SDKs. `Transport(next)` wraps and never replaces the authenticated transport. Criteria 1 and 3.
**Verify:** `cd ai/genai && go test -race ./... && cd ../goopenai && go test -race ./...`.
**Deps:** 14-G-1. **Size:** M. **Files:** `ai/genai/`, `ai/goopenai/`.

#### 14-G-4 ai-langchaingo and ai-eino
**Acceptance:** Callback handlers record calls and usage. The eino handler drains and closes stream copies. Criterion 1.
**Verify:** `cd ai/langchaingo && go test -race ./... && cd ../eino && go test -race ./...`.
**Deps:** 14-G-1. **Size:** M. **Files:** `ai/langchaingo/`, `ai/eino/`.

#### 14-G-5 ai-mcpsdk and ai-mcpgo
**Acceptance:** Both use `{service}/{method}` operations and `rpc.mcp.*` fields. They give identical events for a tool call, a tool error, an unknown tool, and an `input_required` result. The mcp-go hook map expires after 5 minutes and holds 10,000 entries at most. Criterion 4.
**Verify:** `cd ai/mcpsdk && go test -race ./... && cd ../mcpgo && go test -race ./...`.
**Deps:** 14-G-1. **Size:** M. **Files:** `ai/mcpsdk/`, `ai/mcpgo/`.

#### 14-G-6 Recipes: llm-agent and mcp-server
**Acceptance:** Both recipes follow the four parts, and their golden events are valid against the schema. Track F criterion 9.
**Verify:** `cd examples && go test -race ./llm-agent ./mcp-server && go run ../tools/cmd/snippets`.
**Deps:** 14-G-2, 14-G-5. **Size:** M. **Files:** `docs/recipes/`, `examples/llm-agent/`, `examples/mcp-server/`.

#### 14-G-7 `wlog init` v2
**Acceptance:** `init` matches every module in `go.mod` to the adapter table, writes one `wlog_setup.go`, and edits entry points through `go/ast`. `--yes` and `--json` work. It runs `go build` and `wlog doctor` at the end. On the `llm-agent` and `mcp-server` recipe apps, the result builds and passes doctor. Track G criterion 6. Closes PAR-33.
**Verify:** `cd cmd/wlog && go test -race -run 'TestInitV2_' ./...`.
**Deps:** 14-G-6, every phase 12 and 13 module. **Size:** L. **Files:** `cmd/wlog/init.go`, `cmd/wlog/internal/initgen/`, `cmd/wlog/internal/adapters/table.go`.

### Review point 14, v0.9.0

- [ ] Every Track E and G module passes its tests, suites, and floor.
- [ ] `wlog init --yes` works on every recipe app.
- [ ] Human review. Tagging v0.9.0 and the new module tags is ask-first.

## Phase 15, v1.0.0: API freeze

#### 15-1 API freeze
**Acceptance:** `tools release` holds an apidiff baseline for every module. CI fails on an incompatible change in a module at v1. Each exported identifier has a doc comment that explains its flow.
**Verify:** `go run ./tools/cmd/release -dry-run -apidiff`.
**Deps:** Review point 14. **Size:** M. **Files:** `tools/cmd/release/`, `api/*.txt`.

#### 15-2 Parity and comparison pages
**Acceptance:** `docs/evlog-parity.md` is rewritten against evlog at release time. Comparison pages against slog, zap, zerolog, the OTel logs bridge, and evlog state only measured or tested facts. Closes PAR-37, BET-25.
**Verify:** `go run ./tools/cmd/snippets && go run ./tools/cmd/ste docs/*.md`.
**Deps:** 15-1. **Size:** M. **Files:** `docs/evlog-parity.md`, `docs/compare/`.

#### 15-3 Audit close-out
**Acceptance:** Each audit id is closed by a merged task or has a recorded decision in CHANGELOG. `tools verifyplan` maps every id to a passing test name. BET-1 is closed because its listed fixes are closed.
**Verify:** `go run ./tools/cmd/verifyplan -audit tasks/audit-2026-09-16.md`.
**Deps:** 15-2. **Size:** S. **Closes:** BET-1. **Files:** `CHANGELOG.md`, `tools/cmd/verifyplan/`.

### Review point 15, v1.0.0

- [ ] Every CI job, integration test, benchmark budget, and floor test passes.
- [ ] Human review. Tagging v1.0.0 is ask-first.

---

## Parallel work

| Phase | Runs at the same time | Must stay in order |
|---|---|---|
| 10 | redact, core, CLI, and each drain task after 10-PIPE-7 | repo-ci first. pipeline after 10-CORE-7. audit after 10-CORE-5. docs last |
| 11 | problems, default, and calls after the shape tasks. The four preset tasks. The three rebuilt adapters | shape 1 to 5 in order. http-core after work. conformance before setup and adapters |
| 12 | Tracks A, B, C, and F | 12-B-1 before the SQL stores. 12-C-1 before other bridges. 12-F-1 before other Track F tasks |
| 13 | Every Track D task | 13-D-13 after the modules its recipes use |
| 14 | Tracks E and G | 14-E-1 before drains. 14-G-1 before AI modules. 14-G-7 last |

Files that several sessions touch get their own commit: `go.work`, `CHANGELOG.md`, the `wlog init`
adapter table, and `docker-compose.integration.yml`. A session rebases before it edits one of
them.

## Risks and mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Event shape v2 breaks every adapter, drain, and example at once | High | Phase 11 migrates drains in 11-CONF-3 and the rest in 11-MIG-1 before its tag. CHANGELOG gives a migration note per break, and tags stay v0.x |
| A library floor in a spec is wrong | Medium | `tools floor` builds each module at its floor and at the newest release in CI. A wrong floor fails the task, and the spec changes first |
| A vendor fact is marked UNVERIFIED in the research | Medium | Each such drain handles the fact conservatively, says so in its package doc, and runs an integration test where a container exists |
| `otel/log` is v0 and changes in a minor release | Medium | It lives in its own module, `trace-otellog`, and the nightly job builds it against the newest release |
| Upstream semantic convention names move, such as `gen_ai.*` and `rpc.*` | Low | The preset pins semconv 1.43.0 names in test data, and a change is a reviewed diff |
| A benchmark budget fails: 3µs finalize or 50µs middleware | Medium | The budget test runs inside the task that adds the cost. A miss stops the task for a human decision |
| `Measurer` adds cost to every event, sampled or not | Medium | It reads reserved fields only. 11-SHAPE-5 adds `BenchmarkEmit_WithMeasurer` to the bench gate |
| About 60 modules are too many to maintain | Medium | Conformance suites carry most of the test weight. P3 modules can move to "designed for" at a review point |
| Parallel sessions collide in shared files | Low | Shared files get their own commits, and each track owns its directories |

## Open questions for approval

These need a human answer before the phase that uses them. None blocks phase 10.

1. Approve the drafted specs in the CAPABILITIES.md Specs table.
2. Approve new dependencies: `santhosh-tekuri/jsonschema/v6` and `itchyny/gojq` in `tools`,
   `modelcontextprotocol/go-sdk` v1.8.0 in `cmd/wlog`, and each library named in the track specs
   at its floor.
3. Approve the capability map changes: `trace-otellog` split from `trace-otel`,
   `drain-cloudwatch` without `client-aws`, `errors-pkg` and `log-charm` removed,
   `pipeline/httpdrain` made public, the new root packages `query` and `store/sqlshape`, and
   `http-fasthttp` raised to P1.
4. Approve the reserved key changes in SPEC-core-v2: `kind`, `message`, `call_stats`, `audit` as an
   array, `feature_flags` as an array, and the three `llm` key renames. Also `http.scheme` and
   `http.host` in default HTTP capture.
5. Approve the default denylist additions from RED-3 in SPEC-hardening.
