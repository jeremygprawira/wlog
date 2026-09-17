# Spec: phase 10 hardening

> Phase 10 · depends on: `repo-ci`. Module ids: `core`, `redact`, `pipeline`, and `audit`.
> Also the nine drains, `catalog`, `llm`, `errors-herr`, `sample`, `enrich`, `drain-memory`, and
> `wlogtest`. Also `cli-map`, `cli-init`, `cli-doctor`, `cli-agents`, and `docs`. Project-wide rules
> in [SPEC.md](SPEC.md) apply, and this file amends each module's older spec. Each rule names
> the audit ids it closes.

## Objective

Fix the audit findings that the phase 11 rewrites do not replace, so v0.5 is safe to run. Each
rule below is a behavior with a test. Phase 10 reports failures through the existing `OnError`
hook. Phase 11 moves every report to a `wlog.Problem` code.

Phase 11 closes these ids instead, so phase 10 leaves them alone: CORE-12 to CORE-20, CORE-22,
CORE-27, CORE-34, HTTP-1 to HTTP-17, HTTP-19, HTTP-22, and HTTP-23. Phase 12 closes HTTP-18 and
HTTP-20, and phase 14 closes HTTP-21.

## core

### Values and ownership

1. Every write call copies its value into a tree that wlog owns. The tree holds only `nil`,
   `bool`, `string`, `int64`, `uint64`, `float64`, `json.Number`, `map[string]any`, and `[]any`.
   A caller's map or slice is never stored or changed. The copy runs before the event lock is
   taken, so a `MarshalJSON` method that logs through wlog never deadlocks. (CORE-4, SPEC-G1)
2. The copy follows `encoding/json` field rules for structs: tags, `omitempty`, `-`, `string`,
   and embedded structs. It calls `json.Marshaler` and `encoding.TextMarshaler`. A struct field
   tagged `json:"-"` never appears, even on a fallback path. (CORE-2, CORE-3)
3. Conversions: an integer keeps every digit. `NaN`, `+Inf`, and `-Inf` become the strings
   `"NaN"`, `"+Inf"`, and `"-Inf"`. An `error` becomes its message, and a panic inside
   `Error()` becomes `"[error: <type> panicked]"`. A `time.Duration` becomes float milliseconds.
   A `time.Time` becomes RFC 3339 UTC. `json.RawMessage` is decoded. Other `[]byte` becomes
   `"[binary: N bytes]"`. (CORE-5, CORE-6, CORE-26, SPEC-G6)
4. A value that cannot be encoded becomes `"[unencodable: <type>]"`, and nothing else from it is
   kept. Nesting past depth 16 becomes `"[truncated: depth]"`. (CORE-3)
5. `Info`, `Warn`, `Debug`, and `AppendLog` copy their key-value pairs the same way. (CORE-1)
6. After the enrich stage, core copies any value an enricher added, the same way. (CORE-2)
7. Each drain gets the canonical map, and core never changes that map after the drain stage
   starts. The `Drain` doc comment says a drain must not change the map. (CORE-11)

### Isolation and control

8. `wlog.Error` runs the extractor under recover. A panic stores
   `ErrorInfo{Code: "INTERNAL", Message: <safe message>}` and reports through `OnError`.
   (CORE-7)
9. Every call into `OnError` runs under recover. (CORE-8)
10. An event with an `audit` field skips the minimum level filter. (CORE-9)
11. `Detach` gives the child a context from `context.WithoutCancel(parent)`. The child keeps
    every value from the parent, including the Logger, and it applies `StrictKeys`. (CORE-10,
    CORE-28)
12. `SetLevel` and `WithLevel` reject a level outside `debug`, `info`, `warn`, and `error`. They
    report through `OnError` and keep the current level. (CORE-30)
13. `WithService` keeps the env value for any empty argument. (CORE-29)
14. `WithRedactor(nil)` stores `redact.Default()` once, at option time. (CORE-33)
15. All Keepers from `WithSampler` and from plugins run, and they combine with OR. `Plugins()`
    returns a copy. (CORE-31)
