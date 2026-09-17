# wlog

wlog is a Go library for wide-event logging. Your code adds fields to one event per
request or job. wlog redacts, samples, and sends that event once, to any backend.

Status: [v0.1.0](https://github.com/jeremygprawira/wlog/releases/tag/v0.1.0) released. See
[SPEC.md](docs/SPEC.md) for the full design and [CAPABILITIES.md](docs/CAPABILITIES.md) for
the module list and build order.

## Quick start

```go
package main

import (
	"net/http"

	"github.com/jeremygprawira/wlog"
	wlogstd "github.com/jeremygprawira/wlog/middleware/nethttp"
)

func main() {
	logger := wlog.New(wlog.WithService("orders", "1.4.0", "prod"))
	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders", func(w http.ResponseWriter, r *http.Request) {
		wlog.Set(r.Context(), "order_id", "4821")
		w.WriteHeader(http.StatusCreated)
	})
	http.ListenAndServe(":8080", wlogstd.Middleware(logger)(mux))
}
```

One request produces one JSON line on stdout with the method, route, status, duration,
headers, query, body, request id, and `order_id`. The default redactor masks a denied
value before any sink sees it.

`wlog.New()` needs no options. Add options only to change a default.

## Install

The root module holds every stdlib-only package. A package with a third-party import
has its own module.

| Package | Install | Tag |
|---|---|---|
| core, redact, pipeline, sample, enrich, drain/memory, drain/axiom, drain/loki, drain/file, drain/webhook, drain/otlp, drain/sentry, drain/clickhouse, drain/datadog, audit, catalog, llm, wlogtest, http-std, log/slog | `go get github.com/jeremygprawira/wlog@v0.1.0` | `v0.1.0` |
| herr extractor | `go get github.com/jeremygprawira/wlog/errors/herr@v0.1.0` | `errors/herr/v0.1.0` |
| Echo v4 | `go get github.com/jeremygprawira/wlog/middleware/echo@v0.1.0` | `middleware/echo/v0.1.0` |
| Echo v5 | `go get github.com/jeremygprawira/wlog/middleware/echo5@v0.1.0` | `middleware/echo5/v0.1.0` |
| Gin | `go get github.com/jeremygprawira/wlog/middleware/gin@v0.1.0` | `middleware/gin/v0.1.0` |
| slog, in and out | `go get github.com/jeremygprawira/wlog/log/slog@v0.1.0` | `v0.1.0` |
| zap | `go get github.com/jeremygprawira/wlog/log/zap@v0.1.0` | `log/zap/v0.1.0` |
| zerolog | `go get github.com/jeremygprawira/wlog/log/zerolog@v0.1.0` | `log/zerolog/v0.1.0` |
| logrus | `go get github.com/jeremygprawira/wlog/log/logrus@v0.1.0` | `log/logrus/v0.1.0` |
| OpenTelemetry trace link | `go get github.com/jeremygprawira/wlog/trace/otel@v0.1.0` | `trace/otel/v0.1.0` |
| `wlog map` CLI and analyzer | `go get github.com/jeremygprawira/wlog/cmd/wlog@v0.1.0` | `cmd/wlog/v0.1.0` |

The sub-modules carry the tag in the table above. The root module is `v0.1.0` today, and v0.5.0
sets the next one for every module in the same dependency order. The examples module is never
tagged.

## Guides

- [Event shape](docs/event-shape.md): every reserved key and the stage order.
- [Customization](docs/customization.md): change capture, redaction, sampling, field
  names, drains, and more.
- [Cost](docs/cost.md): price model calls and chart spend per route.
- [Best practices](docs/best-practices.md): five rules for an event worth keeping.
- [evlog parity](docs/evlog-parity.md): how each evlog feature maps to a wlog module.
- [Examples](examples): a runnable program for every framework and extension point.

## Safety gates

| Gate | Rule | Evidence |
|---|---|---|
| G1 no leak | A denied value never reaches a sink or drain | [`FuzzRedact_NeverLeaks`](redact/fuzz_test.go), [`TestAxiom_NeverLeaksRedactedValue`](drain/axiom/axiom_test.go), and one leak test per drain |
| G2 race-free | `go test -race` passes on every module | [`make race`](Makefile) |
| G3 never blocks | A slow or panicking drain, enricher, or extractor changes nothing | [`TestCore_Drain_PanicIsolated`](drain_test.go), [`TestCore_StageOrder_DroppedEventSkipsEnrichAndSinks`](stages_test.go) |
| G4 bounded memory | Keys, errors, logs, bodies, buffers, and channels are capped | [`TestCore_Set_KeyCap`](event_test.go), [`TestCore_Error_ListCappedAtTen`](errors_test.go), [`TestAppendLog_FoldsAndCaps`](logs_test.go) |
| G5 audit integrity | The hash chain covers the redacted bytes | [`TestAudit_Verify_DetectsEditedByte`](audit/journal_test.go), [`TestAudit_Verify_DetectsReorderedLines`](audit/journal_test.go) |
| G6 map determinism | Two `wlog map` runs give the same bytes | [`TestGolden`](cmd/wlog/main_test.go) |

## Success criteria

The 15 criteria from [SPEC.md](docs/SPEC.md#success-criteria), with the test that
proves each one.

| # | Criterion | Evidence |
|---|---|---|
| 1 | Zero-config value | `TestConformance` in `middleware/nethttp` |
| 2 | Framework-agnostic | `TestHTTPParity` in `examples`, plus `TestConformance` in every adapter |
| 3 | Error-library-agnostic | `TestExtractor_MapsHerrFields`, `TestCustomExtractor_MapsErrorCode` |
| 4 | Logger-agnostic | `TestSlogOutput_OneRecordWithNestedGroup`, `TestSlogInput_FoldsRecordIntoLogs`, `TestZapOutput_OneEntryWithNestedField`, `TestZerologOutput_OneRecordWithNestedDict`, `TestLogrusOutput_FlattensNestedGroups` |
| 5 | Backend-agnostic | `TestCustomDrain_ReceivesEvent`, `TestAxiom_EnvAlone`, `TestLoki_EnvAlone`, `TestFile_EnvAlone`, `TestWebhook_EnvAlone`, `TestOTLP_EnvHeadersAndEndpoint` |
| 6 | Customizable | `TestConformance` and the option tests in `middleware/nethttp`, `TestWlogtest_UserOptionsApply`, every `examples/*` test |
| 7 | Denylist add and remove, runtime swap | `TestRedactor_Denies`, `TestCore_SetRedactor_ConcurrentSwapsAndEmits`, `FuzzRedact_NeverLeaks` |
| 8 | Gates G1–G4 and Go 1.23 | `make race`, `make compat` |
| 9 | Budget under 50us p50 | `BenchmarkMiddleware` in `middleware/nethttp/bench_test.go` |
| 10 | v1 drains, v1.1 drains, and the audit journal | `TestAxiom_SendBatch_NDJSON`, `TestLoki_SendBatch_GroupsByLabelSet`, `TestFile_AppendNDJSON`, `TestWebhook_JSONArray`, `TestOTLP_SendBatch_Golden`, `TestSentry_SendBatch_ErrorEnvelope`, `TestClickHouse_SendBatch_JSONEachRow`, `TestDatadog_SendBatch_JSONArray`, `TestAudit_Journal_WritesVerifiableNDJSON` |
| 11 | Audit survives a 0% sampler and verifies | `TestAudit_RefundScenario`, `TestAudit_BypassesSampling`, `TestAudit_Verify_DetectsEditedByte` |
| 12 | One plugin, many hooks, panic isolated | `TestCore_Plugin_AllHooksWired`, `TestCore_Plugin_PanicIsolatedAndReported` |
| 13 | Typed fields and StrictKeys | `TestCore_Key_SetStoresUnderItsName`, `TestCore_StrictKeys_FlagsUnregisteredKey_InDev`, `TestTypedKeys_SetAndFlagTypo` |
| 14 | `wlog map` is deterministic and gates CI | `TestGolden`, `TestGates`, `make map` |
| 15 | evlog parity | [docs/evlog-parity.md](docs/evlog-parity.md): every evlog page mapped, with the open gaps ranked |
