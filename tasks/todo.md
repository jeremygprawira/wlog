# Todo: wlog v0.5 to v1.0

Task list for [plan.md](plan.md). Tick a task only after its `Verify` command passes and its tests failed first.


## Phase 10, v0.5: honest build and safety fixes

- [x] 10-CI-1 tools module with `modules` and `affected`
- [x] 10-CI-2 `requires` and `tidy -check`, then fix every sub-module go.mod
- [x] 10-CI-3 Go floor per module
- [x] 10-CI-4 CI workflow rewrite
- [x] 10-CI-5 `snippets`, `ste`, and `verifyplan`
- [x] 10-CI-6 `cover`, `bench`, `vuln`, all fuzz targets
- [x] 10-CI-7 Release hygiene
- [x] Review point 10-CI: every required CI job is green, then human review
  - The nightly jobs are green on 2026-09-17: `vuln`, `integration`, and `fuzz-long`. The
    integration job had never run to the end, which the collector pin hid, so this review
    point also closed three faults in that stack: a registry tag that the collector project
    had deleted, a non-root collector user that could not write the output mount, and a
    ClickHouse default user that the image refuses from outside localhost.
  - Fixed 2026-09-17: `make lint`, `tools floor`, `tools cover -min 85`, `tools tidy -check`,
    `tools requires`, `tools ste`, `tools snippets`, and `make map` all pass clean. `make race`
    passes in every module, and `make fuzz FUZZTIME=20s` finds nothing across all four targets.
    `tools verifyplan` had its own bug. Any `./...` Verify command
    prints "no tests to run" for a sibling package that did not match. The old code failed the
    task on that text alone, and reported 27 false failures against the real 2. It now looks
    for `=== RUN` instead.
  - `TestHTTPDrain_PIPE24_TimeoutApplies` was not flaky, it was a Go 1.21 stdlib difference,
    fixed by testing `net.Error.Timeout()` instead of `errors.Is(err,
    context.DeadlineExceeded)`.
  - `make lint` also had 8 pre-existing failures across `drain/file`, `cmd/wlog`, and
    `tools/cmd/ste` that nothing in this initiative had touched. All fixed:
    - an `==` comparison against `io.EOF`, one file away from `errors.Is` for the same case
    - an ineffectual assignment to a variable a rotation branch throws away two lines later
    - an unwrapped `%v` where `%w` was one line away in the same function
    - two git subprocess calls with no context
    - two functions a since-moved feature left dead
    - a stray blank line
  - Proven true, not fixed in the repo: `GOTOOLCHAIN=go1.26.6 tools vuln` exits 0. The default
    Go on this machine is 1.26.1, and GO-2026-6090, GO-2026-5972, and GO-2026-5856 are
    standard-library CVEs fixed in 1.26.4 through .6. No wlog code or dependency is at fault.
    Upgrading the machine's Go is one option. Adding a `toolchain` line so `go` auto-fetches a
    patched release is another. Either is a decision for the project, not made here.
  - `make integration`'s clickhouse step needs a running Docker daemon, which this machine does
    not have. Not attempted here, since starting it was not asked for.