16. `Logger.Flush(ctx)` flushes every drain that has `Flush`, and it keeps them running.
    `Logger.Close(ctx)` closes all drains at the same time. It returns once all drains finish,
    or once `ctx` ends. After `Close`, an emit reports through `OnError` and sends nothing.
    (CORE-32, SPEC-G19)
17. The default extractor uses `errors.As` for `Code() string`, `Code() any`, and
    `Stack() string`. It lists the causes of an `errors.Join` or multi-`%w` error in `causes`,
    with a depth cap. It reads a `StackTrace()` method that returns a slice of program counters
    through reflection, which covers pkg/errors and cockroachdb/errors with no import. It sets
    `type` to the error's Go type. (CORE-24)
18. `Set`, `SetGroup`, and `Append` count values toward a total size. Past 256 KiB, a write is
    dropped and counted in `wlog.dropped_fields`. Phase 11 replaces this with the full size cap.
    (CORE-25)
19. `SetGroup` merges nested maps one level deeper per call, the same way at every depth. The
    `Set` doc comment says that `Set` replaces a value. `Set` stays a replace, so a caller
    always knows the result. (CORE-23, PAR-2)
20. Every built-in network drain is async by default after the phase 10 drain fixes. The
    `WithDrains` doc comment says that a custom drain that blocks also blocks the request.
    (CORE-21)

## redact

1. Matching cost is linear in the key length. A key is cut to 256 bytes before matching. Token
   runs are compared only up to the longest denylist entry, without building strings. A
   4000-byte key redacts in under 1ms. (RED-1)
2. The token cache holds at most 4096 keys. Past that, a key is tokenized without caching.
   (RED-4)
3. `With` starts from the full resolved config: keys, patterns, pattern toggles, transforms,
   `ReplaceFunc`, limits, replacement, and client IP masking. `Disabled().With(...)` returns an
   error. (RED-2)
4. A key entry also matches its tokens joined with no separator. `api_key` matches `apikey`,
   and `session_id` matches `sessionid`. The default list adds these entries:
   - sessions: `sid`, `jsessionid`, `phpsessid`, `connect.sid`, `csrf`, `xsrf`
   - keys: `passphrase`, `access_key`, `signing_key`, `client_secret`, `signature`,
     `x_amz_signature`
   - connection data: `dsn`, `database_url`

   Changing the default list is ask-first, and approving this spec is that ask. (RED-3)
5. `EnablePatterns`, `RemovePatterns`, and `AddPatterns` return an error for an unknown or
   duplicate name. (RED-5)
6. New built-in patterns: `url_credentials` masks the password in `scheme://user:pass@host`.
   `url_query_secret` masks the value of a query parameter whose name matches the key denylist.
   `basic_auth` masks `Basic <base64>`. `api_key_prefix` masks values that start with `sk_live_`,
   `sk_test_`, `AKIA`, `ghp_`, `xoxb-`, or `glpat-`. (RED-6)
7. The ipv4 pattern skips a match that follows `/` or a letter, and a version string such as
   `126.0.0.0` after a product name. The card pattern needs 13 to 19 digits and a Luhn match.
   It also needs a separator, or a key name that the card key list matches. `maskPhone` keeps
   the country code it found and adds none. (RED-7)
8. `Fingerprint` hashes the whole effective config. Two redactors with any difference give two
   fingerprints. (RED-8)
9. A panicking transform or `ReplaceFunc` masks the whole value it was given, and never passes
   it through. (RED-9)
10. A value of any type the redactor cannot walk is replaced with the replacement text. Integer
    values with 13 or more digits are also scanned by the card pattern. (SPEC-G2)
11. Every doc, spec, and test path uses the real event shape (`http.request_headers`). A path
    entry that can never match a reserved key reports through `OnError` at `New`. (RED-10)
12. Globs use a matcher where `*` matches within one key segment, including `/`. `RemoveKeys`
    matches by token form, and removes every matching entry. Negative limits, and a pattern that
    matches the empty string, return an error. The `NoBuiltinPatterns` doc matches the code.
    (RED-12)

## pipeline

1. The worker recovers a panic in `SendBatch` or `OnDropped`, and counts it as a failed
   attempt. (PIPE-1)
