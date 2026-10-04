package llm_test

import (
	"testing"

	"github.com/jeremygprawira/wlog/llm"
)

// TestLLM_L22_KeepsFirstFieldsAndSkipsZeros proves Add keeps the first call's
// model and leaves zero token counts off the event.
func TestLLM_L22_KeepsFirstFieldsAndSkipsZeros(t *testing.T) {
	ctx, rec, end := start(t)
	llm.Add(ctx, llm.Record{Model: "first", Operation: "chat"})
	llm.Add(ctx, llm.Record{Model: "second", InputTokens: 1})
	end()

	group := llmGroup(t, rec)
	if _, ok := group["output_tokens"]; ok {
		t.Fatalf("output_tokens = %v, want it left off", group["output_tokens"])
	}
	if group["request_model"] != "first" {
		t.Fatalf("request_model = %v, want the first call", group["request_model"])
	}
	if group["operation"] != "chat" {
		t.Fatalf("operation = %v, want the first call", group["operation"])
	}
}
