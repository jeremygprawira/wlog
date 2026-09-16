# wlog — Task Checklist

> Details, acceptance criteria and verification for every id: [plan.md](plan.md).
> Rule: no implementation task starts before its module spec is approved.

## Phase 0 — Foundation
- [x] T0.1 Repository scaffold
- [x] T0.2 Agent guidance + README stub
- [x] T0.3 CI workflow
- [x] T0.4 Write SPEC-core.md (drafted, awaiting approval)
- [x] **Checkpoint 0**: scaffold green, SPEC/SPEC-redact/SPEC-core approved, first commit

## Phase 1A — redact
- [x] R1 Key-token matching, end to end
- [x] R2 Paths, globs, arrays
- [x] R3 Add / remove / replace keys, With, introspection
- [x] R4 Built-in patterns A: credit_card, email, jwt, bearer
- [x] R5 Built-in patterns B: ipv4, phone, iban, nik
- [x] R6 Pattern options + custom patterns
- [x] R7 Transforms, limits, Default/Disabled
- [x] R8 Gates G1/G2, benchmark, examples (15.7us/op, meets the 30us target, see redact/BENCH.md)
- [x] **Checkpoint 1A**: SPEC-redact criteria met, stdlib-only, benchmark meets target, approved

## Phase 1B — core
- [x] C1 Thin wide event: Start → Set → emit JSON
- [x] C2 SetGroup, Append, normalization, caps
- [x] C3 Levels, SetLevel, outcome
- [x] C4 Errors: extractor, error + errors[]
- [x] C5 Detach + sealed events
- [x] C6 Drains, OnError, Close
- [x] C7 Stage order
- [x] C8 Atomic redactor swap
- [x] C9 Field-name presets + renaming
- [x] C10 Plugins
- [x] C11 Typed keys + StrictKeys
- [x] C12 Plain one-off log lines
- [x] C13 Pretty console sink
- [x] C14 Env configuration
- [x] C15 Core gates, benchmark, examples (10.25us/op, coverage 89.7%, examples cover the main entry points, not every exported symbol, see commit)
- [x] **Checkpoint 1B**: event shape reviewed and approved

## Phase 2 — pipeline, HTTP, herr, test tooling, audit
- [x] S2.1 Specs: pipeline, sample, drain-memory, wlogtest
- [x] S2.2 Specs: http-std, enrich, errors-herr, audit
- [x] H1 net/http middleware, thin slice
- [x] H2 Header, query, cookie capture + skip rules (path params: none yet, ServeMux's r.PathValue is available to handlers directly)
- [x] H3 Body capture
- [x] H4 Panics, traceparent, user id, plugin request hooks
- [x] H5 Conformance suite passing for wlogstd and for gorilla/mux (examples/mux, go get approved)
- [x] **Checkpoint 2A**: end-to-end request works (24.8us/op, under the 50us budget)
- [x] P1 Batching + flush
- [x] P2 Retry + backoff
- [x] P3 Bounded buffer, drop-oldest, fan-out isolation
- [x] P4 Shared HTTP drain helper + identity headers
- [x] SA1 Head + tail sampling rules
- [x] SA2 Presets + defaults
- [x] M1 Memory drain: ring buffer + subscribe
- [x] M2 SSE handler
- [x] W1 wlogtest recorder
- [x] E1 Host + deployment enrichers
- [x] E2 User agent enricher
- [x] E3 Geo + user-id enrichers
- [x] EH1 herr error extractor
- [x] A1 Audit records
- [x] A2 Hash chain
- [x] A3 Journal drain + `audit.Verify`
- [x] **Checkpoint 2B**: G1 through G5 green, refund scenario passes as an integration test, human review complete

## Phase 3 — adapters, trace, examples
- [x] S3 Specs: HTTP adapters, logger adapters, trace-otel
- [x] HE4 Echo v4 adapter
- [x] HE5 Echo v5 adapter
- [x] HG Gin adapter
- [x] LS1 slog output
- [x] LS2 slog input
- [x] LZ zap output
- [x] LZR zerolog output
- [x] LL logrus output
- [x] TO OpenTelemetry span link
- [x] EX1 Example apps (HTTP)
- [x] EX2 Example apps (extension points)
- [x] **Checkpoint 3**: 5 HTTP stacks identical, logger swap = one option

## Phase 4 — v1 drains
- [x] S4 Specs: axiom, loki, file, webhook, otlp
- [x] DA Axiom drain
- [x] DL Loki drain
- [x] DF File drain
- [x] DW Webhook drain
- [x] DO OTLP drain
- [x] DI Optional docker integration tests
- [x] **Checkpoint 4**: drains work from env alone, G1 covered per drain

## Phase 5 — cli-map + v1
- [x] S5 Spec: cli-map
- [x] MP1 CLI skeleton + net/http/mux entry points
- [x] MP2 Echo v4/v5 + Gin entry points
- [x] MP3 Rules: coverage, context, errors
- [x] MP4 Rules: sensitive-route audit, print logging, denylisted keys
- [x] MP5 Score, report, CI gates
- [x] MP6 go vet analyzer + dogfooding
- [x] REL1 v1 documentation + parity audit
- [x] **Checkpoint 5**: v1 release gate. Tagged and released as v0.1.0 on GitHub.

## Phase 6 — v1.1 drains
- [x] S6 Specs: sentry, clickhouse, datadog
- [x] DS Sentry drain
- [x] DC ClickHouse drain
- [x] DD Datadog drain
- [x] **Checkpoint 6**: v1.1 (tag v0.2.0 still needs approval)

## Phase 7 — v1.2, public API (evlog gaps 1, 2, 3, 4, 5, 12, 15)
- [x] D1 Plain-English pass over the older specs
- [x] CE1 `ErrorInfo` data and internal split
- [x] CE2 Global modes: enabled, silent, raw values
- [x] CT1 Catalog registry
- [x] CT2 Catalog extractor, agnostic proof
- [x] CT3 herr catalog bridge
- [x] LM1 LLM record and event fields
- [x] LM2 Pricing, cost, enricher
- [x] AX1 Audit record extras
- [x] AX2 `audit.Diff` and the test mock
- [x] AX3 HMAC signing and catalog-driven audit
- [x] MQ1 Memory named stores and queries
- [x] FR1 File reader and tailer
- [x] RF1 Redaction replacement function
- [x] **Checkpoint 7**: v1.2 (tag v0.3.0 still needs approval)

## Phase 8 — v1.3, CLI (evlog gaps 6, 7, 8, 9, 10)
- [x] MR1 Rule: error guidance
- [x] MR2 Rule: swallowed error
- [ ] MR3 Suggestions: catalog use and audit coverage
- [ ] MS1 Entry classes, grades, per-entry weighting
- [ ] MS2 Report forms and strict baseline
- [ ] CI1 `wlog init`
- [ ] CI2 `wlog doctor`
- [ ] CI3 `wlog agents`
- [ ] **Checkpoint 8**: v1.3 (ask first: tag v0.4.0)

## Phase 9 — v1.4, drains, example, docs (evlog gaps 11, 13, 14)
- [ ] DP PostHog drain
- [ ] DB Better Stack drain
- [ ] DH HyperDX drain
- [ ] EXL AWS Lambda example
- [ ] REL2 Best-practice guide and parity close-out
- [ ] **Checkpoint 9**: v1.4 (ask first: tag v1.0.0, the first stable API promise)
