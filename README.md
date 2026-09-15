# wlog

wlog is a Go library for wide-event logging. Your code adds fields to one event per
request or job. wlog redacts, samples, and sends that event once, to any backend.

Status: pre-v0, under active development. See [SPEC.md](docs/SPEC.md) for the full
design and [CAPABILITIES.md](docs/CAPABILITIES.md) for the module list and build order.

## Examples

Each directory under [examples/](examples) is a small, runnable program.

| Example | Shows |
|---|---|
| [nethttp](examples/nethttp) | wlog with net/http |
| [echo](examples/echo) | wlog with Echo v4 |
| [echo5](examples/echo5) | wlog with Echo v5 |
| [gin](examples/gin) | wlog with Gin |
| [mux](examples/mux) | wlog with gorilla/mux (its own module) |
| [detach](examples/detach) | Detach for work that outlives a request |
| [custom-drain](examples/custom-drain) | A hand-written Drain |
| [custom-extractor](examples/custom-extractor) | A custom ErrorExtractor |
| [plugin](examples/plugin) | A plugin that enriches every event |
| [audit-refund](examples/audit-refund) | An audit record on a refund |
| [typed-keys](examples/typed-keys) | Typed keys and StrictKeys |
| [logger-output](examples/logger-output) | Events to zap and slog, slog calls folded back |
