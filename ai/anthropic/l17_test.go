package wloganthropic_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	wloganthropic "github.com/jeremygprawira/wlog/ai/anthropic"
)

// TestAnthropic_L17_TokenTable proves cache counts sit inside the input and reasoning
// sits inside the output, on the recorded message.
func TestAnthropic_L17_TokenTable(t *testing.T) {
	body, err := os.ReadFile("testdata/message.json")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var m anthropic.Message
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	got := wloganthropic.FromMessage(&m)
	cache := got.CachedInputTokens + got.CacheWriteInputTokens + got.CacheWrite1hInputTokens
	if cache > got.InputTokens {
		t.Fatalf("cache %d is outside input %d", cache, got.InputTokens)
	}
	if got.ReasoningTokens > got.OutputTokens {
		t.Fatalf("reasoning %d is outside output %d", got.ReasoningTokens, got.OutputTokens)
	}
}

// TestAnthropic_L17_StreamKeepsTextOffTheRecord proves a stream records no output text
// unless the caller passes WithContent.
func TestAnthropic_L17_StreamKeepsTextOffTheRecord(t *testing.T) {
	events := []string{
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"secret-phrase\"}}\n\n",
	}
	observer := wloganthropic.Observe(streamOf(t, events...))
	for observer.Next() {
	}
	got := observer.Record()
	if got.Content != nil {
		t.Fatalf("content = %+v, want none", got.Content)
	}
}
