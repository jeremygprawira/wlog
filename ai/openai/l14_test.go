package wlogopenai_test

import (
	"encoding/json"
	"os"
	"testing"

	wlogopenai "github.com/jeremygprawira/wlog/ai/openai"
)

// TestOpenAI_L14_StreamStampsTiming proves a chat stream and a responses stream
// set Streamed, the first chunk time, and the duration.
func TestOpenAI_L14_StreamStampsTiming(t *testing.T) {
	t.Run("chat", func(t *testing.T) {
		chunks := []string{
			"data: {\"id\":\"chatcmpl_01\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n",
			"data: [DONE]\n\n",
		}
		observer := wlogopenai.ObserveChat(chatStream(t, chunks...))
		for observer.Next() {
		}
		got := observer.Record()
		if !got.Streamed || got.TimeToFirstToken <= 0 || got.Duration <= 0 {
			t.Fatalf("streamed %v first %s duration %s, want a stamp on each", got.Streamed, got.TimeToFirstToken, got.Duration)
		}
	})
	t.Run("responses", func(t *testing.T) {
		body, err := os.ReadFile("testdata/response.json")
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		compact, err := json.Marshal(json.RawMessage(body))
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		event := "data: {\"type\":\"response.completed\",\"response\":" + string(compact) + "}\n\n"
		observer := wlogopenai.ObserveResponses(responseStream(t, event))
		for observer.Next() {
		}
		got := observer.Record()
		if !got.Streamed || got.TimeToFirstToken <= 0 || got.Duration <= 0 {
			t.Fatalf("streamed %v first %s duration %s, want a stamp on each", got.Streamed, got.TimeToFirstToken, got.Duration)
		}
	})
}
