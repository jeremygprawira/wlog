package wlogopenai_test

import (
	"testing"

	wlogopenai "github.com/jeremygprawira/wlog/ai/openai"
)

// TestOpenAI_L15_ObserveKeepsOutput proves WithContent on a chat stream keeps the output text.
func TestOpenAI_L15_ObserveKeepsOutput(t *testing.T) {
	chunks := []string{
		"data: {\"id\":\"chatcmpl_01\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\n",
		"data: [DONE]\n\n",
	}
	observer := wlogopenai.ObserveChat(chatStream(t, chunks...), wlogopenai.WithContent())
	for observer.Next() {
	}
	got := observer.Record()
	if got.Content == nil || len(got.Content.OutputMessages) == 0 {
		t.Fatal("WithContent on ObserveChat wrote no output")
	}
}
