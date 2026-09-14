# wlog — Task Checklist

> Details, acceptance criteria and verification for every id: [plan.md](plan.md).
> Rule: no implementation task starts before its module spec is approved.

## Phase 0 — Foundation
- [x] T0.1 Repository scaffold
- [x] T0.2 Agent guidance + README stub
- [x] T0.3 CI workflow
- [x] T0.4 Write SPEC-core.md (drafted, awaiting approval)
- [ ] **Checkpoint 0** — scaffold green, SPEC/SPEC-redact/SPEC-core approved, first commit

## Phase 1A — redact
- [x] R1 Key-token matching, end to end
- [x] R2 Paths, globs, arrays
- [x] R3 Add / remove / replace keys, With, introspection
- [x] R4 Built-in patterns A: credit_card, email, jwt, bearer
- [x] R5 Built-in patterns B: ipv4, phone, iban, nik
- [x] R6 Pattern options + custom patterns
- [x] R7 Transforms, limits, Default/Disabled
- [x] R8 Gates G1/G2, benchmark, examples (15.7us/op, meets the 30us target; see redact/BENCH.md)
- [x] **Checkpoint 1A** — SPEC-redact criteria met, stdlib-only, benchmark meets target, approved

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
- [ ] C15 Core gates, benchmark, examples
- [ ] **Checkpoint 1B** — event shape reviewed (last cheap change)

## Phase 2 — pipeline, HTTP, herr, test tooling, audit
- [ ] S2.1 Specs: pipeline, sample, drain-memory, wlogtest
- [ ] S2.2 Specs: http-std, enrich, errors-herr, audit
- [ ] H1 net/http middleware, thin slice
- [ ] H2 Header, query, param, cookie capture + skip rules
- [ ] H3 Body capture
- [ ] H4 Panics, traceparent, user id, plugin request hooks
- [ ] H5 Conformance suite + gorilla/mux proof
- [ ] **Checkpoint 2A** — end-to-end request, 50µs budget measured
- [ ] P1 Batching + flush
- [ ] P2 Retry + backoff
- [ ] P3 Bounded buffer, drop-oldest, fan-out isolation
- [ ] P4 Shared HTTP drain helper + identity headers
- [ ] SA1 Head + tail sampling rules
- [ ] SA2 Presets + defaults
- [ ] M1 Memory drain: ring buffer + subscribe
- [ ] M2 SSE handler
- [ ] W1 wlogtest recorder
- [ ] E1 Host + deployment enrichers
- [ ] E2 User agent enricher
- [ ] E3 Geo + user-id enrichers
- [ ] EH1 herr error extractor
- [ ] A1 Audit records
- [ ] A2 Hash chain
- [ ] A3 Journal drain + Verify
- [ ] **Checkpoint 2B** — G1–G5 green, refund scenario passes

## Phase 3 — adapters, trace, examples
- [ ] S3 Specs: HTTP adapters, logger adapters, trace-otel
- [ ] HE4 Echo v4 adapter
- [ ] HE5 Echo v5 adapter
- [ ] HG Gin adapter
- [ ] LS1 slog output
- [ ] LS2 slog input
- [ ] LZ zap output
- [ ] LZR zerolog output
- [ ] LL logrus output
- [ ] TO OpenTelemetry span link
- [ ] EX1 Example apps (HTTP)
- [ ] EX2 Example apps (extension points)
- [ ] **Checkpoint 3** — 5 HTTP stacks identical, logger swap = one option

## Phase 4 — v1 drains
- [ ] S4 Specs: axiom, loki, file, webhook, otlp
- [ ] DA Axiom drain
- [ ] DL Loki drain
- [ ] DF File drain
- [ ] DW Webhook drain
- [ ] DO OTLP drain
- [ ] DI Optional docker integration tests
- [ ] **Checkpoint 4** — drains work from env alone

## Phase 5 — cli-map + v1
- [ ] S5 Spec: cli-map
- [ ] MP1 CLI skeleton + net/http/mux entry points
- [ ] MP2 Echo v4/v5 + Gin entry points
- [ ] MP3 Rules: coverage, context, errors
- [ ] MP4 Rules: sensitive-route audit, print logging, denylisted keys
- [ ] MP5 Score, report, CI gates
- [ ] MP6 go vet analyzer + dogfooding
- [ ] REL1 v1 documentation + parity audit
- [ ] **Checkpoint 5** — v1 release gate (ask first: remote, push, tag v0.1.0)

## Phase 6 — v1.1 drains
- [ ] S6 Specs: sentry, clickhouse, datadog
- [ ] DS Sentry drain
- [ ] DC ClickHouse drain
- [ ] DD Datadog drain
- [ ] **Checkpoint 6** — v1.1 (ask first: tag v0.2.0)
