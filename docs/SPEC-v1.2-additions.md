# Spec: v1.2 additions to existing modules

> Amends `core`, `audit`, `drain-memory`, `drain-file`, `redact`, and `errors-herr`.
> Each section below is an addition. No behavior already specified changes.
> Project-wide rules in [SPEC.md](SPEC.md) apply.
> Closes gaps 3, 4, 5, 12, and 15 in [evlog parity](evlog-parity.md), plus two partials.

New modules in this phase have their own files: [catalog](SPEC-catalog.md) and
[llm](SPEC-llm.md).

The older module specs carry style debt from before the plain-English lint hook existed.
Task D1 in the plan cleans them and adds a back link to this file from each one.

## core: public and internal error detail (gap 12)

`ErrorInfo` gains two maps that split what an error carries by audience.

```go
type ErrorInfo struct {
    // every field in SPEC-core.md stays as it is
    Data     map[string]any // safe to send to a client, such as a rejected field name
    Internal map[string]any // log only, such as a row id or a query
}
```

`Attrs` stays, so no existing extractor breaks. An extractor that knows the difference
fills `Data` and `Internal` instead. Both maps reach the event, since the whole event is
log-only and passes the redactor first. `Data` answers one question for a transport: what
part of this error is safe to send back.

Two helpers cover the common path.

```go
func ErrorData(ctx context.Context) map[string]any // the current error's Data, or nil
func Errorf(ctx context.Context, format string, a ...any) error
```

`Errorf` builds an error, records it through `wlog.Error`, and returns it. This replaces
the build-then-record pair that every handler writes today.

## core: global modes (gap 15)

```go
func SetEnabled(on bool)  // process-wide off switch, default on
func Enabled() bool
func WithSilent() Option  // this logger emits nothing to stdout, drains still run
func WithRawValues() Option // store a value as given, skip the JSON normalize step
```

`SetEnabled(false)` makes every `Start` return a no-op end func. A test or a batch job can
then turn logging off without rewiring its options. The switch is one `atomic.Bool`. SPEC.md's "no package-level mutable state" rule allows
this one case. It stays read-only on the hot path and never holds per-event data.

`WithSilent` keeps the pipeline and the drains, and drops only the stdout write. A service
that ships to Axiom alone wants no duplicate console output.

`WithRawValues` skips `normalize` for a value that already is a `map[string]any`, a
`[]any`, or a scalar. Redaction still walks it. This exists for a caller that hands over a
large decoded payload and does not want a second JSON round trip.

## audit: the evlog extras (gap 3)

`audit.Record` gains five fields.

```go
type Record struct {
    // Actor, Action, Target, Outcome, Reason stay as they are
    Version        int            // schema version of this record, default 1
    IdempotencyKey string         // dedupes a retried write
    Context        map[string]any // free-form facts, such as a request id or a ticket
}
```

New calls:

```go
func Deny(ctx context.Context, r Record)  // Outcome "denied", for a refused action
func Only(ctx context.Context, r Record)  // audit record with no other event fields
func Wrap(ctx context.Context, r Record, fn func() error) error
func Diff(before, after any) map[string]any
func Sign(key []byte) wlog.Drain          // HMAC over the chain hash
func Mock(t testing.TB) (*wlog.Logger, *Recorder)
```

`Wrap` runs `fn`, sets the outcome from its error, and records once. On an error it keeps
the error's code in `audit.error_code`, so a denied action and a failed action stay apart
in a query.

`Diff` compares two values field by field and returns only what changed, as
`{"field": {"from": x, "to": y}}`. It walks a struct through its JSON tags, which keeps it
agnostic. A field the redactor denies is masked by the normal pipeline, since `Diff` only
builds a map and never writes it out itself.

`Sign(key)` wraps the journal drain. It adds `audit.signature`, an HMAC-SHA256 over the
chain hash. `Verify` then takes the key and rejects a record whose signature fails. This raises the
bar from "an edit is detectable" to "an edit needs the key".

`audit.Catalog(reg *catalog.Registry)` reads audit metadata from a catalog entry. When an
entry sets `ReasonRequired`, a `Do` with an empty `Reason` records `audit.reason_missing`
as true rather than dropping the record. Losing an audit record is worse than an
incomplete one.

