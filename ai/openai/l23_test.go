package wlogopenai_test

import (
	"testing"

	wlogopenai "github.com/jeremygprawira/wlog/ai/openai"
)

// TestOpenAI_L23_ToolCallsFromEveryChoice proves a later choice's tool call is kept.
func TestOpenAI_L23_ToolCallsFromEveryChoice(t *testing.T) {
	chat := chatCompletion(t)
	second := chat.Choices[0]
	call := second.Message.ToolCalls[0]
	call.Function.Name = "get_time"
	second.Message.ToolCalls = append(second.Message.ToolCalls[:0:0], call)
	chat.Choices = append(chat.Choices, second)
	got := wlogopenai.FromChatCompletion(chat)
	if len(got.ToolCalls) != 2 || got.ToolCalls[0].Name != "get_weather" || got.ToolCalls[1].Name != "get_time" {
		t.Fatalf("ToolCalls = %v, want get_weather and get_time", got.ToolCalls)
	}
}
