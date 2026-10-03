package wloggoopenai_test

import (
	"context"
	"testing"

	openai "github.com/sashabaranov/go-openai"

	wlogopenai "github.com/jeremygprawira/wlog/ai/goopenai"
)

// TestGoOpenAI_L15_ObserveKeepsOutput proves WithContent on a chat stream keeps the output text.
func TestGoOpenAI_L15_ObserveKeepsOutput(t *testing.T) {
	body := "data: {\"id\":\"chatcmpl_01\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\ndata: [DONE]\n\n"
	client := openai.NewClientWithConfig(clientConfig(t, body))
	stream, err := client.CreateChatCompletionStream(context.Background(), openai.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []openai.ChatCompletionMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("CreateChatCompletionStream: %v", err)
	}
	defer stream.Close()
	observer := wlogopenai.ObserveChat(stream, wlogopenai.WithContent())
	for observer.Next() {
	}
	got := observer.Record()
	if got.Content == nil || len(got.Content.OutputMessages) == 0 {
		t.Fatal("WithContent on ObserveChat wrote no output")
	}
}
