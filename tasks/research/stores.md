# core-calls research: data stores and outbound clients

Date: 2026-09-16. Method: module proxy (`go list -m -versions`, `go mod download`), source read in
`~/go/pkg/mod`, the Go vuln DB (`vuln.go.dev`), and compile-checked sketches. Everything below
comes from source unless it says **UNVERIFIED**.

Scratch module: `research/stores-work/`
- `latest/` builds every sketch at the latest versions (`go vet ./...` passes, Go 1.26.1).
- `floor/` builds the same sketches with **Go 1.23.12** and a `go 1.23` directive at the floor
  versions in the table below (`go vet ./...` passes).
- `latest/restyw/restyw_test.go` proves resty v2 and v3 transports see every retry.
- `latest/sqlw/sqlw_test.go` proves two `database/sql` wrapper pitfalls (passes on Go 1.26.1 and 1.27.1).
- `latest/shape/` is the SQL statement-shape tokenizer prototype, with tests and a fuzz target
  (20 s, 15M execs, no failure).

Toolchain facts: Go 1.27.1 is the current release. wlog root `go.mod` says `go 1.23`.

---

## 0. Summary table

| Library | Module | Latest stable | License | Latest `go` line | Floor we verified (builds on Go 1.23.12) | Highest version with `go` ≤ 1.23 | Vulns (Go vuln DB) |
|---|---|---|---|---|---|---|---|
| database/sql | stdlib | Go 1.27.1 | BSD-3 | n/a | Go 1.23 (Go 1.27 adds `driver.RowsColumnScanner`) | n/a | n/a |
| pgx | `github.com/jackc/pgx/v5` | v5.11.0 (2026-09-07) | MIT | 1.25.0 | **v5.6.0** (pool tracers). Query tracers exist since v5.0.0. `tracer.go` is byte-identical v5.0.0 to v5.11.0 | v5.7.6 | GO-2024-2606 fixed 5.5.4. GO-2026-4771/4772 fixed 5.9.0 (server-side `pgproto3.Backend.Receive` only). GO-2026-5004 fixed 5.9.2 (simple protocol + dollar quotes). |
| gorm | `gorm.io/gorm` | v1.31.2 | MIT | 1.18 | **v1.20.0** | v1.31.2 (all) | none |
| go-redis | `github.com/redis/go-redis/v9` | v9.22.0 (v9.23.0-beta.1 exists) | BSD-2 | 1.24 | **v9.0.0** builds. Recommend **v9.7.3** (vuln) | v9.18.0 | GO-2025-3540: safe at ≥ 9.7.3 (or 9.6.3, 9.5.5) |
| mongo | `go.mongodb.org/mongo-driver/v2` | v2.9.1 | Apache-2.0 | 1.25.0 | **v2.0.0**. Event structs identical v2.0.0 to v2.9.1 | v2.8.2 | GO-2026-5327 fixed 2.4.2 (GSSAPI `Connect`) |
| AWS SDK | `github.com/aws/aws-sdk-go-v2` + `github.com/aws/smithy-go` | v1.47.0 / v1.28.1 | Apache-2.0 | 1.24 / 1.24 | **aws v1.17.0 + smithy-go v1.13.3** | aws v1.41.2, smithy-go v1.24.1 | none |
| bun | `github.com/uptrace/bun` | v1.2.18 | BSD-2 | 1.24.0 | **v1.1.17** | v1.2.15 | none |
| resty v2 | `github.com/go-resty/resty/v2` | v2.17.2 | MIT | 1.23.0 | **v2.11.0** (vuln floor) | v2.17.2 (all) | GO-2023-2328 fixed 2.11.0 |
| resty v3 | `resty.dev/v3` | **no stable tag**. Newest is v3.0.0-rc.4 (2026-09-06) | MIT | 1.23.0 | n/a | v3.0.0-rc.4 | none |

Floor tension to decide: pgx fixes for 2026 advisories need v5.9.2, which needs Go 1.25. The
vulnerable symbols (`pgproto3.Backend.Receive`, `internal/sanitize.SanitizeSQL`) are not called by
a tracer. So a `store/pgx` module can require v5.6.0 and keep Go 1.23. Apps pick their own newer
pgx through MVS. Whether `govulncheck` in repo-ci stays quiet for the adapter module is
**UNVERIFIED**. It reports reachable symbols, so the expected result is quiet.

---

## 1. Cross-cutting findings (apply to every adapter)

1. **Never change the returned error.** go-redis `Client.Process` does `cmd.SetErr(err)` with the
   hook chain's result. database/sql compares `err != driver.ErrSkip` with `!=`. A wrapped error
   changes behavior. Return exactly what `next` returned.
2. **Callbacks run inline on the app goroutine with no recover.** mongo calls
   `op.CommandMonitor.Started(ctx, started)` directly. pgx calls tracers directly. gorm callbacks run
   in the processor loop. Each adapter needs its own `recover()` so a wlog bug cannot panic the app.
3. **Late records.** Many "end" hooks can fire after the request event emitted, or on another
   goroutine:
   - `sql.Rows` close runs from `Rows.awaitDone` in its own goroutine on ctx cancel (`sql.go:2996`, `3020`).
   - `sql.Tx` rollback runs from `Tx.awaitDone` (`sql.go:2221`).
   - pgx `TraceQueryEnd` for `Query` fires in `baseRows.Close` (`rows.go:207`), whenever the app closes rows.
   - pgxpool keeps connecting in the background after `Acquire` is canceled (`pgxpool/pool.go` comment "Connection will continue in background").
   The `calls[]` append must be race-free and a no-op after emit (gates G2, G3).
