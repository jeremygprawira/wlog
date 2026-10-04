package wloggoopenai_test

import (
	"context"
	"testing"

	openai "github.com/sashabaranov/go-openai"

	wlogopenai "github.com/jeremygprawira/wlog/ai/goopenai"
)

// TestGoOpenAI_L14_StreamStampsTiming proves a chat stream sets Streamed, the
// first chunk time, and the duration.
func TestGoOpenAI_L14_StreamStampsTiming(t *testing.T) {
	body := "data: {\"id\":\"chatcmpl_01\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	client := openai.NewClientWithConfig(clientConfig(t, body))
	stream, err := client.CreateChatCompletionStream(context.Background(), openai.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []openai.ChatCompletionMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("CreateChatCompletionStream: %v", err)
	}
	defer stream.Close()
	observer := wlogopenai.ObserveChat(stream)
	for observer.Next() {
	}
	got := observer.Record()
	if !got.Streamed || got.TimeToFirstToken <= 0 || got.Duration <= 0 {
		t.Fatalf("streamed %v first %s duration %s, want a stamp on each", got.Streamed, got.TimeToFirstToken, got.Duration)
	}
}