2. `FanOut` recovers each drain's panic. It keeps one bounded queue and one goroutine per drain,
   not one goroutine per event. It implements `Flush` and `Close` by calling each drain's own.
   (PIPE-2)
3. `OnDropped` runs after the buffer lock is released, under recover. (PIPE-3)
4. `Flush(ctx)` sends every buffered event and keeps the worker. After `Close`, `Send` counts
   the event as dropped and calls `OnDropped`. (PIPE-4)
5. A `Retry-After` wait is capped at `MaxDelay`. A negative, zero, or overflowing value falls
   back to the backoff. Every wait selects on the worker context. (PIPE-5)
6. `Close(ctx)` cancels in-flight sends once `ctx` ends, and it waits for the worker to stop.
   (PIPE-6)
7. A batch holds at most `BatchSize` events. Events waiting for a retry count toward
   `MaxBuffer`. (PIPE-10)
8. Options clamp values: `MaxBuffer` and `BatchSize` at least 1, `MaxAttempts` at least 1.
   (PIPE-11)
9. Backoff never overflows, and jitter stays inside `MaxDelay`. (PIPE-22)
10. `Dropped()` and `Stats()` report counts. The worker sleeps on a timer, not a 5ms poll.
    (PIPE-24)
11. The HTTP drain helper, public as `pipeline/httpdrain` after rule 13, removes the query string and user info from any URL inside an error.
    It gives each drain `WithHTTPClient`, `WithTimeout`, and `WithUserAgent`. (PIPE-19, PIPE-24)
12. `pipeline.MinLevel(level)` drops an event below `level` before it enters the buffer. An event
    with an `audit` array always passes. `Stats` reports queued events, which covers evlog's
    `pending`. (PIPE-4, PAR-16)
13. The old `internal/httpdrain` is now the public package `pipeline/httpdrain`. A third-party drain
    gets the same status classes, `Retry-After` rule, URL scrubbing, and identity headers.
    (PAR-17)
14. `WithUserAgent("")` sends no `User-Agent`, and `WithIdentityHeaders(false)` sends no wlog
    identity header. The version in each header comes from `internal/version`. (PAR-18)

## audit

1. `Journal` holds the chain state under one lock. It hashes the exact bytes it writes:
   `hash = sha256(prev_hash || line_bytes_without_hash_fields)`. The `Chain` drain is removed.
   (AUD-2, AUD-3, AUD-4)
2. Every 100 lines, and on `Close`, `Journal` writes a marker line with the line count and the
   head hash. With a key, the marker is signed with HMAC-SHA256 and a `key_id`. (AUD-1, AUD-10)
3. `Verify(path, opts...)` fails on an empty file, a missing final marker, a count or head
   mismatch, a duplicate key, or a line that is not canonical JSON. `VerifyHead(path, head)`
   also compares against a head stored outside the file. `VerifySigned` needs a non-empty key
   and compares with `hmac.Equal`. (AUD-1, AUD-4, AUD-10)
4. On open, `Journal` takes an exclusive file lock. It moves a partial last line into
   `<path>.partial` and resumes from the last full line. Lines of any length verify. (AUD-9)
5. An event holds up to 20 records in an `audit` array. `Do` after the event emitted, or with no
   event, emits a standalone audit event. `Do` never loses a record to the key cap. (AUD-5)
6. Every journal failure reports through `OnError`, and non-finite floats never break a line.
   (AUD-6)
7. `Diff` returns a nested tree, so path denylist entries match its output. It reports unexported
   fields as unknown, and returns an error for non-object input. (AUD-7, AUD-11)
8. `Wrap` uses the Logger's extractor from `ctx`. A denial error maps to `denied`. (AUD-8)
9. `Mock` is silent, sets `LevelDebug`, and ignores `WLOG_LEVEL`. (AUD-12)
10. Tests cover SPEC-audit criteria 3 to 5 against real files, and `examples/audit-refund` uses
    `Journal` and `Verify`. (AUD-13)
11. SPEC-audit, SPEC.md, and `catalog.Audit` docs match the code. (AUD-14)
12. An actor `type` is `user`, `service`, `system`, or `agent`. An agent actor also holds
    `model`, `tools`, and `prompt_id`. An outcome is `success`, `denied`, or `failure`. (PAR-26)