- [x] 10-CORE-1 Value copy tree
- [x] 10-CORE-2 Event-shape fuzz test
- [x] 10-CORE-3 Plain lines, enricher values, drain contract
- [x] 10-CORE-4 Hook isolation
- [x] 10-CORE-5 Audit level bypass, Detach context
- [x] 10-CORE-6 Option validation and keepers
- [x] 10-CORE-7 Flush and Close
- [x] 10-CORE-8 Default extractor
- [x] 10-CORE-9 Size counting, group merge, drain docs
- [x] 10-RED-1 Linear key matching and a bounded cache
- [x] 10-RED-2 `With` keeps the full configuration
- [x] 10-RED-3 Joined tokens and the default list
- [x] 10-RED-4 Pattern names and new patterns
- [x] 10-RED-5 Fewer false positives
- [x] 10-RED-6 Fail closed and a full fingerprint
- [x] 10-RED-7 Paths, globs, and docs
- [x] 10-PIPE-1 Recover and unlock
- [x] 10-PIPE-2 Flush, Close, and sends after close
- [x] 10-PIPE-3 Retry timing
- [x] 10-PIPE-4 Batches, clamps, timer, counters
- [x] 10-PIPE-5 FanOut rewrite
- [x] 10-PIPE-6 HTTP drain helper
- [x] 10-AUD-1 Journal owns the chain
- [x] 10-AUD-2 Marker lines and verification rules
- [x] 10-AUD-3 File lock and crash recovery
- [x] 10-AUD-4 Several records and no silent loss
- [x] 10-AUD-5 Diff, Wrap, Mock, tests, and docs
- [x] 10-PIPE-7 MinLevel, a public HTTP drain helper, and identity headers
- [x] 10-AUD-6 Actor types, outcomes, and correlation ids
- [x] 10-AUD-7 JSON Patch and audit-only routing
- [x] 10-DRN-1 drain-sentry
- [x] 10-DRN-2 drain-clickhouse and the integration stack
- [x] 10-DRN-3 drain-file
- [x] 10-DRN-4 drain-loki and drain-otlp
- [x] 10-DRN-5 drain-datadog, drain-axiom, and drain-webhook
- [x] 10-DRN-6 drain-betterstack, drain-hyperdx, and drain-posthog
- [x] 10-CAT-1 Code matching and registries
- [x] 10-CAT-2 Copies, templates, domain, and entry defaults
- [x] 10-CAT-3 Audit catalog fields
- [x] 10-LLM-1 Caps and cost fields
- [x] 10-LLM-2 Token semantics and the price table
- [x] 10-SMP-1 Sampling rules
- [x] 10-ENR-1 Enrichers
- [x] 10-MEM-1 drain-memory and wlogtest
- [x] 10-MAP-1 Handler discovery
- [x] 10-MAP-2 Routes and middleware coverage
- [x] 10-MAP-3 Rule accuracy
- [x] 10-MAP-4 Gates, configuration, and streams
- [x] 10-MAP-5 Map JSON v2 and the text report
- [x] 10-MAP-6 Analyzer, loading cost, and golangci-lint plugin
- [x] 10-MAP-7 Ignore comments, git baselines, and SARIF
- [x] 10-MAP-8 The `keys.strict` rule
- [x] 10-INIT-1 `wlog init` correctness
- [x] 10-INIT-2 `wlog doctor` and `wlog agents`
- [x] 10-DOCS-1 Statuses, README, and CHANGELOG
- [x] 10-DOCS-2 Parity page and guides
- [x] Review point 10, v0.5.0: human review, then ask before tagging
  - Approved 2026-09-17. `tools release -version v0.5.0 -dry-run` shows the tag order and names no
    breaking change against v0.1.0. `tools requires`, `tools tidy -check`, and `tools floor` pass,
    so every sub-module requires its siblings at v0.5.0 with a local replace.

## Phase 11, v0.6: event shape v2 and the five foundations

- [ ] 11-SHAPE-1 Time semantics and ErrorInfo v2
- [ ] 11-SHAPE-2 Reserved keys and the ordered JSON writer
- [ ] 11-SHAPE-3 Summary
- [ ] 11-SHAPE-4 Stage order v2 and the size cap
- [ ] 11-SHAPE-5 Plugin hooks v2
- [x] 11-PROB-1 Problem codes
- [ ] 11-PROB-2 Debug reasons and Stats
- [ ] 11-SHAPE-6 Writers
- [ ] 11-SHAPE-7 Pretty console v2
- [ ] 11-LLM-1 Rename llm event keys
- [ ] 11-DEF-1 SetDefault and a Logger-scoped switch
- [ ] 11-DEF-2 Writes with no event
- [ ] 11-CALL-1 StartCall and call records
- [ ] 11-PROP-1 propagate
- [ ] 11-WORK-1 Units, kinds, and levels
- [ ] 11-WORK-2 Lag, batches, and flush
- [ ] 11-SCH-1 JSON Schemas
- [ ] 11-PRE-1 Preset contract and flat
- [ ] 11-PRE-2 OTel preset
- [ ] 11-PRE-3 ECS and Datadog presets
- [ ] 11-PRE-4 GCP and EMF presets
- [ ] 11-HTTP-1 Views, Exchange, and route rules
- [ ] 11-HTTP-2 Capture policy
- [ ] 11-HTTP-3 Bodies
- [ ] 11-HTTP-4 Panics and the writer wrapper
- [ ] 11-HTTP-5 Problem responses, edge cases, benchmark, and docs
- [ ] 11-CONF-1 Shared test helpers and the http suite
- [ ] 11-CONF-2 work, calls, and log suites
- [ ] 11-CONF-3 drain suite and drain migration
- [ ] 11-SET-1 setup.FromEnv
- [ ] 11-ADP-1 http-std rebuilt
- [ ] 11-ADP-2 http-echo and http-echo5 rebuilt
- [ ] 11-ADP-3 http-gin rebuilt
- [ ] 11-MIG-1 Migrate log outputs, trace-otel, examples, and the CLI to v2 names
- [ ] Review point 11, v0.6.0: human review, then ask before tagging

