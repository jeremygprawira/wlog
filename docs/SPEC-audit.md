# Spec: audit

> Module id `audit` · package `github.com/jeremygprawira/wlog/audit` · root module ·
> depends on: `core`, `catalog`, and `pipeline`. The test mock also imports `testing`.
> Project-wide rules in [SPEC.md](SPEC.md) apply.
> v1.2 additions to this module: [SPEC-v1.2-additions.md](SPEC-v1.2-additions.md).

## Objective

Tamper-evident audit logging for "who did what, to what, with what outcome". It reuses evlog's
audit layer idea and the normal event pipeline rather than a separate logging path. Audit
events are never sampled away. A hash chain makes an edited or deleted line detectable.

## Behaviour

```go
type Record struct {
	Actor   Actor
	Action  string // e.g. "invoice.refund"
	Target  Target
	Outcome string // "success" | "denied" | "failure"
	Reason  string
}
type Actor struct{ Type, ID, Email string }
type Target struct{ Type, ID string }

func Do(ctx context.Context, r Record) // adds r to the reserved "audit" array on the current
                                         // event (inside a Start), or emits a standalone audit
                                         // event (outside one, or after its end ran), via
                                         // wlog.Start(ctx, "audit."+r.Action)

func Journal(path string, opts ...Option) wlog.Drain // append-only NDJSON, fsync'd, mode 0600,
                                       // resumes the hash chain from the file's last line
func WithKey(key []byte) Option      // signs every line, so Verify(path, key) checks the HMAC
func Verify(path string, key ...[]byte) error // walks a journal file, re-derives each hash from
                                       // the bytes on disk, returns the first mismatch or nil
func VerifyHead(path, head string, key ...[]byte) error // as Verify, and the last line must end
                                       // the chain at head
func VerifySigned(path string, key []byte) error // as Verify(path, key): every line must be
                                       // signed
```

### Who acted, and which request it was

Every record names one actor type: `user`, `service`, `system`, or `agent`. An agent actor
also fills `model`, `tools`, and `prompt_id`, so a review can tell which model, which tools,
and which prompt were behind an action. `Actor.Valid` reports whether a type is one of the
four, and `Do` never rejects a record over it, because losing an audit fact is worse than an
odd type.

An outcome is `success`, `denied`, or `failure`. `audit.Success`, `audit.Denied`, and
`audit.Failure` name them.

`Do` fills three fields the caller may leave empty: `correlation_id` from
`trace.request_id`, `causation_id` from `trace.parent_event_id`, and `idempotency_key`, the
first 32 hex characters of a SHA-256 over the action, the actor id, the target type, the
target id, and `trace.request_id`. A retried request with the same request id therefore
records the same key, and a different request does not. A caller that passes its own value
keeps it.

A record may carry `changes`, the RFC 6902 operations `audit.Patch` builds, so a reader sees
what changed and not only its summary.

### Patch and routing

`Patch(before, after, opts...)` returns the RFC 6902 operations that turn one object into
another, in a stable order: add for a key only `after` holds, remove for a key only `before`
holds, and replace for a changed one, with each path escaped the JSON Pointer way. It returns
an error when either side is not an object, exactly as `Diff` does. `WithRedactor` sets the
redactor it consults, and the default is `redact.Default()`: a path the denylist denies
carries the replacement text instead of the value, so a patch can describe a change to a
secret without carrying the secret.

`OnlyDrain(next)` forwards only the audit fact of an event. The forwarded event holds
`timestamp`, `event_id`, `service`, `trace`, and `audit`, and nothing else, and an event with
no audit record forwards nothing at all. Use it in front of a backend that must not receive
the rest of the request.

### Several records per event

`audit` is an array, so a second `Do`, `Deny`, or `Wrap` adds a record instead of replacing
the first. One event holds at most 20 records, per the caps in [SPEC.md](SPEC.md); a record past
the cap counts as a dropped field rather than disappearing quietly. The `audit` array always has
room on its event, even when the event already holds the full set of top-level keys, so the key
cap can never drop an audit record (gate G5).

`Do` writes to the open event when `ctx` carries one that is still accepting writes. After the
event's `end` ran, and outside a `Start` altogether, `Do` opens and closes its own event, so a
late call records the fact instead of writing into a sealed event where it would be lost.

### Never sampled

Core already force-keeps any event carrying the reserved `audit` field (SPEC-core.md's stage
order). This module then needs no sampling override of its own. It only needs to set that field.

### Hash chain