4. **Double recording.** gorm and bun sit on top of `database/sql`. gorm's postgres driver uses pgx
   stdlib. An app that installs `store-gorm` and `store-sql` records each query twice. Pick one rule:
   the higher layer marks the ctx and the lower layer skips, or docs say "install one". The gorm
   otel plugin already replaces `Statement.Context` with a derived ctx (`contextWrapper`), so a ctx
   marker works there.
5. **Background ctx means no event.** go-redis v9.22 AutoPipeliner flushes with
   `context.Background()` (`autopipeline.go:2367`, `2330`). gorm without `WithContext` uses
   `context.Background()` (`gorm.go:238`). resty without `SetContext` sends no ctx. These calls are
   invisible by design. Document it.
6. **Do not store raw error text by default.** AWS `APIError.ErrorMessage()` and database errors
   can echo input. Redis "unknown command" replies echo the first args (**UNVERIFIED**, from memory
   of Redis server behavior). Prefer an error class or code: SQLSTATE, `APIError.ErrorCode()`, the
   Redis error prefix (`WRONGTYPE`, `MOVED`).

---

## 2. database/sql and database/sql/driver (stdlib)

### Hook points
- If the driver exposes a `driver.Connector`: `sql.OpenDB(wrapConnector(c))`.
- `sql.Register("wlog-x", wrapDriver(d))` plus `sql.Open`. `sql.Open` checks
  `driver.DriverContext` first (`sql.go:871`). Otherwise it builds `dsnConnector{dsn, driver}`
  that calls `Driver.Open` and ignores ctx.

### Which optional interfaces database/sql checks, and where (Go 1.26.1 source)
| Interface | Checked on | Where | Default without it |
|---|---|---|---|
| `DriverContext` | Driver | `sql.go:871` | `dsnConnector` |
| `io.Closer` | Connector | `DB.Close`, `sql.go:951` | nothing |
| `SessionResetter` | Conn | `resetSession`, `sql.go:603`. Also `beginDC` `sql.go:1903` | nil |
| `Validator` | Conn | `validateConnection`, `sql.go:618`. Also `beginDC` | true |
| `Pinger` | Conn | `pingDC`, `sql.go:884` | nil |
| `ExecerContext`, then `Execer` | Conn | `execDC`, `sql.go:1700` | Prepare + Stmt exec |
| `QueryerContext`, then `Queryer` | Conn | `queryDC`, `sql.go:1772` | Prepare + Stmt query |
| `ConnPrepareContext` | Conn | `ctxDriverPrepare`, `ctxutil.go:14` | `Prepare`, then ctx check |
| `ConnBeginTx` | Conn | `ctxDriverBegin`, `ctxutil.go:98` | `Begin`. A non-default isolation or ReadOnly gives an error |
| `StmtExecContext` / `StmtQueryContext` | Stmt | `ctxutil.go:64`, `81` | `Exec`/`Query` with `namedValueToValue` |
| `NamedValueChecker` | **Stmt, else Conn** | `driverArgsConnLocked`, `convert.go:129` | `ColumnConverter`, then default |
| `ColumnConverter` (deprecated) | Stmt | `convert.go:133` | default converter |
| `RowsNextResultSet` | Rows | `sql.go:3072`, `3115` | close at EOF |
| `RowsColumnTypeScanType`, `DatabaseTypeName`, `Length`, `Nullable`, `PrecisionScale` | Rows | `rowsColumnInfoSetupConnLocked`, `sql.go:3284-3298` | `any`, "", (0,false), (false,false), (0,0,false) |
| `RowsColumnScanner` (**Go 1.27+ only**) | Rows | Go 1.27 `sql.go` and `driver.go` doc: with this interface, database/sql does not call `Next` | `Next` |

### Rules for a wrapper that does not change behavior
1. **ErrSkip must be the exact sentinel.** `execDC`/`queryDC` test `err != driver.ErrSkip`
   (`sql.go:1715`, `1788`). `driverArgsConnLocked` switches on `driver.ErrSkip` and
   `driver.ErrRemoveArgument` with `==`. Proven in `sqlw_test.go` `TestErrSkipMustBeTheSentinel`:
   `errors.Join(driver.ErrSkip)` makes `db.Exec` fail.
2. **Do not record ErrSkip as a call.** go-sql-driver/mysql `Exec`/`Query` return `driver.ErrSkip`
   whenever args exist and `interpolateParams` is off (`connection.go:441`, `500`). That is the
   default. database/sql then prepares, runs the Stmt, and closes it. Record the Stmt exec. Skip the
   ErrSkip attempt.
3. **Stmt `CheckNamedValue` must fall back to the inner Conn.** database/sql uses only one checker:
   the Stmt's, else the Conn's. A wrapper Stmt that always has `CheckNamedValue` and returns
   `ErrSkip` hides the Conn checker. Proven in `sqlw_test.go`: the naive wrapper fails with
   `sql: converting argument $1 type: unsupported type sqlw.money, a struct`. otelsql v0.44.0 does
   the right fallback (`stmt.go` `CheckNamedValue` comment).
4. **Do not add `ColumnConverter` unless the inner Stmt has it.** `ccChecker` returns nil for
   `index >= want` (`convert.go:55`). That differs from the default converter. go-sql-driver/mysql
   Stmt has both `ColumnConverter` and `CheckNamedValue` (`statement.go:45`, `49`).
5. **Conn methods that copy database/sql defaults are safe to add always.** With no inner
   `Pinger`, `Ping` returns nil. `IsValid` returns true. `ResetSession` returns nil.
   With neither inner form, `ExecContext`/`QueryContext` return `driver.ErrSkip`.
   `BeginTx` without inner `ConnBeginTx` must copy the isolation/ReadOnly errors from
   `ctxDriverBegin`. `PrepareContext` without inner support must copy the post-`Prepare` ctx check.
   Warning: `beginDC` sets `keepConnOnRollback = hasSessionResetter && hasConnectionValidator`
   (`sql.go:1903-1905`). A wrapper that always has both flips this for drivers that have neither.
   To be exact, choose the Conn wrapper type by what the inner conn has, at least for this pair
   (**inference from source, not tested**).
