# Recipe: LLM agent

An agent that answers one question with Claude, where one call is one wide event. The
runnable example lives in [examples/llm-agent](../../examples/llm-agent). Its test answers
one question through a fake Messages API. Then it compares the event with the golden in
`testdata/event.json`.

## 1. Setup

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/jeremygprawira/wlog"
	wloganthropic "github.com/jeremygprawira/wlog/ai/anthropic"
	"github.com/jeremygprawira/wlog/llm"
	"github.com/jeremygprawira/wlog/work"
)

var prices = llm.NewPrices(map[string]llm.Price{
	string(anthropic.ModelClaudeSonnet4_6): {
		InputPerMillion:       3_000_000,
		OutputPerMillion:      15_000_000,
		CachedInputPerMillion: 300_000,
	},
})

func answer(ctx context.Context, logger *wlog.Logger, client anthropic.Client, question string) (string, error) {
	ctx, handle := work.Start(ctx, logger, work.Unit{Kind: work.KindWork, Operation: "answer question"})

	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeSonnet4_6,
		MaxTokens: 1024,
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(question))},
	})
	if err != nil {
		handle.End(err)
		return "", err
	}
	llm.Add(ctx, wloganthropic.FromMessage(msg))

	handle.End(nil)
	return msg.Content[0].Text, nil
}

func main() {
	logger := wlog.New(
		wlog.WithService("llm-agent", "0.0.1", "local"),
		wlog.WithEnrichers(llm.Enricher(prices)),
	)
	client := anthropic.NewClient(option.WithAPIKey(os.Getenv("ANTHROPIC_API_KEY")))

	text, err := answer(context.Background(), logger, client, "What is the capital of France?")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(text)
	_ = logger.Flush(context.Background())
}
```

`FromMessage` maps the response to an `llm.Record`, and `llm.Add` folds its token counts and
its finish reason into the `llm` group. `llm.Enricher` reads the price table and fills
`llm.cost_micros` once the event settles, so `answer` never computes a price itself. A model
the table does not hold sets `llm.cost_unknown` instead of a wrong number.

## 2. The event

One question, answered by a fake response of 1000 input tokens and 200 output tokens, gives
this event. The duration, the event id, and the trace ids change between runs, so the
example test normalizes them.

```json
{"duration_ms":8.25,"event_id":"0191f0b9-1c2f-7a3d-8e4f-0a1b2c3d4e5f","kind":"work","level":"info","llm":{"calls":[{"cost_micros":6000,"finish_reasons":["end_turn"],"input_tokens":1000,"operation":"chat","output_tokens":200,"provider":"anthropic","request_model":"claude-sonnet-4-6","response_id":"msg_01ABC123","total_tokens":1200}],"cost_micros":6000,"finish_reasons":["end_turn"],"input_tokens":1000,"operation":"chat","output_tokens":200,"provider":"anthropic","request_model":"claude-sonnet-4-6","response_id":"msg_01ABC123","tool_call_count":0,"total_tokens":1200},"operation":"answer question","outcome":"success","summary":"answer question success in {d}","timestamp":"2026-01-01T00:00:00Z","trace":{},"wlog":{"schema_version":2}}
```

## 3. Five questions

| Question | wlog query | jq | Backend |
|---|---|---|---|
| Which calls cost the most | `wlog query --stats llm.cost_micros ./logs` | `jq -s 'map(.llm.cost_micros) \| sort' logs.ndjson` | ClickHouse: `SELECT operation, sum(llm.cost_micros) FROM events GROUP BY operation ORDER BY 2 DESC` |
| Which models are in use | `wlog query --group-by llm.request_model --count ./logs` | `jq -r '.llm.request_model' logs.ndjson \| sort \| uniq -c` | Grafana: count by llm.request_model |
| Which calls fail most | `wlog query --level error --group-by operation --count ./logs` | `jq -r 'select(.level=="error") \| .operation' logs.ndjson \| sort \| uniq -c` | Loki: `sum by (operation) (count_over_time({service="llm-agent"} \| json \| level="error" [1h]))` |
| Which calls are slowest | `wlog query --stats duration_ms ./logs` | `jq -s 'map(.duration_ms) \| sort' logs.ndjson` | Honeycomb: p95 of duration_ms, grouped by operation |
| How many tokens one call used | `wlog query --stats llm.total_tokens ./logs` | `jq -r '.llm.total_tokens' logs.ndjson \| sort -n \| tail -1` | Elasticsearch: `SELECT max(llm.total_tokens) FROM events` |

## 4. Explain ids

- `wlog explain llm` names the llm group. It does not list the token, cost, or finish reason fields. Those are in SPEC-track-g.md.
- `wlog explain kind` names the kind field. It does not list what a work event holds.
- `wlog explain WLOG_CAP_REACHED` for what happens past the 200 call cap on `llm.calls`.
- When an event arrives after `Close`, `wlog explain WLOG_LOGGER_CLOSED`.
