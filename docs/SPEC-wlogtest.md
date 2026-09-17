# Spec: wlogtest

> Module id `wlogtest` · package `github.com/jeremygprawira/wlog/wlogtest` · root module ·
> depends on: `drain-memory`, `core`. Project-wide rules in [SPEC.md](SPEC.md) apply.

## Objective

Let a user assert on what their own code logged, in their own tests, without parsing JSON off
stdout.

## Behaviour

<!-- snippet:sketch -->
```go
func New(t testing.TB, opts ...wlog.Option) (*wlog.Logger, *Recorder)

type Recorder struct{ /* wraps drain/memory.Memory */ }

func (r *Recorder) Events() []map[string]any
func (r *Recorder) Last() map[string]any // nil if none
func (r *Recorder) Count() int

func (r *Recorder) RequireField(t testing.TB, key string, want any)
func (r *Recorder) RequireErrorCode(t testing.TB, code string)
func (r *Recorder) RequireCount(t testing.TB, n int)
```

`New` builds a real `*wlog.Logger` with `WithFormat(wlog.FormatJSON)`, which silences the pretty
console during tests. It adds a `drain/memory.Memory`-backed drain, and it merges in any
user-supplied `opts`, so a custom redactor or sampler can still be tested. It returns both. A
`Require*` failure calls `t.Helper()` and prints the closest event (via `Last()`) alongside the
mismatch. A failing assertion therefore shows what was logged, and not just "not equal".

## Success Criteria

1. A handler calling `wlog.Set(ctx, "order_id", "4821")` is visible via
   `rec.RequireField(t, "order_id", "4821")` without the test touching stdout.
2. `RequireErrorCode` fails with a readable message (showing `Last()`) for a code that does
   not match, and for a run that logged no error.
3. User options passed to `New` (for example a custom redactor) take effect, proven by a test that
   configures `redact.Disabled()` and confirms an unmasked value shows up in `Events()`.
4. Zero imports outside the standard library plus `drain/memory` and `core`, both in-module.

## Testing

Package `wlogtest_test`, using `wlogtest` on itself (dogfooding) is acceptable and encouraged.

## Boundaries

- **Always:** call `t.Helper()` in every `Require*`.
- **Ask first:** changing the default format/redactor `New` applies.
- **Never:** write to real stdout during a `wlogtest`-backed test.

## Open Questions

None.
