# redact benchmark

Measured 2026-09-15, Apple M4 Pro, `go test -run=xxx -bench=BenchmarkRedact_Apply -benchmem ./redact`.

Event: 50 fields, 3 levels deep, 10 string values (`buildBenchEvent` in `bench_test.go`).

```
BenchmarkRedact_Apply-12    21919    55718 ns/op    4738 B/op    321 allocs/op
```

SPEC-redact.md's target is ≤ 30µs/op and ≤ 10 allocs/op. The measured result is over
both: 55.7µs/op and 321 allocs/op.

Likely cause: every field allocates a new `fullPath` slice (two appends) even though
`Default()` has no dotted (path-anchored) denylist entries, so that path is never used
for matching. Each of the 10 string fields also runs all 8 value patterns in sequence.

This gap is flagged to the user, not silently accepted or silently fixed. A fast path
that skips `fullPath` allocation when `r.paths` is empty is the likely first fix; it
needs its own failing test before it lands, per this project's TDD rule.