6. **Rows: every column-type method can exist with the defaults above.** `NextResultSet` can return
   `io.EOF` and `HasNextResultSet` false. That is equal to absent (see `nextLocked`,
   `sql.go:3050-3080`). otelsql v0.44.0 does **not** forward `ColumnTypeScanType`. Under otelsql,
   `ColumnType.ScanType()` returns `any` for pgx stdlib, which has it (`stdlib/sql.go:710`). Do not
   copy that bug.
7. **Go 1.27 `RowsColumnScanner`** needs a `//go:build go1.27` file that returns a second wrapper
   type. Use it for inner Rows that have the interface. otelsql does this (`rows_go1.27.go`, `rows_pre_go1.27.go`).
   Count rows in `NextRow` too.
8. **`sql.Conn.Raw` exposes the wrapper conn**, not the driver conn (`sql.go:2076`, `f(dc.ci)`).
   Code that does `dc.(*stdlib.Conn)` breaks under any wrapper. otelsql adds `Raw() driver.Conn`
   (issue 98). Do not wrap `driver.Result`: mysql documents `res.(mysql.Result).AllRowsAffected()`
   through `Raw` (`result.go:15-20`).
9. `driver.ErrBadConn` is checked with `errors.Is` (`sql.go:1482` and others). Pass it through unchanged.

### ctx and fields
- ctx: `ExecContext`, `QueryContext`, `PrepareContext`, `BeginTx`, `Ping`, `ResetSession`, and
  Stmt `*Context` receive the app ctx. `driver.Tx.Commit`/`Rollback` and `Rows.Next`/`Close` take no
  ctx. Keep the `BeginTx` ctx in the Tx wrapper and the query ctx in the Rows wrapper.
  For an inline pool open, `Connector.Connect(ctx)` gets the request ctx (`sql.go:1431`). For the
  opener goroutine, it gets that goroutine's ctx (`sql.go:1275`). The doc says "for dialing purposes only".
- `DB.Prepare` re-prepares lazily on other connections with the ctx of a later `Exec`. A "prepare"
  call can land in an unrelated request. Do not record prepare by default. Keep the query text on
  the Stmt wrapper.
- kind `db`. system: no API. Infer it from the driver type name (`%T`: `*stdlib.Driver`,
  `*pq.Driver`, `*mysql.MySQLDriver`), or take an option. operation: first keyword of the shape.
  target: none without parsing. status/error from err. rows: `Result.RowsAffected()` for exec. gorm
  calls it eagerly too (`callbacks/raw.go`). mysql's `RowsAffected` indexes
  `affectedRows[len-1]` (`result.go:43`), so guard with recover. Query rows: count `Next`/`NextRow`
  until `io.EOF` and end at `Rows.Close`. Otherwise duration only covers time to first response.

---

## 3. jackc/pgx v5 (v5.11.0)

### Signatures (`tracer.go`, `pgxpool/tracer.go`)
```go
type QueryTracer interface {
    TraceQueryStart(ctx context.Context, conn *Conn, data TraceQueryStartData) context.Context
    TraceQueryEnd(ctx context.Context, conn *Conn, data TraceQueryEndData)
}
type TraceQueryStartData struct{ SQL string; Args []any }
type TraceQueryEndData struct{ CommandTag pgconn.CommandTag; Err error }

type BatchTracer interface {
    TraceBatchStart(ctx context.Context, conn *Conn, data TraceBatchStartData) context.Context
    TraceBatchQuery(ctx context.Context, conn *Conn, data TraceBatchQueryData)
    TraceBatchEnd(ctx context.Context, conn *Conn, data TraceBatchEndData)
}
type TraceBatchStartData struct{ Batch *Batch }
type TraceBatchQueryData struct{ SQL string; Args []any; CommandTag pgconn.CommandTag; Err error }
type TraceBatchEndData struct{ Err error }

type CopyFromTracer interface {
    TraceCopyFromStart(ctx context.Context, conn *Conn, data TraceCopyFromStartData) context.Context
    TraceCopyFromEnd(ctx context.Context, conn *Conn, data TraceCopyFromEndData)
}
type TraceCopyFromStartData struct{ TableName Identifier; ColumnNames []string }
type TraceCopyFromEndData struct{ CommandTag pgconn.CommandTag; Err error }

type PrepareTracer interface {
    TracePrepareStart(ctx context.Context, conn *Conn, data TracePrepareStartData) context.Context
    TracePrepareEnd(ctx context.Context, conn *Conn, data TracePrepareEndData)
}
type TracePrepareStartData struct{ Name, SQL string }
type TracePrepareEndData struct{ AlreadyPrepared bool; Err error }

type ConnectTracer interface {
    TraceConnectStart(ctx context.Context, data TraceConnectStartData) context.Context
    TraceConnectEnd(ctx context.Context, data TraceConnectEndData)
}
type TraceConnectStartData struct{ ConnConfig *ConnConfig }
type TraceConnectEndData struct{ Conn *Conn; Err error }

// pgxpool (since v5.6.0)
type AcquireTracer interface {
    TraceAcquireStart(ctx context.Context, pool *Pool, data TraceAcquireStartData) context.Context
    TraceAcquireEnd(ctx context.Context, pool *Pool, data TraceAcquireEndData)
}
type TraceAcquireStartData struct{}
type TraceAcquireEndData struct{ Conn *pgx.Conn; Err error }
type ReleaseTracer interface { TraceRelease(pool *Pool, data TraceReleaseData) }
type TraceReleaseData struct{ Conn *pgx.Conn }
```

