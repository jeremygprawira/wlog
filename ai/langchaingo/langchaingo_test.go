package wloglangchaingo_test

import (
	"context"
	"errors"
	"testing"

	"github.com/tmc/langchaingo/llms"

	"github.com/jeremygprawira/wlog"
	wloglangchaingo "github.com/jeremygprawira/wlog/ai/langchaingo"
	"github.com/jeremygprawira/wlog/llm"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// openAIResponse returns a ContentResponse shaped the way langchaingo's OpenAI provider
// builds one: a single choice carrying the whole message and its usage.
func openAIResponse() *llms.ContentResponse {
	return &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{
				Content:    "hello",
				StopReason: "tool_calls",
				GenerationInfo: map[string]any{
					"PromptTokens":       1000,
					"CompletionTokens":   200,
					"TotalTokens":        1200,
					"ReasoningTokens":    40,
					"PromptCachedTokens": 500,
				},
				ToolCalls: []llms.ToolCall{
					{ID: "call_1", Type: "function", FunctionCall: &llms.FunctionCall{Name: "get_weather", Arguments: "{}"}},
				},
			},
		},
	}
}

// anthropicResponse returns a ContentResponse shaped the way langchaingo's Anthropic
// provider builds one: one choice per content block, each repeating the same usage.
func anthropicResponse() *llms.ContentResponse {
	usage := map[string]any{
		"InputTokens":              int64(1000),
		"OutputTokens":             int64(200),
		"CacheCreationInputTokens": int64(100),
		"CacheReadInputTokens":     int64(500),
	}
	return &llms.ContentResponse{
		Choices: []*llms.ContentChoice{
			{Content: "hello", StopReason: "tool_use", GenerationInfo: usage},
			{
				StopReason:     "tool_use",
				GenerationInfo: usage,
				ToolCalls: []llms.ToolCall{
					{ID: "call_1", FunctionCall: &llms.FunctionCall{Name: "get_weather", Arguments: "{}"}},
				},
			},
		},
	}
}

// TestLangchaingo_FromContentResponse_OpenAI proves the OpenAI GenerationInfo keys map to
// the golden.
func TestLangchaingo_FromContentResponse_OpenAI(t *testing.T) {
	got := wloglangchaingo.FromContentResponse(openAIResponse(), "openai", "gpt-4o")
	check(t, got, llm.Record{
		Provider:          "openai",
		Model:             "gpt-4o",
		Operation:         "chat",
		InputTokens:       1000,
		CachedInputTokens: 500,
		OutputTokens:      200,
		ReasoningTokens:   40,
		FinishReason:      "tool_calls",
		ToolCalls:         []llm.ToolCall{{Name: "get_weather"}},
	})
}

// TestLangchaingo_FromContentResponse_Anthropic proves the usage on the repeated
// GenerationInfo blocks is read once, not once per content-block choice, and that a tool
// call on a later choice still reaches the record.
func TestLangchaingo_FromContentResponse_Anthropic(t *testing.T) {
	got := wloglangchaingo.FromContentResponse(anthropicResponse(), "anthropic", "claude-sonnet-4-6")
	check(t, got, llm.Record{
		Provider:              "anthropic",
		Model:                 "claude-sonnet-4-6",
		Operation:             "chat",
		InputTokens:           1600,
		CachedInputTokens:     500,
		CacheWriteInputTokens: 100,
		OutputTokens:          200,
		FinishReason:          "tool_use",
		ToolCalls:             []llm.ToolCall{{Name: "get_weather"}},
	})
}

// TestLangchaingo_ContentOptIn proves no text reaches the record by default, and
// WithContent adds every choice's text and tool call.
func TestLangchaingo_ContentOptIn(t *testing.T) {
	if got := wloglangchaingo.FromContentResponse(anthropicResponse(), "anthropic", "claude-sonnet-4-6"); got.Content != nil {
		t.Error("content reached the record without WithContent")
	}
	got := wloglangchaingo.FromContentResponse(anthropicResponse(), "anthropic", "claude-sonnet-4-6", wloglangchaingo.WithContent())
	if got.Content == nil {
		t.Fatal("WithContent wrote no content")
	}
	if len(got.Content.OutputMessages) != 2 {
		t.Fatalf("output messages = %d, want 2", len(got.Content.OutputMessages))
	}
	if got.Content.OutputMessages[0].Parts[0].Content != "hello" {
		t.Errorf("the text part is missing")
	}
	if got.Content.OutputMessages[1].Parts[0].Name != "get_weather" {
		t.Errorf("the tool call part is missing")
	}
}

// TestLangchaingo_FromContentResponse_Nil proves a nil or choice-less response is a
// zero-value record, never a panic.
func TestLangchaingo_FromContentResponse_Nil(t *testing.T) {
	if got := wloglangchaingo.FromContentResponse(nil, "openai", "gpt-4o"); got.Provider != "" || got.InputTokens != 0 {
		t.Errorf("nil response = %+v, want a zero Record", got)
	}
	empty := &llms.ContentResponse{}
	if got := wloglangchaingo.FromContentResponse(empty, "openai", "gpt-4o"); got.Provider != "" || got.InputTokens != 0 {
		t.Errorf("choice-less response = %+v, want a zero Record", got)
	}
}

