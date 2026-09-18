# Spec: sample

> Module id `sample` · package `github.com/jeremygprawira/wlog/sample` · root module ·
> depends on: `core`. Project-wide rules in [SPEC.md](SPEC.md) apply.

## Objective

A `wlog.HeadSampler` for head sampling (drop by random rate per level) and a `wlog.Keeper`
for tail sampling (force-keep on a rule), matching evlog's two-tier model. `New` returns one
value that implements both interfaces. Default (no rate configured, or `New()` with no
options): keep 100%.

## Behaviour

<!-- snippet:sketch -->
```go
type Sampler interface {
	wlog.HeadSampler
	wlog.Keeper
}

func New(opts ...Option) (Sampler, error) // refuses a rate outside 0-100 and a bad glob
func MustNew(opts ...Option) Sampler

func Rate(level wlog.Level, percent float64) Option // head: 0-100, default 100 for every level
func KeepStatus(atLeast int) Option                 // tail: keep if http.status >= atLeast
func KeepDuration(atLeast time.Duration) Option     // tail: keep if duration_ms >= atLeast
func KeepPath(glob string) Option                   // tail: keep if http.path matches glob
func KeepFunc(fn func(ctx context.Context, event wlog.Event) bool) Option // tail: custom rule
func KeepErrorsAndSlow(slow time.Duration, healthyRate float64) Option
```

`Sample` makes the head decision for one level. It reads the level and the trace id alone,
and it returns the rate that decided, so core can record `wlog.sample_rate` on the event.
Rule 1 draws against `Rate(level, percent)` for the event's level. The default rate is 100,
which keeps every event. Rule 2 hashes the trace id. Every service and every retry of one
trace then agrees, because the same id lands in the same bucket. When the event carries
no trace id, rule 3 draws at random.

`Keep` makes the tail decision on the enriched event, read through the read-only view.
Rule 1 keeps an event that matches a tail condition. The conditions are `KeepStatus`,
`KeepDuration`, `KeepPath`, and `KeepFunc`, and they combine with OR. Rule 2 force-keeps
`level == wlog.LevelError`, whatever the tail options say. An error is never sampled away,
which matches SPEC.md. `Keep` never drops an event, because the head decision owns that
answer. `wlog.Logger` already force-keeps any event with an `audit` field, before it calls
either method at all (core's stage order). `sample` therefore never special-cases audit
itself.

`KeepErrorsAndSlow(slow, healthyRate)` is sugar for `New(KeepDuration(slow), KeepStatus(500),
KeepFunc(warn), Rate(wlog.LevelInfo, healthyRate), Rate(wlog.LevelDebug, 0))`. Errors are
always kept per rule (2) above.

## Success Criteria

1. `New()` (no options) reports keep at rate 100 for every level from `Sample`.
2. `Rate(wlog.LevelInfo, 0)` drops every info event, and `Rate(wlog.LevelInfo, 100)` keeps
   all of them. `Sample` still reports keep at rate 100 for an error level, so `Keep` keeps
   the error event.
3. `KeepStatus(500)` force-keeps a 503 event, whatever the level's head rate is.
4. `KeepDuration(time.Second)` force-keeps a 2s-duration event at head rate 0.
5. `KeepPath("/api/payments/**")` force-keeps a matching path even at head rate 0. A
   non-matching path is not kept by `Keep`.
6. `KeepErrorsAndSlow` keeps all errors and everything over its slow threshold, and applies
   `healthyRate` to the rest.
7. A fixed trace id always gets the same answer from `Sample`, so the test suite has no
   flaky sampling tests.
8. Zero imports outside the standard library.

## Testing

Table tests per rule, package `sample_test`, black-box. `path.Match` for `KeepPath` (same glob
mechanics as `redact`'s globs, reused for consistency, no second glob implementation).

## Boundaries

- **Always:** leave the drop decision to `Sample`, so a tail rule can force an event back.
- **Ask first:** changing default rates (100, that is no sampling) or preset thresholds.
- **Never:** let this package see or need to know about `audit`, that bypass lives in core.

## Open Questions

None.