`Mock` returns a logger and a recorder with assertion helpers for tests:
`RequireAction`, `RequireOutcome`, `RequireActor`, and `RequireNoAudit`.

## drain-memory: named stores and queries (gap 5)

```go
func Named(name string, size int) *Memory // returns the same store for the same name
func Stores() []string
func (m *Memory) Query(f Filter) []map[string]any
func (m *Memory) Clear()

type Filter struct {
    Level    string    // exact match, empty means any
    Since    time.Time // zero means any
    Until    time.Time // zero means any
    Contains map[string]any // every pair must match, by exact value
    Match    func(map[string]any) bool // runs last, after the cheap checks
    Limit    int // 0 means every match
}
```

`Named` keeps a package-level registry of stores behind one mutex. A handler in one
package and a debug endpoint in another then reach the same buffer, with no pointer passed
between them. `Clear` empties a store and keeps its size.

## drain-file: read and tail (gap 4)

```go
func Read(path string, f memory.Filter) ([]map[string]any, error)
func Tail(ctx context.Context, path string, f memory.Filter) (<-chan map[string]any, error)
```

`Read` walks the NDJSON file once and returns every matching line. A malformed line is
skipped, rather than failing the whole read. `Read` counts those skips in a returned
`ParseErrors` value, which a caller can ignore.

`Tail` follows a file the way `tail -f` does. It watches by polling the file size every
200ms. Polling needs no third-party watcher, which keeps this package in the root module.
A changed inode means a rotation, and `Tail` reopens the path. Once `ctx` is done, the
channel closes.

Both reuse `memory.Filter`, so one filter type covers both readers.

## redact: a replacement function (partial)

```go
func ReplaceFunc(fn func(match string) string) Option
```

Today the replacement is a fixed string. `ReplaceFunc` takes the matched value and returns
its mask, so a caller keeps a last-4 tail, a domain, or a stable hash. The function runs inside the redactor. A panic in it falls back to the fixed replacement
string, so a bad mask function can never leak the raw value.

## errors-herr: the catalog bridge (gap 2)

```go
func Catalog(classes ...*herr.Class) []catalog.Entry
```

Converts herr classes into catalog entries, so a herr user declares a code once and gets
both libraries' behavior. This lives in the `errors/herr` module, never in `catalog`, which
keeps the root module free of herr. A user of standard `errors` builds the same registry by
hand and loses nothing.

## Success Criteria

1. `ErrorInfo.Data` and `ErrorInfo.Internal` reach the event, and an old extractor that
   fills only `Attrs` still works unchanged.
2. `Errorf` records the error and returns the same value it recorded.
3. `SetEnabled(false)` makes `Start` return a no-op, proven by a recorder with no events.
4. `WithSilent` writes nothing to stdout and still reaches every drain.
5. `Deny` records outcome "denied". `Wrap` records "success" on a nil error and "error"
   otherwise, with the error's code kept.
6. `Diff` returns only changed fields, in from and to form, and returns an empty map for
   two equal values.
7. A signed journal fails `Verify` under the wrong key, and passes under the right one.
8. An entry with `ReasonRequired` and an empty reason records `audit.reason_missing`, and
   the record still reaches the journal.
9. `Named` returns the same store for the same name, under `-race`.
10. `Query` filters by level, by time, by contained pair, and by a custom function, and
    honors `Limit`.
11. `Read` skips a malformed line and still returns every good one.
12. `Tail` delivers a line appended after it started and survives a rotation. Once `ctx`
    is done, its channel closes.
13. `ReplaceFunc` masks with its own result, and a panicking function falls back to the
    fixed string with no raw value in the output. A fuzz test covers this (gate G1).
14. `Catalog` maps a herr class to an entry with the same code, kind, and status.
15. The root module still imports nothing outside the standard library.

## Testing

Each section tests in its own package's existing black-box test package. The gate G1 fuzz
test gains the `ReplaceFunc` panic path. The audit signing tests use a fixed key, never a
random one, so a failure reproduces.

## Boundaries

- **Always:** keep `Attrs` working, since extractors outside this repo fill it.
- **Ask first:** adding a second piece of package-level state beyond the enabled switch.
- **Never:** let `Diff` write to the event itself. It returns a map, and the caller passes
  that map through the normal redacted path.

## Open Questions

None.