### How to set
- One field: `pgx.ConnConfig.Tracer QueryTracer` (`conn.go:25`). For a pool:
  `pgxpool.Config.ConnConfig.Tracer`.
- pgx type-asserts the same value for the other interfaces: Batch/CopyFrom/Prepare at connect
  (`conn.go:264-271`), Connect (`conn.go:245`), and pool Acquire/Release in `NewWithConfig`
  (`pgxpool/pool.go:254-259`). One struct that has all methods covers everything.

### Fields
- kind `db`, system `postgresql`.
- operation: `CommandTag.String()` gives a tag like `"INSERT 0 1"`. Helpers `Insert()`, `Update()`,
  `Delete()`, `Select()` exist (`pgconn.go:893-908`). On error the tag is empty, so take the
  operation from the SQL shape.
- target: `TraceCopyFromStartData.TableName`. Nothing for queries without parsing.
  `conn.Config()` has Host/Port/Database but it **copies** the config (`conn.go:474`). Cache it per
  conn, or read `TraceConnectStartData.ConnConfig` once.
- rows: `CommandTag.RowsAffected()`. For SELECT the tag is `SELECT n`, so it counts returned rows.
- error: `Err`. duration: keep start state in the ctx that `TraceQueryStart` returns. pgx passes
  that ctx to End (`rows.ctx`, `rows.go:208`). This costs one `context.WithValue` alloc per query.
- `SQL` can be a prepared statement name. `Args` holds values: never read them.

### ctx
- Start gets the app ctx. End gets the ctx that Start returned.
- `Exec` ends inline (`conn.go:492`). `Query` ends at `rows.Close()` (`rows.go:207`), which includes
  row reading time. Batch queries report through `TraceBatchQuery` at each result close.
- `TraceRelease` has no ctx, so it cannot be tied to an event.
- Pool connect: puddle's constructor ctx. Whether it keeps request values is **UNVERIFIED**.

### Composition
- Only one Tracer slot. `github.com/jackc/pgx/v5/multitracer` (since **v5.7.0**) exists:
  `multitracer.New(tracers ...pgx.QueryTracer) *Tracer` splits by interface and calls each in order
  (`multitracer/tracer.go`). v5.6.0 does not have it. So wlog needs its own `Wrap(next pgx.QueryTracer)`.
  For each method that `next` has, `Wrap` forwards the call. Without it, wlog drops otelpgx.
- Chain Start ctx in order: `ctx = next.TraceQueryStart(ctx, ...)`.

### Gotchas
- An app that sets `ConnConfig.Tracer` after wlog replaces wlog silently.
- Rows the app never closes never end the call.
- `TracePrepareEnd` with `AlreadyPrepared: true` is a cache hit. It is not a round trip.
- `deallocateInvalidatedCachedStatements` errors end the query before execution (`conn.go:483-487`).

---

## 4. gorm.io/gorm (v1.31.2)

### Signatures
```go
type Plugin interface { Name() string; Initialize(*DB) error }         // interfaces.go:24
func (db *DB) Use(plugin Plugin) error                                   // gorm.go:534, ErrRegistered on duplicate Name
func (db *DB) Callback() *callbacks
func (cs *callbacks) Create() / Query() / Update() / Delete() / Row() / Raw() *processor
func (p *processor) Before(name string) *callback
func (p *processor) After(name string) *callback
func (c *callback) Register(name string, fn func(*DB)) error
func (db *DB) InstanceSet(key string, value interface{}) *DB             // per-Statement store (sync.Map)
func (db *DB) InstanceGet(key string) (interface{}, bool)
```
Default callback names (`callbacks/callbacks.go:42-82`): `gorm:create`, `gorm:query`,
`gorm:update`, `gorm:delete`, `gorm:row`, `gorm:raw`. Wrap each with
`Before("gorm:X")` and `After("gorm:X")`. This matches gorm.io/plugin/opentelemetry v0.1.16.

### Fields (in the after callback)
- system: `db.Dialector.Name()` (`"postgres"`, `"mysql"`, `"sqlite"`, `"sqlserver"`).
- operation: processor name, or the first keyword of the SQL.
- target: `db.Statement.Table` (empty for many Raw/Exec calls).
- SQL: `db.Statement.SQL.String()`. It holds placeholders with values in `Statement.Vars`. But
  `LIMIT`/`OFFSET` are literal numbers (`clause/limit.go:17`), and `gorm.Expr`/`Raw` text can hold
  literals, so still shape it. `processor.Execute` resets `SQL` and `Vars` after the chain
  (`callbacks.go:145-148`), so read them inside the after callback.
- rows: `db.RowsAffected` (`-1` for `Row()`/`Rows()`, `callbacks/row.go`).
- error: `db.Error`. `gorm.ErrRecordNotFound` comes from `scan.go:367` and is not a failure.
  Also `errors.Is`-wrapped chains form through `AddError` (`gorm.go:413-417`, `fmt.Errorf("%v; %w")`).
- duration: `db.InstanceSet(key, time.Now())` in before, `InstanceGet` in after. Or wrap
  `Statement.Context` like the otel plugin does.

### ctx
`db.Statement.Context`. It is set by `WithContext(ctx)`. The default is `context.Background()`
(`gorm.go:238`). With `DefaultContextTimeout`, `Execute` replaces it with a timeout ctx
(`callbacks.go:97-101`). Values stay.

### Composition
- Callbacks sort by before/after names. Several plugins can wrap the same `gorm:*` callback.
  Duplicate names only warn (`sortCallbacks`). Use a unique prefix like `wlog:`.
- `db.Use` twice with the same `Name()` returns `ErrRegistered`.

