---
name: analyze-wlog-output
description: Read an emitted event, and answer a question from a set of them.
---

<!-- wlog:skill -->

# Analyze wlog output

One event answers one request. Read the core fields first:

- `operation`, `duration_ms`, `outcome` say what ran and how it ended.
- `service.name` and `service.env` say where.
- `trace.request_id` links one request across services.
- `error.code`, `error.why`, and `error.fix` say what failed and what to do.
- `llm.cost_micros` says what a model call spent, in whole micros.

To answer a question from many events, read the NDJSON history with `file.Read`, or a
live buffer with `memory.Query`. Filter before you count, and group by `http.route`.

A dropped field is counted in `wlog.dropped_fields`, and a dropped log line in
`wlog.dropped_logs`. A non-zero count means a cap was reached.
