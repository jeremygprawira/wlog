package llm_test

import (
	"testing"

	"github.com/jeremygprawira/wlog/llm"
)

// TestLLM_L17_OverCacheUsesTheSonnetRow proves extra cache is clamped to the input
// before the claude-sonnet-4-6 rates are applied.
func TestLLM_L17_OverCacheUsesTheSonnetRow(t *testing.T) {
	cost, ok := llm.DefaultPrices().Cost(llm.Record{
		Model:                   "claude-sonnet-4-6",
		InputTokens:             100,
		CachedInputTokens:       500,
		CacheWriteInputTokens:   500,
		CacheWrite1hInputTokens: 500,
		OutputTokens:            10,
		ReasoningTokens:         50,
	})
	if !ok {
		t.Fatal("Cost refused claude-sonnet-4-6")
	}
	// 100 cache-read tokens at $0.30 per million, and 10 output tokens at $15.
	// The writes do not fit in the input, so they cost nothing.
	if cost.InputMicros != 0 || cost.CacheReadMicros != 30 || cost.CacheWriteMicros != 0 || cost.CacheWrite1hMicros != 0 {
		t.Fatalf("parts = input %d read %d write %d write1h %d, want 0, 30, 0, 0",
			cost.InputMicros, cost.CacheReadMicros, cost.CacheWriteMicros, cost.CacheWrite1hMicros)
	}
	if cost.TotalMicros != 180 {
		t.Fatalf("TotalMicros = %d, want 180", cost.TotalMicros)
	}
}
