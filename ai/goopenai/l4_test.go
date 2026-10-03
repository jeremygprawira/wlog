package wlogopenai_test

import (
	"testing"

	openai "github.com/sashabaranov/go-openai"

	wlogopenai "github.com/jeremygprawira/wlog/ai/goopenai"
)

// TestGoopenai_L4_IncludeUsage proves the helper turns usage on for a chat stream.
func TestGoopenai_L4_IncludeUsage(t *testing.T) {
	var req openai.ChatCompletionRequest
	wlogopenai.WithIncludeUsage(&req)
	if req.StreamOptions == nil || !req.StreamOptions.IncludeUsage {
		t.Fatal("WithIncludeUsage left include_usage off")
	}
}
