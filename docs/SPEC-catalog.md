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
	Audit   *Audit // nil when the code needs no audit record
}

// Audit is the audit policy for one code. The audit module reads it.
type Audit struct {
	Action         string // such as "invoice.refund"
	TargetType     string // such as "invoice"
	Severity       string // "low", "medium", "high", or "critical"
	ReasonRequired bool   // true means audit.Do rejects an empty Reason
}

type Registry struct{ /* unexported */ }

func New(prefix string, entries ...Entry) *Registry // panics on a duplicate or empty code
func (r *Registry) Prefix() string
func (r *Registry) Codes() []string          // full codes, sorted, prefix included
func (r *Registry) Get(code string) (Entry, bool) // accepts a full or a short code
func (r *Registry) Err(code string, params ...any) error // builds an error for that code

// Extractor decorates next. It fills Kind, Status, Why, Fix, and Link from the entry
// whose full code matches the ErrorInfo.Code that next produced. Fields next already
// filled win, so a per-request detail is never replaced by a static default.
func Extractor(next wlog.ErrorExtractor, registries ...*Registry) wlog.ErrorExtractor
```

### Full codes and the prefix

`New("invoice", Entry{Code: "not_found"})` registers the full code `INVOICE_NOT_FOUND`.
The prefix is upper-cased, the short code is upper-cased, and a `_` joins them.
`Get` accepts either spelling, so a caller reads `Get("not_found")` inside its own domain.

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

## Success Criteria

1. `New` registers entries under one prefix, and `Codes` returns them sorted and prefixed.
2. `New` panics on a duplicate code and on an empty code, since both are author mistakes
   that must fail at startup, not at request time.
3. `Extractor` fills `Kind`, `Status`, `Why`, `Fix`, and `Link` on an `ErrorInfo` whose
   code matches an entry, and leaves every field the wrapped extractor already filled.
4. `Extractor` passes an unmatched code through untouched.
5. The same registry produces the same `ErrorInfo` through the herr extractor and through
   the default extractor, proving the agnostic claim.
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
