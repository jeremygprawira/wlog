package wlogenai_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"google.golang.org/genai"

	"github.com/jeremygprawira/wlog"
	wlogenai "github.com/jeremygprawira/wlog/ai/genai"
	"github.com/jeremygprawira/wlog/llm"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// response returns the recorded response the tests map.
func response(t *testing.T) *genai.GenerateContentResponse {
	t.Helper()
	body, err := os.ReadFile("testdata/generate_content.json")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var r genai.GenerateContentResponse
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	return &r
}

// want returns the hand-written golden record for response().
func want() llm.Record {
	return llm.Record{
		Provider:          "gcp.gemini",
		Model:             "gemini-2.5-pro",
		Operation:         "generate_content",
		ResponseID:        "resp_01",
		InputTokens:       1000,
		CachedInputTokens: 100,
		OutputTokens:      200,
		ReasoningTokens:   50,
		FinishReason:      "STOP",
		ToolCalls:         []llm.ToolCall{{Name: "get_weather"}},
	}
}

// TestGenAI_FromGenerateContent proves the typed helper matches the golden and the token
// table: the tool-use prompt count joins the input, and the thoughts count joins the
// output and doubles as the reasoning count.
func TestGenAI_FromGenerateContent(t *testing.T) {
	got := wlogenai.FromGenerateContent(response(t), genai.BackendGeminiAPI)
	check(t, got, want())
}

// TestGenAI_VertexProvider proves the backend picks the provider name.
func TestGenAI_VertexProvider(t *testing.T) {
	got := wlogenai.FromGenerateContent(response(t), genai.BackendVertexAI)
	if got.Provider != "gcp.vertex_ai" {
		t.Errorf("Provider = %q, want gcp.vertex_ai", got.Provider)
	}
}

// TestGenAI_ContentOptIn proves no text reaches the record by default, and WithContent
// adds it.
func TestGenAI_ContentOptIn(t *testing.T) {
	if got := wlogenai.FromGenerateContent(response(t), genai.BackendGeminiAPI); got.Content != nil {
		t.Error("content reached the record without WithContent")
	}
	got := wlogenai.FromGenerateContent(response(t), genai.BackendGeminiAPI, wlogenai.WithContent())
	if got.Content == nil {
		t.Fatal("WithContent wrote no content")
	}
	if len(got.Content.OutputMessages) != 2 {
		t.Fatalf("output messages = %d, want 2", len(got.Content.OutputMessages))
	}
	if got.Content.OutputMessages[0].Parts[0].Content != "hello" {
		t.Errorf("the text part is missing")
	}
	if got.Content.OutputMessages[1].Parts[0].Name != "get_weather" {
		t.Errorf("the tool call part is missing")
	}
}

// TestGenAI_Observe proves a stream gives the same record as the full response, the
// caller still reads every chunk, and the last non-nil usage wins over an earlier one.
func TestGenAI_Observe(t *testing.T) {
	chunks := []*genai.GenerateContentResponse{
		{
			ModelVersion: "gemini-2.5-pro",
			ResponseID:   "resp_01",
			Candidates: []*genai.Candidate{{
				Content: &genai.Content{Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "get_weather"}}}},
			}},
			UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
				PromptTokenCount:     700,
				CandidatesTokenCount: 10,
			},
		},
		{
			Candidates: []*genai.Candidate{{FinishReason: genai.FinishReasonStop}},
			UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
				PromptTokenCount:        800,
				ToolUsePromptTokenCount: 200,
				CachedContentTokenCount: 100,
				CandidatesTokenCount:    150,
				ThoughtsTokenCount:      50,
			},
		},
	}
	observer := wlogenai.Observe(seqOf(chunks...), genai.BackendGeminiAPI)
	read := 0
	for observer.Next() {
		read++
		_ = observer.Current()
	}
	if err := observer.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if read != len(chunks) {
		t.Errorf("the caller read %d chunks, want %d", read, len(chunks))
	}
	check(t, observer.Record(), want())
}

// TestGenAI_ObserveErr proves a stream error stops the observer and Err reports it.
func TestGenAI_ObserveErr(t *testing.T) {
	wantErr := context.Canceled
	seq := func(yield func(*genai.GenerateContentResponse, error) bool) {
		if !yield(&genai.GenerateContentResponse{ResponseID: "resp_01"}, nil) {
			return
		}
		yield(nil, wantErr)
	}
	observer := wlogenai.Observe(seq, genai.BackendGeminiAPI)
	read := 0
	for observer.Next() {
		read++
	}
	if read != 1 {
		t.Errorf("the caller read %d chunks, want 1", read)
	}
	if !errors.Is(observer.Err(), wantErr) {
		t.Errorf("Err() = %v, want %v", observer.Err(), wantErr)
	}
}

// TestGenAI_Transport proves the transport counts one attempt per round trip on the
// current event, and never replaces what the wrapped round tripper returns.
func TestGenAI_Transport(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://example.invalid", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(""))}
	next := roundTripFunc(func(*http.Request) (*http.Response, error) { return resp, nil })

	rt := wlogenai.Transport(next)
	got, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if got != resp {
		t.Error("Transport replaced the response the wrapped round tripper returned")
	}
	if _, err := rt.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	end()

	group, _ := rec.Last()["llm"].(map[string]any)
	if group["attempts"] != int64(2) {
		t.Errorf("attempts = %v, want 2", group["attempts"])
	}
}

// roundTripFunc adapts a function to an http.RoundTripper, the way http.HandlerFunc
// adapts a function to an http.Handler.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// seqOf returns an iter.Seq2 that yields each of chunks in order, with a nil error.
func seqOf(chunks ...*genai.GenerateContentResponse) func(func(*genai.GenerateContentResponse, error) bool) {
	return func(yield func(*genai.GenerateContentResponse, error) bool) {
		for _, c := range chunks {
			if !yield(c, nil) {
				return
			}
		}
	}
}

// check compares two records field by field.
func check(t *testing.T, got, want llm.Record) {
	t.Helper()
	if got.Provider != want.Provider || got.Model != want.Model || got.Operation != want.Operation {
		t.Errorf("identity = %s/%s/%s, want %s/%s/%s", got.Provider, got.Model, got.Operation, want.Provider, want.Model, want.Operation)
	}
	if got.ResponseID != want.ResponseID {
		t.Errorf("ResponseID = %q, want %q", got.ResponseID, want.ResponseID)
	}
	for name, pair := range map[string][2]int{
		"input":     {got.InputTokens, want.InputTokens},
		"read":      {got.CachedInputTokens, want.CachedInputTokens},
		"output":    {got.OutputTokens, want.OutputTokens},
		"reasoning": {got.ReasoningTokens, want.ReasoningTokens},
	} {
		if pair[0] != pair[1] {
			t.Errorf("%s = %d, want %d", name, pair[0], pair[1])
		}
	}
	if got.FinishReason != want.FinishReason {
		t.Errorf("FinishReason = %q, want %q", got.FinishReason, want.FinishReason)
	}
	if len(got.ToolCalls) != len(want.ToolCalls) {
		t.Fatalf("ToolCalls = %v, want %v", got.ToolCalls, want.ToolCalls)
	}
	for i := range got.ToolCalls {
		if got.ToolCalls[i].Name != want.ToolCalls[i].Name {
			t.Errorf("ToolCalls[%d].Name = %q, want %q", i, got.ToolCalls[i].Name, want.ToolCalls[i].Name)
		}
	}
}
