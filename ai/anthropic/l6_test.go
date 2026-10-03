package wloganthropic_test

import (
	"testing"

	wloganthropic "github.com/jeremygprawira/wlog/ai/anthropic"
)

// TestAnthropic_L6_DeltaOverwritesInputCounts proves message_delta replaces the input
// and cache counts it carries, the same way the SDK Accumulate does.
func TestAnthropic_L6_DeltaOverwritesInputCounts(t *testing.T) {
	events := []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_01\",\"model\":\"claude-sonnet-4-6\",\"usage\":{\"input_tokens\":10,\"cache_read_input_tokens\":1,\"cache_creation_input_tokens\":2,\"output_tokens\":1}}}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"input_tokens\":40,\"cache_read_input_tokens\":15,\"cache_creation_input_tokens\":5,\"output_tokens\":9}}\n\n",
	}
	observer := wloganthropic.Observe(streamOf(t, events...))
	for observer.Next() {
	}
	got := observer.Record()
	if got.InputTokens != 60 || got.CachedInputTokens != 15 || got.CacheWriteInputTokens != 5 {
		t.Fatalf("tokens = input %d cache read %d cache write %d, want 60, 15, 5", got.InputTokens, got.CachedInputTokens, got.CacheWriteInputTokens)
	}
}
