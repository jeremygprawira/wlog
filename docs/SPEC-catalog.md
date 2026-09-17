# Spec: catalog

> Module id `catalog`. Package `github.com/jeremygprawira/wlog/catalog`.
> Root module, standard library only. Depends on `core`.
> Project-wide rules in [SPEC.md](SPEC.md) apply. Closes gap 2 in [evlog parity](evlog-parity.md).

## Objective

One registry per domain that holds the static facts about an error code: its status, its
message template, its repair guidance, and the audit rules that apply to it. Declare each
code once. Every event that carries that code then gets the same fields, with no repeated
literals across handlers.

The registry knows about no error library. It decorates whatever `wlog.ErrorExtractor` is
already in use, so a herr user, a standard `errors` user, and a custom-extractor user all
get the same result.

## Behaviour

```go
// Entry is one code's static facts. A registry copies it, so a caller cannot change an
// entry after New returns.
type Entry struct {
	Code    string // short code inside the domain, such as "not_found"
	Kind    string // maps to wlog.ErrorInfo.Kind
	Status  int    // HTTP status, 0 means unset
	Message string // template, with {name} placeholders filled from params
	Why     string
	Fix     string
	Link    string
	Data     map[string]any // defaults safe to show a client, merged under the call-site values
	Internal map[string]any // log-only defaults, merged the same way
	Audit   *Audit // nil when the code needs no audit record
}

// Audit is the audit policy for one code. The audit module reads it.
type Audit struct {
	Action          string   // such as "invoice.refund"
	TargetType      string   // such as "invoice"
	Severity        string   // "low", "medium", "high", or "critical"
	Description     string   // one sentence for a reader, never part of an event
	ReasonRequired  bool     // true means a record with no reason breaks a rule
	RequiresChanges bool     // true means a record must carry the changes it made
	RedactPaths     []string // paths inside the changes whose values must be masked
}

type Registry struct{ /* unexported */ }

func New(prefix string, entries ...Entry) *Registry // panics on a duplicate or empty code
func (r *Registry) Prefix() string
func (r *Registry) Codes() []string          // full codes, sorted, prefix included
func (r *Registry) Get(code string) (Entry, bool) // accepts a full or a short code
func (r *Registry) Err(code string, params ...any) error // builds an error for that code

// Extractor decorates next. It fills Kind, Status, Why, Fix, and Link from the entry
// whose full code matches the ErrorInfo.Code that next produced. Fields next already
// filled win, so a per-request detail is never replaced by a static default. It returns
// an error when two registries define the same full code.
func Extractor(next wlog.ErrorExtractor, registries ...*Registry) (wlog.ErrorExtractor, error)
func MustExtractor(next wlog.ErrorExtractor, registries ...*Registry) wlog.ErrorExtractor
func (r *Registry) AllowShortCodes() *Registry // also resolve a short code in the extractor
```

### Full codes and the prefix

`New("invoice", Entry{Code: "not_found"})` registers the full code `INVOICE_NOT_FOUND`.
The prefix is upper-cased, the short code is upper-cased, and a `_` joins them.
`Get` accepts either spelling, so a caller reads `Get("not_found")` inside its own domain.

A code that already starts with the registry's own prefix keeps it, so the full code is
never doubled: `New("billing", Entry{Code: "BILLING_NOT_FOUND"})` registers
`BILLING_NOT_FOUND` with the short code `NOT_FOUND`. An error library that hands over a
domain-qualified code, such as a herr class code, therefore matches the same entry its short
spelling does.

### Matching, duplicates, and short codes

`Extractor` matches full codes. A registry resolves a short code only after
`AllowShortCodes`, because a short spelling is easy to hit by accident: the default
extractor's own `INTERNAL` would otherwise pick up an entry named `internal` and attach that
entry's status and guidance to every plain error. A nil registry is skipped.

`Extractor` refuses two registries that define the same full code, because the answer would
otherwise depend on the order of the arguments, and `MustExtractor` panics instead. Two
registries with different prefixes never collide.

`errors.Is(err, entry)` is true only inside the registry the error came from: the entry
carries its domain, so an entry with the same short code in another domain, and a hand-built
`Entry` that belongs to no registry, never match.

### The agnostic path