### Gotchas
- Transactions (`db.Transaction`, `Begin`, `Commit`) do not run callbacks. Create/Update/Delete wrap
  `gorm:begin_transaction` outside `gorm:create`, so BEGIN/COMMIT are not in the timed span.
- `Row()`/`Rows()` durations stop before iteration. Rows is -1.
- Preload runs extra queries inside `gorm:preload`, after `gorm:query`. They run through the Query
  processor again, so they record separately (**UNVERIFIED** in detail).
- Register callbacks at init. `compile()` mutates processor slices without a lock (`callbacks.go:194`).
- Floor v1.20.0 builds all fields used here on Go 1.23.

---

## 5. redis/go-redis v9 (v9.22.0)

### Signatures (`redis.go:55-65`)
```go
type Hook interface {
    DialHook(next DialHook) DialHook
    ProcessHook(next ProcessHook) ProcessHook
    ProcessPipelineHook(next ProcessPipelineHook) ProcessPipelineHook
}
type (
    DialHook            func(ctx context.Context, network, addr string) (net.Conn, error)
    ProcessHook         func(ctx context.Context, cmd Cmder) error
    ProcessPipelineHook func(ctx context.Context, cmds []Cmder) error
)
func (c *Client) AddHook(hook Hook)   // also on ClusterClient, Ring, UniversalClient
const Nil = proto.Nil                  // RedisError("redis: nil")
```
Identical in v9.0.0, v9.7.3, v9.17.0, v9.18.0, and v9.22.0.

### Fields
- kind `cache` or `db`, system `redis`.
- operation: `cmd.Name()` is lowercase first arg. `cmd.FullName()` is `"cluster info"` style.
- target: none safe. `cmd.Args()` holds keys and values, so never read it by default.
- error: `next` result. `redis.Nil` means a miss, not an error. Compare with `errors.Is(err, redis.Nil)`.
- pipeline: `len(cmds)`. Per-command `cmd.Err()`. The pipeline error is the first command error
  (`cmdsFirstErr`, `command.go:263`) and can be `redis.Nil`.
- TxPipeline: hooks see `multi` and `exec` added around the commands (`wrapMultiExec`, `tx.go:199`).
- rows: no. Duration: time around `next`.

### ctx
The caller's ctx for `Process` and pipelines. `DialHook` gets the ctx of the command that needed a
connection, or a pool ctx for background refill (**UNVERIFIED**). AutoPipeliner (new in **v9.22.0**)
runs hooks with `context.Background()` (`autopipeline.go:2330`, `2367`). Those commands never show up.

### Composition
- Hooks form a FIFO chain. `rebuild` wraps from last to first (`redis.go:95-106`). A nil return
  keeps `next`. redisotel and a wlog hook stack fine.
- `rebuild` calls `ProcessPipelineHook` twice per hook, once for `pipeline` and once for
  `txPipeline`. Build no per-install state there.
- ClusterClient: its own hooks wrap cluster `process`. Node clients have separate hooks through
  `OnNewNode` (`osscluster.go:1207`). The cluster dial hook slot is nil and never runs.

### Gotchas
- Before v9.22.0, `AddHook` appends with no lock (`hs.slice = append(...)`, checked in v9.18.0).
  Call it before first use. v9.22.0 uses an atomic copy-on-write snapshot.
- In v9.22, `Cmder.Err()` can block on async autopipeline batches (`await`, `command.go:411`).
  A guard stops self-deadlock on the executor goroutine. Check "does ctx carry an event" first and
  return early before touching cmds.
- Return `next`'s error unchanged. `Client.Process` stores the chain result with `cmd.SetErr(err)`
  (`redis.go:2146`).

---

## 6. mongo-driver v2 (v2.9.1)

### Signatures (`event/monitoring.go`)
```go
type CommandMonitor struct {
    Started   func(context.Context, *CommandStartedEvent)
    Succeeded func(context.Context, *CommandSucceededEvent)
    Failed    func(context.Context, *CommandFailedEvent)
}
type CommandStartedEvent struct {
    Command bson.Raw; DatabaseName, CommandName string; RequestID int64; ConnectionID string
    ServerConnectionID *int64; ServiceID *bson.ObjectID
}
type CommandFinishedEvent struct {
    Duration time.Duration; CommandName, DatabaseName string; RequestID int64; ConnectionID string
    ServerConnectionID *int64; ServiceID *bson.ObjectID
}
type CommandSucceededEvent struct { CommandFinishedEvent; Reply bson.Raw }
type CommandFailedEvent struct { CommandFinishedEvent; Failure error }

func (c *ClientOptions) SetMonitor(m *event.CommandMonitor) *ClientOptions   // one slot: ClientOptions.Monitor
```

### Fields
- kind `db`, system `mongodb`.
- operation: `CommandName`. target: `DatabaseName`. The collection is only in
  `CommandStartedEvent.Command`, as the first element's string value (`{"find": "users"}`).
  Read it with `cmd.IndexErr(0)` and `Value().StringValueOK()` (compile-checked in `mongow.go`).
- duration: `CommandFinishedEvent.Duration`. The driver measures only the round trip
  (`operation.go:814-842`).
- error: `Failure`. rows: `Reply.Lookup("n")` for insert/update/delete. Find batches need an array
  length (**UNVERIFIED** as worth the cost).
- Correlate Started with Finished by `RequestID` plus `ConnectionID`. The Finished-only design below
  needs no correlation.

### Avoid capturing bodies, and the cost finding
- If `Started` is set, the driver **copies the full command**, including document sequences
  (every inserted document), for each command (`redactStartedInformationCmd`,
  `operation.go:182-233`). If `Started` is nil, `canPublishStartedEvent` is false and no copy
  happens (`operation.go:2144`).
