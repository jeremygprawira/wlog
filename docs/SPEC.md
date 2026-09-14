# Spec: wlog (project-wide)

> Shared contract for every module in [CAPABILITIES.md](CAPABILITIES.md). Module specs
> (`SPEC-<id>.md`) add module-specific objectives and criteria; they may not contradict this file.

## Objective

**wlog** ("wide log") is a Go library for **wide-event logging**: one rich, structured event per
unit of work instead of scattered log lines. Code adds to the event through `context.Context`
as work progresses; the event is enriched, redacted, sampled and emitted exactly once at the end.

It generalizes the logger proven in `go-echo-boilerplate` (`internal/pkg/logger` +
`middleware/logger.go`) and adopts the best of [evlog](https://www.evlog.dev/): auto-redaction,
head/tail sampling, drain pipeline, structured error guidance, pretty dev output, enrichers.

**Users:** Go backend teams who want observability that drops into any framework, any error
library, any logging library and any log backend, without editing wlog.

### Design pillars (in priority order)

1. **Useful with zero config.** `wlog.New()` + one middleware line gives a complete, redacted
   wide event per request on stdout. Defaults are rich (capture everything).
2. **Customizable everywhere.** Every default is an option; every edge is an interface:
   framework, error library, logging library, field names, enrichers, sampling, redaction, drains.
3. **Agnostic.** Nothing in core knows about Echo, Gin, herr, zap or any vendor. Adapters are
   separate modules you import only if you need them.
4. **Safe by construction.** Redaction runs before every sink, stdout included, in every
   environment, and cannot be skipped per drain.
5. **Never hurts the app.** Logging failures, slow backends or full buffers never block, panic
   or change a response. Usability wins over raw speed **within the budget** in Success Criteria.

### Target usage (illustrative — `SPEC-core.md` owns the real API)

```go
log := wlog.New(
    wlog.WithService("go-customer", "1.4.0", "prod"),
    wlog.WithRedactor(redact.MustNew(redact.AddKeys("nik", "*_pin"))),
    wlog.WithErrorExtractor(wlogherr.Extractor()),
    wlog.WithEnrichers(enrich.Host(), enrich.UserAgent()),
    wlog.WithSampler(sample.KeepErrorsAndSlow(time.Second, 0.10)),
    wlog.WithDrains(pipeline.Wrap(axiom.New()), pipeline.Wrap(loki.New())), // read AXIOM_*/LOKI_* env
)
defer log.Close(context.Background())                    // flush buffered events

e := echo.New()
e.Use(wlogecho.Middleware(log))                          // or wlogstd.Middleware(log) / wloggin.Middleware(log)

// anywhere below the handler:
wlog.Set(ctx, "order_id", order.ID)
wlog.SetGroup(ctx, "payment", "method", "va", "amount", 150000)
wlog.Error(ctx, err)                                     // detail pulled by the plugged extractor

go func(ctx context.Context) {                           // work that outlives the response
    ctx, end := wlog.Detach(ctx, "send_receipt_email")   // child event, linked by request/trace id
    defer end()
    wlog.Set(ctx, "provider", "ses")
}(ctx)
```

## Decisions (settled 2026-09-14 — don't re-litigate)

| Topic | Decision |
|---|---|
| Packaging | Multi-module like herr; root module stdlib-only |
| Go version | **Go 1.23+** for root and all modules, except `errors-herr` which follows herr's `go 1.26.1` |
| Denylist runtime policy | **Hybrid (C):** immutable `*redact.Redactor` values; logger holds an `atomic.Pointer`; `log.SetRedactor(next)` swaps the whole config at runtime |
| Redaction default | On in **every** environment; `redact.Disabled()` is the explicit opt-out |
| Units of work | Core is transport-neutral (`wlog.Start`). v1 ships **HTTP only**; jobs, queues, gRPC, outbound HTTP are later adapters |
| Frameworks | net/http + gorilla/mux (one middleware), Echo v4, Echo v5, Gin — all thin adapters over `http-std` |
| HTTP capture default | **Everything**: method, route, path, status, duration, sizes, IP, UA, headers, query, path params, cookies, request + response bodies (10KB cap each, JSON parsed when possible). Every item toggleable globally and per route; skip paths (e.g. `/health`) and content-type filters |
| Event shape default | **Namespaced snake_case**: wlog fields nested under `http`, `error`, `service`, `trace`, `user`; user keys at top level. Presets `FieldsOTel()`, `FieldsFlat()` and per-key renaming |
| Errors | Main `error` (the one that decided the outcome) + `errors[]` of earlier ones (cap 10). `ErrorInfo` = code, message, kind, status, cause, stack, **why, fix, link**, plus free-form attrs. Any error library plugs in via `ErrorExtractor`; std `errors` fallback built in |
| Background goroutines | `wlog.Detach(ctx, name)` → linked child event (parent request_id/trace_id), emitted when the goroutine ends. Writes to a parent event after it was emitted are ignored and counted |
| Logger interop | **Output** (write events through slog/zap/zerolog/logrus) in v1; **plain one-off lines** (`log.Info("server started")`) in v1; **input** (fold existing log calls into the event, capped `logs[]`) for slog in v1, zap/zerolog/logrus later |
| Sampling default | Keep 100%. Ready-made presets opt-in (e.g. keep errors, 4xx, slow; 10% of the rest) |
| Drains | Pluggable via one `Drain` interface. v1: stdout, Axiom, Loki, File, Webhook, OTLP. v1.1: Sentry, ClickHouse, Datadog. All built-ins use stdlib HTTP (no vendor SDKs) |
| Sentry | Error events → Sentry issues grouped by error code, wide event attached; all-events mode opt-in |
| Overflow | Bounded buffer, **drop oldest**, `OnDropped` callback + dropped counter; no disk spill |
| Config | Code options + env vars (e.g. `AXIOM_TOKEN`, `LOKI_URL`, `WLOG_LEVEL`); **code wins** over env |
| v1 extras | Pretty console (auto when env is `local`/`dev`), `wlogtest` recorder, built-in enrichers, W3C `traceparent` in core + optional OTel span module |
| Release | Public `github.com/jeremygprawira/wlog`, MIT, `v0.x` until v1 modules are proven in go-echo-boilerplate |
| Pipeline order | Fixed per event: **accumulate → emit trigger → keep (tail) / sample (head) → enrich → redact → rename fields → sinks/drains.** Dropped events are never enriched; enriched fields are always redacted |
| Level control | `wlog.SetLevel(ctx, level)` overrides the level inferred from status/errors |
| Plugins | `wlog.WithPlugins(p...)`: a plugin is any value with `Name() string` that also implements any subset of optional hook interfaces: `RequestStarter`, `Enricher`, `Keeper`, `Drain`, `RequestFinisher`, `Setup`. Each hook is panic-isolated |
| Typed fields | Opt-in `wlog.Key[T]` (`var OrderID = wlog.NewKey[string]("order_id")`, `OrderID.Set(ctx, id)`) gives compile-time key/type safety; `wlog.StrictKeys(...)` flags unregistered keys (dev: warning in event, prod: allowed). Untyped `Set` stays |
| Audit | `wlog.Audit(ctx, audit.Record{Actor, Action, Target, Outcome, Reason})` sets reserved `audit` field; audit events bypass sampling; hash chain (`audit.prev_hash`, `audit.hash` = SHA-256 over the **redacted** canonical JSON); `audit.Journal` append-only fsync'd NDJSON drain + `audit.Verify(path)`; usable inside or outside a request |
| Stream | `drain/memory`: ring buffer (default 1000), `Snapshot()`, `Subscribe(ctx)` channel (slow subscribers drop, never block), optional `SSEHandler()`; local-process only |
| Geo | `enrich.Geo()` reads CDN headers (Cloudflare `CF-IPCountry`, CloudFront `CloudFront-Viewer-*`, Vercel `X-Vercel-IP-*`, custom header map) into `geo.*` |
| Identity headers | Every built-in HTTP drain sends `User-Agent: wlog/<version>` and `X-Wlog-Source: <drain>`; overridable or disabled per drain |
| `wlog map` CLI | In v1 (phase 5). `go/analysis` + `go/packages`; finds entry points (net/http, mux, Echo, Gin handlers); rules: handler covered by wlog middleware, business context set, returned errors reach wlog, sensitive routes (payment/auth/transfer patterns, 2× weight, configurable) call `Audit`, no `fmt.Print*`/`log.Print*` in handlers, no denylisted key names passed to `Set`. Deterministic 0–100 score, top-3 fixes, `wlog.map.json`, `--min-score`, `--baseline`; same rules exported as a `go vet`-compatible analyzer |

## Tech Stack

- Go **1.23+** (dev workspace `go.work` on 1.26.1 because of the herr module)
- Root module: standard library only (`log/slog`, `net/http`, `encoding/json`, `regexp`, `sync/atomic`, `crypto/rand`)
- Sub-modules and their single third-party dependency:

| Module path | Requires |
|---|---|
| `wlog/errors/herr` | `github.com/jeremygprawira/herr` v0.1.0 |
| `wlog/middleware/echo` | `github.com/labstack/echo/v4` |
| `wlog/middleware/echo5` | `github.com/labstack/echo/v5` |
| `wlog/middleware/gin` | `github.com/gin-gonic/gin` |
| `wlog/log/zap` · `log/zerolog` · `log/logrus` | `go.uber.org/zap` · `github.com/rs/zerolog` · `github.com/sirupsen/logrus` |
| `wlog/trace/otel` | `go.opentelemetry.io/otel/trace` |
| `wlog/cmd/wlog` | `golang.org/x/tools` (`go/analysis`, `go/packages`) |

## Commands

```bash
make test        # go test ./... in every module listed in go.work
make race        # go test -race ./... in every module                                  (gate G2)
make fuzz        # go test -run=xxx -fuzz=FuzzRedact_NeverLeaks -fuzztime=30s ./redact   (gate G1)
make bench       # go test -run=xxx -bench=. -benchmem ./ ./redact ./pipeline ./middleware/nethttp
make lint        # go vet ./... && golangci-lint run ./...  in every module
make tidy        # go mod tidy in every module && go work sync
make cover       # coverage report → coverage/coverage.html
make compat      # go test ./... with Go 1.23 toolchain (GOTOOLCHAIN=go1.23.0) on the root module
make map         # go run ./cmd/wlog map --min-score 80 ./examples/...   (dogfoods the CLI)
```

Single test: `go test -race -run TestRedact_KeyTokenMatch ./redact`

## Project Structure

```
wlog/
├── go.mod  go.work  Makefile  README.md  LICENSE (MIT)
├── CAPABILITIES.md  SPEC.md  SPEC-<id>.md     → specs (living docs)
├── CLAUDE.md                                   → agent guidance + golden rules
├── tasks/plan.md  tasks/todo.md                → plan + task list
│
├── *.go                  → core (package wlog)
├── sink/                 → JSON + pretty console sinks
├── redact/  sample/  pipeline/  enrich/  wlogtest/  audit/
├── cmd/wlog/  (go.mod)   → cli-map (`wlog map`) + analyzer package
├── middleware/
│   ├── nethttp/          → http-std   (package wlogstd)
│   ├── echo/   (go.mod)  → http-echo  (package wlogecho)
│   ├── echo5/  (go.mod)  → http-echo5 (package wlogecho)
│   └── gin/    (go.mod)  → http-gin   (package wloggin)
├── errors/herr/ (go.mod) → errors-herr (package wlogherr)
├── log/
│   ├── slog/             → log-slog   (package wlogslog)
│   └── zap/ zerolog/ logrus/  (each go.mod)
├── trace/otel/ (go.mod)  → trace-otel
├── drain/
│   └── memory/ axiom/ loki/ file/ webhook/ otlp/ sentry/ clickhouse/ datadog/   (stdlib, root module)
├── internal/             → shared unexported helpers (glob, json tree, env, httpfake)
└── examples/ (go.mod)    → runnable: nethttp, mux, echo, echo5, gin, detach, custom-drain, custom-extractor
```

## Code Style

Follows herr: a doc comment on every package/type/func explaining the **flow**, functional
options, unexported concrete types behind small interfaces, no package-level mutable state,
fail loud at construction, fail safe at runtime.

```go
// Package redact scrubs sensitive data from a wide event before it reaches any sink.
//
// Read top to bottom: New compiles options into an immutable *Redactor; Apply walks one
// event snapshot and masks it in place. A *Redactor never changes after New returns — to
// change the denylist, derive a new one with With and swap it on the logger.
package redact

// AddKeys extends the key denylist. Entries are case-insensitive; an entry without a dot
// matches that key at any depth, an entry with a dot is a path from the event root, and
// `*` globs within one segment ("*_token", "headers.*").
func AddKeys(keys ...string) Option {
	return func(c *config) { c.keys = append(c.keys, keys...) }
}
```

- Adapter packages are prefixed `wlog…` (`wlogecho`, `wloggin`, `wlogherr`, `wlogstd`, `wlogslog`)
  so they never clash with the library they wrap.
- Constructors return errors; `Must…` variants for `main`. Runtime paths (`Set`, emit, drains)
  never return errors to app code — failures go to an `OnError` hook.
- Event keys `snake_case`. Core-owned keys are reserved and listed in `SPEC-core.md`.
- Every extension point is an interface with a function adapter
  (`type Drain interface{…}` + `DrainFunc`), so a one-off plug-in is one closure.
- `gofmt`, `go vet`, `golangci-lint` clean. Use `any`, not `interface{}`.
- Prose (README, `docs/`, specs, plans, doc comments, commit messages) uses Simple English, as defined in
  [AminBlg/SimpleEnglish](https://github.com/AminBlg/SimpleEnglish). Use 20 words at most per instruction and
  25 per description. Use active voice and the modals can, will and must. Do not use semicolons or em-dashes.
  Do not change code, identifiers, commands or paths.

## Testing Strategy

- **Strict TDD, vertical slices:** one failing test → minimal code → refactor → commit.
- Root module uses **stdlib `testing` only** (keeps `go.mod` dependency-free). Sub-modules may use testify.
- **Black-box tests** in `package <name>_test`; white-box only for unexported algorithms (tokenizer, glob).
- Levels:
  - Unit: every module, table-driven.
  - Fuzz: `redact` (G1), JSON sink escaping.
  - Race: everything under `-race` (G2), including concurrent `Set` + emit + `SetRedactor` + `Detach`.
  - Integration: middleware through `httptest` for net/http, mux, Echo v4/v5, Gin — one shared
    conformance suite that every HTTP adapter must pass with identical output.
  - Drains: against `httptest.Server` fakes asserting the vendor wire format. **No real network in
    `go test`.** Optional `//go:build integration` tests hit local docker Loki/ClickHouse/OTel collector.
  - Benchmarks: hot paths with budgets in module specs; regressions >20% block merge.
- Coverage ≥ 85% statements per root-module package; every exported function has an `Example…` test.
- `make compat` proves the Go 1.23 floor.

### Safety gates (must never regress)

| Gate | Invariant | Guarded by |
|---|---|---|
| **G1 no leak** | A value denied by the active redactor never appears in any sink or drain output | `FuzzRedact_NeverLeaks` + test running every built-in sink/drain fake |
| **G2 race-free** | No shared mutable state without synchronization | `make race` over concurrent enrich/emit/swap/detach tests |
| **G3 never blocks** | A failing, slow or panicking drain/enricher/extractor cannot block, panic or change the HTTP response | pipeline + middleware tests with hanging/panicking fakes |
| **G4 bounded memory** | Event keys, `errors[]`, `logs[]`, captured bodies, drain buffers, ring buffer and subscriber channels are all capped | tests exceeding each cap assert drop + counter |
| **G5 audit integrity** | Audit events are never sampled out; editing, reordering or deleting any journal line makes `audit.Verify` fail | tests with aggressive samplers + tampered journal fixtures |
| **G6 map determinism** | Same source + same CLI version → byte-identical `wlog.map.json` | golden-file tests over `cmd/wlog/testdata` fixture apps |

## Boundaries

- **Always:**
  - Write the failing test first; commit per green cycle with a Co-Authored-By trailer.
  - Run `make race` and `make fuzz` when touching redaction, event storage or emit.
  - Keep the root `go.mod` free of third-party requirements.
  - Update the module's `SPEC-<id>.md` *before* changing behavior it specifies.
  - Add every new HTTP adapter to the shared conformance suite.
- **Ask first:**
  - Adding any dependency, to any module.
  - Changing the default denylist, built-in patterns, or capture defaults (security-relevant).
  - Changing the default event field names or reserved keys once `core` is approved.
  - Changing public API after the first tag.
  - Creating the GitHub remote, pushing or tagging a release.
- **Never:**
  - Let a sink or drain receive an event that has not passed through the redactor.
  - Package-level mutable state (e.g. the boilerplate's `SensitiveFields` slice).
  - Block, panic, or return logging errors into app code.
  - Import a framework, error library, logging library or vendor SDK in the root module.
  - Make real network calls in default `go test`.
  - Edit `~/Documents/herr` or `~/Documents/go-echo-boilerplate` as part of this initiative.

## Success Criteria (initiative-level)

1. **Zero-config value:** `wlog.New()` + one middleware line emits one redacted wide event per
   request with method, route, status, duration, headers, query, bodies and request id.
2. **Framework-agnostic:** net/http, gorilla/mux, Echo v4, Echo v5 and Gin examples pass the same
   conformance suite and produce identical core fields; each needs ≤ 5 lines of wlog setup.
3. **Error-library-agnostic:** swapping herr ↔ std `errors` ↔ a custom `ErrorExtractor` changes one option and no other code; `why`/`fix`/`link` appear when the extractor provides them.
4. **Logger-agnostic:** the same event can be written through slog, zap, zerolog or logrus by changing one option; slog calls made during a request appear in the event's `logs[]`.
5. **Backend-agnostic:** a custom drain is one function (`wlog.DrainFunc`); all v1 drains work from env vars alone.
6. **Customizable:** every capture item, field name, enricher, sampler, redaction rule and drain can be changed through options; each has an example test.
7. **Denylist:** keys and patterns can be added and removed; `log.SetRedactor` swaps config at runtime under `-race` with no dropped or half-redacted events.
8. **Gates G1–G4** pass in CI on every module; `make compat` passes on Go 1.23.
9. **Budget:** middleware + core overhead ≤ 50µs p50 per request with default capture and a 1KB JSON body, excluding drain network time, on an M-series Mac. Anything that breaks the budget becomes opt-in.
10. v1 ships stdout, Memory, Axiom, Loki, File, Webhook and OTLP drains plus the audit journal; v1.1 adds Sentry, ClickHouse and Datadog.
11. **Audit:** a refund handler calling `wlog.Audit` produces an event that survives a 0% sampler, appears in both the main drain and the journal, and `audit.Verify` passes on the journal (fails if one byte changes).
12. **Plugins:** a single struct implementing `RequestStarter` + `Enricher` + `Drain` works across all HTTP adapters; a panicking hook doesn't affect the response or other plugins.
13. **Typed fields:** `OrderID.Set(ctx, 42)` fails to compile for a `Key[string]`; with `StrictKeys` in dev, an unregistered key is flagged in the event.
14. **`wlog map`:** on the fixture apps it scores deterministically, lists top-3 fixes, exits non-zero under `--min-score` or on regression vs `--baseline`, and `make map` passes on `examples/`.
15. **Evlog parity:** every non-TypeScript-specific feature in the evlog docs index has an equivalent module or a documented "designed for" entry in CAPABILITIES.md.

## Open Questions

None blocking. Detail-level choices (exact reserved keys, env var names, Loki label set,
ClickHouse DDL, pretty-console layout) belong to each module's spec and get reviewed there.