`Extractor` never looks at an error's Go type. It reads the `Code` that the wrapped
extractor produced, and then looks that code up. This is what keeps the module agnostic:

- A herr user wraps `wlogherr.Extractor()`. herr supplies the code.
- A standard `errors` user wraps the default extractor and uses `Registry.Err`.
- A custom-extractor user wraps their own.

`Registry.Err` returns an error that carries its code, its rendered message, and a pointer
back to its entry. It works with `errors.Is` and `errors.As`, and it wraps a cause through
a `%w` param.

### Message templates

`Message` holds `{name}` placeholders. `Err("not_found", "id", 42)` renders
`invoice {id} was not found` into `invoice 42 was not found`. A placeholder with no param
stays as written, so a missing value never panics and never renders an empty gap.

Rendering is one pass: a parameter value that itself holds a placeholder is written as it
stands and never filled again, so one parameter cannot rewrite another's text.

The extractor never copies `Message` into the `ErrorInfo`. The entry holds a template, not a
message, and the wrapped extractor's own message describes the error far better.

### Domain, and entry defaults

The extractor records the registry's domain in `error.attrs.domain`, as the lower-case
prefix: the code `BILLING_DECLINED` reports `billing`. A value the wrapped extractor already
put in `attrs` wins.

`Entry.Data` and `Entry.Internal` hold defaults that merge under the values a per-request
extractor filled, at the top level: a static default never replaces a real detail. Both maps
are copies, so an event never shares a map with the registry.

### The audit policy

`audit.Catalog(reg)` reads the policy of the entry whose `Audit.Action` matches an audit
record's action. It fills `target.type` when the record has none, lists every rule the record
breaks in `record.violations`, keeps `record.reason_missing` for the older single-rule field,
and masks the value of every change operation that `RedactPaths` names.

The rule names are `reason_required` and `changes_required`. A record that breaks a rule is
never dropped, because an incomplete fact is worth more than a lost one, and a reader that
sees the violation knows to ask for the rest. A `RedactPaths` entry matches a change
operation's JSON Pointer exactly or as a prefix, so `user.creds` covers `user.creds.nik`.

## Success Criteria

1. `New` registers entries under one prefix, and `Codes` returns them sorted and prefixed.
2. `New` panics on a duplicate code and on an empty code, since both are author mistakes
   that must fail at startup, not at request time.
3. `Extractor` fills `Kind`, `Status`, `Why`, `Fix`, and `Link` on an `ErrorInfo` whose
   code matches an entry, and leaves every field the wrapped extractor already filled. It
   never fills `Message`.
4. `Extractor` passes an unmatched code through untouched.
5. The same registry produces the same `ErrorInfo` through the herr extractor and through
   the default extractor, proving the agnostic claim. The herr class code is
   domain-qualified, so both paths hand over the same full code.
10. `Extractor` matches a full code only, `AllowShortCodes` opts one registry in, a nil
    registry is skipped, and two registries that define one code are refused.
11. `errors.Is` matches an entry from the error's own registry only.
12. `Get` and `CodedError.Entry` return deep copies, so a caller cannot change a registry
    through what it received.
13. `Extractor` writes `error.attrs.domain`, and merges `Data` and `Internal` defaults under
    the call-site values.
14. `Audit` holds `Description`, `RequiresChanges`, and `RedactPaths`. A record that breaks a
    rule is kept with `violations` naming each rule, and `RedactPaths` masks the value of a
    matching change operation.
6. `Registry.Err` supports `errors.Is` against its own entry and `errors.As` to reach the
   coded error, and it unwraps to a cause passed as `%w`.
7. A template renders its params, and a missing param leaves its placeholder in place.
8. `Get` resolves a short code and a full code to the same entry.
9. Zero imports outside the standard library plus `core`.

## Testing

Package `catalog_test`, black-box. One test builds two registries with different prefixes
and proves the codes never collide. The agnostic test runs one table through two different
wrapped extractors and compares the two `ErrorInfo` values field by field.

## Boundaries

- **Always:** copy an `Entry` into the registry, so a caller's later change cannot reach it.
- **Ask first:** adding a second lookup key beyond the code, such as a kind or a status.
- **Never:** import an error library. `Extractor` reads codes, never Go types.

## Open Questions

None.
