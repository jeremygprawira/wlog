# Spec: outbound calls and data stores (track B)

> Phase 12 · depends on: `core-calls`, `propagate`, the `calls` conformance suite. Root module
> ids: `client-http` (`wlog/client/http`), `store-sql` (`wlog/store/sql`), and the shared SQL
> shaper in `wlog/sqlshape`. Own-module ids: `store-pgx`, `store-gorm`, `store-redis`,
> `store-mongo`, `client-aws`, and `store-bun`. Project-wide rules in [SPEC.md](SPEC.md) apply.
> Facts come from the API research of 2026-09-16, checked against source, with sketches built on
> Go 1.23.

## Objective

One event shows where a unit of work spent its time and what failed: each HTTP call, query,
cache command, and cloud API call, with no parameter values. Adding a client or store is one line.
The client or driver behaves exactly as it does without wlog.

## Shared rules

1. An adapter returns exactly the error, result, and value the wrapped code returned. database/sql
   compares `driver.ErrSkip` with `!=`, and go-redis stores the hook chain's error.
2. Every callback records under recover, because drivers call hooks with no recovery of their own.
3. An end hook that fires after the event emitted records nothing.
4. An adapter under another adapter reads `wlog.CallFromContext`, and skips a call of the same
   kind. `store-gorm` and `store-bun` pass the call context down, so the driver wrapper below skips.
5. A context with no event records nothing. The package doc lists the known cases:
   - gorm without `WithContext`
   - resty without `SetContext`
   - the go-redis v9.22 auto pipeliner, which uses `context.Background()`
6. No adapter reads query parameters, command arguments, request bodies, or reply bodies. The
   `error` in a call record holds a code, such as a SQLSTATE, an AWS error code, or a Redis error
   prefix. It never holds raw database or API message text, because that text can echo input.
7. SQL text becomes a statement shape through `sqlshape`. A call record never holds raw SQL.

## sqlshape (root, package `store/sqlshape`)

<!-- snippet:sketch -->
```go
type Dialect int // Unknown, Postgres, MySQL, SQLite, SQLServer

func Shape(sql string, d Dialect, max int) string // max default 512
func Operation(shape string) string                 // first keyword: SELECT, INSERT, ...
```

- One pass over bytes. It copies identifiers, keywords, operators, and punctuation. It writes `?`
  for strings, numbers, and positional placeholders, and drops comments.
- Where a construct's meaning depends on the dialect or a server setting, it stops and writes `…`.
  Backslash inside `'...'` stops under Postgres, MySQL, and Unknown. A dollar-quoted body with no
  exact closer stops.
- `IN (?, ?, ?)` becomes `IN (?)`, and `VALUES (?, ?), (?, ?)` becomes `VALUES (?)`.
- It scans at most 64 KiB, and the output is valid UTF-8 cut at `max` bytes.
- The prototype and its tests in `tasks/research/prototypes/sqlshape/` (saved as `.go.txt`) are
  the starting point.
- `FuzzShape_NeverCopiesLiteral` builds a query around a secret inside each literal form, then
  asserts the secret is absent. It also checks bounded output, valid UTF-8, and no panic.

## Modules

