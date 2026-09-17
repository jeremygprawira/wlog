# redact benchmark

Measured 2026-09-15, Apple M4 Pro, `go test -run=xxx -bench=BenchmarkRedact_Apply -benchmem ./redact`.

Event: 50 fields, 3 levels deep, 10 string values (`buildBenchEvent` in `bench_test.go`).
The benchmark times the event build plus `Apply` together, since that build cost is
part of any real call site.

```
BenchmarkRedact_Apply-12    68451    15676 ns/op    3678 B/op    23 allocs/op
```

This meets SPEC-redact.md's 30µs/op target with headroom. Allocations (23) are close
to, but above, the 10-alloc target. Most of that is the 47-entry map build itself
(interface boxing for each `int`/`string` value), not `Apply`.

## History

The first measurement (before optimization) was 55.7µs/op, 321 allocs/op. Two fixes,
found from `go tool pprof`, closed the gap:

1. **Tokenizing every key on every call.** `matchesKey` tokenized each field name fresh
   on every `Apply`, though a real service logs the same field names over and over.
   72% of allocations. Fix: `Redactor.cachedTokenize` memoizes per key in a `sync.Map`.
2. **Regex backtracking on strings that plainly cannot match.** After allocations
   dropped, profiling showed 76% of CPU inside `regexp`'s backtracking engine, from
   running all 8 value patterns over every string. The fix gives each built-in pattern a
   cheap `prefilter`, for example email needs "@" and ipv4 needs three ".". A plain byte scan
   checks it before the regex runs at all.

Neither change alters matching behavior. A prefilter skips only a pattern the regex
   cannot match anyway, and the tokenize cache memoizes a pure function. The tests in
   `redact/patterns_test.go` and `patterns_b_test.go` still pass unchanged.
