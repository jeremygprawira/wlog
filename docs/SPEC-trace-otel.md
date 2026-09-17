# Spec: trace-otel

> Module id `trace-otel`. Package `github.com/jeremygprawira/wlog/trace/otel` (`wlogotel`).
> Own `go.mod`, since it pulls in OpenTelemetry. Depends on `core`. Project-wide rules in
> [SPEC.md](SPEC.md) apply.

## Objective

Fill `trace.trace_id` and `trace.span_id` from an active OpenTelemetry span. This serves a team
that already runs OTel tracing, without requiring OTel for everyone else. `http-std`'s own
`traceparent` parsing already covers the common case, with zero extra dependencies.

## Pinned versions

| Module | Version |
|---|---|
| `go.opentelemetry.io/otel` | v1.46.0 |
| `go.opentelemetry.io/otel/trace` | v1.46.0 |

## Behaviour

<!-- snippet:sketch -->
```go
func Enricher() wlog.Enricher
```

`Enricher()` reads `trace.SpanContextFromContext(ctx)`. When that span context is valid (checked
with the OTel API's own `IsValid()`), it sets `trace.trace_id` and `trace.span_id` from it. If `http-std` set `traceparent`-derived values earlier, this call is the only thing that
overrides them. It never writes an empty or invalid value over ones that are already there.

## Success Criteria

1. With a valid span in `ctx`, the emitted event's `trace.trace_id`/`trace.span_id` match the
   span's own IDs, whether or not `http-std` also set them from a header.
2. With no span in `ctx` (or an invalid one), any `traceparent`-derived values from `http-std`
   pass through untouched.
3. Zero imports outside the standard library plus `core`, `go.opentelemetry.io/otel`, and
   `go.opentelemetry.io/otel/trace`.

## Testing

Package `otel_test`, black-box, using `go.opentelemetry.io/otel/trace.ContextWithSpanContext`
to build a test span context directly, with no real tracer or exporter needed.

## Boundaries

- **Always:** check `IsValid()` before using a span context's IDs.
- **Ask first:** adding OTel metrics or log-bridge support beyond this one trace-id enricher.
- **Never:** create a span. This module only reads one that already exists in `ctx`.

## Open Questions

None.
