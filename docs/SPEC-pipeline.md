# Spec: pipeline

> Module id `pipeline` · package `github.com/jeremygprawira/wlog/pipeline` · root module ·
> depends on: `core`. Project-wide rules in [SPEC.md](SPEC.md) apply.

## Objective

Wrap a batch-sending backend with batching, retry, and a bounded buffer. A real drain
(Axiom, Loki, a file) then never slows down or blocks the request that logged through it
(gate G3). It also never grows memory without bound (gate G4). `wlog.Drain.Send` has no error
return, and retry needs a slightly richer interface than core's, so `pipeline` defines that
interface. It hands back a plain `wlog.Drain` that any `wlog.Logger` uses.

## Behaviour

<!-- snippet:sketch -->
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

A batch flushes on `BatchSize` events. It also flushes after `BatchInterval`, counted from the
first event in it. A full or timed-out batch is sent as one
`next.SendBatch(ctx, batch)` call.

### Retry

A `SendBatch` error is retried up to `MaxAttempts` total tries. The wait between tries follows
`Backoff`, `InitialDelay`, and `MaxDelay`. `Exponential` waits 1s, then 2s, then 4s, capped at
`MaxDelay`. `Linear` waits 1s, then 2s, then 3s. `Fixed` waits `InitialDelay` every time. Jitter
of up to 20% avoids a thundering herd. When the attempts run out, the batch is dropped.
A caller that set `OnDropped` then receives the batch and the error.

### Buffer and overflow

`MaxBuffer` events are queued at once, across the channel and any batch awaiting retry. A `Send`
past that limit drops the **oldest** buffered event. It increments a dropped counter, and it
calls `OnDropped([]map[string]any{dropped}, nil)` for a caller that set one. A `Send` never blocks and
never panics (G3, G4).

### Close

The returned `Drain`'s `Close(ctx context.Context) error` flushes every buffered event as one
final `SendBatch`. The retry policy applies, and `ctx`'s deadline bounds it. `Close` then stops
the background goroutine.

### FanOut

`FanOut(drains...)` returns a `Drain` whose `Send` queues the event for every drain. It returns
immediately, without waiting for any of them. That matches gate G3, because `FanOut` is itself
just a `wlog.Drain`. Core calls `Send` synchronously from the event pipeline.

Each drain gets one bounded queue of 256 events. One goroutine reads that queue. A full queue
drops the newest event for that drain, instead of blocking the caller. Memory and goroutines
therefore stay bounded, however many events arrive (G4). Every delivery runs under recover, so a panicking
drain never climbs out of the FanOut goroutine (G3), and the drain keeps receiving later events.
A hanging or slow drain never delays delivery to the others, or the caller.

The result also implements `Flush(ctx) error` and `Close(ctx) error`. Both first wait until the
queues hold no undelivered event. They then forward the call to every drain that implements it. A
`Logger.Flush` or `Logger.Close` therefore reaches a `pipeline.Wrap` inside the FanOut. `Close` then stops
the goroutines. Both waits are bounded by `ctx`.

Drains receive the event map read-only, and they run at the same time. A drain that must change
the event copies it first, per the core drain contract (CORE-11).

## Success Criteria

1. 50 events sent within 5s produce exactly one `SendBatch` call with all 50, before the
   interval elapses.
2. A partial batch (< `BatchSize`) flushes via `SendBatch` after `BatchInterval`.
3. A `SendBatch` that errors twice then succeeds is retried with the configured backoff and
   delivers all events. A `SendBatch` that always errors exhausts `MaxAttempts` and calls
   `OnDropped` once with the whole batch.
4. `Close(ctx)` flushes every pending event through one last `SendBatch` and returns only after
   it settles or `ctx`'s deadline passes.
5. A `MaxBuffer` that overflows drops the oldest event, calls `OnDropped`, and never blocks the
   caller. A `Sender` whose `SendBatch` never returns proves that, under a tight test timeout.
6. `FanOut` delivers to every drain. One drain hanging does not prevent the others from
   receiving the event, and costs one queue and one goroutine, not one goroutine per event.
   A panicking drain is recovered, and `Flush` and `Close` reach every drain that implements
   them.
7. Zero imports outside the standard library. Passes `-race` with concurrent `Send` and `Close`.

## Testing

Table/unit tests with a fake `Sender` and an injectable clock (no real `time.Sleep` beyond a
few ms in any test). Package `pipeline_test`, black-box.

## Boundaries

- **Always:** never call `SendBatch` synchronously from the caller's goroutine.
- **Ask first:** changing default batch size/interval/buffer size/retry policy.
- **Never:** let a hung `SendBatch` block `Send` or `Close` past its context deadline.

## Open Questions

None.