| Module | Hook point | Call fields | Floor |
|---|---|---|---|
| `client-http` (root) | `wlogclient.Transport(next http.RoundTripper) http.RoundTripper` | kind `http`, system `http`, operation method, target host plus any known route template, status, duration to headers. Injects `traceparent` and `X-Request-ID` | Go 1.21 |
| `store-sql` (root) | `wlogsql.Wrap(connector driver.Connector, opts...)` for `sql.OpenDB`, and `wlogsql.WrapDriver(d driver.Driver, opts...)` for a name the app registers itself | kind `db`, system from `WithSystem` or the driver type name, operation and shape from `sqlshape`, rows from `RowsAffected` or counted `Next`. A query call ends at `Rows.Close` | Go 1.21, with a `//go:build go1.27` file for `RowsColumnScanner` |
| `store-pgx` | `wlogpgx.Tracer(next pgx.QueryTracer)`, set in `ConnConfig.Tracer`. It forwards every interface `next` has | kind `db`, system `postgresql`, shape from `TraceQueryStartData.SQL`, rows from `CommandTag.RowsAffected()`, COPY table name. `Args` is never read. A cached prepare is not a call | pgx v5.6.0, Go 1.21 |
| `store-gorm` | `db.Use(wloggorm.Plugin())`, with callbacks named `wlog:*` around each `gorm:*` callback | kind `db`, system `Dialector.Name()`, target `Statement.Table`, shape from `Statement.SQL`, rows `RowsAffected`. `gorm.ErrRecordNotFound` is not an error | gorm v1.20.0, Go 1.21 |
| `store-redis` | `rdb.AddHook(wlogredis.Hook())`, before first use | kind `cache`, system `redis`, operation `cmd.Name()`. `redis.Nil` is a miss, not an error. A pipeline is one call with `rows` set to its command count. `Args` is never read | go-redis v9.7.3 (first safe from GO-2025-3540), Go 1.21 |
| `store-mongo` | `opts.SetMonitor(wlogmongo.Monitor(opts.Monitor))`, which wraps an existing monitor | kind `db`, system `mongodb`, operation `CommandName`, target `DatabaseName`, duration from the driver. `Started` stays nil unless `WithCollection()`, because setting it copies every command body | mongo-driver v2.0.0, Go 1.21 |
| `client-aws` | `wlogaws.Append(&cfg.APIOptions)`, an Initialize-step middleware added once, guarded by `Stack.Initialize.Get(id)` | kind `rpc`, system `aws`, operation `{ServiceID}.{OperationName}`, attrs `region`, `request_id`, `attempts`. Error code from `smithy.APIError.ErrorCode()`. `Parameters` are never read | aws-sdk-go-v2 v1.17.0 with smithy-go v1.13.3, Go 1.21 |
| `store-bun` | `db.AddQueryHook(wlogbun.Hook())` | kind `db`, system from the dialect, shape from `QueryEvent.Query` (args already inlined, so shaping is required), rows from `Result` | bun v1.1.17, Go 1.21 |

### store-sql rules from database/sql

- Return `driver.ErrSkip` as the exact sentinel, and never record the skipped attempt.
- A wrapper `Stmt` implements `CheckNamedValue` by calling the inner `Stmt` checker, then the inner
  `Conn` checker, then `driver.ErrSkip`.
- If the inner `Stmt` lacks `ColumnConverter`, the wrapper `Stmt` lacks it too.
- Pick the `Conn` wrapper type by whether the inner conn has `SessionResetter` and `Validator`.
  If either is missing, database/sql drops the connection on rollback, and the wrapper must match.
- Forward every `Rows` column type method, and implement `Raw() driver.Conn`.
- Never wrap `driver.Result`.
- `DB.Prepare` is not a call by default, because a lazy re-prepare runs under a later request's
  context.

### Resty

resty v2 and v3 send every attempt through `http.Client.Transport`, so `client-http` covers them.
The recipe says to set TLS and proxy options before wrapping the transport in resty v2.

## Success criteria

1. Each module passes the `calls` conformance suite.
2. `store-sql` passes the database/sql behavior tests from the research: the `ErrSkip` sentinel, a
   custom `NamedValueChecker` type, column `ScanType`, and `Raw` access. Root tests use a fake
   `driver.Driver`. The module `store/sql/drivertest`, with its own `go.mod` and no tag, runs the
   same tests with pgx stdlib, go-sql-driver/mysql, and modernc SQLite.
3. gorm over pgx stdlib with both `store-gorm` and `store-sql` installed records each query once.
4. A query with a secret inside every literal form in the `sqlshape` pitfall table records a shape
   without the secret.
5. A Redis `GET` miss records status `miss` with no error. An AWS `PutObject` retried twice records
   one call with `attempts` 3.
6. A mongo insert of 1,000 documents allocates no copy of the documents with default options.
7. Each module passes `tools floor` at its library floor.

## Testing

Unit tests use in-memory or fake servers: `httptest`, a fake `driver.Driver`, pgx against a fake
connection where possible, and `miniredis` in the `store-redis` module. Real databases run only
under `make integration`.

## Boundaries

- **Always:** return the wrapped call's results unchanged.
- **Ask first:** reading any parameter, argument, or body.
- **Never:** store raw SQL text, database error text, or AWS error messages.

## Open questions

None.
