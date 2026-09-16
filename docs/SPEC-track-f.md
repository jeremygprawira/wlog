# Spec: search and agents (track F)

> Phase 12 · depends on: `core-shape`, `core-problems`, `event-schema`, `drain-file`,
> `drain-memory`, `cli-agents`. Module ids: `cli-query`, `cli-explain`, `cli-mcp`, and
> `agent-docs` in module `cmd/wlog`. Also `search-recipes` in `integrations/search`,
> `drain-memory` additions in the root module, and `recipes` in `docs/recipes` and `examples`.
> Project-wide rules in [SPEC.md](SPEC.md) apply.

## Objective

Make the events easy to search in three places: a terminal, an agent's shell or MCP client, and
a team's own backend. One question, such as "which requests failed with
`PAYMENT_DECLINED` in the last hour", has one short answer in each place.

## cli-query: `wlog query` and `wlog tail`

```
wlog query [flags] [source...]
wlog tail  [flags] [source...]     # same as: wlog query --follow
```

Sources: files, folders (reads `*.ndjson`, `*.jsonl`, `*.log`, and `.gz` of each), `-` for
stdin, and `--url` for a live app's memory endpoint. With no source and a piped stdin, the command
reads stdin. With no source and no pipe, it reads `.wlog/logs/`, the default folder of
`drain-file`.

| Flag | Meaning |
|---|---|
| `--level error,warn` | Levels to keep |
| `--kind request,message` | Kinds to keep |
| `--since 15m` / `--until <time>` | A duration back from now, or an RFC 3339 time |
| `--op 'POST /orders/*'` | A glob on `operation`, where `**` crosses `/` |
| `--status '>=500'` | Compares `http.status`, `rpc.status_code`, or `cli.exit_code`, whichever the event has |
| `--code PAYMENT_DECLINED` | `error.code` equals the value |
| `--trace <id>` / `--request-id <id>` / `--event-id <id>` | Exact id match |
| `--where 'llm.cost_micros>1000'` | Repeatable. Operators: `=`, `!=`, `>`, `>=`, `<`, `<=`, `~` (regex), `?` (exists) |
| `--text 'declined'` | Substring of `summary` or `message`, ignoring case |
| `--format summary\|json\|pretty\|table` | Default `summary` on a terminal, `json` otherwise |
| `--fields a,b,c` | Columns for `table`, or keys to keep for `json` |
| `--limit 100` / `--oldest` | Newest N matches by default |
| `--group-by http.route --count` | Count per group, sorted by count |
| `--stats duration_ms` | Count, p50, p95, p99, and max of a numeric field. With `--group-by`, one row per group |
| `--follow` | Keep reading appended lines and rotated files |
| `--size` | Bytes per event by kind and operation, and the GB per month at the sample's event rate. With `--drain axiom`, it also prints that drain's monthly ingest estimate (BET-21) |

- Exit code 0 means at least one match, 1 means no match, and 2 means a usage or read error, like
  `grep`.
- A line that is not a JSON object is skipped and counted. The count prints to stderr at the end.
- `summary` output prints one line per event:
  `2026-09-16T08:16:25Z ERROR POST /orders/{id} 502 in 840.2ms: PAYMENT_DECLINED card declined (fix: Ask the customer for another card.) (order_id=4821)`.
- Matching runs on the stream, with memory bounded by `--limit` and the group count. A 1 GB file
  reads in under 10 seconds on an M-series Mac.
- The filter language is the root package `github.com/jeremygprawira/wlog/query`, stdlib only.
  `wlog query`, the memory endpoint, and `wlog mcp` all use it, so one filter means the same thing
  everywhere.

## cli-explain: `wlog explain`, `wlog rules`, `wlog schema`, `wlog version`

```
wlog explain <id> [--json]     # WLOG_DRAIN_FAILED, middleware.coverage, http.status, AXIOM_TOKEN
wlog explain env [--json]      # every var setup.FromEnv reads, set or unset, never secret values
wlog rules [--json]            # every map rule: id, weight, applies_to, why, fix, example
wlog schema [event|map]        # prints the embedded JSON Schema
wlog version [--json]          # tool, rules, and schema versions, and the Go version
```

- An id can be a problem code, a doctor code, a map rule id, a catalog code, a reserved field path,
  or an env var name. An
  unknown id exits 1 and lists the three closest ids.
- Text output has sections `What`, `Why`, `Fix`, `Example`, and `Link`. JSON output has the same
  keys.
- Every entry comes from the same source as the runtime: `wlog.Problems()`, the rule table, the
  schema, and `setup.Resolve`. A test compares every source with `explain`, and an id missing from `explain` fails it.

## agent-docs

- `make docs` builds `llms.txt` at the repo root. It follows the llms.txt format: a title, a one
  line summary, and one linked line per guide, spec, recipe, and package.
- `make docs` also builds `llms-full.txt`: every guide and recipe, then `go doc -all` for each
  public package, in a stable order.
- `wlog agents` writes skills as `.agents/skills/<name>/SKILL.md` with front matter, plus
  `references/` files. It writes an `AGENTS.md` block and a `CLAUDE.md` that imports `@AGENTS.md`
  when no CLAUDE.md exists.
