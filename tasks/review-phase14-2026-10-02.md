# Review: phase 14 (Tracks E and G), 2026-10-02

Scope: the 20 tasks 14-E-1 to 14-G-7 (commits after 308a639, up to cfc093f), their shared-code
changes, and every gate. The range holds 22 commits, 131 files, and 14,315 added lines. Six
reviewers read the code against `docs/SPEC-track-e.md` and `docs/SPEC-track-g.md`. Each finding
comes from the code, a gate run, a CI log, or a throwaway probe test. Each probe ran in the
scratchpad through `go test -overlay` or a scratch copy. The lead read the code of ten high
findings again, and each one holds. The review changed no repository file. The only exception is this report.

## Result

Phase 14 is not ready for the v0.9.0 tag. Three local gates are red at HEAD. CI was red on every
phase 14 push, and the last 8 commits are not on the remote. Review point 14 fails on two of its
three lines: `tools floor` fails, and `wlog init --yes` fails on 3 of 8 recipe apps.

The `Verify` command of every phase 14 task passes. But the passing tests hide real bugs, as in
phase 13. After merging the duplicates across reviewers, the review found 43 high, 69 medium,
and 29 low findings. Most high findings are in behavior that no test exercises. The reviewers
broke the code on purpose in more than 60 places, and every test still passed.

The phase has two halves, and the commit trailers show the border.

- Tasks 14-E-1 to 14-G-2b are tidy, pinned, and pushed.
- Tasks 14-G-3a to 14-G-7 are the last 8 commits. They skipped `make tidy`, the floor pins,
  `make lint`, and the push. Every red local gate except X-2 comes from this half.

Phase 14 changed or exposed these pieces of shared code.

| Shared change | Commit | Effect |
|---|---|---|
| `pipeline.PartialError` and `dropPartial` | 972f8be | The accounting is correct. A typed nil kills the worker (P-11), and no code reports `WLOG_DRAIN_DROPPED` (P-7) |
| `httpdrain.PostFor`, and `Post` now calls it | 708c767 | If the body read fails, every older drain now fails a 2xx (P-10). A non-2xx body is never returned, so the Splunk codes never run (D-1) |
| `eventView.Fields()` in root `summary.go` | 7540c6a | A new exported root method with no spec. It hands the live event map to a Finisher (O-10) |
| `wlogotel.Enricher` removed | 7540c6a | The spec names this break. The release dry run shows it. CHANGELOG has no entry (X-7) |
| `integrations/search/elastic/index-template.json` | e761632 | Breaks the phase 12 test `TestSearchRecipes_Files` (X-2) |
| Splunk and VictoriaLogs in the Compose stack | 9095841 | The Splunk container never becomes healthy, so the nightly job runs no integration test (X-4) |
| `flattenInto` in `internal/conformance/harness.go` reads `[]any` | af7658b | An empty array now equals a missing key, and no test covers the change (D-22) |
| `wlog init` writes `wlog_setup.go` and not `wlog.go` | cfc093f | The v1 contract changed with no spec entry (I-4, I-15) |
| `measureOf` and `propagate.Extract` (older code, first used by phase 14 metrics and spans) | none | Metrics record every request as kind `work` (O-1). The OTel ids are replaced (O-2) |

## Gate results

| Gate | Result |
|---|---|
| `go build ./...` | pass |
| `verifyplan -only 14-` | pass |
| Race tests of all 69 modules with `-count=1` | fail in 2 modules. `tools`: `TestSearchRecipes_Files` (X-2). Root: `TestHoneycomb_Options`, a flaky test (P-12) |
| `make race` with a warm test cache | reports pass. The result for `tools` is a stale cache entry (T-1) |
| `make lint` | fail, 3 issues in `examples`, `ai/genai`, and `ai/goopenai` (X-5) |
| `tools tidy -check` | fail in 6 modules (X-3) |
| `tools requires`, `ste`, `snippets`, `pkgstate` | pass |
| `tools cover -min 85` (root module only) | pass. Not gated for own modules: otellog 47.2%, otel 63.0%, prometheus 76.1%, mcpsdk 77.2%, mcpgo 77.2%, `cmd/init` 81.0% (X-8) |
| `make map` | pass, score 100 |
| `tools floor -libs` | fail. `tools` fails on X-2. `flag/openfeature` failed once under load and passes alone (T-5) |
| `make fuzz FUZZTIME=20s` | pass, 5 targets |
| `tools vuln` | fail. Standard library issues of go1.26.1 and the known `grpc` v1.84.0 issue in `examples`, as in phase 13. No phase 14 dependency adds one |
| `tools release -version v0.9.0` (dry run) | pass. 69 tags. One incompatible change: `trace/otel` `Enricher` removed |
| CI on `main` | fail on all 14 phase 14 pushes. 8 commits are not pushed (X-1) |
| Nightly `integration` job | fail every night. Since 9095841 the Splunk container is unhealthy, so no test runs (X-4) |
| Nightly `fuzz-long` job | pass since 2026-09-24. Two older crash issues are open with no cause (T-3) |
| `make integration` on this machine | not run |
| `wlog init --yes` on recipe copies | fail on 3 of 8: `llm-agent`, `lambda`, `rest-api` (I-1, I-2) |
| `make docs` in a scratch copy | changes `llms.txt` and `llms-full.txt`. The tracked files are stale (X-7) |

## Decisions for you

These findings need an owner decision before a fix, because each one changes a spec rule, adds
public API, or touches an ask-first boundary.

1. The `llm.Record` shape (L-9). The spec names a `calls` entry from `llm.Add` and seven more
   fields. Plan task 14-G-1 asks for two fields only. Build the spec, or cut the spec to the plan.
2. The middleware body tee (L-10). The spec gives it a 1 MiB cap and `usage_unknown`. No module
   has a tee. Build it, or remove the rule.
3. The reach of `wlog init` (I-7). It wires 8 HTTP frameworks and only lists the other 60 rows.
   The spec says it "installs each plugin". Narrow the spec text, or build the wiring.
4. The call kind `agent` (A-5). The event schema rejects it. Use `other`, or add `agent` to the
   schema and SPEC-core. A new enum value is ask-first.
5. `rpc.mcp.session_id` (A-13). The default denylist masks it in every event. Store a hash
   under another key, or drop the field from the spec table.
6. New core API. L-1 needs one helper that updates a group under the event lock. O-10 already
   added `Fields()` with no spec. Approve both and record them in SPEC-core-v2, or find another
   way.
7. The drain counters (O-5). No drain reports stats, so the counters never export. Make the
   pipeline wrapper a `StatsReporter`, or cut the counters from both modules and the spec.
8. The package name `wlogopenai` (L-21). Two modules use it. Rename `ai/goopenai` before the
   first tag, or accept the import alias.
9. "Identical events" for the two MCP modules (A-11). Choose one of the two: the `error` group
   is in the comparison, or it is not.
10. Prices (L-19). No Gemini row exists, so every `ai/genai` call sets `cost_unknown`. The
    prefix match prices `gpt-4.1-mini` as `gpt-4.1`. A new price row is ask-first.
11. The 8 local commits (X-1). Pushing to `main` is allowed, but the tidy gate fails on them.
    Fix X-3 first, then push.

## Suggested fix order

1. Make the gates green and push: X-2, X-3, X-5, X-6, then X-1. Fix the Compose stack (X-4).
2. Stop lost and repeated events in the drains: P-1, D-1, D-3, D-2, P-2, P-3, D-10, P-4, D-4.
3. Fix the shared code that the new modules read: O-1, O-2, O-3, L-1, L-8.
4. Fix wrong counts and costs: L-3, L-4, L-5, L-7, A-4, A-8, A-9. Fix the masking gap L-11.
5. Stop the MCP panics and the lost events: A-1, A-2, A-3.
6. Make `wlog init` work on every recipe: I-1, I-2, I-6, I-4, I-5, I-3.
7. Answer the decisions, then sync the specs, the schema, and the CHANGELOG (X-7).
8. Close the proof gaps (P-13, D-6, D-7, O-6, L-17, A-18, I-14), so a later change cannot hide
   these bugs again.

---

## Repository and gate findings

### X-1 · high · CI · CI was red on every phase 14 push, and 8 commits are not pushed
- Where: `gh run list --workflow ci`, `git status -sb` (`main...origin/main [ahead 8]`)
- What: all 14 pushed phase 14 commits have a failed CI run. The commits c5ec039 to cfc093f
  exist only on this machine. The session prompt says to push after each green task.
- Causes on the last pushed commit (3c58dc1): X-2, and the two older causes in T-2.
- Fix: close X-2, X-3, X-5, and T-2. Then push, and read the CI result before the next task.