- `Reply` is `bson.Raw(info.response)`, a view with no copy (`operation.go:236`).
- **Recommendation:** set only `Succeeded` and `Failed` by default (op, db, duration, error).
  Collection name is opt-in through `Started`, and costs a body copy.
- The driver redacts auth commands to an empty `Command`/`Reply`: `authenticate`, `saslStart`,
  `saslContinue`, `getnonce`, `createUser`, `updateUser`, `copydb*`, and `hello` with
  `speculativeAuthenticate` (`operation.go:2124`). wlog must still never store bodies.
- Do not keep `Reply` after the callback returns. Its buffer lifetime is **UNVERIFIED**.

### ctx
The operation ctx is passed directly (`publishStartedEvent(ctx, ...)`, `operation.go:789`, `844`).
Each retry attempt is its own Started/Finished pair, so a retried op records several calls. SDAM
heartbeats use a nil monitor (`topology/server.go:828`).

### Composition
One slot. Wrap an existing monitor: `o.SetMonitor(Monitor(o.Monitor))` and call `next` callbacks.
Leave `Started` nil unless `next.Started` is set. See the sketch in `latest/mongow/mongow.go`. Order matters:
wlog must install after otelmongo, or the later `SetMonitor` wins.

---

## 7. aws-sdk-go-v2 (v1.47.0, smithy-go v1.28.1)

### Signatures
```go
// config: LoadOptions.APIOptions / aws.Config.APIOptions
APIOptions []func(*middleware.Stack) error                                   // aws/config.go:116
func config.WithAPIOptions(v []func(*middleware.Stack) error) LoadOptionsFunc // config/load_options.go:854

// smithy-go/middleware
func InitializeMiddlewareFunc(id string, fn func(context.Context, InitializeInput, InitializeHandler) (InitializeOutput, Metadata, error)) InitializeMiddleware
func FinalizeMiddlewareFunc(id string, fn func(context.Context, FinalizeInput, FinalizeHandler) (FinalizeOutput, Metadata, error)) FinalizeMiddleware
func DeserializeMiddlewareFunc(id string, fn func(context.Context, DeserializeInput, DeserializeHandler) (DeserializeOutput, Metadata, error)) DeserializeMiddleware
func (s *InitializeStep) Add(m InitializeMiddleware, pos RelativePosition) error
func (s *InitializeStep) Insert(m InitializeMiddleware, relativeTo string, pos RelativePosition) error
func (s *InitializeStep) Get(id string) (InitializeMiddleware, bool)

// aws/middleware (awsmiddleware)
func GetServiceID(ctx context.Context) string      // "S3", "DynamoDB", "SQS"
func GetOperationName(ctx context.Context) string  // "PutObject"
func GetRegion(ctx context.Context) string
func GetRequestIDMetadata(metadata middleware.Metadata) (string, bool)
func GetRawResponse(metadata middleware.Metadata) interface{}   // *smithyhttp.Response

// aws/retry
func GetAttemptResults(metadata middleware.Metadata) (AttemptResults, bool)  // len(Results) = attempts

// smithy-go/transport/http
func (e *ResponseError) HTTPStatusCode() int
// smithy-go: APIError interface { ErrorCode() string; ErrorMessage() string; ErrorFault() ErrorFault }
```

### Where to hook
- **Initialize step, `middleware.After`**: runs once per operation, outside the retry loop.
  `Retry` is a Finalize middleware inserted before `Signing` (`aws/retry/middleware.go:514`).
  The metadata from the Initialize `next` call has three items. They are attempt results
  (`addAttemptResults`, line 177), the request ID (`RequestIDRetriever`, Deserialize step), and the
  raw response (`AddRawResponseToMetadata`).
- Service ID, operation, and region are in ctx before any middleware runs.
  `resolveServiceMetadata` sets them in `invokeOperation` (checked in service/sqs v1.52.0,
  `api_client.go:272`). Older clients set them with an Initialize middleware
  "RegisterServiceMetadata". `After` covers both (**UNVERIFIED** for old clients, but the floor
  sketch builds against aws v1.17.0).
- A Finalize middleware added after Retry sees each attempt. A Deserialize middleware also sees
  each attempt.

### Fields
- kind `rpc` or `cloud`, system `aws`.
- service: `GetServiceID`. operation: `GetOperationName`. region: `GetRegion`.
- request id: `GetRequestIDMetadata(md)`. status: `GetRawResponse(md).(*smithyhttp.Response).StatusCode`,
  or on error `errors.As(err, &*smithyhttp.ResponseError)` then `HTTPStatusCode()`.
- error code: `errors.As(err, &smithy.APIError)` then `ErrorCode()`. Avoid `ErrorMessage()`.
- attempts: `len(retry.GetAttemptResults(md).Results)`.
- target (bucket, table, queue URL) lives only in `in.Parameters`, a typed input per service.
  Getting it needs service imports or reflection on known field names (`Bucket`, `TableName`,
  `QueueUrl`). That is a design choice, not researched further.
- The stack error is raw inside middleware. `invokeOperation` wraps it into
  `*smithy.OperationError` after the stack returns (`api_client.go:327`).

### ctx
The ctx passed to `client.Op(ctx, ...)`. The stack sees it with stack values added.
`middleware.ClearStackValues` runs first (`api_client.go:255`). Normal ctx values stay.

### Composition and gotchas
- APIOptions is a slice. otelaws `AppendMiddlewares(&cfg.APIOptions)` and wlog add their own IDs and coexist.
- **Guard double installs with `stack.Initialize.Get(id)`.** In smithy-go v1.28.1, `Add` "never
  returns an error" for duplicates (`step_initialize.go` comment), so a double install records
  twice. Older smithy-go used `orderedIDs`, which errors on a duplicate ID (`ordered_group.go:39`),
  and that fails the operation. When the change happened is **UNVERIFIED**.
