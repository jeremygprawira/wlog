package wlogenai_test

import (
	"testing"

	"google.golang.org/genai"

	wlogenai "github.com/jeremygprawira/wlog/ai/genai"
)

// TestGenai_L16_NilCandidateIsSkipped proves a null candidate does not panic.
func TestGenai_L16_NilCandidateIsSkipped(t *testing.T) {
	resp := &genai.GenerateContentResponse{Candidates: []*genai.Candidate{nil}}
	got := wlogenai.FromGenerateContent(resp, genai.BackendGeminiAPI)
	if got.FinishReason != "" || len(got.ToolCalls) != 0 {
		t.Fatalf("record = %+v, want an empty record", got)
	}
}
