# Spec: pipeline

> Module id `pipeline` · package `github.com/jeremygprawira/wlog/pipeline` · root module ·
> depends on: `core`. Project-wide rules in [SPEC.md](SPEC.md) apply.

## Objective

Wrap a batch-sending backend with batching, retry, and a bounded buffer, so a real drain
(Axiom, Loki, a file) never slows down or blocks the request that logged through it (gate G3),
and never grows memory without bound (gate G4). `wlog.Drain.Send` has no error return, so retry
needs a slightly richer interface than core's — `pipeline` defines it and hands back a plain
`wlog.Drain` that any `wlog.Logger` can use.

## Behaviour

```go
// Sender is what pipeline.Wrap needs from a real backend: send a batch, report failure.
// A phase-4 drain (Axiom, Loki, ...) implements this, not wlog.Drain directly.
type Sender interface {
	SendBatch(ctx context.Context, events []map[string]any) error
}

func Wrap(next Sender, opts ...Option) wlog.Drain // also implements Close(ctx) error

type Option func(*config)

func BatchSize(n int) Option              // default 50
func BatchInterval(d time.Duration) Option // default 5s
func MaxAttempts(n int) Option            // default 3, includes the first try
func Backoff(kind BackoffKind) Option     // default Exponential
func InitialDelay(d time.Duration) Option // default 1s
func MaxDelay(d time.Duration) Option     // default 30s
func MaxBuffer(n int) Option              // default 1000
func OnDropped(fn func(events []map[string]any, err error)) Option

type BackoffKind int
const (
	Exponential BackoffKind = iota
	Linear
	Fixed
)

func FanOut(drains ...wlog.Drain) wlog.Drain // one Send call reaches every drain
```

`Wrap`'s returned `Drain.Send` never blocks the caller: it pushes the event onto an
internal buffered channel and returns immediately. A background goroutine (started on
first `Send`, stopped by `Close`) reads that channel and manages batching.

### Batching

A batch flushes when it reaches `BatchSize` events or `BatchInterval` has passed since the
first event currently in it, whichever comes first. A full or timed-out batch is sent as one
`next.SendBatch(ctx, batch)` call.

### Retry

A `SendBatch` error is retried up to `MaxAttempts` total tries, waiting between tries per
`Backoff`/`InitialDelay`/`MaxDelay` (`Exponential`: 1s, 2s, 4s, ... capped at `MaxDelay`;
`Linear`: 1s, 2s, 3s, ...; `Fixed`: `InitialDelay` every time), with jitter of up to 20% to
avoid a thundering herd. Attempts exhausted: the batch is dropped and `OnDropped(batch, err)`
is called if set.

### Buffer and overflow

`MaxBuffer` events are queued at once (across the channel and any batch awaiting retry). A
`Send` that would exceed it drops the **oldest** buffered event, increments a dropped counter,
and calls `OnDropped([]map[string]any{dropped}, nil)` if set. Never blocks, never panics (G3,
G4).

### Close

The returned `Drain`'s `Close(ctx context.Context) error` flushes every buffered event (one
final `SendBatch`, retried per the usual policy but bounded by `ctx`'s deadline) and stops the
background goroutine.

### FanOut

`FanOut(drains...)` returns a `Drain` whose `Send` dispatches to every drain concurrently
(one goroutine per drain) and returns immediately, without waiting for any of them — matching
gate G3, since `FanOut` is itself just a `wlog.Drain` and core calls `Send` synchronously from
the event pipeline. Each drain is already panic-isolated by `wlog.Logger`'s own dispatch;
`FanOut` adds no isolation of its own. A hanging or slow drain never delays delivery to, or
blocks, the others, or the caller.

## Success Criteria

1. 50 events sent within 5s produce exactly one `SendBatch` call with all 50, before the
   interval elapses.
2. A partial batch (< `BatchSize`) flushes via `SendBatch` after `BatchInterval`.
3. A `SendBatch` that errors twice then succeeds is retried with the configured backoff and
   delivers all events; a `SendBatch` that always errors exhausts `MaxAttempts` and calls
   `OnDropped` once with the whole batch.
4. `Close(ctx)` flushes every pending event through one last `SendBatch` and returns only after
   it settles or `ctx`'s deadline passes.
5. `MaxBuffer` exceeded drops the oldest event, calls `OnDropped`, never blocks the caller —
   proven with a `Sender` whose `SendBatch` never returns, under a tight test timeout.
6. `FanOut` delivers to every drain; one drain hanging does not prevent the others from
   receiving the event.
7. Zero imports outside the standard library; passes `-race` with concurrent `Send` and `Close`.

## Testing

Table/unit tests with a fake `Sender` and an injectable clock (no real `time.Sleep` beyond a
few ms in any test). Package `pipeline_test`, black-box.

## Boundaries

- **Always:** never call `SendBatch` synchronously from the caller's goroutine.
- **Ask first:** changing default batch size/interval/buffer size/retry policy.
- **Never:** let a hung `SendBatch` block `Send` or `Close` past its context deadline.

## Open Questions

None.
