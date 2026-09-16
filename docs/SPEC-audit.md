# Spec: audit

> Module id `audit` · package `github.com/jeremygprawira/wlog/audit` · root module ·
> depends on: `core`, `pipeline`. Project-wide rules in [SPEC.md](SPEC.md) apply.
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
	Outcome string // "success" | "denied" | "error"
	Reason  string
}
type Actor struct{ Type, ID, Email string }
type Target struct{ Type, ID string }

func Do(ctx context.Context, r Record) // sets the reserved "audit" field on the current event
                                         // (inside a Start), or emits a standalone audit event
                                         // (outside one), via wlog.Start(ctx, "audit."+r.Action)

func Journal(path string) wlog.Drain // append-only NDJSON, fsync'd, mode 0600, resumes the
                                       // hash chain from the file's last line on restart
func Verify(path string) error       // walks a journal file, re-derives each hash, returns the
                                       // first mismatch (line number + reason) or nil
```

### Never sampled

Core already force-keeps any event carrying the reserved `audit` field (SPEC-core.md's stage
order). This module then needs no sampling override of its own. It only needs to set that field.

### Hash chain

Computed at emit time, **after redaction** (so the chain covers exactly the bytes that leave the
process, never raw secrets): `hash = sha256(prev_hash || canonical_json(redacted_audit_event))`,
where `canonical_json` sorts object keys for a stable byte sequence. `prev_hash` and `hash` are
added as `audit.prev_hash`/`audit.hash` on the event before it reaches sinks/drains. Chain state
(the last hash) is kept per-`*wlog.Logger`, guarded by one mutex, so concurrent audit events get
a strict, well-defined order.

### Journal

`Journal(path)` is a `wlog.Drain` (meant to run alongside the main drain via `wlog.WithDrains`,
not instead of it): appends one NDJSON line per event it receives, `O_APPEND|O_CREATE`, fsync
after each write. Audit logs are low-volume, so durability beats throughput here. The file mode
is 0600. On `New` or the first `Send` after process start, it reads the file's last line to
recover `prev_hash`. A restart then continues the same chain instead of starting a new one
silently.

### Verify

`Verify(path)` reads the file line by line. For each line, it recomputes the `hash` from the
stored bytes (minus the `hash` field) plus the previous line's `hash`. It then compares the two.
A byte change, a deleted line, or a reordered line breaks the chain. The first mismatch is
reported with its line number. A deleted line breaks the very next line's `prev_hash` reference.

## Success Criteria

1. `Do(ctx, Record{...})` inside a `Start`'d event sets `audit.actor/action/target/outcome/
   reason` on that event. Outside one, it creates its own standalone event.
2. A sampler configured to keep 0% of events still lets every audit-flagged event through (gate
   G5 part 1). `sample.New(sample.Rate(wlog.LevelInfo, 0))` proves it.
3. 100 concurrent `Do` calls on one Logger produce a chain that `Verify` accepts, under `-race`.
4. Editing one byte in a journal file, deleting a line, or swapping two lines each make `Verify`
   fail and name the line.
5. The Actor's `Email` is masked by the default redactor like any other field, and the stored
   hash covers the *masked* bytes (so `Verify` still passes after redaction, and the journal
   never contains the raw email).
6. Zero imports outside the standard library plus `core` and `pipeline`.

## Testing

Package `audit_test`, black-box. Fixtures for tampered journals under `audit/testdata/`.

## Boundaries

- **Always:** compute the hash after redaction, never before.
- **Ask first:** changing the hash algorithm or canonicalization.
- **Never:** let `Verify` "fix" a broken chain. It only reports.

## Open Questions

None.
