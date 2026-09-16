# Customization

Every default can change through an option. An option always wins over an environment
variable, and one option of the same kind given twice wins with the last call.

```go
log := wlog.New(
    wlog.WithService("orders", "1.4.0", "prod"),
    wlog.WithRedactor(redact.MustNew(redact.AddKeys("iban"))),
)
```

## Service, level, and format

```go
wlog.WithService("orders", "1.4.0", "prod") // service.name/version/env
wlog.WithLevel(wlog.LevelInfo)              // drop debug events
wlog.WithFormat(wlog.FormatPretty)          // console output for development
```

## Redaction

```go
wlog.WithRedactor(redact.MustNew(
    redact.AddKeys("iban", "*_pin"),          // add key names and globs
    redact.RemoveKeys("token"),               // stop masking one name
    redact.AddPatterns(redact.Pattern{...}),  // add a value pattern
    redact.RemovePatterns("ipv4"),            // remove a built-in pattern
    redact.Replacement("[redacted]"),         // change the mask text
    redact.Transform(func(event map[string]any) { ... }), // custom rewrite
))
```

Swap the redactor at runtime with `log.SetRedactor(r)`. It is atomic and safe under
`-race`.

## Capture (HTTP)

```go
wlogstd.Middleware(log,
    wlogstd.SkipPaths("/health", "/metrics"),
    wlogstd.CaptureHeaders("Authorization", "X-Tenant"),
    wlogstd.CaptureQuery("page"),
    wlogstd.CaptureCookies("session"),
    wlogstd.CaptureBody("application/json"),
    wlogstd.MaxBodyCapture(4096),
    wlogstd.WithUserFunc(func(r *http.Request) string { return r.Header.Get("X-User") }),
    wlogstd.WithRouteFunc(func(r *http.Request) string { return "custom.route" }),
)
```

The Echo, Echo v5, and Gin adapters re-export the same options. Each adapter sets the
route from its own router, so `WithRouteFunc` stays in the `middleware/nethttp` package.

## Field names

```go
wlog.WithFieldNames(wlog.FieldsFlat())  // service, http.method, error
wlog.WithFieldNames(wlog.FieldsOTel())  // service.name, http.request.method
wlog.WithFieldNames(wlog.FieldNames{"level": "severity"}) // one key at a time
```

## Sampling

```go
wlog.WithSampler(sample.New(
    sample.Rate(wlog.LevelInfo, 10),                  // keep 10% of info events
    sample.KeepStatus(500),                           // always keep a 5xx
    sample.KeepDuration(time.Second),                 // always keep a slow event
    sample.KeepPath("/orders/*"),                     // always keep one route
    sample.KeepFunc(func(ctx context.Context, e map[string]any) bool { ... }),
))
```

`sample.KeepErrorsAndSlow(time.Second, 10)` is the common preset. Audit events bypass
sampling, because their reserved key is set.

## Enrichers

```go
wlog.WithEnrichers(
    enrich.Host(),        // host.name, host.pid, host.pod
    enrich.Deployment(),  // deploy.region, deploy.commit, deploy.version
    enrich.UserAgent(),   // client.browser, client.os, client.device
    enrich.Geo(),         // geo.country, geo.city from CDN headers
    enrich.User(func(ctx context.Context) string { return userIDFrom(ctx) }),
)
```

An enricher runs after sampling and before redaction, so its fields are masked like any
other field.

## Drains

A one-off drain is one function:

```go
wlog.WithDrains(wlog.DrainFunc(func(ctx context.Context, event map[string]any) {
    // ship the event
}))
```

A backend drain implements `SendBatch`, then `pipeline.Wrap` adds batching, retry, and a
bounded buffer:

```go
wlog.WithDrains(pipeline.Wrap(
    axiom.MustNew(),                        // reads AXIOM_* env
    pipeline.BatchSize(100),
    pipeline.BatchInterval(5*time.Second),
    pipeline.MaxAttempts(3),
    pipeline.MaxBuffer(1000),
    pipeline.OnDropped(func(batch []map[string]any, err error) { ... }),
))
```