### X-2 · high · tools · A phase 12 test fails at HEAD, and the test cache hides it
- Where: `tools/searchrecipes_test.go:59-61`, `integrations/search/elastic/index-template.json`
- What: if the Elastic template exists, `TestSearchRecipes_Files` fails, because the template
  belonged to phase 14. Commit e761632 added the template and left the test. On a real test
  run, `make test`, `make race`, `tools floor`, and CI all fail on it.
- Evidence: `go test -race -count=1 -run TestSearchRecipes_Files .` in `tools` fails. The
  `./...` form prints `ok (cached)` on this machine (T-1).
- Fix: turn the assertion around. The file must exist, and it must equal
  `elastic.Template(Elasticsearch)`, which the 14-E-6 acceptance text asks for.

### X-3 · high · modules · `tools tidy -check` fails in 6 modules
- Where: `ai/eino`, `ai/genai`, `ai/langchaingo`, `ai/mcpgo`, `ai/mcpsdk`, `examples`
- What: `go mod tidy` changes `go.mod` and `go.sum` in each one. CI runs `make tidy-check`, so
  the first push of these commits fails.
- Fix: run `make tidy`, and commit the result.

### X-4 · high · integration · No integration test ran in phase 14 (criterion 10)
- Where: `docker-compose.integration.yml:100-113`, the nightly `integration` job
- What: since 9095841 the nightly log ends with `container wlog-splunk-1 is unhealthy`. So
  `docker compose up --wait` fails, and no test runs, the older Loki, OTLP, and ClickHouse
  tests included. The plan keeps `make integration` as a manual step, and no record shows a
  run. Criterion 10 names Elasticsearch 8, OpenSearch 2, Splunk, and VictoriaLogs.
- Likely cause, not proven: the `splunk/splunk` image serves HEC over TLS by default. The
  health probe and the test both use `http://localhost:8088`.
- Older cause: before 9095841 the job failed on `TestPreset_BET14_CollectorReadsFilelog`.
- Fix: make the Splunk probe pass, then run `make integration` and record the result. See D-5
  and D-23 for the two tests that prove too little.

### X-5 · medium · lint · `make lint` fails with 3 issues
- `examples/llm-agent/main.go:24`: an unnecessary conversion (unconvert).
- `ai/genai/genai_test.go:145` and `ai/goopenai/goopenai.go:204`: `!=` on an error, where
  `errors.Is` is correct (errorlint). The second one compares with `io.EOF` in shipped code.

### X-6 · medium · floors · Six new modules have no line in `tools/floor-pins.txt`
- What: `ai/genai`, `ai/goopenai`, `ai/langchaingo`, `ai/eino`, `ai/mcpsdk`, and `ai/mcpgo`.
  Each `go.mod` matches its spec floor today. But `tools floor` skips a module with no line,
  so a moved floor fails no gate.
- Fix: add six lines.

```
./ai/genai go=1.24 google.golang.org/genai=v1.71.0
./ai/goopenai go=1.21 github.com/sashabaranov/go-openai=v1.42.1
./ai/langchaingo go=1.24.4 github.com/tmc/langchaingo=v0.1.14
./ai/eino go=1.21 github.com/cloudwego/eino=v0.9.19
./ai/mcpsdk go=1.25.0 github.com/modelcontextprotocol/go-sdk=v1.8.0
./ai/mcpgo go=1.25.5 github.com/mark3labs/mcp-go=v1.1.0
```

### X-7 · medium · docs · The documents do not follow the code
- `CHANGELOG.md` has no phase 14 entry. It needs the new modules, the `Enricher` removal, and
  the `wlog init` v2 change.
- `docs/SPEC-llm.md` shows the phase 10 `Record`. SPEC-track-g shows a second shape, and the
  code is a third (L-18).
- `schema/event.v1.json` lists none of the new `llm` keys (L-18).
- `docs/SPEC-cli-v1.3.md` still describes `wlog.go` and no `--yes` (I-15).
- `tasks/plan.md` names `cmd/wlog/internal/initgen/`, which does not exist.
- `llms.txt` and `llms-full.txt` are stale. `make docs` adds six recipes and the async page.
- Seven doc comment lines in the `ai` modules hold semicolons, against golden rule 6.

### X-8 · medium · coverage · The cover gate does not see the new own modules
- What: `tools cover` reads the root module only. The fresh numbers are otellog 47.2%, otel
  63.0%, prometheus 76.1%, mcpsdk 77.2%, mcpgo 77.2%, and `cmd/wlog/cmd/init` 81.0%.
- Note: the root drains reach 90% to 97%, and 25 mutations still pass there (D-6, D-7). So
  line coverage is a weak signal here. The proof gaps matter more than the number.

## Tooling findings (most are older than phase 14, but they hid phase 14 problems)

### T-1 · medium · make · `make test` and `make race` trust a stale test cache
- What: the Go test cache does not see a file outside the module root. A `tools` test reads
  files of the root module, so its cached pass stays valid after those files change.
- Evidence: the first full race run printed `ok ... tools (cached)`. The same run with
  `-count=1` fails (X-2).
- Fix: run the `tools` module with `-count=1` in the Makefile.

### T-2 · medium · CI · Two older faults keep CI red
- `middleware/hertz` does not build on linux/amd64. `github.com/bytedance/sonic` v1.5.0 fails
  with `undefined: _ModuleData`. Both the `test` and the `floor` job fail. This machine is
  arm64, so the fault does not appear here.
- `tools/cmd/schema` `TestSchema_BET7_GoldensValid` reads `wlog.map.json` from the repository
  root. The file is in `.gitignore`, so the test passes here and fails on CI.

### T-3 · medium · fuzz · Two nightly fuzz crashes of a no-leak target have no cause
- Where: GitHub issues 1 and 2, nightly runs of 2026-09-20 and 2026-09-23
- What: `FuzzCore_SinksNeverLeak` exited with status 1 after about 85 seconds. The log holds
  no failing input and no failure text. The job uploads no `testdata/fuzz` artifact. The
  target passed on the 9 nights after that. Two local runs of 4 and 5 minutes also passed in this
  review. G1 is a safety gate, so the issues need an answer.
- Fix: upload `testdata/fuzz` on failure, print the last lines of the fuzz output, and run the
  target for 10 minutes until it fails again or the issues can close.

### T-4 · low · tools · `floor -libs` and `vuln` edit `go.mod` in place
- A concurrent `go` command in the workspace then rewrites `go.work.sum`, and a killed run
  leaves upgraded `go.mod` files. This review saw both. Run the upgrade in a temp copy.

### T-5 · low · flag/openfeature · `TestOpenFeature_C9_Evaluation` is flaky under load
- `openfeature.SetProvider` starts the provider in a goroutine. The test read the flag too
  early once, with `PROVIDER_NOT_READY`. Use `SetProviderAndWait`.

### T-6 · low · docs · No gate runs `make docs`
- A check mode in `tools docs` closes the stale files of X-7 for good.

## Pipeline and HTTP drains: pipeline, httpdrain, drain/honeycomb, drain/newrelic, drain/elastic

### P-1 · high · drain-newrelic · The drain posts to `/`, not `/log/v1`
- Where: `drain/newrelic/newrelic.go:176`, `:303-316`
- What: `endpointOf` returns the bare host, and `httpdrain.New(endpoint, ...)` adds no path.
  Every region and `WithEndpoint` post to the root path. `setup.FromEnv` builds the drain this
  way, so no event can arrive. The test fake records only the body.
- Fix: `httpdrain.New(endpoint+"/log/v1", ...)`. Assert the path and the `Api-Key` header in
  the golden test.

### P-2 · high · drain-honeycomb · A failed dataset makes the pipeline send the accepted datasets again
- Where: `drain/honeycomb/honeycomb.go:204-207`
- What: if one dataset group fails, `SendBatch` returns the raw error. The pipeline then
  retries or drops the whole batch.
- Evidence: probe ran. `checkout` answers 202 and `billing` answers 503 with 3 attempts.
  `checkout` got 3 requests, and stats show `Sent:0 Dropped:2`.
- Fix: put the failed group into `Retry` or `Dropped`, continue, and return the `PartialError`.

### P-3 · high · drain-elastic · A failed chunk makes the pipeline send the accepted chunks again
- Where: `drain/elastic/elastic.go:218-229`
- What: the same fault after the byte split. A data stream `create` has no `_id`, so each
  resend is a real duplicate document. The probe saw chunk `a` arrive 3 times.
- Fix: the same as P-2.

### P-4 · high · three drains · Shared drain rules 3 and 4 are not built
- Where: `newrelic.go:186-196`, `honeycomb.go:34-39,201-203`, `elastic.go:243-258`
- What: New Relic has no 1,000,000 byte split. Honeycomb has no 1 MB event drop and no 5 MB
  split, and its two limit constants have no reader. No drain drops an oversize event as
  `too_large`. No drain splits in half after a 413, so a 413 loses the whole batch.