13. A record holds `correlation_id`, default `trace.request_id`, and `causation_id`, default
    `trace.parent_event_id`. Without a caller key, `idempotency_key` is the first 32 hex
    characters of a SHA-256 over action, actor id, target type, target id, and
    `trace.request_id`. So a retried request with the same request id gets the same key. (PAR-26)
14. `audit.Patch(before, after)` returns RFC 6902 JSON Patch operations, next to the `Diff` tree.
    `audit.OnlyDrain(d)` wraps a drain and forwards only events with an `audit` array. Each
    forwarded event holds `timestamp`, `event_id`, `service`, `trace`, and `audit`. (PAR-26)

## Drains

Every network drain keeps two constructors. `New(opts...) (wlog.Drain, error)` returns an async
drain with pipeline defaults, and `WithPipeline(pipeline.Option...)` changes them.
`NewSender(opts...) (*Sender, error)` returns the raw `pipeline.Sender`. `MustNew` panics on a
construction error. A missing required env value returns an error that names the variable.
(PIPE-17, CORE-21)

| Drain | Rules | Closes |
|---|---|---|
| `drain-sentry` | One envelope per error event, with the header `event_id` equal to the item's id. Log items use typed flat attributes and a trace id, and error events are not sent as logs. DSNs with a path prefix or no scheme parse | PIPE-7, PIPE-8, PIPE-21 |
| `drain-clickhouse` | Timestamps use `2006-01-02 15:04:05.000000000` in UTC. `DDL(database, table)` uses a `String` column for the event on servers older than 25.3. Identifiers must match `^[A-Za-z_][A-Za-z0-9_]*$`. `CLICKHOUSE_URL` with a query string parses correctly | PIPE-9, PIPE-19, PIPE-20 |
| `drain-file` | `Read` uses a line reader that skips and counts a line over 1 MiB. `Tail` detects rotation with `os.SameFile`, drains the old file first, and caps a pending line at 1 MiB. Age rotation uses the time the file opened. `Close(ctx)` syncs and closes. A write after close returns an error. With no path, the drain writes `.wlog/logs/<YYYY-MM-DD>.jsonl` and creates `.wlog/.gitignore` holding `*`. `MaxFiles(n)` keeps the newest n daily files, default 7 | PIPE-12, PAR-21 |
| `drain-loki` | Label names must match `^[a-zA-Z_][a-zA-Z0-9_]*$`. Dotted paths resolve into the event, and their label names replace dots with `_`. The cardinality denylist applies to the path and its last part. Basic auth needs both a user and a password | PIPE-13 |
| `drain-otlp` | Header values are percent-decoded. `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT` is used as is. A map inside an array becomes a `kvlistValue`. The environment attribute is `deployment.environment.name` | PIPE-14 |
| `drain-datadog` | Batches split before sending at 1000 entries or 5 MB. After a 413, only the failed half is retried. `DD_SERVICE` and `DD_ENV` are read once in `New` | PIPE-15 |
| `drain-betterstack` | `New` requires a scheme and host, and adds `https://` to a bare host. `BETTERSTACK_INGESTING_HOST` sets the per-source host, `BETTERSTACK_HOST` stays an alias, and CHANGELOG notes the new name | PIPE-16 |
| `drain-hyperdx` | An endpoint with an empty path gets `/v1/logs` | PIPE-16 |
| `drain-posthog` | An event without `user.id` sets `$process_person_profile` to false | PIPE-18 |
| every drain | Passes the `drain` conformance suite from phase 11. Until then, each has a leak test and an env-alone test with no `With...` option. Each also has a status test for 2xx, 400, 401, 403, 413, 429, and 5xx | PIPE-25 |

Integration tests pin `clickhouse/clickhouse-server` 25.3 or newer. The OTel collector config
binds `0.0.0.0:4318`. Every Compose service has a health probe. (PIPE-9)

## catalog, llm, and errors-herr

1. `catalog.Extractor` matches full codes only. `catalog.AllowShortCodes()` opts into short
   codes for one registry. (CAT-6)
