package wlogopenai_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"

	wlogopenai "github.com/jeremygprawira/wlog/ai/openai"
)

// TestOpenAI_L4_NoUsageMarksUnknown proves a stream with no usage is not a measured zero.
func TestOpenAI_L4_NoUsageMarksUnknown(t *testing.T) {
	chunk := "data: {\"id\":\"chatcmpl_01\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"
	observer := wlogopenai.ObserveChat(chatStream(t, chunk, "data: [DONE]\n\n"))
	for observer.Next() {
	}
	if !observer.Record().UsageUnknown {
		t.Fatal("a stream with no usage left usage_unknown unset")
	}

	var params openai.ChatCompletionNewParams
	wlogopenai.WithIncludeUsage(&params)
	body, err := json.Marshal(params.StreamOptions)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(body), `"include_usage":true`) {
		t.Fatalf("stream options = %s, want include_usage true", body)
	}
}