- Evidence: probes ran. New Relic sent one request of 2,018,812 bytes. Honeycomb sent one
  request of 12,002,777 bytes.
- Fix: one byte-split helper in `pipeline/httpdrain`, plus a 413 halving that returns a
  `PartialError`. `drain/datadog` already has the halving.

### P-5 · medium · honeycomb, newrelic · Criterion 6 has no status table test
- Neither test file covers 2xx, 400, 401, 403, 408, 413, 429, and 5xx. `drain/elastic` has
  `TestElastic_StatusTable`.

### P-6 · medium · five drains · Gzip is off by default (shared rule 7)
- Where: the `gzip` config field in honeycomb, newrelic, elastic, splunk, and victorialogs
- What: the field starts false, and the research says all five backends accept gzip. A change
  that turns `WithGzip(true)` into a no-op passes the Honeycomb tests.
- Fix: start each config with `gzip: true`, and test both states.

### P-7 · medium · pipeline · No code reports `WLOG_DRAIN_DROPPED`
- Where: `pipeline/pipeline.go:330-333,359-365,475-480`
- What: the spec says the wrapped drain reports it once per reason per minute. The code exists
  only in the problem catalog. With no `OnDropped`, a refused event is silent. Honeycomb and
  Elastic also have no `Setup` (shared rule 8).
- Fix: keep the Logger in `wrapped.Setup`, and report from `reportDrop` with
  `PartialError.Reason` as the key.

### P-8 · medium · honeycomb, elastic · A response with fewer items than events counts the rest as sent
- Where: `honeycomb.go:214-227`, `elastic.go:301-315`. Probes: `[]`, `null`, and `{}` all
  return nil for three events. Put an event without a result into `Retry`.

### P-9 · medium · honeycomb, newrelic · The field cap drops trace ids and keeps user keys
- Where: `honeycomb.go:355-368`, `newrelic.go:240-252`. Both sort by name, not by the
  `trace-otel` priority order. A probe with user keys `a000...` lost `trace.id`,
  `service.name`, `level`, and `operation`. Sort reserved keys first.

### P-10 · medium · httpdrain · `Post` now fails a 2xx when the body read fails
- Where: `pipeline/httpdrain/httpdrain.go:65-68,127-133`. The pipeline then sends an accepted
  batch again, for every older drain. `Post` also reads up to 4 MiB to throw it away. Give
  both entry points one private `post` with a `wantBody` flag.

### P-11 · medium · pipeline · A typed-nil `*PartialError` kills the process (G3)
- Where: `pipeline/pipeline.go:268-271,308`. A probe got SIGSEGV in the worker goroutine. Use
  `errors.As(err, &pe) && pe != nil`.

### P-12 · medium · tests, pipeline · `TestHoneycomb_Options` is flaky, because `Flush` does not wait for a batch in flight
- Where: `honeycomb_test.go:148-163`, the same pattern in the New Relic and Elastic tests,
  root cause `pipeline.go:431-444`. The test failed in the full race run of this review. The
  same gap exists for `Logger.Flush` before a Lambda freeze.

### P-13 · medium · tests · Code can break and the tests still pass
- Each change below left the package tests and the conformance suite green.
  - The index map across datasets, and the index map after a split.
  - The request path, both `Authorization` headers, and `filter_path`.
  - The `Api-Key` header name, the eu and jp hosts, and the 4 MiB response cap.
- `index-template.json` is byte-equal to `json.MarshalIndent` output, so it likely came from
  the code under test. This is not proven.

### P-14 · medium · elastic · The SigV4 example in the package doc is not valid Go
- Where: `drain/elastic/elastic.go:13-16`

### P-15 · medium · honeycomb, elastic · The drop reason can belong to another item
- Where: `honeycomb.go:220-226`, `elastic.go:308-313`. Statuses `[400,503]` give
  `Dropped=[0] Reason="status_503"`. Set the reason only in the dropped branch.

### Low
- P-16 · elastic: the byte cap does not count the 15 byte action line, and each event is
  encoded twice.
- P-17 · elastic: an event without `timestamp` gives a line without `@timestamp`.
- P-18 · duplicates: `flatten` in two drains repeats `preset.Flat().Apply`. `firstEnv` exists
  5 times. `chunkEnd` exists 3 times. `honeycomb.clientFor` caches a client that costs nothing.
- P-19 · honeycomb: the 64 KB cut can split a rune, and a string inside an array is not cut.
- P-20 · honeycomb, elastic: a `json` error wrapped with `%w` puts a response number into an
  error string.
- P-21 · small drift: the Elastic integration test sends no golden event, and
  `HONEYCOMB_API_ENDPOINT` is not in the spec.

## Drains: drain/splunk, drain/victorialogs, drain/syslog, drain/cloudwatch, setup, conformance

### D-1 · high · drain-splunk · The HEC code table never runs for a real error answer
- Where: `drain/splunk/splunk.go:238-266`, `pipeline/httpdrain/httpdrain.go:127-135`
- What: `PostFor` returns the body only for 2xx. Splunk sends code 6 with HTTP 400, and the
  other error codes with 4xx or 503. So the split, `hec_code_6`, and every `hec_code_<n>`
  reason are dead code. A code 6 drops the whole batch as "unexpected status 400". The unit
  test answers HTTP 200 for every code.
- Second fault: when the split runs, a half with more than one event is dropped whole.
- Fix: return the capped body next to the `StatusError`, and read the code on a 400. Make the
  test fake send the real status for each code.

### D-2 · high · setup, victorialogs · `VICTORIALOGS_STREAM_FIELDS="a, b"` through setup drops every event
- Where: `setup/setup.go:543`, `drain/victorialogs/victorialogs.go:164-168,245-251`
- What: the factory splits on `,` and does not trim. The query then holds a space, and every
  request fails with a permanent 400. No problem appears at startup. A leading space also
  passes the high-cardinality check.
- Fix: trim in `newSender` before the check, and build the query with `url.Values`.

### D-3 · high · drain-cloudwatch · One failed `CreateLogStream` stops the drain until restart
- Where: `drain/cloudwatch/cloudwatch.go:301-316`
- What: `createOnce` sets `created = true` before the call. After one throttle, no later
  attempt creates the stream, and every batch is dropped.
- Fix: delete the flag and the mutex. Create on each `ResourceNotFoundException`, and treat
  `ResourceAlreadyExistsException` as success. This is less code.

### D-4 · high · drain-syslog · A TCP or TLS write has no deadline, so a stalled collector blocks the worker and `Close` (G3)
- Where: `drain/syslog/syslog.go:216-264`
- Evidence: probe ran. With a listener that never reads, `SendBatch` was still blocked after
  5 s, and `Sender.Close` blocked too.
- Fix: set a write deadline before each batch, from `ctx` and a fixed timeout.

### D-5 · high · drain-splunk · The integration test passes when Splunk refuses every event
- Where: `drain/splunk/integration_test.go:22-42`
- What: the test asserts only that `Flush` and `Close` return nil. Both always do. A fake that
  answers 403 passes the test. Criterion 10 asks for a query that finds the event.
- Fix: fail on `OnDropped`, expose port 8089, and search for the operation.

### D-6 · high · splunk, cloudwatch · The per-item tests do not prove the exact sets (criterion 7)
- What: the Splunk code 6 case asserts only that some index is dropped. The CloudWatch batch
  is already sorted, so the index map never runs. No test covers the expired range or a split.
- Evidence: 12 mutations pass. One returns the sorted position in place of the original index.
- Fix: one Splunk test with a fake that fails only the body with the bad event, and that
  asserts the exact set. One CloudWatch test with an unsorted batch and all three ranges. One
  test per split limit.

### D-7 · high · drain-syslog · No RFC 5424 ABNF parser exists (criterion 8)
- Where: `drain/syslog/syslog_test.go:54-91`
- What: `parseFrame` splits on spaces and accepts any PRI, version, and timestamp. Nine
  mutations of the frame rules pass, for example no escape and no TLS 1.2 floor.