2. `catalog.Extractor(next, regs...)` returns an error for a code that two registries define, and
   `MustExtractor` panics on it for `main`. It skips a nil registry. (CAT-6, CORE-7)
3. `errors.Is` matches an entry only from the same registry. (CAT-7)
4. `Get` and `Entry()` return a deep copy, including `Audit`. (CAT-8)
5. A template renders in one pass, so a parameter value is never read as a placeholder. The
   extractor does not fill `Message`. (CAT-9, CAT-5)
6. `llm.calls` and `llm.tool_calls` hold at most 200 items each, and extra items count in
   `wlog.dropped_fields`. (CAT-1)
7. `llm.Record` follows OpenTelemetry token semantics. `InputTokens` counts every input token.
   `CacheReadInputTokens` and `CacheWriteInputTokens` are parts of it, and so is
   `CacheWrite1hInputTokens`. `ReasoningTokens` is part of `OutputTokens`. Event keys keep their names, and phase 11 renames them. `Cost` prices uncached
   input, cache reads, 5 minute writes, 1 hour writes, and output, each at its own rate.
   `docs/cost.md` shows the Anthropic sum rule and one example per provider. (CAT-2)
8. `DefaultPrices` holds only rows copied from the official pricing pages on 2026-09-16, and the
   doc comment holds that date. Anthropic rows cover the Claude 5 and Claude 4.x models. OpenAI
   rows cover `gpt-6-astra`, the `gpt-5.6` family, `gpt-5.5`, `gpt-5.4`, and `gpt-4.1`. They also
   cover `gpt-4o`, `gpt-4o-mini`, and both `text-embedding-3` models. A model id matches exactly, then by its longest known prefix, so a dated snapshot finds
   its row. Batch, long-context, regional, and fast-mode multipliers are out of scope, and
   `docs/cost.md` says so. (CAT-3)
9. The enricher keeps a cost the caller already set, and it includes a record written by `Set`.
   `llm.cost_usd` is removed, because money stays in micros. (CAT-4, CAT-11)
10. `errors-herr` and every sub-module require the root module at a tag. (CAT-10)
11. The code format stays flat, such as `BILLING_PAYMENT_DECLINED`, so one token finds it in any
    search. The extractor writes the registry domain to `error.attrs.domain`. An `Entry` can hold
    default `Data` and `Internal` maps, and a call-site value wins for the same key. (PAR-10)
12. `catalog.Audit` gains `Description`, `RequiresChanges`, and `RedactPaths`. If a record breaks
    `RequiresReason` or `RequiresChanges`, `audit.Do` still writes it, with `violations` naming
    each broken rule. `RedactPaths` masks those paths inside `changes`. (PAR-11)

## sample, enrich, drain-memory, and wlogtest

1. `sample.KeepPath` supports `**` across segments, and `New` returns an error for a bad glob.
   (SMP-3)
2. `sample.Rate` takes a float percent from 0 to 100, and rejects other values. If an event has
   `trace.trace_id`, head sampling hashes it, so one trace gets one decision. A kept event records
   `wlog.sample_rate`. `WithRand(src)` injects the random source for tests. (SMP-4, BET-18)
3. `KeepErrorsAndSlow` keeps every warn and error event and every status of 500 or more.
   `KeepDuration` compares exact durations. (SMP-2, SMP-4)
4. `enrich.Geo(provider)` reads headers from one named provider (`Cloudflare`, `CloudFront`,
   `Vercel`, or custom). Its doc says clients can spoof these headers without a trusted proxy.
   (SMP-5)
5. An enricher never replaces a non-map value at its group key. `UserAgent` and `User` accept
   `Overwrite`. Each enricher has its own option type. `Host` reads the hostname once. Geo
   latitude and longitude are floats, and a Vercel city is URL-decoded. (SMP-6, SMP-10)
6. An unknown user agent sets only `raw`. Generic bot and tool names class as `bot` or `tool`.
   The test table holds at least 30 real user agents. Docs name the real field paths. (SMP-7)
7. `drain-memory` deep-copies each event once per subscriber and once per snapshot entry.
   (SMP-1)