- Skills: `instrument-with-wlog`, `analyze-wlog-output` (uses `wlog query` and `wlog tail`),
  `audit-a-handler`, `debug-missing-events` (uses `WLOG_DEBUG`, `wlog doctor`, and `wlog explain`),
  and `add-a-wlog-adapter` (uses the conformance suites).
- Each skill states the commands and the event fields it relies on. A test compiles every Go
  snippet and runs every shell snippet against a fixture app.

## drain-memory additions

```go
func (m *Memory) QueryHandler(opts ...HandlerOption) http.Handler // GET /events
func (m *Memory) StreamHandler(opts ...HandlerOption) http.Handler // GET /events/stream (SSE v2)
func WithToken(token string) HandlerOption                         // needs Authorization: Bearer
func WithAnyAddress() HandlerOption                                // default: loopback clients only
```

- `GET /events` takes the same filters as `wlog query`, as query parameters with the flag names.
  It returns a JSON array, newest first.
- SSE v2 frames: `event: hello` with `{"version": 1, "store": ..., "size": ...}`, then
  `event: event` per event, and a `: ping` comment every 15 seconds. `?since=<event_id or time>`
  replays from that point.
- With no token, a request from a non-loopback address gets 403. `WithAnyAddress()` changes that.

## search-recipes

Files under `integrations/search/`, each loaded by a test:

| Path | Content |
|---|---|
| `lnav/wlog.json` | lnav format: `timestamp`, `level`, `summary` as body, and `operation` and `error.code` as values |
| `jq/cookbook.md` | 12 questions answered with `jq`, each run by a test against a fixture with gojq |
| `grafana/loki.json`, `grafana/clickhouse.json` | Dashboards: rate, errors, and p95 by operation, top error codes, slowest calls |
| `clickhouse/views.sql` | Views for errors by operation, p95 by operation, and calls by system |
| `axiom/queries.apl`, `datadog/facets.json`, `honeycomb/queries.md` | Saved queries for the same questions |
| `elastic/index-template.json` | The bytes of `elastic.Template(Elasticsearch)`, added in phase 14. A test compares the two |
| `collectors/vector.toml`, `collectors/fluent-bit.conf`, `collectors/otel-collector.yaml` | Read wlog JSON from stdout or files and forward it unchanged |
| `collectors/otel-filelog.yaml` | A `filelog` receiver with operators that parse the `otel` output preset into log records, added in phase 11 with the preset |

The `tools` module adds `github.com/itchyny/gojq` to run the jq recipes in tests.

## recipes

`docs/recipes/<name>.md` plus `examples/<name>/` with a test. Each recipe ships in the phase of the
modules it uses. Phase 12: `rest-api`, `grpc-service`, and `cli-tool`. Phase 13: `kafka-consumer`,
`cron-job`, and `lambda`. Phase 14: `llm-agent` and `mcp-server`. Each recipe has four parts:

1. Setup code that `tools snippets` compiles.
2. One golden event from the example test.
3. Five questions with their `wlog query`, `jq`, and backend query forms. Examples: which operations
   fail most, the slowest calls, one user's actions, one trace across services, and cost per
   operation.
4. The `wlog explain` ids a reader meets on that path.

## cli-mcp

`wlog mcp` ships in phase 12 with this track, and [SPEC-track-g.md](SPEC-track-g.md) defines it.
It uses the root `query` package, like `wlog query`.

## Success criteria

1. `wlog query --status '>=500' --since 1h ./fixtures` returns exactly the golden set of events,
   and exits 1 on a fixture with no match.
2. `wlog query --group-by operation --stats duration_ms` on a fixture prints counts, p50, p95, p99,
   and max that match a hand-computed golden.
3. `wlog tail` prints a line appended after it started, and keeps reading after a rotation.
4. `wlog query --url` against a test app's `QueryHandler` returns the same events as the same query
   against the app's NDJSON file.
5. Every problem code, rule id, reserved field, and setup var has a `wlog explain` entry, proven by
   a test that walks each source.
6. `make docs` builds `llms.txt` and `llms-full.txt`, and a second run gives the same bytes.
7. Every skill snippet compiles, and every shell snippet runs against a fixture app with exit 0.
8. The `jq` cookbook recipes give their golden output under gojq, and every dashboard and template
   file loads as valid JSON or SQL text.
9. Each recipe's example test passes, and its golden event is valid against the schema.
10. `wlog query` reads a 1 GB fixture in under 10 seconds on an M-series Mac.

## Testing

Black-box tests in `cmd/wlog` for the CLI, with fixtures under `cmd/wlog/testdata/query/`. The
`tools` module runs the jq recipes. The memory handlers use `httptest`.

## Boundaries

- **Always:** keep one filter language, in the root `query` package, for the CLI, the endpoint, and
  MCP.
- **Ask first:** a new top-level `wlog` command.
- **Never:** print a secret value in `wlog explain env`, or serve the memory endpoint to a
  non-loopback address without a token or `WithAnyAddress()`.

## Open questions

None.
