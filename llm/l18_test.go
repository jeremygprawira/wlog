package llm_test

import (
	"os"
	"strings"
	"testing"
)

// TestLLM_L18_SchemaHasRecordKeys proves the event schema names the record keys.
func TestLLM_L18_SchemaHasRecordKeys(t *testing.T) {
	body, err := os.ReadFile("../schema/event.v1.json")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := string(body)
	for _, key := range []string{
		"response_id",
		"cache_write_1h_input_tokens",
		"steps",
		"output_tokens_per_second",
		"input_messages",
		"output_messages",
		"request_ids",
		"attempts",
	} {
		if !strings.Contains(text, `"`+key+`"`) {
			t.Errorf("schema missing %s", key)
		}
	}
}
