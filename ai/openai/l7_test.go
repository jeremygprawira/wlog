package wlogopenai_test

import (
	"testing"

	wlogopenai "github.com/jeremygprawira/wlog/ai/openai"
)

// TestOpenAI_L7_FromResponseCacheWrite proves FromResponse keeps cache write tokens.
func TestOpenAI_L7_FromResponseCacheWrite(t *testing.T) {
	got := wlogopenai.FromResponse(response(t))
	if got.CacheWriteInputTokens != 100 {
		t.Fatalf("CacheWriteInputTokens = %d, want 100", got.CacheWriteInputTokens)
	}
}