// TestLangchaingo_Handler_Tool proves a tool start and end become one call record.
func TestLangchaingo_Handler_Tool(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")

	h := wloglangchaingo.Handler()
	h.HandleToolStart(ctx, "input")
	h.HandleToolEnd(ctx, "output")
	end()

	calls, _ := rec.Last()["calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("calls = %v, want 1 entry", calls)
	}
	call, _ := calls[0].(map[string]any)
	if call["kind"] != "agent" || call["operation"] != "tool" {
		t.Errorf("call = %v, want kind agent, operation tool", call)
	}
	if _, hasErr := call["error"]; hasErr {
		t.Errorf("call has an error, want none: %v", call)
	}
}

// TestLangchaingo_Handler_ChainError proves a chain start and error become one failed call
// record, and that it never collides with a tool call sharing the same event.
func TestLangchaingo_Handler_ChainError(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")

	h := wloglangchaingo.Handler()
	h.HandleChainStart(ctx, map[string]any{})
	h.HandleToolStart(ctx, "input")
	h.HandleToolEnd(ctx, "output")
	h.HandleChainError(ctx, errors.New("boom"))
	end()

	calls, _ := rec.Last()["calls"].([]any)
	if len(calls) != 2 {
		t.Fatalf("calls = %v, want 2 entries", calls)
	}
	var sawTool, sawChain bool
	for _, c := range calls {
		call, _ := c.(map[string]any)
		switch call["operation"] {
		case "tool":
			sawTool = true
		case "chain":
			sawChain = true
			if _, hasErr := call["error"]; !hasErr {
				t.Errorf("chain call has no error: %v", call)
			}
		}
	}
	if !sawTool || !sawChain {
		t.Errorf("calls = %v, want one tool and one chain entry", calls)
	}
}

// check compares two records field by field.
func check(t *testing.T, got, want llm.Record) {
	t.Helper()
	if got.Provider != want.Provider || got.Model != want.Model || got.Operation != want.Operation {
		t.Errorf("identity = %s/%s/%s, want %s/%s/%s", got.Provider, got.Model, got.Operation, want.Provider, want.Model, want.Operation)
	}
	for name, pair := range map[string][2]int{
		"input":     {got.InputTokens, want.InputTokens},
		"read":      {got.CachedInputTokens, want.CachedInputTokens},
		"write":     {got.CacheWriteInputTokens, want.CacheWriteInputTokens},
		"output":    {got.OutputTokens, want.OutputTokens},
		"reasoning": {got.ReasoningTokens, want.ReasoningTokens},
	} {
		if pair[0] != pair[1] {
			t.Errorf("%s = %d, want %d", name, pair[0], pair[1])
		}
	}
	if got.FinishReason != want.FinishReason {
		t.Errorf("FinishReason = %q, want %q", got.FinishReason, want.FinishReason)
	}
	if len(got.ToolCalls) != len(want.ToolCalls) {
		t.Fatalf("ToolCalls = %v, want %v", got.ToolCalls, want.ToolCalls)
	}
	for i := range got.ToolCalls {
		if got.ToolCalls[i].Name != want.ToolCalls[i].Name {
			t.Errorf("ToolCalls[%d].Name = %q, want %q", i, got.ToolCalls[i].Name, want.ToolCalls[i].Name)
		}
	}
}

// TestLangchaingo_A4_OpenAIToolCallCountedOnce proves the legacy FuncCall copy of
// ToolCalls[0] is not counted again. A choice with only FuncCall still counts once.
func TestLangchaingo_A4_OpenAIToolCallCountedOnce(t *testing.T) {
	call := &llms.FunctionCall{Name: "get_weather", Arguments: `{"city":"Jakarta"}`}
	resp := &llms.ContentResponse{
		Choices: []*llms.ContentChoice{{
			FuncCall: call,
			ToolCalls: []llms.ToolCall{
				{ID: "call_1", FunctionCall: call},
				{ID: "call_2", FunctionCall: &llms.FunctionCall{Name: "get_time", Arguments: "{}"}},
			},
		}},
	}
	got := wloglangchaingo.FromContentResponse(resp, "openai", "gpt-4o")
	if len(got.ToolCalls) != 2 {
		t.Fatalf("ToolCalls = %v, want get_weather and get_time once each", got.ToolCalls)
	}

	legacy := &llms.ContentResponse{Choices: []*llms.ContentChoice{{
		FuncCall: &llms.FunctionCall{Name: "get_weather", Arguments: "{}"},
	}}}
	got = wloglangchaingo.FromContentResponse(legacy, "openai", "gpt-4o")
	if len(got.ToolCalls) != 1 || got.ToolCalls[0].Name != "get_weather" {
		t.Fatalf("legacy FuncCall = %v, want one get_weather", got.ToolCalls)
	}

	got = wloglangchaingo.FromContentResponse(resp, "openai", "gpt-4o", wloglangchaingo.WithContent())
	parts := 0
	for _, msg := range got.Content.OutputMessages {
		for _, part := range msg.Parts {
			if part.Type == "tool_call" {
				parts++
			}
		}
	}
	if parts != 2 {
		t.Fatalf("content tool_call parts = %d, want 2", parts)
	}
}
