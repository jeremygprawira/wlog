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
func Plugin(opts ...Option) (wlog.Plugin, error)
```

`wlogotel.Enricher` is removed. `Plugin` is a Starter, a Finisher, and a Measurer. On start it
copies the active recording span's trace id and span id onto the event. With no recording span
it leaves the event alone. On finish it copies the redacted event onto that span. It never
creates a span.

## Success Criteria

1. With a valid span in `ctx`, the emitted event's `trace.trace_id`/`trace.span_id` match the
   span's own IDs, whether or not `http-std` also set them from a header.
2. With no span in `ctx` (or an invalid one), any `traceparent`-derived values from `http-std`
   pass through untouched.
3. Imports stay inside the standard library, the root module, and the OpenTelemetry modules
   named in this module's `go.mod`.

## Testing

Package `otel_test`, black-box, using `go.opentelemetry.io/otel/trace.ContextWithSpanContext`
to build a test span context directly, with no real tracer or exporter needed.

## Boundaries

- **Always:** check `IsValid()` before using a span context's IDs.
- **Ask first:** a new exported entry point besides `Plugin`.
- **Never:** create a span. This module only reads one that already exists in `ctx`.

## Open Questions

None.