- APIOptions set per client or per call (`optFns`) add more functions after config ones
  (`api_client.go:284`).
- smithy-go also has an `InterceptorRegistry` (`transport/http/interceptor.go`, `AddAfterExecution`
  and others). It is newer. When it appeared is **UNVERIFIED**. The middleware route works from
  aws v1.17.0, so use middleware.

---

## 8. uptrace/bun (v1.2.18)

### Signatures (`hook.go`)
```go
type QueryHook interface {
    BeforeQuery(context.Context, *QueryEvent) context.Context
    AfterQuery(context.Context, *QueryEvent)
}
type QueryEvent struct {
    DB *DB
    IQuery        Query
    Query         string
    QueryTemplate string
    QueryArgs     []any
    Model         Model
    StartTime time.Time
    Result    sql.Result
    Err       error
    Stash map[any]any
}
func (e *QueryEvent) Operation() string
func (db *DB) AddQueryHook(hook QueryHook)        // deprecated in 1.2.16+, present in all versions
func (db *DB) WithQueryHook(hook QueryHook) *DB   // added in v1.2.16 (go 1.24). Absent in v1.2.15
```
The `QueryHook` shape is the same in v1.1.17, v1.2.0, v1.2.5, v1.2.10, v1.2.15, and v1.2.18.

### Fields
- system: `e.DB.Dialect().Name().String()`. operation: `e.Operation()`, which uses `IQuery.Operation()`
  or the first word, capped at 16 bytes.
- duration: `time.Since(e.StartTime)`. The event sets `StartTime` (`hook.go:78`).
- rows: for a non-nil Result, `e.Result.RowsAffected()`. Select paths pass nil (`query_select.go:871`).
- error: `e.Err`. bun itself does not count `sql.ErrNoRows` as an error (`hook.go:94-99`).
- **`Query` is the formatted SQL with args inlined** (`db.go:359`, `db.format(query, args)`).
  Builder queries pass the formatted string as both template and query (`query_select.go:869`).
  **Always shape it.** `QueryArgs` holds values.
- `Stash` lets Before pass state to After with no ctx alloc. It exists in v1.2.10+. Earlier support
  is **UNVERIFIED**.

### ctx
Before gets the app ctx and returns a ctx that bun passes to the driver and to After. `BEGIN`,
`COMMIT`, and `ROLLBACK` are events too (`db.go:560`, `638`, `660`, `684`). Commit/Rollback use the
ctx from `BeginTx` (`tx.ctx`).

### Composition
A slice of hooks. Before runs in order. After runs in reverse (`afterQueryFromIndex`). bunotel
coexists. A hook with `Init(*bun.DB)` gets it called on add (`db.go:305`).

### Gotchas
- `AddQueryHook` mutates the slice with no lock. Call it at init.
- `Rows()`-style queries end before iteration.
- bun runs on `database/sql`, so see the double-recording note in section 1.

---

## 9. go-resty v2 and v3: does SetTransport see every request?

**Yes, including retries.** Proven by `latest/restyw/restyw_test.go`: a server returns 503 twice,
then 200. A wrapping `http.RoundTripper` counts **3** round trips for v2.17.2 and for
v3.0.0-rc.4. v3 needs a retry condition for 5xx: default conditions retry only on errors
(`request.go:1541`).

Why:
- v2: `Request.Execute` calls `client.execute` per attempt inside `Backoff`
  (`request.go:1049-1061`). That calls `c.httpClient.Do(req.RawRequest)` (`client.go:1251`), which
  calls `Transport.RoundTrip`. ctx: after `SetContext`, resty calls
  `r.RawRequest.WithContext(r.ctx)` (`middleware.go:266`). Without it, no ctx.
- v3: the loop `for i := 0; i <= r.RetryCount; i++ { res, err = r.client.execute(r) }`
  (`request.go:1510-1514`) calls `c.Client().Do(req.withTimeout())` (`client.go:2490`).
  The request uses `http.NewRequestWithContext(r.Context(), ...)` (`middleware.go:243`).
- The `http.Client` redirect loop also calls RoundTrip once per hop (stdlib behavior).

Gotchas:
- **v2: wrapping breaks later TLS/proxy setters.** `SetTLSClientConfig`, `SetProxy`,
  `SetCertificates`, and `SetRootCertificate` go through `c.Transport()`, which needs
  `*http.Transport` (`client.go:1361`). Otherwise they log "current transport is not an
  *http.Transport instance" and do nothing. Apps must configure TLS and proxy first, then wrap.
- **v3:** `SetTLSClientConfig` accepts any transport that has
  `TLSClientConfiger { TLSClientConfig() *tls.Config; SetTLSClientConfig(*tls.Config) error }`
  (`client.go:146`, `1624`). A wlog transport can forward both to the inner transport.
  `SetProxy` still needs `*http.Transport` (`HTTPTransport()`, `client.go:1995`).
- v3 `SetHedging` wraps the current transport (`client.go:1556`). If wlog is inside, each hedged
  copy is a call, and losers are canceled. If wlog is outside, there is one call, but
  `SetHedging(nil)` can no longer unwrap because it type-asserts `Hedger`. v3 `SetDigestAuth` wraps
  too, and digest does 2 round trips (`digest.go:75`, `108`).
- v3 circuit breaker rejections never reach the transport (`client.go:2469`).
- RoundTrip returns at headers. Body read time is not in the duration unless the body is wrapped.
- v3 has no stable release. Do not name a v3 floor.

---

## 10. SQL statement shape with the standard library only

### Why not text/scanner, go/scanner, or regexp
- `text/scanner` and `go/scanner` lex Go. `'abc'` is a bad char literal, backquotes are raw strings,
  `#` and `--` are not comments, and nesting is not handled.