The v1.1 drains use the same shape: `sentry.MustNew()`, `clickhouse.MustNew()`, and
`datadog.MustNew()`. ClickHouse needs `clickhouse.DDL(table)` run once before the
drain inserts.

Fan several drains out with `pipeline.FanOut(a, b, c)`. A drain never blocks the caller
and never panics into the request.

## Errors

```go
wlog.WithErrorExtractor(wlogherr.Extractor()) // herr code, public message, why/fix/link
wlog.WithErrorExtractor(myExtractor{})        // anything implementing ErrorExtractor
```

The default extractor sets `code: INTERNAL` and walks `errors.Unwrap` for the cause.

## Logger adapters

```go
wlog.WithDrains(wlogslog.Drain(handler)) // one slog record per event
wlog.WithDrains(wlogzap.Drain(logger))
wlog.WithDrains(wlogzerolog.Drain(logger))
wlog.WithDrains(wloglogrus.Drain(logger))

slog.SetDefault(slog.New(wlogslog.Handler(slog.Default().Handler()))) // fold slog back in
```

## Trace

`http-std` parses the W3C `traceparent` header already. Add the OpenTelemetry enricher to
take the ids from an active span instead:

```go
wlog.WithEnrichers(wlogotel.Enricher())
```

## Plugins

A plugin is any value with `Name() string` that also implements `Setup`, `Enricher`,
`Keeper`, `Drain`, `RequestStarter`, or `RequestFinisher`. One struct can implement
several.

```go
wlog.WithPlugins(myPlugin{})
```

## Typed keys

```go
var OrderID = wlog.NewKey[string]("order_id")
OrderID.Set(ctx, order.ID)               // compile error for a non-string

wlog.StrictKeys(OrderID)                 // in local/dev, flag an unregistered name
```

## Audit

```go
audit.Do(ctx, audit.Record{
    Actor:   audit.Actor{Type: "user", ID: "u-42"},
    Action:  "refund.create",
    Target:  audit.Target{Type: "order", ID: orderID},
    Outcome: "success",
})
```

Audit events are never sampled. `audit.Journal(path)` writes an append-only hash-chained
NDJSON file, and `audit.Verify(path)` checks it.

## Environment variables

| Area | Variables |
|---|---|
| core | `WLOG_SERVICE` `WLOG_VERSION` `WLOG_ENV` `WLOG_LEVEL` `WLOG_FORMAT` |
| Axiom | `AXIOM_TOKEN` `AXIOM_DATASET` `AXIOM_URL` |
| Loki | `LOKI_URL` `LOKI_USERNAME` `LOKI_PASSWORD` `LOKI_TENANT_ID` |
| file | `WLOG_FILE_PATH` |
| webhook | `WLOG_WEBHOOK_URL` |
| OTLP | `OTEL_EXPORTER_OTLP_ENDPOINT` `OTEL_EXPORTER_OTLP_HEADERS` |
| Sentry | `SENTRY_DSN` `SENTRY_ALL_EVENTS` |
| ClickHouse | `CLICKHOUSE_URL` `CLICKHOUSE_USER` `CLICKHOUSE_PASSWORD` `CLICKHOUSE_DATABASE` `CLICKHOUSE_TABLE` |
| Datadog | `DD_API_KEY` `DD_SITE` (`DD_ENV` and `DD_SERVICE` as fallbacks) |

## `wlog map` rules

```yaml
# wlog.map.yaml
sensitive_routes:
  - "^/v1/payouts"
min_score: 80
```

Run the analyzer through go vet, or register it with golangci-lint:

```yaml
# .golangci.yml
linters-settings:
  custom:
    wlogmap:
      path: $(go env GOPATH)/bin/wlogvet
      description: wlog observability rules
      original-url: github.com/jeremygprawira/wlog/cmd/wlog
```
