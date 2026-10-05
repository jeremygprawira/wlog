# wlog and the OpenTelemetry Logs Bridge

The OpenTelemetry Logs Bridge is the part of the OpenTelemetry API that turns an
application's own log record into an OTel log record. The wlog bridge lives in
`trace/otellog`, which is its own module.

## What the bridge does

`wlogotellog.New` returns a drain. The drain sends one wlog event as one OTel log record.
The application owns the provider, so it owns batching, export, and the resource. `New`
takes a `log.LoggerProvider`, or nil for the global one.

The record takes its timestamp from the event and its severity from the level. It takes
its body from the summary and its name from the kind, as in `wlog.request`. Attributes
come from the otel output preset, so a span and a log record name the same field the same
way. A record links to its trace with no span in the context. `Send` builds a span
context from the trace ids of the event.

## The two OTel paths

| Path | Package | What it does |
|---|---|---|
| Logs Bridge | `trace/otellog` | One event becomes one OTel log record in the application's provider |
| OTLP drain | `drain/otlp` | The event is sent to an OTLP endpoint over the wire |

The bridge fits an application that already runs an OTel pipeline. The drain fits an
application that wants an OTLP endpoint and no provider.

## What the tests prove

`trace/otellog` holds the record shape and the trace link as a test. The `otel/log` API is
v0 and can change in a minor release, so the module pins the version it was written
against.

## Not measured

No page here holds a throughput or an allocation comparison between wlog and an OTel
pipeline. The repository measures wlog alone, with `make bench` and the budget in
`bench/baseline.txt`.
