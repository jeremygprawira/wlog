# Cost

The `llm` package records what a model call cost on the same wide event as the request.
One event then answers how long the request took, what it returned, and what it spent.

## The fields

| Field | Meaning |
|---|---|
| `llm.provider` | `openai`, `anthropic`, `google`, or any string |
| `llm.model` | the exact model id billed |
| `llm.operation` | `chat`, `embedding`, `rerank`, or any string |
| `llm.input_tokens` | prompt tokens billed |
| `llm.output_tokens` | completion tokens billed |
| `llm.cached_input_tokens` | prompt tokens served from a cache |
| `llm.reasoning_tokens` | reasoning tokens the model reports |
| `llm.total_tokens` | input plus output |
| `llm.tool_calls` `llm.tool_call_count` `llm.tool_call_failures` | tool use |
| `llm.time_to_first_chunk_ms` | stream start, for a streamed call |
| `llm.duration_ms` | the model call itself, not the request |
| `llm.streamed` `llm.finish_reason` | how the call ended |
| `llm.cost_micros` `llm.cost_usd` | money, see below |
| `llm.cost_unknown` | true when the table does not hold the model |
| `llm.calls[]` | one object per call, written by `llm.Add` |

A zero field stays off the event. The module never records prompt or completion text.

## Money in micros

`llm.cost_micros` is the authoritative value. One micro is one millionth of a US
dollar, so `llm.cost_micros: 3000` is three tenths of a cent. The value is a whole
integer, so it adds up exactly across a million requests.

`llm.cost_usd` is the same value as a float, for a human reading one event. Chart the
micros field and divide once, at the dashboard.

## Price a call

```go
log := wlog.New(
    wlog.WithEnrichers(llm.Enricher(llm.DefaultPrices())),
)

ctx, end := wlog.Start(ctx, "chat")
defer end()
llm.Set(ctx, llm.Record{
    Provider: "anthropic", Model: "claude-sonnet-5", Operation: "chat",
    InputTokens: 1200, OutputTokens: 340, CachedInputTokens: 800,
})
```

The enricher prices the call and writes `llm.cost_micros`. A model the table does not
hold gets `llm.cost_unknown: true` and no cost, so a dashboard can count what it could
not price.

## Override a price

`DefaultPrices` is a convenience, checked on the date in its doc comment. It is not a
source of truth. Build a table from your own contract, or extend the default.

```go
prices := llm.DefaultPrices().With("house-model", llm.Price{
    InputPerMillion:       500_000, // 0.50 USD per million tokens
    OutputPerMillion:    1_500_000, // 1.50 USD per million tokens
    CachedInputPerMillion: 50_000,
})
```

`Prices` is immutable. `With` returns a new table, so one table can serve many
goroutines.

## Several calls in one request

```go
llm.Add(ctx, first)
llm.Add(ctx, second)
```

`Add` folds the token, tool-call, and cost totals, and appends each call to
`llm.calls[]`. The enricher then prices every call and writes one total.

## Chart spend per route

Group `llm.cost_micros` by `http.route` and `service.name`:

```sql
-- Axiom or ClickHouse style
SELECT http.route, sum(llm.cost_micros) / 1000000.0 AS usd
FROM events
WHERE llm.cost_micros > 0
GROUP BY http.route
ORDER BY usd DESC
```

The `ClickHouse` drain stores `llm` in its `event` JSON column. Query it with
`JSONExtract` or a materialized view, and add a column if you chart it often.

## A note on the stream field name

`llm.time_to_first_chunk_ms` avoids the word "token". The default redactor denies any
key whose tokens include `token`, so a name such as `time_to_first_token_ms` would be
masked before a drain saw it.
