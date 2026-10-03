package wlogenai_test

import (
	"encoding/json"
	"os"
	"testing"

	"google.golang.org/genai"

	wlogenai "github.com/jeremygprawira/wlog/ai/genai"
)

// TestGenai_L17_TokenTable proves cache counts sit inside the input and reasoning
// sits inside the output, on the recorded generateContent response.
func TestGenai_L17_TokenTable(t *testing.T) {
	body, err := os.ReadFile("testdata/generate_content.json")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var resp genai.GenerateContentResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	got := wlogenai.FromGenerateContent(&resp, genai.BackendGeminiAPI)
	if got.CachedInputTokens > got.InputTokens {
		t.Fatalf("cache %d is outside input %d", got.CachedInputTokens, got.InputTokens)
	}
	if got.ReasoningTokens > got.OutputTokens {
		t.Fatalf("reasoning %d is outside output %d", got.ReasoningTokens, got.OutputTokens)
	}
}
