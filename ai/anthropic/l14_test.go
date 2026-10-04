package wloganthropic_test

import (
	"testing"

	wloganthropic "github.com/jeremygprawira/wlog/ai/anthropic"
)

// TestAnthropic_L14_StreamStampsTiming proves a stream sets Streamed, the first
// chunk time, and the duration.
func TestAnthropic_L14_StreamStampsTiming(t *testing.T) {
	events := []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_01\",\"model\":\"claude-sonnet-4-6\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\n",
	}
	observer := wloganthropic.Observe(streamOf(t, events...))
	for observer.Next() {
	}
	got := observer.Record()
	if !got.Streamed || got.TimeToFirstToken <= 0 || got.Duration <= 0 {
		t.Fatalf("streamed %v first %s duration %s, want a stamp on each", got.Streamed, got.TimeToFirstToken, got.Duration)
	}
}
