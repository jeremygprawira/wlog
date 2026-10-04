package llm_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/llm"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestLLM_L15_AddsKeepEveryOutput proves later Add calls do not drop earlier output.
func TestLLM_L15_AddsKeepEveryOutput(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "chat")
	for _, text := range []string{"one", "two"} {
		llm.Add(ctx, llm.Record{
			Provider:  "openai",
			Operation: "chat",
			Content: &llm.Content{OutputMessages: []llm.Message{{
				Role:  "assistant",
				Parts: []llm.Part{{Type: "text", Content: text}},
			}}},
		})
	}
	end()
	body, err := json.Marshal(rec.Last())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(body), "one") || !strings.Contains(string(body), "two") {
		t.Fatalf("output = %s, want both calls", body)
	}
}
