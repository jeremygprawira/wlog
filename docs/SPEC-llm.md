# Spec: llm

> Module id `llm`. Package `github.com/jeremygprawira/wlog/llm`.
> Root module, standard library only. Depends on `core`.
> Project-wide rules in [SPEC.md](SPEC.md) apply. Closes gaps 1 and 13 in
> [evlog parity](evlog-parity.md).

## Objective

One typed record for an LLM call, written onto the wide event under `llm`. It holds the
model, the token counts, the tool calls, the stream timings, and the money the call cost.
A team can then answer "what did this request spend, and where" from the same event that
already holds the request's status and duration.

The module depends on no LLM SDK. A caller fills the record from whatever client it uses,
which keeps this package in the root module and free of a vendor's release cycle.

## Behaviour

<!-- snippet:sketch -->
```go
// Record is one model call. A zero field is left off the event, so a caller fills only
// what its own client reports.
type Record struct {
	Provider string // "openai", "anthropic", "google", or any string
	Model    string // the exact model id billed, such as "claude-sonnet-5"
	Operation string // "chat", "embedding", "rerank", or any string

	InputTokens       int
	OutputTokens      int
	CachedInputTokens int // tokens served from a prompt cache, billed at a lower rate
	ReasoningTokens   int

	ToolCalls []ToolCall

	TimeToFirstToken time.Duration // stream only, zero for a whole-response call
	Duration         time.Duration
	Streamed         bool

	FinishReason string // "stop", "length", "tool_calls", "content_filter", or any string
	Cost         *Cost  // nil until Price fills it
}

type ToolCall struct {
	Name     string
	Duration time.Duration
	Failed   bool
}

// Cost is money, in whole millionths of a US dollar, so no float rounding reaches the
// event. USD() renders it for a human.
type Cost struct {
	InputMicros  int64
	OutputMicros int64
	TotalMicros  int64
}

func (c Cost) USD() float64

// Set writes r onto the current event under the "llm" group. Outside a wlog.Start, it
// does nothing, the same as wlog.Set.
func Set(ctx context.Context, r Record)

// Add folds r into the current event's llm totals and appends it to llm.calls[]. Use it
// when one request makes several model calls.
func Add(ctx context.Context, r Record)
```

### Pricing

<!-- snippet:sketch -->
```go
// Price is one model's rate, in micros per million tokens.
type Price struct {
	InputPerMillion       int64
	OutputPerMillion      int64
	CachedInputPerMillion int64
}

type Prices struct{ /* unexported */ }

func NewPrices(byModel map[string]Price) *Prices
func DefaultPrices() *Prices        // a small built-in table, dated in its doc comment
func (p *Prices) Cost(r Record) (Cost, bool) // false when the model is unknown
func (p *Prices) With(model string, price Price) *Prices // returns a new Prices
```

`Prices` is immutable, and `With` returns a copy, the same contract `redact.Redactor`
already uses. The built-in table is a convenience, not a source of truth. Its doc comment
carries the date it was checked, and a caller overrides any row with `With`.

### Enricher

<!-- snippet:sketch -->
```go
func Enricher(p *Prices) wlog.Enricher
```

The enricher reads the `llm` group the handler already set, prices every call whose model
the table knows, and writes `llm.cost` and the per-call costs. An unknown model leaves the
cost off the event and sets `llm.cost_unknown` to true. A dashboard can then count what it
failed to price, instead of reading a silent zero.

### Fields on the event

```
llm.provider llm.model llm.operation
llm.input_tokens llm.output_tokens llm.cached_input_tokens llm.reasoning_tokens
llm.total_tokens
llm.tool_calls llm.tool_call_count llm.tool_call_failures
llm.time_to_first_chunk_ms llm.duration_ms llm.streamed llm.finish_reason
llm.cost_micros llm.cost_unknown
llm.calls[]  (one object per call, set by Add)
```

The stream timing field avoids the word "token" on purpose. The default redactor
denies any key whose tokens include "token", so a name such as `time_to_first_token_ms`
would be masked before it reached a drain.

## Success Criteria

1. `Set` writes every non-zero field under `llm`, and leaves every zero field off.
2. `Add` folds token counts and costs into one total, and appends each call to `llm.calls[]`,
   capped at 200 entries each by llm itself, with every entry past the cap counted in
   wlog.dropped_fields (gate G4). Money stays in whole micros: an event never carries a
   dollar float, because a float would round.
3. `Cost` prices a record from the token counts, and prices a cached input token at the
   cached rate.
4. `Cost` reports false for a model the table does not hold, and the enricher then sets
   `llm.cost_unknown`.
5. Money never passes through a float before it reaches the event. A test prices a call
   whose exact dollar value has no float representation and compares whole micros.
6. `With` returns a new `Prices` and leaves the original unchanged, under `-race`.
7. `Set` and `Add` outside a `wlog.Start` do nothing and do not panic.
8. A redacted prompt or completion never reaches the event, since this module writes no
   prompt text at all. A test asserts no field holds message content.
9. Zero imports outside the standard library plus `core`.

## Cost guide

`docs/cost.md` ships with this module. It explains the token fields, the micros unit, how
to override a price, and how to chart spend per route from the emitted event. This closes
the documentation half of gap 13.

## Testing

Package `llm_test`, black-box, with `wlogtest`. The pricing tests use a fixed table built
in the test, never `DefaultPrices`, so a later table update cannot break them.

## Boundaries

- **Always:** keep money in whole micros inside the record and the event.
- **Ask first:** adding a model to the built-in price table, and changing the micros unit.
- **Never:** write prompt or completion text onto the event. This module records the shape
  of a call, never its content.

## Open Questions

None.
