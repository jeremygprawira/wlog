package wlogeino_test

import (
	"context"
	"testing"
	"time"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/jeremygprawira/wlog"
	wlogeino "github.com/jeremygprawira/wlog/ai/eino"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// runInfo returns the RunInfo eino hands every callback for a chat model named provider.
func runInfo(provider string) *callbacks.RunInfo {
	return &callbacks.RunInfo{Type: provider, Component: components.ComponentOfChatModel}
}

// TestEino_Handler_OnEnd proves a whole response folds into the llm group through
// llm.Add, reading usage from CallbackOutput.TokenUsage.
func TestEino_Handler_OnEnd(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")

	h := wlogeino.Handler()
	h.OnEnd(ctx, runInfo("OpenAI"), &model.CallbackOutput{
		Config: &model.Config{Model: "gpt-4o"},
		TokenUsage: &model.TokenUsage{
			PromptTokens:            1000,
			PromptTokenDetails:      model.PromptTokenDetails{CachedTokens: 500},
			CompletionTokens:        200,
			CompletionTokensDetails: model.CompletionTokensDetails{ReasoningTokens: 40},
		},
		Message: &schema.Message{
			Role:    schema.Assistant,
			Content: "hello",
			ToolCalls: []schema.ToolCall{
				{ID: "call_1", Function: schema.FunctionCall{Name: "get_weather", Arguments: "{}"}},
			},
			ResponseMeta: &schema.ResponseMeta{FinishReason: "tool_calls"},
		},
	})
	end()

	group, _ := rec.Last()["llm"].(map[string]any)
	if group["provider"] != "OpenAI" || group["request_model"] != "gpt-4o" {
		t.Fatalf("llm group = %v, want provider OpenAI, request_model gpt-4o", group)
	}
	if group["input_tokens"] != int64(1000) || group["output_tokens"] != int64(200) {
		t.Errorf("tokens = %v, want input 1000, output 200", group)
	}
	if group["cache_read_input_tokens"] != int64(500) || group["reasoning_tokens"] != int64(40) {
		t.Errorf("token details = %v, want cache_read 500, reasoning 40", group)
	}
	finishReasons, _ := group["finish_reasons"].([]any)
	if len(finishReasons) != 1 || finishReasons[0] != "tool_calls" {
		t.Errorf("finish_reasons = %v, want [tool_calls]", group["finish_reasons"])
	}
	if group["tool_call_count"] != int64(1) {
		t.Errorf("tool_call_count = %v, want 1", group["tool_call_count"])
	}
	if _, hasContent := group["output_messages"]; hasContent {
		t.Error("output_messages reached the event without WithContent")
	}
}

// TestEino_Handler_UsageFallback proves a nil TokenUsage falls back to
// Message.ResponseMeta.Usage.
func TestEino_Handler_UsageFallback(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")

	h := wlogeino.Handler()
	h.OnEnd(ctx, runInfo("Claude"), &model.CallbackOutput{
		Message: &schema.Message{
			Role:    schema.Assistant,
			Content: "hello",
			ResponseMeta: &schema.ResponseMeta{
				Usage: &schema.TokenUsage{PromptTokens: 700, CompletionTokens: 150},
			},
		},
	})
	end()

	group, _ := rec.Last()["llm"].(map[string]any)
	if group["input_tokens"] != int64(700) || group["output_tokens"] != int64(150) {
		t.Errorf("fallback tokens = %v, want input 700, output 150", group)
	}
}

// TestEino_Handler_Content proves WithContent adds the text and the tool call.
func TestEino_Handler_Content(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")

	h := wlogeino.Handler(wlogeino.WithContent())
	h.OnEnd(ctx, runInfo("OpenAI"), &model.CallbackOutput{
		Message: &schema.Message{
			Role:    schema.Assistant,
			Content: "hello",
			ToolCalls: []schema.ToolCall{
				{ID: "call_1", Function: schema.FunctionCall{Name: "get_weather", Arguments: "{}"}},
			},
		},
	})
	end()

	group, _ := rec.Last()["llm"].(map[string]any)
	messages, _ := group["output_messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("output_messages = %v, want 2 entries", group["output_messages"])
	}
}

// TestEino_Handler_Stream proves a streamed call is drained on its own copy and folds
// into the event once done, with the last chunk's usage and finish reason winning.
func TestEino_Handler_Stream(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")

	chunks := []callbacks.CallbackOutput{
		&model.CallbackOutput{Message: &schema.Message{Role: schema.Assistant, Content: "hel"}},
		&model.CallbackOutput{
			Message: &schema.Message{
				Role:         schema.Assistant,
				Content:      "lo",
				ResponseMeta: &schema.ResponseMeta{FinishReason: "stop"},
			},
			TokenUsage: &model.TokenUsage{PromptTokens: 1000, CompletionTokens: 200},
		},
	}
	stream := schema.StreamReaderFromArray(chunks)

	h := wlogeino.Handler()
	h.OnEndWithStreamOutput(ctx, runInfo("OpenAI"), stream)

	deadline := time.Now().Add(time.Second)
	for rec.Count() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	end()

	group, _ := rec.Last()["llm"].(map[string]any)
	if group["input_tokens"] != int64(1000) || group["output_tokens"] != int64(200) {
		t.Fatalf("stream tokens = %v, want input 1000, output 200", group)
	}
	finishReasons, _ := group["finish_reasons"].([]any)
	if len(finishReasons) != 1 || finishReasons[0] != "stop" {
		t.Errorf("finish_reasons = %v, want [stop]", group["finish_reasons"])
	}
	if group["streamed"] != true {
		t.Errorf("streamed = %v, want true", group["streamed"])
	}
}

// TestEino_Handler_ProviderFallback proves a RunInfo with no Type names the provider
// "eino". The framework always supplies a RunInfo, but a component implementation can
// leave Type empty.
func TestEino_Handler_ProviderFallback(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")

	h := wlogeino.Handler()
	h.OnEnd(ctx, runInfo(""), &model.CallbackOutput{Message: &schema.Message{Content: "hi"}})
	end()

	group, _ := rec.Last()["llm"].(map[string]any)
	if group["provider"] != "eino" {
		t.Errorf("provider = %v, want eino", group["provider"])
	}
}
