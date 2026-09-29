// This file answers one question through a fake Messages API and compares the event with
// the recipe's hand-written golden. The schema tool validates the golden against
// schema/event.v1.json.
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/internal/conformance"
	"github.com/jeremygprawira/wlog/llm"
)

// response is the fake Messages API response: 1000 input tokens and 200 output tokens,
// so the golden's cost comes out in round micros.
const response = `{
	"id": "msg_01ABC123",
	"type": "message",
	"role": "assistant",
	"model": "claude-sonnet-4-6",
	"content": [{"type": "text", "text": "Paris."}],
	"stop_reason": "end_turn",
	"usage": {"input_tokens": 1000, "output_tokens": 200}
}`

// TestLLMAgent_GoldenEvent proves that one answered question gives the event the recipe
// documents, with the token counts and the cost of the fake response folded in.
func TestLLMAgent_GoldenEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()

	rec := conformance.NewMemoryRecorder()
	client := anthropic.NewClient(option.WithAPIKey("test"), option.WithBaseURL(server.URL))

	got, err := answer(context.Background(), rec.Logger(wlog.WithEnrichers(llm.Enricher(prices))), client, "What is the capital of France?")
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	if got != "Paris." {
		t.Errorf("answer = %q, want Paris.", got)
	}

	events := rec.Events()
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	want := golden(t)
	gotEvent := conformance.Normalize(events[0])
	if diff := conformance.Diff(conformance.Normalize(want), gotEvent); diff != "" {
		t.Errorf("the event differs from the golden:\n%s", diff)
	}
}

// TestLLMAgent_APIError proves that a failed call ends the event with the error, and
// records no llm group.
func TestLLMAgent_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"api_error","message":"boom"}}`))
	}))
	defer server.Close()

	rec := conformance.NewMemoryRecorder()
	client := anthropic.NewClient(option.WithAPIKey("test"), option.WithBaseURL(server.URL))

	if _, err := answer(context.Background(), rec.Logger(), client, "What is the capital of France?"); err == nil {
		t.Fatal("answer returned no error for a failed call")
	}
	last := rec.Last()
	if last["level"] != "error" {
		t.Errorf("level = %v, want error", last["level"])
	}
	if _, hasLLM := last["llm"]; hasLLM {
		t.Error("a failed call recorded an llm group")
	}
}

// golden reads the recipe's hand-written event.
func golden(t *testing.T) map[string]any {
	t.Helper()
	body, err := os.ReadFile("testdata/event.json")
	if err != nil {
		t.Fatalf("read the golden: %v", err)
	}
	event := map[string]any{}
	if err := json.Unmarshal(body, &event); err != nil {
		t.Fatalf("parse the golden: %v", err)
	}
	return event
}