8. `drain-memory` counts subscriber drops in `Dropped()`. `Query` with `Limit` returns the newest
   N, and `Contains` compares numbers by value. SSE takes the replay snapshot after the
   subscription starts. Each write has a deadline, and `Named` stores have `Remove`. (SMP-8)
9. `wlogtest.New` is silent. `RequireField` compares with `reflect.DeepEqual`, and it accepts
   dotted paths. The recorder size is an option with an unbounded default for tests. Each
   exported helper has an example. (SMP-9)

## cli-map, cli-init, cli-doctor, and cli-agents

### `wlog map`

1. Handler discovery resolves a handler through `go/types` across every loaded package. It
   unwraps `http.HandlerFunc(fn)` conversions, closure factories such as `s.refund()`, and types
   with `ServeHTTP`. It finds Echo `Add` and `Match`, Gin `Match`, and mux method chains.
   (CLI-1)
2. The report prints `N handlers found`. Zero handlers exits 2 with `no handlers found`. (CLI-1)
3. A route keeps prefixes from Echo and Gin groups and from mux subrouters. A constant route
   resolves through `TypesInfo`. The Echo handler is the argument after the path, and later
   arguments are middleware. mux `Methods(...)` gives one entry per method. (CLI-8)
4. Middleware coverage follows the router value. A middleware call in any loaded package covers
   every handler on the router it wraps. (CLI-9)
5. A rule that does not apply reports `n/a`, and `n/a` adds no points either way. `error-guidance`
   treats an unresolved error value as guided. `SPEC-cli-map.md` gets the new formula before the
   code changes. (CLI-7)
6. `print` flags only `fmt.Print*`, `log.Print*`, and writes to `os.Stdout` or `os.Stderr`.
   `swallowed-error` skips a write to the response writer. The return walk skips function
   literals and needs an `error` result type. Sensitive route words match whole path segments
   or words. Generated files are skipped. `keys.no_denylisted` also covers `NewKey` names.
   (CLI-14)
7. A failed gate writes no output file. `--out` equal to `--baseline` exits 2. (CLI-5)
8. Config comes only from `wlog.map.yaml`. A flag set on the command line always wins over
   config, and `--min-score 0` turns the gate off. (CLI-6)
9. Status lines go to stderr. With `--json`, stdout holds exactly one JSON document. Color follows
   `NO_COLOR`, and the text report wraps at `COLUMNS`. (CLI-12, PAR-28)
10. The map JSON has `version: 2`, `tool_version`, `rules_version`, short framework ids, module
    relative file paths, and `top_fixes` as objects with `rule`, `points`, and `handlers`. It
    also holds a `summary` object and `evidence` with `file:line` for each rule result.
    (CLI-19, PAR-32)
11. The text report lists each failing handler with `file:line`, the rule id, and a fix line.
    Suggestions print `SUGGEST`. `--strict` without `--baseline` exits 2. `--strict` compares
    per handler and per rule. `wlog --help` lists every command and the exit codes. `FIX FIRST`
    lists the three fixes worth the most points, then the projected score. Each fix line ends
    with a docs link. (CLI-21, PAR-29)
12. The vet analyzer skips suggestions unless `-suggest` is set. It skips test files, reads
    `wlog.map.yaml`, and reports at the offending call. It has `-rules` to pick rules. (CLI-16)
13. Loading uses `NeedTypes` with export data for dependencies, not `NeedDeps`. A two-file app
    peaks under 150 MB. (CLI-17)
14. `swallowed-error` walks the control flow graph from `golang.org/x/tools/go/cfg`, as
    SPEC-cli-v1.3 requires. (CLI-20)
15. A golangci-lint v2 module plugin (`cmd/wlog/golangci`) registers the analyzer, and the docs
    show the `.custom-gcl.yml` setup. (CLI-13)
16. `//wlog:ignore <rule> -- <reason>` on a handler, or on the line before it, suppresses that
    rule. A comment with no reason is itself a finding. The report prints the suppressed count.
    (PAR-30, BET-9)
17. `--baseline git:<ref>` reads the map file at that ref through `git show`. `--no-write` skips
    the output file. A regression never rewrites the baseline. (PAR-31, BET-9)
