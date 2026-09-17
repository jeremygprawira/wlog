# Spec: sample

> Module id `sample` · package `github.com/jeremygprawira/wlog/sample` · root module ·
> depends on: `core`. Project-wide rules in [SPEC.md](SPEC.md) apply.

## Objective

A `wlog.Keeper` for head sampling (drop by random rate per level) and tail sampling (force-keep
on outcome), matching evlog's two-tier model. Default (no sampler configured, or `New()` with no
options): keep 100%.

## Behaviour

```go
func New(opts ...Option) wlog.Keeper

func Rate(**a percentage, fractions allowed**)(level wlog.Level, percent int) Option // head: 0-100, default 100 for every level
func KeepStatus(atLeast int) Option              // tail: keep if event["http"]["status"] >= atLeast
func KeepDuration(atLeast time.Duration) Option  // tail: keep if event["duration_ms"] >= atLeast.Milliseconds()
func KeepPath **(a `**` segment matches any run of segments, including none)**(glob string) Option                // tail: keep if event["http"]["path"] matches glob
func KeepFunc(fn func(ctx context.Context, event map[string]any) bool) Option // tail: custom predicate

// Preset combining the common production shape.
func New(opts ...Option) (wlog.Keeper, error) // refuses a rate outside 0-100 and a bad glob
func MustNew(opts ...Option) wlog.Keeper
func KeepErrorsAndSlow(slow time.Duration, healthyRate float64) Option
```

`Keep` evaluates in this order: (1) any tail condition (`KeepStatus`/`KeepDuration`/`KeepPath **(a `**` segment matches any run of segments, including none)**`/
`KeepFunc`) matching → keep, OR-combined; (2) `level == wlog.LevelError` always force-kept
regardless of tail options (never sampled away, matching SPEC.md); (3) otherwise, head sampling:
a random draw against `Rate(**a percentage, fractions allowed**)(event's level)` (default 100, i.e. always kept). `wlog.Logger`
already force-keeps any event with an `audit` field before calling this `Keeper` at all
(core's stage order), so `sample` never needs to special-case audit itself.

`KeepErrorsAndSlow(slow, healthyRate)` is sugar for `New(KeepDuration(slow), Rate(**a percentage, fractions allowed**)(wlog.LevelInfo,
healthyRate), Rate(**a percentage, fractions allowed**)(wlog.LevelDebug, healthyRate))` — errors are always kept per rule (2) above.

Randomness is injectable for tests (`rand.Source` via an unexported option), so a rate test is
deterministic rather than statistical-and-flaky.

## Success Criteria

1. `New()` (no options) keeps every event.
2. `Rate(**a percentage, fractions allowed**)(wlog.LevelInfo, 0)` drops every info event; `Rate(**a percentage, fractions allowed**)(wlog.LevelInfo, 100)` keeps all;
   `Rate(**a percentage, fractions allowed**)(wlog.LevelError, 0)` still keeps error events (rule 2 overrides head sampling for
   errors).
3. `KeepStatus(500)` force-keeps a 503 event even when its level's head rate is 0.
4. `KeepDuration(time.Second)` force-keeps a 2s-duration event even at head rate 0.
5. `KeepPath **(a `**` segment matches any run of segments, including none)**("/api/payments/**")` force-keeps a matching path even at head rate 0; a
   non-matching path still goes to head sampling.
6. `KeepErrorsAndSlow` keeps all errors and everything over its slow threshold, and applies
   `healthyRate` to the rest.
7. Deterministic under a fixed random source: the same seed and rate always keep/drop the same
   sequence, so the test suite has no flaky sampling tests.
8. Zero imports outside the standard library.

## Testing

Table tests per rule, package `sample_test`, black-box. `path.Match` for `KeepPath **(a `**` segment matches any run of segments, including none)**` (same glob
mechanics as `redact`'s globs, reused for consistency — no second glob implementation).

## Boundaries

- **Always:** evaluate tail conditions before head sampling.
- **Ask first:** changing default rates (100, i.e. no sampling) or preset thresholds.
- **Never:** let this package see or need to know about `audit` — that bypass lives in core.

## Open Questions

None.
