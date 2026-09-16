package llm_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/llm"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// start begins an event and returns its context, the recorder, and the end func.
func start(t *testing.T) (context.Context, *wlogtest.Recorder, func()) {
	t.Helper()
	log, rec := wlogtest.New(t)
	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "chat")
	return ctx, rec, end
}

// llmGroup reads the emitted llm group from the last event.
func llmGroup(t *testing.T, rec *wlogtest.Recorder) map[string]any {
	t.Helper()
	group, ok := rec.Last()["llm"].(map[string]any)
	if !ok {
		t.Fatalf("no llm group in %v", rec.Last())
	}
	return group
}

// TestLLM_Set proves every non-zero field reaches the event under llm.
func TestLLM_Set(t *testing.T) {
	ctx, rec, end := start(t)
	llm.Set(ctx, llm.Record{
		Provider:          "openai",
		Model:             "gpt-x",
		Operation:         "chat",
		InputTokens:       10,
		OutputTokens:      5,
		CachedInputTokens: 3,
		ReasoningTokens:   2,
		ToolCalls:         []llm.ToolCall{{Name: "search", Duration: 50 * time.Millisecond, Failed: true}},
		TimeToFirstToken:  100 * time.Millisecond,
		Duration:          2 * time.Second,
		Streamed:          true,
		FinishReason:      "stop",
		Cost:              &llm.Cost{InputMicros: 100, OutputMicros: 200, TotalMicros: 300},
	})
	end()

	group := llmGroup(t, rec)
	want := map[string]any{
		"provider": "openai", "model": "gpt-x", "operation": "chat",
		"input_tokens": int64(10), "output_tokens": int64(5), "cached_input_tokens": int64(3),
		"reasoning_tokens": int64(2), "total_tokens": int64(15),
		"tool_call_count": int64(1), "tool_call_failures": int64(1),
		"time_to_first_chunk_ms": int64(100), "duration_ms": int64(2000),
		"streamed": true, "finish_reason": "stop",
		"cost_micros": int64(300),
	}
	for key, value := range want {
		if group[key] != value {
			t.Errorf("llm.%s = %v (%T), want %v", key, group[key], group[key], value)
		}
	}
}

// TestLLM_Set_ZeroFieldsOff proves an empty record leaves every field off.
func TestLLM_Set_ZeroFieldsOff(t *testing.T) {
	ctx, rec, end := start(t)
	llm.Set(ctx, llm.Record{Model: "m"})
	end()

	group := llmGroup(t, rec)
	for _, key := range []string{"input_tokens", "output_tokens", "cached_input_tokens", "reasoning_tokens", "total_tokens", "tool_calls", "tool_call_count", "time_to_first_chunk_ms", "duration_ms", "streamed", "finish_reason", "cost_micros", "cost_usd"} {
		if _, present := group[key]; present {
			t.Errorf("llm.%s present on a zero record: %v", key, group[key])
		}
	}
}

// TestLLM_Add_FoldsTotals proves Add sums tokens across calls and appends each call.
func TestLLM_Add_FoldsTotals(t *testing.T) {
	ctx, rec, end := start(t)
	llm.Add(ctx, llm.Record{Model: "m", InputTokens: 10, OutputTokens: 2, ToolCalls: []llm.ToolCall{{Name: "a"}}})
	llm.Add(ctx, llm.Record{Model: "m", InputTokens: 3, OutputTokens: 4, ToolCalls: []llm.ToolCall{{Name: "b", Failed: true}}})
	end()

	group := llmGroup(t, rec)
	if group["input_tokens"] != int64(13) || group["output_tokens"] != int64(6) || group["total_tokens"] != int64(19) {
		t.Errorf("folded tokens = %v/%v/%v, want 13/6/19", group["input_tokens"], group["output_tokens"], group["total_tokens"])
	}
	calls, _ := group["calls"].([]any)
	if len(calls) != 2 {
		t.Fatalf("calls = %v, want 2", group["calls"])
	}
	if group["tool_call_count"] != int64(2) || group["tool_call_failures"] != int64(1) {
		t.Errorf("tool call totals = %v/%v, want 2/1", group["tool_call_count"], group["tool_call_failures"])
	}
}

// TestLLM_OutsideStart proves both calls are safe no-ops outside a Start.
func TestLLM_OutsideStart(t *testing.T) {
	llm.Set(context.Background(), llm.Record{Model: "m"})
	llm.Add(context.Background(), llm.Record{Model: "m"})
}

// TestLLM_NoPromptText proves the module never writes message content onto the event.
func TestLLM_NoPromptText(t *testing.T) {
	ctx, rec, end := start(t)
	wlog.Set(ctx, "prompt", "secret prompt text")
	llm.Set(ctx, llm.Record{Model: "m", InputTokens: 1})
	end()

	group := llmGroup(t, rec)
	for _, key := range []string{"prompt", "completion", "messages", "content"} {
		if _, present := group[key]; present {
			t.Errorf("llm.%s present, but the module must not record message content", key)
		}
	}
	if strings.Contains(rec.Last()["prompt"].(string), "llm") {
		t.Errorf("llm wrote into the prompt field: %v", rec.Last()["prompt"])
	}
}