- Note: the frame code itself is correct. Only the proof is missing.
- Fix: write the parser from the ABNF. Add one event with `"`, `\`, `]`, a space, and a
  character outside ASCII. Add a test where the listener closes after one frame.

### D-8 · medium · splunk, victorialogs · No status table test, and a 413 drops the whole batch
- Where: `drain/splunk/splunk.go:238-241`, `drain/victorialogs/victorialogs.go:212-214`, and
  both test files
- What: neither package tests 400, 401, 403, 408, 413, 429, or 5xx. A 413 is a permanent
  error, so one request loses the whole batch.
- Fix: add the table test to both, as `TestElastic_StatusTable` does. For a 413, split in half
  and send each half once, as `drain/datadog` does. See P-4.

### D-10 · medium · splunk, cloudwatch · A failure in a later chunk sends the earlier chunks again
- Where: `drain/splunk/splunk.go:214-217`, `drain/cloudwatch/cloudwatch.go:186-189`
- What: both loops return the plain error of the failing chunk, and the pipeline retries the
  whole batch. The probe saw one event delivered 2 times in each drain.
- Fix: for a retryable chunk error, return a `PartialError`. `Retry` holds this chunk and the
  rest. `Dropped` holds the drops so far.

### D-11 · medium · cloudwatch · An event over 1 MiB is sent, and its refusal fails the whole batch
- Where: `drain/cloudwatch/cloudwatch.go:202-222`
- Fix: in `SendBatch`, put an entry with `len(message)+26 > maxBytes` into `Dropped` with
  reason `too_large`, and never send it.

### D-12 · medium · syslog · A batch written to a closed connection counts as sent
- Where: `drain/syslog/syslog.go:216-229`, package doc lines 8 to 11
- What: a TCP write to a closed peer succeeds on this side. The drain does not dial again,
  and the frames are lost. The doc says at least once.
- Fix: before a batch on an existing connection, do one read with an immediate deadline. On
  `io.EOF` or a reset, close and dial again. Say "best effort" for UDP in the doc.

### D-13 · medium · syslog · The truncation count and the Logger are stored and never read
- Where: `drain/syslog/syslog.go:122,126,210-213,311`
- Fix: report a truncation through `s.logger.Report` with `WLOG_CAP_REACHED`. Or delete both
  fields and the spec sentence.

### D-14 · medium · syslog · A facility outside 0 to 23 writes an invalid PRI
- Where: `drain/syslog/syslog.go:164-170,292`, and the syslog factory in `setup/setup.go`
- What: facility 99 gives `<795>`, and facility -1 gives `<-5>`.
- Fix: for a facility outside 0 to 23, return an error from `newSender`.

### D-15 · medium · syslog · TLS has no default port 6514
- Where: `drain/syslog/syslog.go:174-176,276-284`
- Fix: in `newSender`, for network `tls` and an address with no port, use
  `net.JoinHostPort(addr, "6514")`.

### D-16 · medium · syslog · `WithTLSConfig` changes the caller's `tls.Config`
- Where: `drain/syslog/syslog.go:77-83,188-196`
- Fix: `c.tlsConfig = cfg.Clone()`.

### D-17 · medium · syslog · The UDP summary form has no cap, and one refused datagram fails the batch
- Where: `drain/syslog/syslog.go:219-226,309-315`
- Fix: cut the summary at a rune boundary, so the frame fits `maxUDP`. For UDP, skip a frame
  that fails with `EMSGSIZE`, and report it as dropped.

### D-18 · medium · splunk · `WLOG_DRAIN_BACKPRESSURE` goes out once per batch, not once per minute
- Where: `drain/splunk/splunk.go:246-248,390-399`
- Fix: keep the time of the last report in the `Sender`, and skip a report inside one minute.
  Send two batches in the test.

### D-19 · medium · cloudwatch · Three faults in the error map
- Where: `drain/cloudwatch/cloudwatch.go:279-298`
- What: a retryable error wraps the SDK error with `%w`, so the AWS message text reaches
  `OnDropped` (shared rule 6). With `WithCreateStream(false)`, a missing stream is retried,
  and that fault is permanent. `ServiceUnavailable` is not in the retry table.
- Fix: give `codeError` a `retry bool`, and use it for both classes with no wrap. With create
  off, return a permanent `codeError`. Add `ServiceUnavailable` to the table.

### D-20 · medium · victorialogs · The tests do not prove the URL, the tenant rule, or the env
- Where: `drain/victorialogs/victorialogs_test.go:89,136-152,182-190`,
  `drain/victorialogs/victorialogs.go:195-220`
- What: four mutations pass. Two examples are a URL with no `_msg_field` and tenant headers
  that always go out. No code tests the `timestamp` that the spec promises.
- Fix: assert the full request URI and the absent headers. Test the timestamp in the drain,
  or remove the sentence from the spec.

### D-22 · medium · conformance · CloudWatch is outside the drain suite, and the `[]any` change has no test
- Where: `internal/conformance/drain/suite.go:54-94`,
  `drain/cloudwatch/cloudwatch_test.go:297-321`, `internal/conformance/harness.go:241-244`
- What: CloudWatch copies the leak scenario and skips `CheckV2Body`. The suite has no HTTP
  part, which is older than phase 14. `Diff({"calls": []}, {})` returns no difference.
- Fix: import the suite from the CloudWatch module. Add one test for a nested array
  difference, and keep an empty array as a leaf.

### D-23 · medium · compose · VictoriaLogs has no health probe
- Where: `docker-compose.integration.yml:115-119`
- Fix: add a probe on `/health`.

### Low
- D-24 · cleanup: `pathValue` exists in 4 drains, and the timestamp parse in 9. Splunk encodes
  each event twice. `SPLUNK_HEC_TOKEN`, `HONEYCOMB_API_KEY`, and `NEW_RELIC_LICENSE_KEY` are
  not marked `Secret` in setup. An unreadable 200 body is dropped as `hec_code_-1`.

## OpenTelemetry and Prometheus: trace/otel, trace/otellog, metrics/prometheus

### O-1 · high · root measure · Every HTTP request and every work unit is measured as kind `work`
- Where: `measure.go:51-57`, `middleware/httpcore/exchange.go:60-63`, `work/work.go:92`
- What: `measureOf` reads `e.kind` and `e.operation`. The adapters set both with `wlog.Set`,
  which writes only `e.fields`. So `http.server.request.duration` is never recorded, and every
  route of one method lands in one series. Criterion 3 fails with real requests. The module
  tests call `Measure` by hand.
- Evidence: probe ran through `wlogstd.Middleware`. The event shows `kind=request`. Prometheus
  shows `kind="work", operation="GET unmatched"`.
- Fix: read kind and operation from `e.fields` first. Then cap the kind, because an app can
  set it and it becomes a label.

### O-2 · high · propagate, httpcore, work · The OTel ids are replaced on every request (HTTP-21)
- Where: `middleware/httpcore/exchange.go:60-64`, `work/work.go:86-89`,
  `propagate/propagate.go:83-90`
- What: the Starter writes the OTel ids. Then the adapter calls `propagate.Extract`, which
  always makes a new span id. With no incoming `traceparent`, it also makes a new trace id.
- Evidence: probe ran. The event span id never equals the OTel span id.
- Fix: after `Extract`, put back the trace id, span id, and sampled flag that a Starter set.

### O-3 · high · trace-otel · A nested unit or a plain log line writes onto the request span
- Where: `trace/otel/span.go:22-48`
- What: `OnFinish` copies any event whose context holds a recording span. A failed inner unit
  leaves a successful request span at status Error. `wlog.Info` puts its pairs on the span.
- Fix: skip kind `log`, and skip a unit that starts under a span that another unit claimed.

### O-4 · medium · otel · Span attributes go in name order, not in the spec priority order
- Where: `trace/otel/span.go:35-37,71-82`
- What: the SDK keeps the first 128 attributes. With 140 user keys it dropped `http.route`,
  `error.type`, `outcome`, and `operation`.
- Fix: sort by rank, then by name. Rank 0 is the preset-mapped reserved keys and
  `error.type`. Rank 1 is the other reserved keys. Rank 2 is groups. Rank 3 is user keys.
  Add a test with more than 128 keys.

### O-5 · medium · otel, prometheus, core stats · The drain counters never export a point
- Where: `stats.go:47-51`, `pipeline/pipeline.go:189`, `trace/otel/metrics.go:156-161`,
  `metrics/prometheus/stats.go:48-53`
- What: no drain implements `wlog.StatsReporter`, because the pipeline `Stats()` returns
  another type. `Queued` is a depth, but both modules export it as a counter. Two drains with
  one name make the Prometheus scrape fail.
- Fix: decision 7.

### O-6 · medium · tests · The suites pass with the code broken
- Where: `trace/otel/otel_test.go`, `trace/otellog/otellog_test.go`,
  `metrics/prometheus/prometheus_test.go`
- Changes that pass every test:
  - `hist = existing` removed (`metrics/prometheus/prometheus.go:111`).
  - `_OTHER` replaced by the raw name in both modules.
  - `methodOf` returns the raw method, and `skipSpanAttribute` always returns false.
  - The `error.type` append removed, and the stats cut to `emitted`.
  - `SetObservedTimestamp` removed in otellog.
- No test at all: criterion 3 as written, typed slices, a span that does not record,
  `WithSpans(false)`, and `WithStats`.
- Fix: one assertion per item, and name each test after its criterion.

### O-7 · medium · prometheus · Buckets out of order pass `New`, then every event panics inside `Measure`
- Where: `metrics/prometheus/prometheus.go:60-66,97-101`
- Fix: in `New`, return an error for buckets that are not strictly increasing.

### O-8 · medium · doctor · The OTel order check gives wrong results
- Where: `cmd/wlog/cmd/doctor/otel.go:18,51-72`
- What: it compares the first OTel token with the first wlog token across all files. The
  gRPC stats handler runs before every interceptor, so text order means nothing there. A
  comment counts as code. One correct server hides one wrong server.
- Fix: remove `otelgrpc.NewServerHandler` from the tokens, strip comments, and compare per
  file. Add the three cases to the test.

### O-9 · medium · otel · The package doc does not match the code
- Where: `trace/otel/otel.go:10-31,83-92,167,176`
- What: the snippets do not compile, because each middleware call needs a `*wlog.Logger`.
  `wlogNetHTTP` exists nowhere, and the package is `wlogstd`. `WithSpans(false)` also stops
  the trace id copy. `WithStats(true)` does nothing with `WithMetrics(false)`.
- Fix: correct the snippets, remove the `cfg.spans` gate from `OnStart`, and gate the stats on
  `cfg.stats` alone.

### O-10 · medium · root · `Fields()` hands the live event map to any Finisher
- Where: `summary.go:65-67`, `trace/otel/span.go:50-62`
- What: the method is not in the spec, and no root test calls it. A Finisher that writes to
  the map changes the redacted event before every drain.
- Fix: decision 6.

### Low
- O-11 · prometheus: reuse ignores the second caller's buckets, and each recorder has its own cap.
- O-12 · prometheus: `New(nil)` and `StatsCollector(nil)` panic. The second kills the process.
- O-13 · otel: the stats counters build in `Setup`, so `Plugin` never returns their error.
- O-14 · otellog: `Enabled` sees the context without the event span.
- O-15 · otellog: the severity text is `ERROR`, and the preset writes `error`.
- O-16 · otellog: the doc snippet passes an exporter where a processor is needed.
- O-17 · otel: `plugin.tracer` is never read, and a loop in `spanErrorType` repeats the preset.
- O-18 · duplicates: about 110 attribute lines exist in both OTel modules, and the operation
  cap exists twice.
- O-19 · otel: the request metric has no 5xx fallback for `error.type`.
- O-20 · two unproven risks: `otelhttp` records the same metric name, and `url.scheme` can
  come from a client header.

## llm and the LLM SDK modules: llm, ai/anthropic, ai/openai, ai/genai, ai/goopenai

Promised entry points against the code:

| Module | Missing | Under another shape |
|---|---|---|
| `ai-anthropic` | none | none. The retry count never works (L-5) |
| `ai-openai` | `WithIncludeUsage` (L-4) | none |
| `ai-genai` | none | `Observe` returns a pull-based `*Observer`, not a re-yielding `iter.Seq2` (L-2) |
| `ai-goopenai` | none | none |
| all four | the body tee, its cap, `usage_unknown`, and the HTTP status (L-10) | none |
| root `llm` | the `calls` entry and seven `Record` fields (L-9) | none |

### L-1 · high · ai/genai, llm · The attempt counter and `llm.Add` race on the live group map (G2)
- Where: `ai/genai/genai.go:231-243`, `llm/set.go:34-98`
- What: both read the stored map outside the event lock, while `SetGroup` writes it. Two
  model calls on one event is a normal fan-out.
- Evidence: `go test -race` reports the race. 1,600 round trips record 354 to 929 attempts.
- Fix: one core helper that updates a group under the lock. This is decision 6.

### L-2 · high · ai/genai · The Observer leaks a goroutine and the HTTP body when the caller stops early
- Where: `ai/genai/genai.go:148-173`. `iter.Pull2` is stopped only on a stream error, and the
  Observer has no `Close`. Return a re-yielding `iter.Seq2`, as the spec says.

### L-3 · high · ai/openai · `ObserveResponses` ignores `response.incomplete` and `response.failed`
- Where: `ai/openai/openai.go:234`. A reply cut by `max_output_tokens` is billed and records
  zero tokens, no model, and no id.
- Fix: fill the record from `response.completed`, `response.incomplete`, and `response.failed`.

### L-4 · high · ai/openai, ai/goopenai · A chat stream without usage records zero tokens and cost 0
- Where: `ai/openai/openai.go:157-208`, `ai/goopenai/goopenai.go:194-252`
- What: neither SDK sends usage on a stream by default. `WithIncludeUsage` is missing, and
  no doc names the option. The event shows `cost_micros: 0`, which reads as a measured value.
- Fix: add the helper. If a stream ends with no usage chunk, write `usage_unknown`.

### L-5 · high · ai/anthropic · The middleware never records the retry count
- Where: `ai/anthropic/anthropic.go:270`. It reads `X-Stainless-Retry-Count` from the
  response. The SDK sets it on the request. The test builds a response with the header.
- Fix: read `req.Header.Get("X-Stainless-Retry-Count")`, and set the header on the request in
  the test.

### L-7 · high · ai/openai · `FromResponse` drops `cache_write_tokens`
- Where: `ai/openai/openai.go:82-91`. The cost is 7,280 micros where 7,380 is correct. The
  fixture leaves the field out.
- Fix: add `CacheWriteInputTokens: int(r.Usage.InputTokensDetails.CacheWriteTokens)`, and put
  the field in the fixture.

### L-8 · high · llm · The Enricher prices one hour cache writes at the plain input rate
- Where: `llm/price.go:248-256`. `recordFrom` does not read `cache_write_1h_input_tokens`.
  `Cost` gives 34,500 micros, and the Enricher gives 31,500. Every shipped path uses the
  Enricher. The test calls `Cost`.
- Fix: add `CacheWrite1hInputTokens: intOf(m["cache_write_1h_input_tokens"])` to `recordFrom`,
  and add one Enricher test.

### L-9 · high · llm · `llm.Add` writes no `calls` entry, and most `Record` v2 fields do not exist
- Where: `llm/set.go:28-99`, `llm/record.go:15-61`
- What: missing are `ResponseModel`, `Status`, `FinishReasons`, `Attempts`, `RequestIDs`,
  `Err`, and tool call ids. Every module puts the response model into `request_model`. This
  is decision 1.

### L-11 · high · six modules · With `WithContent`, a denied key inside a JSON string is not masked (G1)
- Where: `ai/openai/openai.go:116`, `ai/goopenai/goopenai.go:146,173`, `ai/eino/eino.go:156`,
  `ai/langchaingo/langchaingo.go:132,141`, `ai/mcpsdk/mcpsdk.go:165-167`,
  `ai/mcpgo/mcpgo.go:259-261`
- What: the key denylist walks trees, not strings. Tool arguments in four modules are one JSON
  string, and an MCP typed tool repeats its output as JSON text.
- Evidence: probes ran. `{"password":"hunter2"}` reaches the event. The Anthropic module masks
  the same argument. This needs the opt-in, so it is not a default leak.
- Fix: one root helper that turns a valid JSON string into a tree before the write.

### L-6 · medium · anthropic · `Observe` ignores the input and cache counts in `message_delta`
- Where: `ai/anthropic/anthropic.go:238-243`
- What: the observer reads those counts from `message_start` only. The SDK `Accumulate`
  overwrites them from `message_delta`. The probe used a hand-built stream, so the lead
  lowered this to medium.
- Fix: overwrite each count whose `event.Usage.JSON.<Field>.Valid()` is true, as the SDK does.

### L-10 · medium · all four · No middleware reads a body
- Where: `ai/anthropic/anthropic.go:254-278`, `ai/openai/openai.go:252-262`,
  `ai/genai/genai.go:225-243`, `ai/goopenai/goopenai.go:268-276`
- What: the tee, its 1 MiB cap, `usage_unknown`, and the HTTP status do not exist.
- Fix: decision 2.

### L-12 · medium · three modules · `request_ids` keeps only the last id
- Where: `ai/anthropic/anthropic.go:268`, `ai/openai/openai.go:257`,
  `ai/goopenai/goopenai.go:272`
- What: three attempts give `[req_3]`. `ai/genai` records no request id.
- Fix: append under the lock, with the helper of L-1.

### L-13 · medium · genai · `attempts` sums over calls, and the `Transport` doc is wrong for Vertex AI
- Where: `ai/genai/genai.go:1-11,208-217`
- What: three calls with no retry give `attempts: 3`. With `HTTPClient` set,
  `genai.NewClient` adds no auth. So the wiring in the doc sends no `Authorization` header.
- Fix: count attempts for each call. Tell a Vertex user to wrap the transport of an
  authenticated client.

### L-14 · medium · all four · No observer sets `Streamed`, the first chunk time, or `Duration`
- What: so `output_tokens_per_second` never appears through a shipped path. The `llm-agent`
  recipe sets no `Duration`.
- Fix: each observer stamps the first chunk and the end, and sets `Streamed`.

### L-15 · medium · all four · `WithContent` is partial
- Where: `ai/anthropic/anthropic.go:171,191`, `ai/openai/openai.go:158`,
  `ai/goopenai/goopenai.go:195`, `llm/set.go:168-188`
- What: three observers accept the option and discard it. No module fills `InputMessages`.
  `betaContentOf` drops the tool arguments. After several `Add` calls the event keeps only
  the messages of the last call.
- Fix: make the option work in the three observers, or remove it there. Say in each package
  doc that only output is recorded.

### L-16 · medium · all four · An observer panic is raised, not reported
- Where: `ai/genai/genai.go:91-92,161`, `ai/anthropic/anthropic.go:200`,
  `ai/goopenai/goopenai.go:202`
- What: `Observe(nil).Next()` panics. `{"candidates":[null]}` panics in `FromGenerateContent`.
- Fix: skip a nil candidate. Add a recover in each `consume` that calls `Logger.Report`.

### L-17 · medium · tests · Proof rule faults
- `TestLLM_TokenInvariants` (`llm/additions_test.go:121`) passes with every clamp removed
  from `Cost`.
- No module test asserts the two invariants of the token table.
- The fixtures are hand-written. `beta_message.json` is byte-equal to `message.json`. No
  stream fixture exists.
- No stream test proves that no text reaches output.
- The 30,150 test uses a hand-built table, not the `claude-sonnet-4-6` row.
- Fix: record real responses and streams, and add one test for each point.

### L-18 · medium · docs · Three `Record` shapes exist
- Where: `docs/SPEC-llm.md:20-118`, `llm/record.go:1-18`, `schema/event.v1.json`
- Fix: make SPEC-llm match the code. Add the new keys to the schema: `response_id`,
  `cache_write_1h_input_tokens`, `steps`, `output_tokens_per_second`, `input_messages`,
  `output_messages`, `request_ids`, and `attempts`.

### L-19 · medium · llm · Prices
- Where: `llm/price.go:123-143`, `llm/prices.go:19-48`
- What: the prefix match prices `gpt-4.1-mini` and `gpt-4.1-nano` as `gpt-4.1`, and
  `gpt-5.5-pro` as `gpt-5.5`. No Gemini row exists. The reviewer found no vendor page to
  compare the newest rows with.
- Fix: decision 10.

### L-21 · medium · naming · `ai/openai` and `ai/goopenai` are both `package wlogopenai`
- Where: `ai/openai/openai.go:10`, `ai/goopenai/goopenai.go:10`,
  `cmd/wlog/internal/adapters/table.go:27,37`
- Fix: decision 8.

### Low
- L-22 · llm: top level fields come from the last call, and `Add` writes two zero fields.
- L-23 · cleanup: `Option`, `config`, `WithContent`, and `resolve` are identical in four
  modules. `FromChatCompletion` reads the tool calls of choice 0 only.

## Agent frameworks, MCP, and recipes: ai/langchaingo, ai/eino, ai/mcpsdk, ai/mcpgo, examples

### A-1 · high · ai-mcpgo · A JSON-RPC id that is an array or object panics the before hook
- Where: `ai/mcpgo/mcpgo.go:67-70,108`. The id is a map key, and a slice has no hash. The
  input comes from the client. Key on `fmt.Sprintf("%v", id)`.

### A-2 · high · ai-mcpgo · A tool handler that returns `nil, nil` panics the after hook
- Where: `ai/mcpgo/mcpgo.go:179,327-331`. Without wlog the server answers `"result":null`.

### A-3 · high · ai-mcpgo · A request that runs longer than 5 minutes gives no event
- Where: `ai/mcpgo/mcpgo.go:122-124`. `take` rejects an expired entry, and nothing reports it.
  Let `take` return the entry at any age, and expire only in the prune loop.

### A-4 · high · ai-langchaingo · `FromContentResponse` counts the first OpenAI tool call twice
- Where: `ai/langchaingo/langchaingo.go:70-77,129-143`. The provider copies `ToolCalls[0]`
  into the legacy `FuncCall`. If `ToolCalls` is empty, read `FuncCall`. Otherwise skip it.

### A-5 · high · ai-langchaingo · `Handler()` writes calls of kind `agent`, which the schema rejects
- Where: `ai/langchaingo/langchaingo.go:172,188`. This is decision 4.

### A-6 · high · ai-langchaingo · The handler leaks one map entry per tool start that gets no end (G4)
- Where: `ai/langchaingo/langchaingo.go:159-160,171-173`. The library's own calculator tool
  returns with no end callback. 1000 such requests leave 1000 entries.
- Fix: bound the map with a cap and an age, as mcpgo does, and report each drop.

### A-7 · high · ai-langchaingo · Nested or parallel pairs on one context overwrite each other
- Where: `ai/langchaingo/langchaingo.go:157-161`. A real executor does 4 units of work and
  records 3 calls. Keep a stack per context, not one slot.

### A-8 · high · ai-eino · A streamed call loses its `llm` record when the event ends after the last chunk
- Where: `ai/eino/eino.go:59-62,82-95`
- What: the drain goroutine calls `llm.Add` after its own EOF, while the caller ends the event.
- Evidence: probe ran. 299 of 300 events have no `llm` group with no gap, and 0 of 300 with
  50 µs of idle time. The module test always sleeps one second.
- Fix: record on a `wlog.Detach` child that the goroutine ends, or expose a wait.

### A-9 · high · ai-eino · One streamed tool call counts once per delta chunk
- Where: `ai/eino/eino.go:115-117,153-158`. Four deltas give `tool_call_count: 4`.
  `schema.ConcatMessages` merges both tool calls and text.

### A-11 · high · mcpsdk, mcpgo · Criterion 4 has no test, and the events are not identical
- What: no shared golden exists, and no test compares the two outputs. A live diff shows the
  `rpc` group equal except `session_id`. The `error` group differs. This is decision 9.
- Fix: one shared golden per fixture in a root internal package, compared after
  `conformance.Normalize`.

### A-12 · medium · mcpgo · The tool handler never sees the event
- Where: `ai/mcpgo/mcpgo.go:142-143`
- What: the event context stays in the pending map, and the handler gets the SDK context. So
  `wlog.Set` and `llm.Add` inside a tool do not join the event. mcpsdk passes the context.
- Fix: say so in the package doc, or add a tool middleware that only swaps in the stored
  context.

### A-13 · medium · both MCP · `rpc.mcp.session_id` is always `[REDACTED]`
- Where: `redact/defaults.go:15`
- Fix: decision 5.

### A-14 · medium · both MCP · `input_required` round trips do not link
- Where: `ai/mcpsdk/mcpsdk.go:143-155`, `ai/mcpgo/mcpgo.go:237-249`
- What: only the `requestState` of the result is hashed. The retry carries the same state in
  `Params.RequestState`, and no code reads it.
- Fix: if `Params.RequestState` is set, hash it into the same field.

### A-15 · medium · mcpgo · The cap and the expiry drop events with no report and no test (G4)
- Where: `ai/mcpgo/mcpgo.go:100-107`
- What: after 10,000 orphan before hooks, the next request gives no event and no problem.
  Each `store` at the cap scans 10,000 entries under one mutex.
- Fix: on prune and on cap, call `log.Report`, and end a pruned event with an error. Add the
  two tests.

### A-16 · medium · both MCP · A handler panic gives no event
- Where: `ai/mcpsdk/mcpsdk.go:67-71`
- Fix: mcpsdk records the panic and raises it again, as `work.Run` does. The mcpgo doc tells
  users to add `server.WithRecovery()`.

### A-17 · medium · mcpgo · `protocol_version` is missing for a legacy session
- Where: `ai/mcpgo/mcpgo.go:195-207`
- Fix: read it from the initialize message, and keep it per session.

### A-18 · medium · all four · No recorded fixtures, and eight mutations pass
- What: no module has a `testdata/` folder. Hand-built literals hid A-4 and A-9.
- Changes that pass every module test:
  - mcpsdk: no typed-nil guard, and no nil `InitializeParams()` test.
  - Both MCP modules: no -32700, -32600, or -32601 in `isClientCode`.
  - eino: no `stream.Close()`, no `go`, and no `recover`.
- Fix: record real SDK responses under `testdata/`, and add one test for each change.

### A-19 · medium · eino · Drain failure paths
- Where: `ai/eino/eino.go:52-63,82-95`
- What: a source that never closes leaks one goroutine per call. A stream that fails mid-way
  records zero tokens at level `info`. `OnError` is not handled. A recovered panic is not
  reported.
- Fix: stop on `ctx.Done()`, mark a failed stream on the record, handle `OnError`, and log a
  recovered panic on the event.

### A-20 · medium · langchaingo, eino · Provider handling
- Where: `ai/langchaingo/langchaingo.go:86-101`, `ai/eino/eino.go:69-74`
- What: an unmapped provider records zero tokens, which read as measured values. The
  langchaingo doc says that `Handler` records LLM calls, and it never does. eino writes the
  provider as `OpenAI`, and the other modules write `openai`.
- Fix: leave the token fields off for an unmapped provider. Correct the doc sentence. Write
  the eino provider names in lower case.

### Low
- A-22 · recipes: two `wlog explain` claims do not hold, one Loki label is wrong, and
  `examples/llm-agent/main.go:48` indexes `msg.Content[0]` with no length test.
- A-23 · duplicates: about 90 lines are identical in the two MCP modules.
- A-24 · cleanup: four `rpc.mcp.*` fields are not in the spec table, and a handler error that
  quotes its arguments reaches `error.message` without `WithContent`.

## cli-init v2: cmd/wlog

Results of `wlog init --yes` on full copies of the recipe apps:

| App | Exit | Note |
|---|---|---|
| `llm-agent`, `lambda` | 1 | I-1, `no router declaration found for nethttp` |
| `rest-api` | 1 | I-2, `no router declaration found for chi` |
| `mcp-server`, `kafka-consumer`, `cron-job`, `cli-tool`, `grpc-service` | 0 | Builds and passes doctor. `init` writes a `NewLogger` that nothing calls |

### I-1 · high · `_test.go` files count as app source, so `init --yes` fails on `llm-agent` and `lambda`
- Where: `cmd/wlog/cmd/init/init.go:245-288,416-439`, `initv2_test.go:82`
- What: detection, the package clause, and the rewrite read every `.go` file. A test file that
  imports `net/http` selects `nethttp`. A test file can also get the patch, or give
  `wlog_setup.go` the clause `package main_test`. The criterion 6 test copies only `main.go`.
- Fix: skip `_test.go` in one shared file lister, and copy the whole recipe in the test.

### I-2 · high · Any existing `.Use(...)` on the router makes `init` fail with a wrong message
- Where: `rewrite.go:109,145-164`. A chi app with `r.Use(middleware.Recoverer)` cannot be set
  up. Match only a wlog call. If wlog is already present, exit 0.

### I-3 · high · The rewrite moves the user's comments into the inserted call
- Where: `rewrite.go:88-118,166-169`. The echo README shape becomes `e.Use(`, then
  `// Routes`, then `LoggerMiddleware())`. The output compiles, so no test sees it.
- Fix: splice the text at the end offset of the statement, then run `format.Source`. Add a
  fixture with comments.

### I-4 · high · `init` v2 on a v1 tree writes code that does not build
- Where: `init.go:299-302`. The guard reads only `wlog_setup.go`, so `NewLogger` and
  `WrapHandler` exist twice. If `wlog.go` exists, refuse.

### I-5 · high · Generated names collide with user code
- Where: `setup.go:22-66,88`. An app with its own `NewLogger` fails to build after the write.
- Fix: before the write, parse the package, and fail the plan for a name that is taken.

### I-6 · high · The generated logger ignores the environment identity
- Where: `setup.go:89-90`. `wlog.WithService(module, "0.0.1", "local")` comes after
  `setup.FromEnv()` and wins. With `WLOG_ENV=production` the event shows `env=local`. The
  generated comment says the opposite. Delete the `WithService` line.

### I-7 · medium · `init` wires 8 HTTP frameworks and only lists the other 60 rows
- Where: `cmd/wlog/cmd/init/setup.go:18-73`, `cmd/wlog/cmd/init/init.go:331-350`
- Fix: decision 3.

### I-8 · medium · The `cmd/<name>/main.go` layout does not work
- Where: `cmd/wlog/cmd/init/init.go:201-212,268-288`, `docs/SPEC-hardening.md:322-331`
- What: at the module root, `init` says `no .go files in .`. With `--dir cmd/api` it finds no
  `go.mod`. Four CLI-15 rules are not in the code: the walk up to `go.mod`, the `_test.go`
  skip, the Logger close in the setup, and all temp files before any rename.
- Fix: walk up from `--dir` to `go.mod`.

### I-9 · medium · A failed build leaves the tree changed, and a second run is blocked
- Where: `cmd/wlog/cmd/init/init.go:84-91,152-159`, package doc lines 1 to 3
- Fix: after a failed `verify`, restore the old bytes and remove the created files.

### I-10 · medium · `verify` forces `GOWORK=off` and `-mod=mod`
- Where: `cmd/wlog/cmd/init/init.go:101-103`
- What: `init` fails in a `go.work` project. `-mod=mod` rewrites `go.mod` outside the plan.
  `go build ./...` leaves a binary in the tree. The `GOFLAGS` of the user is replaced.
- Fix: run `go build -o os.DevNull ./...` with the environment of the user. Make the
  requirement change a named plan step.

### I-11 · medium · Detection reports adapters that the app does not use
- Where: `cmd/wlog/cmd/init/init.go:369-381,389-412`
- What: `// indirect` lines count, and lines inside a `replace (` block count. `Installed` is
  always true.
- Fix: skip `// indirect`, track the block kind, and drop `installed`.

### I-12 · medium · `--json --yes` prints text after the JSON
- Where: `cmd/wlog/cmd/init/init.go:65-72,88-91,108-109`
- Fix: with `--json`, send progress and doctor output to stderr.

### I-13 · medium · `wlog doctor` prints adapter lines in map order (G6)
- Where: `cmd/wlog/cmd/doctor/tracks.go:29`. This is older code.
- Fix: range over `adapters.Table`, append the Elastic line, and delete `SetupLines` and the
  map.

### I-14 · medium · Nine mutations pass the whole `cmd/wlog` suite
- Where: `initv2_test.go`, `init_test.go`, `table_test.go`
- Changes that pass every test:
  - A deleted table row, and a `Wlog` path that points at a missing module.
  - A setup line that names a missing function.
  - A run without `--yes` that writes files, and a removed `verify` call.
  - Broken fiber and fiber3 templates.
- `assertNoTempFiles` (`init_test.go:334`) can never fail.
- Fix: one test that reads `go.work`. It proves that each adapter module has a row, and that
  the directory and the function of each row exist. Add fiber and fiber3 to the build cases.
  Assert the run without `--yes`.

### I-15 · medium · The v1 contract changed with no spec or CHANGELOG entry
- Where: `docs/SPEC-cli-v1.3.md:17-31,115-117`, `CHANGELOG.md`
- What: a plain `wlog init` now writes nothing and exits 0, so a v1 script does nothing and
  shows no error.
- Fix: update SPEC-cli-v1.3, and add the CHANGELOG entry.

### I-16 · medium · Table rows: one stale alias, one missing module
- Where: `cmd/wlog/internal/adapters/table.go:132-133,155`, `queue/kafkago/doc.go:1,13`
- What: the `queue-kafkago` row prints the alias `wlogkafka`, and the package is
  `wlogkafkago`. `drain/cloudwatch` has no row. `sqlshape` has a row with no setup line.
- Fix: correct the alias in both files, and add the CloudWatch row.

### Low
- I-17 · a symlinked `main.go` becomes a regular file, and mode `0600` becomes `0644`.
- I-18 · dead code: `ByWlog`, `ByLib`, `validateGlobs`, and `--dry-run` as a copy of no `--yes`.
- I-19 · `wlog help` lists 5 of 13 commands.
- I-20 · a run without `--yes` exits 0 with one stderr line. This is a design choice.

## Probe files

The probe tests of the reviewers are in a temporary session folder. The system can delete it.
Use a probe as a model for a failing test, and never copy one into the repository unread.

- Base: `/private/tmp/claude-502/-Users-jeremygeraldprawira-Documents-wlog/99ef46a9-cf2a-456b-b719-e706041c9f72/scratchpad/`
- `rev-drains-a/` for P findings, `rev-drains-b/` for D, `rev-otel/copy/` for O, `rev-llm/` for
  L, `rev-agents/` for A, and `rev-init/` for I.

## Correct as built

- `dropPartial` counts each event once. An index outside the batch is ignored, and an index in
  both lists is dropped. Criterion 1 holds as written.
- No new package-level mutable state exists. `tools pkgstate` passes.
- Every spec floor matches its `go.mod`. `trace/otel` and `metrics/prometheus` pass on Go
  1.21, and `trace/otellog` passes on Go 1.25.
- Without `WithContent`, no prompt, completion, tool argument, or tool result reaches an event,
  in all eight `ai` modules. No module calls an SDK method that builds the full text.
- The token rules of Anthropic, OpenAI Chat, and Gemini match the table. The Sonnet 4.6
  example prices at 30,150 micros from the response to the Enricher.
- The OTel plugin and the log drain see only the redacted event. The operation cap is correct
  under 32 goroutines.
- The syslog frame code is correct for PRI, timestamp, BOM, octet counting, and the escapes.
- The CloudWatch sort, the 26 byte overhead, the 24 hour split, and the range maths are correct.
- The Elastic body, the `filter_path`, and the template fields match the spec. The drain never
  reads `error.reason`.
- Both MCP modules give one `rpc` event per request, skip notifications, and apply the level
  rules. `requestState` is stored only as a hash.
- Every new drain has its row in `docs/SPEC-setup.md` and its factory.
- The adapter table has 68 rows, and no row points at a missing module.
- `init --json` and `--dry-run` write nothing. A user-edited `wlog_setup.go` is never
  overwritten. The generated code turns on no capture.
- Both recipes follow the four parts, and their goldens pass `tools schema`.

## Not done

- `make integration`. Docker runs on this machine, but the Splunk image is large and the
  review started no container. The HEC TLS default is not proven.
- A run against a recorded provider stream, real `otelhttp` middleware, or MCP over stdio.
- A run of each `ai` module on its own Go floor toolchain, apart from `tools floor`.
- The shared decision store (GBrain) refused its token, so no earlier decision was read.

## Fixed, 2026-10-02

Batch A, gates: X-2, X-3, X-5, X-6, T-1, T-2, T-5. The push also closes X-1. CI run
36994411778 is green on `6461328`.

| Id | Commit | What changed |
|---|---|---|
| X-2 | `cd6643f` | The Elastic template test asserts the file is present and equals `elastic.Template(elastic.Elasticsearch)` |
| X-3 | `2bee1fd` | `go mod tidy` over the six drifted modules, with no direct version raised |
| X-5 | `c593cb8` | The unconvert and the two errorlint findings |
| X-6 | `fe3f859` | Six floor lines, and a test that every `ai` module has one |
| T-1 | `c473da3` | `make test` and `make race` pass `-count=1` |
| T-2 | `f4b34d8`, `6461328` | The hertz build on linux, and the ignored schema golden |
| T-5 | `8809a82` | `SetProviderAndWait` in the two evaluation tests |
| X-1 | the batch A push | CI is green on the pushed tree |

T-2 needed two commits. The first raised `github.com/bytedance/sonic` to v1.8.0, which
compiled but did not link, so the hertz test job stayed red. The second raised it to
v1.15.0, the lowest version that compiles and links on Go 1.26, and its companion
indirect pins rose with it.

Batch B, pipeline and httpdrain: P-11, P-10, P-7, P-12, P-4, D-1. CI run 36997767283 is
green on `68e6b39`.

| Id | Commit | What changed |
|---|---|---|
| P-11 | `ac7b86c` | A typed-nil `*PartialError` no longer panics the worker |
| P-10 | `b8057d4` | `Post` never reads a response body, so a broken 2xx body cannot fail it |
| P-7 | `1d5ff0e` | The wrapped drain reports `WLOG_DRAIN_DROPPED` through the Logger |
| P-12 | `a811805` | `Flush` waits for a batch in flight |
| P-4 | `1b4426d` | `httpdrain.Chunks` and `httpdrain.SendChunk`, the shared split and the 413 halving |
| D-1 | `967eedb` | `StatusError` carries the capped body of a non-2xx answer |
| CI | `68e6b39` | `TestMongo_B2_WithCollection` builds an ordered command document |

P-4 and D-1 landed the shared helpers only. Batch C wires them into the drains, which is
where each backend's limits live, and batch D reads the Splunk HEC code from the body.

The store/mongo commit is not a review id. That test built its command with a map literal,
which the driver marshals in random order, so it failed about one run in twenty and CI hit
it.

Batch C, HTTP drains: P-1 to P-17, P-19, and the spec half of P-21. CI run 37005501695 is
green on `1a1a07f`. P-18 and the integration half of P-21 stay open.

| Id | Commit | What changed |
|---|---|---|
| P-1 | `51b7185` | New Relic posts to `{endpoint}/log/v1`, which the spec already asked for |
| P-2 | `0f3f14a` | A failed Honeycomb dataset stays inside its own group |
| P-3 | `eee5fcb` | A failed Elastic chunk stays inside its own group |
| P-4 | `df38dc8` | The byte split and the 413 halving, wired into the three drains |
| P-5 | `468ef1f` | The status table for Honeycomb and New Relic |
| P-6 | `2997071`, `f3fa6a4` | Gzip on by default in five drains, and the fake decodes it |
| P-8 | `18cdc2f` | An event with no per-item result is retried |
| P-9 | `0b18ec4` | The field cap keeps the reserved keys |
| P-13 | `50422db`, `8406cd1` | The index map, the Elastic request shape, the region hosts, the response cap |
| P-14 | `535d63a` | The SigV4 example compiles |
| P-15 | `9069c7b` | The reason names the dropped event |
| P-16 | `dfcbbde` | The byte cap counts the action line |
| P-17 | `23d6f6b` | A missing timestamp gets an `@timestamp` |
| P-19 | `01249aa` | The 64 KB cut lands on a rune, and an array is cut too |
| P-21 | `51598d4` | SPEC-track-e names `HONEYCOMB_API_URL` and `HONEYCOMB_API_ENDPOINT` |
| P-20 | not reproduced | See below |
| P-18 | open | The duplicate `flatten`, `firstEnv`, `chunkEnd`, and the client cache |

The coverage gate failed first at 84.7% for `pipeline` and 83.5% for `pipeline/httpdrain`,
because the batch B and C code added no tests of its own. `1a1a07f` raised them to 89.6%
and 90.6%.

P-20 is not reproduced. The report says a `json` error wrapped with `%w` puts a response
number into an error string. In this tree the wrapped error never reaches the caller: the
dataset and chunk loops turn a failed request into a `PartialError` with reason `transport`,
so `SendBatch` returns that error, and a test that looks for a `*json.SyntaxError` in the
chain passes with the wrap and without it. The wrap is unchanged.

P-21's integration half, a golden event in the Elastic integration test, needs the Compose
stack, which is batch K.

Batch D, Splunk and VictoriaLogs: D-1, D-2, D-8, D-10, D-18, D-20. CI run 37010650809 is
green on `72c7b9e`.

| Id | Commit | What changed |
|---|---|---|
| D-1 | `3b06021` | The HEC code is read from the body of a refused answer, and the code 6 split keeps halving |
| D-2 | `6bfcd34` | VictoriaLogs trims the stream fields and builds the query with `url.Values` |
| D-8 | `db4aae7` | A 413 halves the request in Splunk and VictoriaLogs, and VictoriaLogs gains the status table |
| D-10 | `700cb98` | A failed chunk retries that chunk and the rest, in Splunk and CloudWatch |
| D-18 | `e0cb93f` | Splunk reports backpressure once a minute |
| D-20 | `72c7b9e` | The VictoriaLogs test asserts the URI, the absent tenant headers, and the timestamp |

Batch E, syslog, CloudWatch, and conformance, is part done: D-3, D-4, D-13, D-14, D-15,
and D-16. CI run 37014150796 is green on `cc59a9e`.

| Id | Commit | What changed |
|---|---|---|
| D-3 | `90f7e27` | CloudWatch creates the stream on every missing-stream error |
| D-4 | `cc59a9e` | Every syslog write carries a deadline |
| D-13 | `5baf25e` | A UDP truncation reports `WLOG_CAP_REACHED` |
| D-14 | `5baf25e` | A facility outside 0 to 23 is refused |
| D-15 | `5baf25e` | A TLS address with no port uses 6514 |
| D-16 | `5baf25e` | The TLS config is cloned before the floor is set |
| D-19 | `0fa66ab` | The CloudWatch error names the code and keeps the AWS message out |
| D-11 | `6726de3` | CloudWatch drops an entry over the byte limit as `too_large` |
| D-24 | `1d393c0` | The three credentials are marked `Secret`, and a blank answer is retried, not dropped as `hec_code_-1` |
| D-17 | `fd08330` | The UDP summary form is cut to the cap, and a datagram the path refuses is skipped |

D-6, D-7, D-12, and D-22 stay open, D-24 keeps its dedup parts open, and batches F to K are
not started. CI run 37027523125 is green on `fd08330`, after one rerun: the first attempt
failed on a proxy download during a network outage and on `TestAsynq_ServerRecordsTask`,
which masks one UUID segment at random and is a separate redaction fault worth a finding.
