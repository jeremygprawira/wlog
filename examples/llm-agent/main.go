// Command llm-agent answers one question with Claude and records the call as one wide
// event. It is the llm-agent recipe's example: FromMessage maps the response, and llm.Add
// folds the token counts, the cost, and the finish reason into the event that already
// holds the request's own fields.
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

// prices rates the one model this recipe calls, in micros per million tokens. A caller
// with more models adds a row per model.
var prices = llm.NewPrices(map[string]llm.Price{
	string(anthropic.ModelClaudeSonnet4_6): {
		InputPerMillion:       3_000_000,
		OutputPerMillion:      15_000_000,
		CachedInputPerMillion: 300_000,
	},
})

// answer sends one question to Claude, inside one event that records the call: its token
// counts, its cost, and its finish reason.
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
	// The process ends here, so the pending events are sent now.
	_ = logger.Flush(context.Background())
}