18. `--format sarif` writes SARIF 2.1.0 for GitHub code scanning, one result per failed rule
    with its `file:line`. (BET-9)
19. The rule `keys.strict` compares each `Set` key with each `NewKey` name in the module. It
    lowercases both and removes `_` and `-`. If they match but differ as written, such as
    `orderID` and `order_id`, the rule flags the `Set` call. (PAR-6, BET-22)

### `wlog init`, `wlog doctor`, and `wlog agents`

1. `init` rewrites code through `go/ast`, never a regex. `http.ListenAndServe(addr, nil)` wraps
   `http.DefaultServeMux`. An `http.Server{Handler: h}` literal wraps `h`. (CLI-4)
2. `init` merges into an existing `.env.example`, and it never removes a line. To pick a package
   name, it skips `_test.go` files. It finds `go.mod` by walking up from `--dir`.
   An unknown `--drain` exits 2. (CLI-15)
3. `init --dry-run` prints a unified diff. Writes go to temporary files first, then renames them
   all, so a failure leaves no partial tree. The generated setup closes the Logger on shutdown.
   (CLI-15)
4. The init test builds each generated app and serves one request that must return 200 and emit
   one event. (CLI-4)
5. `doctor` loads packages with `packages.Config.Dir` set to `--dir`. A load error fails the
   run. The drain and redactor tests walk every loaded file. A drain configured in code passes
   with a note. Module matches use exact module paths. (CLI-10)
8. `doctor` gives each finding a stable `WLOG_DOCTOR_*` code with why and fix. `--json` prints one
   JSON object with every finding. From phase 12, `wlog explain` knows each code. (PAR-34)
6. `agents` refuses to edit a file with an unpaired fence, and exits 1 with the line numbers. It
   searches for the end fence only after the start fence. It never overwrites a skill file
   without its generated marker. It keeps the file's line endings. (CLI-11)
7. Every template snippet compiles and runs in a test. `instrument-with-wlog.md` wraps the mux
   in `http.ListenAndServe`. `audit-a-handler.md` sets `Reason` and names only real functions.
   `analyze-wlog-output.md` uses shell commands. `agents-block.md` has no semicolon. (CLI-3,
   CLI-22)

## docs

1. CAPABILITIES.md statuses match reality. The v1.2 to v1.4 specs are marked approved with
   today's date, and this gap is written in CHANGELOG.md. (DOC-2)
2. README lists every module with its install line, and its status says the latest tag. The
   gate and criteria tables link to tests that pass. (DOC-3)
3. `docs/evlog-parity.md` is rewritten from the PAR rows in the audit, with no row marked "built"
   unless its detail-level behavior matches. (DOC-4)
4. `docs/event-shape.md`, `docs/customization.md`, and `docs/best-practices.md` state only what
   the code does, and every Go block compiles under `tools snippets`. (DOC-5)
5. SPEC.md v4 replaces v3, plan.md is archived, and every spec lists its real API. (DOC-6)
6. CLAUDE.md adds the proof rule and the `SetDefault` exception, and its gate list becomes G1 to G8
   from SPEC.md. (DOC-8)

## Success criteria

1. Each rule above, outside the docs section, has a test that fails on the current `main` and
   passes after its fix. The test name appears in the plan task that closes the rule. Each docs
   rule is proven by `tools snippets` and `tools ste`.
2. The event-shape fuzz test runs 10 minutes with no leak. Against the current `main`, it finds
   the CORE-1 and CORE-2 leaks.
3. `make race`, `make floor`, `make snippets`, `make cover`, and the full CI pass on the v0.5 tag
   candidate.
4. Each closed audit id is listed in CHANGELOG.md under v0.5.0.

## Testing

Follows SPEC.md. Each module keeps its existing black-box test package. A regression test for
each audit id is named `Test<Module>_<AuditID>_<Behavior>`, for example
`TestCore_CORE4_CallerMapUnchanged`, so a search for the id finds its proof.

## Boundaries

- **Always:** write the regression test from the audit repro first, and watch it fail.
- **Ask first:** any change to the default denylist beyond RED-3's list, or to capture defaults.
- **Never:** fix a phase 11 id here, or change the event shape here.

## Open questions

None.
