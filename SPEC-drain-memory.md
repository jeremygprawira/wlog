# Spec: drain-memory

> Module id `drain-memory` · package `github.com/jeremygprawira/wlog/drain/memory` · root module ·
> depends on: `core`. Project-wide rules in [SPEC.md](SPEC.md) apply.

## Objective

An in-process `wlog.Drain` that keeps recent events in a ring buffer and lets live code
subscribe to new ones — the base for `wlogtest` and for a dev "tail my logs" view. Local-process
only, no network.

## Behaviour

```go
func New(size int) *Memory // size <= 0 defaults to 1000; implements wlog.Drain

func (m *Memory) Snapshot() []map[string]any // a copy, oldest first
func (m *Memory) Subscribe(ctx context.Context) <-chan map[string]any
func (m *Memory) SSEHandler() http.Handler // text/event-stream; ?replay=N query param
```

`Send` appends to the ring buffer (overwriting the oldest entry once full) and fans out a copy
to every active subscriber channel, non-blocking: a subscriber channel is buffered (`cap 100`);
if it's full, the newest event is dropped for that subscriber only (counted, never blocks
`Send`, gate G3/G4). `Subscribe`'s channel closes when `ctx` is done, and the subscription is
removed.

`SSEHandler` writes each subscribed event as one `data: <json>\n\n` frame; on connect it replays
the last `replay` (default 0) events from `Snapshot()` before switching to live. It closes
cleanly when the client disconnects (`r.Context().Done()`), leaking no goroutines.

## Success Criteria

1. `Snapshot()` after N sends (N < size) returns all N, oldest first; after size+K sends,
   returns the most recent `size`, oldest first.
2. Two concurrent `Subscribe` calls both receive every subsequent `Send`.
3. A subscriber that stops reading does not block `Send` for anyone else; its channel drops
   newest-first once full and the drop is counted.
4. `SSEHandler` with `?replay=5` sends the 5 most recent events then live ones, and a goroutine
   count check shows no leak after the client disconnects.
5. Zero imports outside the standard library; passes `-race` with concurrent `Send`/`Subscribe`/
   `Snapshot`.

## Testing

`httptest` for `SSEHandler`; table/concurrency tests for the ring buffer and fan-out. Package
`memory_test`, black-box.

## Boundaries

- **Always:** copy events before handing them to a caller (`Snapshot`, channel sends) — the
  buffer's internal slice is never aliased out.
- **Ask first:** changing the default size (1000) or subscriber channel capacity (100).
- **Never:** block `Send` on a slow or absent subscriber.

## Open Questions

None.