Computed at write time, **after redaction** (so the chain covers exactly the bytes that leave the
process, never raw secrets). `hash = sha256(line_bytes)`, where `line_bytes` is the exact JSON
line that `Journal` writes, with the value of `audit.hash` and, when a key is set,
`audit.signature` replaced by zeros. The hash therefore covers every other byte on the line, so
JSON whitespace, a reordered key, a duplicate key, and a number written in another form all fail
`Verify`. Byte edits such as `1` to `1.0` or `9007199254740993` to `9007199254740992` change the
covered bytes, and a CRLF ending does too.

### Marker lines

Every 100 records, and once more on `Close`, `Journal` writes a marker line:
`{"audit.marker":{"count":N,"head":"H"},"audit.hash":"...","audit.prev_hash":"..."}`. `count` is
the number of records the chain covered, and `head` is the hash of the last line before the
marker. The marker is a chain link like any record, so its own hash covers the count and head it
states. With a key the marker also carries `key_id`, the first 16 hex characters of
`sha256(key)`, so a reader learns which key to ask for.

`Verify` checks each marker against the lines before it, so a record deleted from the middle
fails on the count and the head as well as on the chain. A cut at the very end removes the last
marker too, so the only way to catch it is `VerifyHead` with a head kept outside the file. A
journal that holds no record at all fails `Verify`.

`Journal` owns the chain state (the last hash) and the write lock, so the lines land in chain
order whatever the arrival order. `prev_hash` and `hash` are set as `audit.prev_hash` and
`audit.hash` on the event, so a drain listed after `Journal` sees them. There is no separate
chain drain. A keyed `Journal` adds `audit.signature`, an HMAC-SHA256 over the chain hash.

### Journal

`Journal(path)` is a `wlog.Drain` (meant to run alongside the main drain via `wlog.WithDrains`,
not instead of it): it hashes the line it is about to write, appends it, and fsyncs it, all under
one lock. One NDJSON line per event it receives, opened `O_APPEND|O_CREATE`, mode 0600. Audit
logs are low-volume, so durability beats throughput here.

One journal file has one writer. On Unix, `Journal` takes an exclusive `flock` on the file, and
a second writer is refused with a reported error. The kernel drops that lock when the process
ends, so a crash leaves no stale lock. On Windows the lock is a sibling `<path>.lock` file,
because the standard library exposes no file lock there. A crash can leave that file behind, and
the error names it.

On the first `Send` after start, `Journal` reads the file, counts its records, and recovers the
chain head, so a restart continues the same chain. A partial last line, meaning an unterminated
line that is not a valid chain link, moves to `<path>.partial` and is cut from the chain. An
unterminated line that is complete and valid keeps its record, and `Journal` writes the missing
newline. A terminated line that does not match the chain is an error, and `Journal` refuses to
open rather than continue a tampered file.

`Close` writes the closing marker, syncs the file, and syncs the directory that holds it.

### Verify

`Verify(path, key...)` reads the file line by line. For each line it recomputes the hash from the
bytes on disk, with the chain and signature values zeroed as the writer did, and compares it with
the stored `audit.hash`. The line's `audit.prev_hash` must also equal the previous line's hash,
which catches a deleted or a reordered line. The first mismatch is reported with its line number.
An empty line, a line without a valid `audit.hash`, and a malformed chain field are errors.
`Verify` only reports; it never repairs a chain.

## Success Criteria

1. `Do(ctx, Record{...})` inside a `Start`'d event adds a record holding
   `actor/action/target/outcome/reason` to that event's `audit` array, up to 20 records.
   Outside one, or after the event ended, it creates its own standalone event.
2. A sampler configured to keep 0% of events still lets every audit-flagged event through (gate
   G5 part 1). `sample.New(sample.Rate(wlog.LevelInfo, 0))` proves it.
3. 100 concurrent `Do` calls on one Logger produce a chain that `Verify` accepts, under `-race`.
4. Editing one byte in a journal file, deleting a line, or swapping two lines each make `Verify`
   fail and name the line. A cut at the end fails `VerifyHead`. An empty file fails `Verify`.
5. The Actor's `Email` is masked by the default redactor like any other field, and the stored
   hash covers the *masked* bytes (so `Verify` still passes after redaction, and the journal
   never contains the raw email).
6. Zero imports outside the standard library plus `core`, `catalog`, and `pipeline`. The test
   mock also imports `testing`.

## Testing

Package `audit_test`, black-box. Each tampering test builds its journal in a temp directory,
changes the bytes it needs, and asserts on `Verify`'s error, so no fixture file can drift from
the writer's format.

## Boundaries

- **Always:** compute the hash after redaction, never before.
- **Ask first:** changing the hash algorithm or canonicalization.
- **Never:** let `Verify` "fix" a broken chain. It only reports.

## Open Questions

None.
