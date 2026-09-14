# wlog

wlog is a Go library for wide-event logging. Your code adds fields to one event per
request or job. wlog redacts, samples, and sends that event once, to any backend.

Status: pre-v0, under active development. See [SPEC.md](docs/SPEC.md) for the full
design and [CAPABILITIES.md](docs/CAPABILITIES.md) for the module list and build order.
