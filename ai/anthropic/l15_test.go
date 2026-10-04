package wloganthropic_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	wloganthropic "github.com/jeremygprawira/wlog/ai/anthropic"
)

// TestAnthropic_L15_BetaContentKeepsArguments proves a beta tool call keeps its arguments.
func TestAnthropic_L15_BetaContentKeepsArguments(t *testing.T) {
	body, err := os.ReadFile("testdata/beta_message.json")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var m anthropic.BetaMessage
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	got := wloganthropic.FromBetaMessage(&m, wloganthropic.WithContent())
	if got.Content == nil || len(got.Content.OutputMessages) == 0 {
		t.Fatal("WithContent wrote no content")
	}
	var args any
	for _, msg := range got.Content.OutputMessages {
		for _, part := range msg.Parts {
			if part.Type == "tool_call" {
				args = part.Arguments
			}
		}
	}
	if args == nil {
		t.Fatal("tool arguments were dropped")
	}
}

// TestAnthropic_L15_ObserveKeepsOutput proves WithContent on a stream keeps the output text.
func TestAnthropic_L15_ObserveKeepsOutput(t *testing.T) {
	events := []string{
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n",
	}
	observer := wloganthropic.Observe(streamOf(t, events...), wloganthropic.WithContent())
	for observer.Next() {
	}
	got := observer.Record()
	if got.Content == nil || len(got.Content.OutputMessages) == 0 {
		t.Fatal("WithContent on Observe wrote no output")
	}
}