## Phase 12, v0.7: everyday stack, search, and agents

- [ ] 12-A-1 http-chi
- [ ] 12-A-2 http-fasthttp
- [ ] 12-A-3 http-fiber and http-fiber3
- [ ] 12-A-4 rpc-grpc
- [ ] 12-A-5 http-httprouter and http-gozero
- [ ] 12-A-6 rpc-connect
- [ ] 12-A-7 rpc-gqlgen
- [ ] 12-A-8 http-hertz and http-kratos
- [ ] 12-A-9 http-huma and rpc-twirp
- [ ] 12-A-10 Recipes: rest-api and grpc-service
- [ ] 12-B-1 sqlshape
- [ ] 12-B-2 client-http
- [ ] 12-B-3 store-sql
- [ ] 12-B-4 store-pgx and store-gorm
- [ ] 12-B-5 store-redis
- [ ] 12-B-6 store-mongo and client-aws
- [ ] 12-B-7 store-bun
- [ ] 12-C-1 log-slog
- [ ] 12-C-2 log-logr and log-zap
- [ ] 12-C-3 log-zerolog, log-logrus, and log-hclog
- [ ] 12-C-4 log-std
- [ ] 12-C-5 errors-validator and errors-oops
- [ ] 12-C-6 errors-cockroach and flag-openfeature
- [ ] 12-F-1 `wlog query` filters and output
- [ ] 12-F-2 Group, stats, size, and tail
- [ ] 12-F-3 drain-memory query endpoint and SSE v2
- [ ] 12-F-4 `wlog explain`, `rules`, `schema`, and `version`
- [ ] 12-F-5 Agent docs
- [ ] 12-F-6 Search recipes
- [ ] 12-F-7 `wlog mcp`
- [ ] 12-F-8 Recipe: cli-tool
- [ ] 12-F-9 `wlog doctor` additions
- [ ] Review point 12, v0.7.0: human review, then ask before tagging

## Phase 13, v0.8: messages, jobs, functions, and commands ([SPEC-track-d.md](../docs/SPEC-track-d.md))

- [ ] 13-D-1 queue-kafkago
- [ ] 13-D-2 queue-sarama and queue-franz
- [ ] 13-D-3 queue-confluent
- [ ] 13-D-4 queue-watermill
- [ ] 13-D-5 queue-sqs
- [ ] 13-D-6 queue-nats and queue-amqp
- [ ] 13-D-7 queue-pubsub and queue-cloudevents
- [ ] 13-D-8 job-asynq and job-river
- [ ] 13-D-9 job-temporal and job-cron
- [ ] 13-D-10 faas-lambda
- [ ] 13-D-11 faas-gcf
- [ ] 13-D-12 command-cobra, command-urfave, and command-kong
- [ ] 13-D-13 Recipes: kafka-consumer, cron-job, and lambda
- [ ] Review point 13, v0.8.0: human review, then ask before tagging

## Phase 14, v0.9: destinations, OpenTelemetry, and AI

- [ ] 14-E-1 pipeline.PartialError
- [ ] 14-E-2 trace-otel spans and metrics
- [ ] 14-E-3 trace-otellog
- [ ] 14-E-4 metrics-prometheus
- [ ] 14-E-5 drain-honeycomb and drain-newrelic
- [ ] 14-E-6 drain-elastic
- [ ] 14-E-7 drain-splunk and drain-victorialogs
- [ ] 14-E-8 drain-syslog
- [ ] 14-E-9 drain-cloudwatch
- [ ] 14-G-1 llm additions
- [ ] 14-G-2 ai-anthropic and ai-openai
- [ ] 14-G-3 ai-genai and ai-goopenai
- [ ] 14-G-4 ai-langchaingo and ai-eino
- [ ] 14-G-5 ai-mcpsdk and ai-mcpgo
- [ ] 14-G-6 Recipes: llm-agent and mcp-server
- [ ] 14-G-7 `wlog init` v2
- [ ] Review point 14, v0.9.0: human review, then ask before tagging

## Phase 15, v1.0.0: API freeze

- [ ] 15-1 API freeze
- [ ] 15-2 Parity and comparison pages
- [ ] 15-3 Audit close-out
- [ ] Review point 15, v1.0.0: human review, then ask before tagging