- RE2 `regexp` has no backreferences, so it cannot match `$tag$...$tag$` or nested `/* */`. Several
  regex passes also conflict: `--` inside a string, or a quote inside a comment.
- A one-pass byte scanner is the minimal correct tool. All delimiters are ASCII, so bytes are safe
  with UTF-8. Prototype: `latest/shape/shape.go`, about 350 lines with comments.

### The safety rule the prototype uses
Copy only identifiers, keywords, operators, and punctuation. Write `?` for strings, numbers, and
positional placeholders. Drop comments. **When a construct's meaning depends on the dialect or a
server setting, stop and drop the rest of the input** (append `…`). A lexing doubt then removes
text. It never copies a literal.

### Pitfalls and the prototype's rule for each (all in `shape_test.go`)
| Pitfall | Why it matters | Rule |
|---|---|---|
| Backslash in `'...'` | Escape under MySQL default mode and Postgres `standard_conforming_strings=off`. A plain byte otherwise. `'a\''SECRET'` leaks under one reading or the other (a test caught this in an earlier version) | Postgres, MySQL, Unknown: **stop**. SQLite, SQL Server: plain byte |
| Postgres `E'...'` | Backslash escapes always | Postgres: escape mode. The prefix is dropped |
| `N'..'`, `X'..'`, `B'..'`, `U&'..'`, MySQL `_utf8mb4'..'` | Identifier touches a quote | Treat as one literal. Drop 1-letter and `_charset` prefixes. Keep `DATE`/`interval` words |
| `$tag$...$tag$`, `$$...$$` | Body can hold quotes and `--` | Postgres: find the exact closer, else stop. Unknown: a body with `'` or `"` means stop. `$` after an identifier byte is part of the identifier (`a$b$`). pgx's own lexer skips this check (`internal/sanitize/sanitize.go:241-256`) |
| `$1` | Placeholder, different per list length | Write `?` so `IN ($1,$2)` and `IN ($1,$2,$3)` group together |
| `"..."` | Identifier in Postgres. String in MySQL (no ANSI_QUOTES). With no matching column, SQLite reads it as a string. SQL Server reads it as a string with QUOTED_IDENTIFIER OFF | Keep only for Postgres. Write `?` otherwise |
| `` `...` `` | MySQL/SQLite identifier | Keep verbatim |
| `[...]` | SQL Server/SQLite identifier. Postgres array subscript | Identifier only for SQL Server and SQLite |
| `--` | MySQL needs whitespace after it. `1--1` is `1 - -1` | MySQL exact rule. Unknown: a quote in the comment line means stop |
| `#` | MySQL comment. Postgres XOR operator | MySQL: comment. Unknown: comment, and a quote in the line means stop. Postgres, SQLite, SQL Server: operator |
| `/* /* */ */` | Nests in Postgres and SQL Server. Does not nest in MySQL or SQLite | Exact per dialect. Unknown: stop on an inner `/*` |
| MySQL `/*! ... */` | Executable comment | Dropped (over-redacts, safe) |
| sqlcommenter `/*traceparent='..'*/` | Comment has values | Dropped |
| Numbers: `1e-3`, `.5`, `0x1F`, `1_000`, `0b1` | Must not touch `t1`, `col2` | Number scanner runs only at a token start. A digit run followed by a letter is an identifier (MySQL `2fa_codes`) |
| `?`, `?1` (SQLite), `:name`, `@p1` | Placeholders | `?NNN` becomes `?`. Named ones stay (stable) |
| Postgres JSON `?`, `?|`, `@>` | Operators that look like placeholders | Kept. Harmless in a shape |
| `IN (?, ?, ?)`, `VALUES (?, ?), (?, ?)` | Cardinality splits groups | Post-pass: `(?, ?, …)` becomes `(?)`, then `(?), (?)` becomes `(?)` |
| Huge SQL (bulk insert) | CPU | Scan at most 64 KiB. Drop the rest |
| Output cap | Size | Cut at `max` bytes on a rune start. `strings.ToValidUTF8`. Add `…` |
| Unterminated string, comment, or dollar quote | Rest is literal | Stop |
| Oracle `q'[...]'` | Quote delimiter is custom | Not handled. Oracle is out of scope. The generic prefix rule stops at the first `'`, which can leak. Document it |

Examples from the tests:
- Postgres `select a from t where b in (1, 2, 3) and c = 'x'` becomes `select a from t where b in (?) and c = ?`.
- MySQL `INSERT INTO t (a,b) VALUES (1,'x'),(2,'y')` becomes `INSERT INTO t (a, b) VALUES (?)`.
- Postgres `SELECT '2020-01-01'::date, interval '1 day', DATE'2020'` becomes `SELECT ?::date, interval ?, DATE ?`.
- Postgres `SELECT 'C:\' , name FROM t` becomes `SELECT ?…` (stopped). SQLite gives `SELECT ?, name FROM t`.

### Recommendation
- Ship the one-pass byte scanner with a `Dialect` input: Unknown, Postgres, MySQL, SQLite, SQLServer.
- Each adapter passes its dialect: pgx gives Postgres, gorm `Dialector.Name()`, bun
  `Dialect().Name()`, store-sql from the driver type name or an option. Unknown is the safe default.
- Keep the "stop on ambiguity" rule. It is the only rule that held in the leak tests without a
  dual-reading state machine.
- Gates: a leak table test (SECRET inside each literal form), and a fuzz target that checks bounded
  output, valid UTF-8, and no panic. A stronger G1 fuzz oracle (build a query around a secret in a
  literal, then assert it is absent) is not written yet.
- Lowercasing keywords for grouping is optional. The prototype keeps the input case.
