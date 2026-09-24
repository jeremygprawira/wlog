package llm_test

import (
	"context"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/llm"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestLLM_CacheWrite1hPricing proves the one hour cache writes price at twice the input
// rate, next to the five minute writes.
func TestLLM_CacheWrite1hPricing(t *testing.T) {
	cost, ok := llm.DefaultPrices().Cost(llm.Record{
		Model:                   "claude-sonnet-4-6",
		InputTokens:             10_000,
		CacheWrite1hInputTokens: 1_000,
		OutputTokens:            100,
	})
	if !ok {
		t.Fatal("DefaultPrices has no row for claude-sonnet-4-6")
	}
	// 9,000 fresh input at 3 micros, 1,000 one hour writes at 6 micros, 100 output at 15.
	if cost.InputMicros != 27_000 {
		t.Errorf("InputMicros = %d, want 27000", cost.InputMicros)
	}
	if cost.CacheWrite1hMicros != 6_000 {
		t.Errorf("CacheWrite1hMicros = %d, want 6000", cost.CacheWrite1hMicros)
	}
	if cost.TotalMicros != 34_500 {
		t.Errorf("TotalMicros = %d, want 34500", cost.TotalMicros)
	}
}

// TestLLM_AddSetsSpeedAndSteps proves Add sets the decode speed from the output count and
// the duration, and keeps the step count on the event.
func TestLLM_AddSetsSpeedAndSteps(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	llm.Add(ctx, llm.Record{
		Model:        "claude-sonnet-4-6",
		OutputTokens: 100,
		Duration:     2 * time.Second,
		Steps:        3,
	})
	end()

	group, _ := rec.Last()["llm"].(map[string]any)
	if group["output_tokens_per_second"] != float64(50) {
		t.Errorf("output_tokens_per_second = %v, want 50", group["output_tokens_per_second"])
	}
	if group["steps"] != int64(3) {
		t.Errorf("steps = %v, want 3", group["steps"])
	}
}

// TestLLM_NoContentByDefault proves the default record writes no prompt or completion,
// which is the SPEC-llm rule.
func TestLLM_NoContentByDefault(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	llm.Set(ctx, llm.Record{Model: "claude-sonnet-4-6", InputTokens: 10})
	end()

	group, _ := rec.Last()["llm"].(map[string]any)
	for _, key := range []string{"input_messages", "output_messages"} {
		if _, ok := group[key]; ok {
			t.Errorf("%s reached the event without WithContent", key)
		}
	}
}

// TestLLM_ContentOptIn proves Content writes the prompt and the completion under the llm
// group, and Add keeps the content out of every call entry.
func TestLLM_ContentOptIn(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")
	llm.Add(ctx, llm.Record{
		Model: "claude-sonnet-4-6",
		Content: &llm.Content{
			InputMessages: []llm.Message{{
				Role:  "user",
				Parts: []llm.Part{{Type: "text", Content: "hello"}},
			}},
			OutputMessages: []llm.Message{{
				Role: "assistant",
				Parts: []llm.Part{{
					Type:      "tool_call",
					ID:        "toolu_1",
					Name:      "get_weather",
					Arguments: map[string]any{"city": "Jakarta"},
				}},
			}}},
	})
	end()

	group, _ := rec.Last()["llm"].(map[string]any)
	if _, ok := group["input_messages"]; !ok {
		t.Error("input_messages is missing")
	}
	if _, ok := group["output_messages"]; !ok {
		t.Error("output_messages is missing")
	}
	calls, _ := group["calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("calls = %v, want one entry", group["calls"])
	}
	call, _ := calls[0].(map[string]any)
	for _, key := range []string{"input_messages", "output_messages"} {
		if _, ok := call[key]; ok {
			t.Errorf("the call entry repeats %s", key)
		}
	}
}

// TestLLM_TokenInvariants proves the token rules hold for each provider shape: every
// cache count is a part of the input, reasoning is a part of the output, and the priced
// parts add up to the total even when a caller reports more cache than input.
func TestLLM_TokenInvariants(t *testing.T) {
	for _, tc := range []struct {
		name string
		r    llm.Record
	}{
		{"anthropic", llm.Record{
			Model: "claude-sonnet-4-6", InputTokens: 1_000,
			CachedInputTokens: 800, CacheWriteInputTokens: 100, CacheWrite1hInputTokens: 50,
			OutputTokens: 100, ReasoningTokens: 40,
		}},
		{"openai-chat", llm.Record{
			Model: "gpt-4o", InputTokens: 1_000,
			CachedInputTokens: 500, CacheWriteInputTokens: 100,
			OutputTokens: 200, ReasoningTokens: 100,
		}},
		{"openai-responses", llm.Record{
			Model: "gpt-5.6-sol", InputTokens: 1_000,
			CachedInputTokens: 200, CacheWriteInputTokens: 50,
			OutputTokens: 300, ReasoningTokens: 150,
		}},
		{"gemini", llm.Record{
			Model: "gpt-4o-mini", InputTokens: 1_000,
			CachedInputTokens: 200, OutputTokens: 300, ReasoningTokens: 150,
		}},
		{"over-reported", llm.Record{
			Model: "claude-sonnet-4-6", InputTokens: 100,
			CachedInputTokens: 500, CacheWriteInputTokens: 500, CacheWrite1hInputTokens: 500,
			OutputTokens: 10, ReasoningTokens: 50,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cost, ok := llm.DefaultPrices().Cost(tc.r)
			if !ok {
				t.Fatalf("DefaultPrices has no row for %s", tc.r.Model)
			}
			parts := map[string]int64{
				"input":   cost.InputMicros,
				"read":    cost.CacheReadMicros,
				"write":   cost.CacheWriteMicros,
				"write1h": cost.CacheWrite1hMicros,
				"output":  cost.OutputMicros,
			}
			for name, micros := range parts {
				if micros < 0 {
					t.Errorf("%s micros = %d, want at least 0", name, micros)
				}
			}
			sum := cost.InputMicros + cost.CacheReadMicros + cost.CacheWriteMicros +
				cost.CacheWrite1hMicros + cost.OutputMicros
			if cost.TotalMicros != sum {
				t.Errorf("TotalMicros = %d, want the parts to sum to %d", cost.TotalMicros, sum)
			}
		})
	}
}
