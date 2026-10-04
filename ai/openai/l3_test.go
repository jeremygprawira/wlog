package wlogopenai_test

import (
	"encoding/json"
	"os"
	"testing"

	wlogopenai "github.com/jeremygprawira/wlog/ai/openai"
)

// TestOpenAI_L3_IncompleteAndFailedFillRecord proves a cut or failed response still fills the record.
func TestOpenAI_L3_IncompleteAndFailedFillRecord(t *testing.T) {
	body, err := os.ReadFile("testdata/response.json")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	compact, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, kind := range []string{"response.incomplete", "response.failed"} {
		t.Run(kind, func(t *testing.T) {
			event := "data: {\"type\":\"" + kind + "\",\"response\":" + string(compact) + "}\n\n"
			observer := wlogopenai.ObserveResponses(responseStream(t, event))
			for observer.Next() {
			}
			got := observer.Record()
			if got.InputTokens == 0 || got.Model == "" || got.ResponseID == "" {
				t.Fatalf("record = %+v, want the response fields", got)
			}
		})
	}
}
