package wlogopenai_test

import (
	"testing"

	wlogopenai "github.com/jeremygprawira/wlog/ai/goopenai"
)

// TestGoOpenAI_L17_TokenTable proves cache counts sit inside the input and reasoning
// sits inside the output, on the recorded chat completion.
func TestGoOpenAI_L17_TokenTable(t *testing.T) {
	got := wlogopenai.FromChatCompletionResponse(chatCompletion(t))
	cache := got.CachedInputTokens + got.CacheWriteInputTokens
	if cache > got.InputTokens {
		t.Fatalf("cache %d is outside input %d", cache, got.InputTokens)
	}
	if got.ReasoningTokens > got.OutputTokens {
		t.Fatalf("reasoning %d is outside output %d", got.ReasoningTokens, got.OutputTokens)
	}
}
