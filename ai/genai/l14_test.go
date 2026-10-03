package wlogenai_test

import (
	"testing"

	"google.golang.org/genai"

	wlogenai "github.com/jeremygprawira/wlog/ai/genai"
)

// TestGenai_L14_StreamStampsTiming proves a stream sets Streamed, the first
// chunk time, and the duration.
func TestGenai_L14_StreamStampsTiming(t *testing.T) {
	chunk := &genai.GenerateContentResponse{ModelVersion: "gemini-2.5-pro", ResponseID: "resp_01"}
	seq, record := wlogenai.Observe(seqOf(chunk), genai.BackendGeminiAPI)
	for _, err := range seq {
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
	}
	got := record()
	if !got.Streamed || got.TimeToFirstToken <= 0 || got.Duration <= 0 {
		t.Fatalf("streamed %v first %s duration %s, want a stamp on each", got.Streamed, got.TimeToFirstToken, got.Duration)
	}
}
