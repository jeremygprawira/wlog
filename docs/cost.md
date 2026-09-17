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
| `llm.cached_input_tokens` | prompt tokens served from a cache, billed at the cache-read rate |
| `llm.cache_write_input_tokens` | prompt tokens written to a cache, billed above the input rate |
| `llm.reasoning_tokens` | reasoning tokens the model reports |
| `llm.total_tokens` | input plus output |
| `llm.tool_calls` `llm.tool_call_count` `llm.tool_call_failures` | tool use |
| `llm.time_to_first_chunk_ms` | stream start, for a streamed call |
| `llm.duration_ms` | the model call itself, not the request |
| `llm.streamed` `llm.finish_reason` | how the call ended |
| `llm.cost_micros` | money, in whole micros: see below |
| `llm.cost_unknown` | true for a model the table does not hold |
| `llm.calls[]` | one object per call, written by `llm.Add` |

A zero field stays off the event. The module never records prompt or completion text.

## Money in micros

`llm.cost_micros` is the authoritative value. One micro is one millionth of a US
dollar, so `llm.cost_micros: 3000` is three tenths of a cent. The value is a whole
integer, so it adds up exactly across a million requests.

An event never carries a dollar float. A float rounds, and a rounded cent is a wrong
answer after a million calls. Divide once, at the dashboard.

## Price a call

<!-- snippet:sketch -->
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

## Map your provider's token counts

`InputTokens` is the whole input count, as the provider reports it. `CachedInputTokens`
and `CacheWriteInputTokens` are subsets of it. Providers report the parts differently:

- **Anthropic** reports `input_tokens` without the cache parts, so add them:
  `InputTokens = input_tokens + cache_creation_input_tokens + cache_read_input_tokens`,
  with `CachedInputTokens = cache_read_input_tokens` and
  `CacheWriteInputTokens = cache_creation_input_tokens`. A call can report 50 input tokens
  and 100,000 cache reads. It then has 100,050 input tokens, with 100,000 of them cached, so
  the cache rate covers those 100,000. Pricing it as 50 fresh tokens is wrong by a
  factor of a thousand.
- **OpenAI and Gemini** include the cache parts in the prompt count already, so
  `InputTokens` maps across as it stands, with
  `CachedInputTokens = prompt_tokens_details.cached_tokens`.

`ReasoningTokens` is a subset of `OutputTokens`. A call whose cache parts exceed its input
count is clamped, so no token is billed twice and no part goes negative.

## The price table

`DefaultPrices` holds the rows the vendor pages listed on 2026-09-16, in micros per million
tokens: one million micros is one dollar, so $2.50 per million tokens is `2_500_000`.

A model id that is not a row prices by the longest row name it starts with, at a
separator. The dated snapshot `gpt-4o-2024-08-06` therefore prices as `gpt-4o`. The id
`gpt-4o-mini-2024-07-18` prices as `gpt-4o-mini`, and never as `gpt-4o`. A model no row
names, and that names no row, is `llm.cost_unknown` instead of a guess.

An Anthropic cache write costs 1.25x the input rate for the 5 minute cache and 2x for the
1 hour cache. One field cannot hold both, so the table carries the 5 minute rate. A
service that uses the 1 hour cache prices those writes itself:

<!-- snippet:sketch -->
```go
prices := llm.DefaultPrices().With("claude-sonnet-5", llm.Price{
    InputPerMillion: 2_000_000, CachedInputPerMillion: 200_000, CacheWritePerMillion: 4_000_000,
    OutputPerMillion: 10_000_000,
})
```

Long context, batch, and regional processing change a price too, and no rate field holds
them. Price those calls with your own table.

The enricher prices the call and writes `llm.cost_micros`. A model the table does not
hold gets `llm.cost_unknown: true` and no cost. A dashboard can then count what it cannot
price.

## Override a price

`DefaultPrices` is a convenience, checked on the date in its doc comment. It is not a
source of truth. Build a table from your own contract, or extend the default.

<!-- snippet:sketch -->
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

<!-- snippet:sketch -->
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

The `ClickHouse` drain stores `llm` in its `event` JSON column. Query that column with
`JSONExtract`, or with a materialized view. When you chart it often, add a column.

## A note on the stream field name

`llm.time_to_first_chunk_ms` avoids the word "token". The default redactor denies any
key whose tokens include `token`. A name such as `time_to_first_token_ms` is therefore
masked before a drain sees it.
