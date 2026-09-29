package wlogopenai_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	openai "github.com/sashabaranov/go-openai"

	"github.com/jeremygprawira/wlog"
	wlogopenai "github.com/jeremygprawira/wlog/ai/goopenai"
	"github.com/jeremygprawira/wlog/llm"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// chatCompletion returns the recorded Chat Completions response.
func chatCompletion(t *testing.T) *openai.ChatCompletionResponse {
	t.Helper()
	body, err := os.ReadFile("testdata/chat_completion.json")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var r openai.ChatCompletionResponse
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	return &r
}

// response returns the recorded Responses response.
func response(t *testing.T) *openai.CreateResponseResponse {
	t.Helper()
	body, err := os.ReadFile("testdata/response.json")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var r openai.CreateResponseResponse
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	return &r
}

// TestGoOpenAI_FromChatCompletionResponse proves the typed helper matches the golden. This
// SDK reports no cache write count for Chat, so CacheWriteInputTokens stays 0.
func TestGoOpenAI_FromChatCompletionResponse(t *testing.T) {
	got := wlogopenai.FromChatCompletionResponse(chatCompletion(t))
	check(t, got, llm.Record{
		Provider:          "openai",
		Model:             "gpt-4o",
		Operation:         "chat",
		ResponseID:        "chatcmpl_01",
		InputTokens:       1000,
		CachedInputTokens: 500,
		OutputTokens:      200,
		ReasoningTokens:   40,
		FinishReason:      "tool_calls",
		ToolCalls:         []llm.ToolCall{{Name: "get_weather"}},
	})
}

// TestGoOpenAI_FromResponse proves the Responses helper matches the golden. This shape
// does report a cache write count, unlike Chat.
func TestGoOpenAI_FromResponse(t *testing.T) {
	got := wlogopenai.FromResponse(response(t))
	check(t, got, llm.Record{
		Provider:              "openai",
		Model:                 "gpt-5.6-sol",
		Operation:             "responses",
		ResponseID:            "resp_01",
		InputTokens:           1000,
		CachedInputTokens:     200,
		CacheWriteInputTokens: 100,
		OutputTokens:          200,
		ReasoningTokens:       50,
		ToolCalls:             []llm.ToolCall{{Name: "get_weather"}},
	})
}

// TestGoOpenAI_ContentOptIn proves no text reaches the record by default.
func TestGoOpenAI_ContentOptIn(t *testing.T) {
	if got := wlogopenai.FromChatCompletionResponse(chatCompletion(t)); got.Content != nil {
		t.Error("content reached the record without WithContent")
	}
	got := wlogopenai.FromChatCompletionResponse(chatCompletion(t), wlogopenai.WithContent())
	if got.Content == nil {
		t.Fatal("WithContent wrote no content")
	}
	if len(got.Content.OutputMessages) != 2 {
		t.Fatalf("output messages = %d, want 2", len(got.Content.OutputMessages))
	}

	resp := wlogopenai.FromResponse(response(t), wlogopenai.WithContent())
	if resp.Content == nil {
		t.Fatal("WithContent wrote no content for the Responses shape")
	}
	if len(resp.Content.OutputMessages) != 2 {
		t.Fatalf("output messages = %d, want 2", len(resp.Content.OutputMessages))
	}
}

// TestGoOpenAI_ObserveChat proves the chat stream gives the same record as the response,
// built over a real *openai.ChatCompletionStream fed by a fake HTTPDoer, because the SDK's
// stream reader has no exported constructor.
func TestGoOpenAI_ObserveChat(t *testing.T) {
	chunks := []string{
		`{"id":"chatcmpl_01","model":"gpt-4o","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather"}}]},"finish_reason":""}]}`,
		`{"id":"chatcmpl_01","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1000,"completion_tokens":200,"prompt_tokens_details":{"cached_tokens":500},"completion_tokens_details":{"reasoning_tokens":40}}}`,
	}
	var body strings.Builder
	for _, c := range chunks {
		body.WriteString("data: " + c + "\n\n")
	}
	body.WriteString("data: [DONE]\n\n")

	client := openai.NewClientWithConfig(clientConfig(t, body.String()))
	stream, err := client.CreateChatCompletionStream(context.Background(), openai.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []openai.ChatCompletionMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("CreateChatCompletionStream: %v", err)
	}
	defer stream.Close()

	observer := wlogopenai.ObserveChat(stream)
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
	check(t, observer.Record(), llm.Record{
		Provider:          "openai",
		Model:             "gpt-4o",
		Operation:         "chat",
		ResponseID:        "chatcmpl_01",
		InputTokens:       1000,
		CachedInputTokens: 500,
		OutputTokens:      200,
		ReasoningTokens:   40,
		FinishReason:      "tool_calls",
		ToolCalls:         []llm.ToolCall{{Name: "get_weather"}},
	})
}

// TestGoOpenAI_Doer proves the doer records the request id onto the current event.
func TestGoOpenAI_Doer(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://example.invalid", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Request-Id": []string{"req_1"}},
		Body:       io.NopCloser(strings.NewReader("")),
	}
	next := doerFunc(func(*http.Request) (*http.Response, error) { return resp, nil })

	got, err := wlogopenai.Doer(next).Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if got != resp {
		t.Error("Doer replaced the response the wrapped doer returned")
	}
	end()

	group, _ := rec.Last()["llm"].(map[string]any)
	ids, _ := group["request_ids"].([]any)
	if len(ids) != 1 || ids[0] != "req_1" {
		t.Errorf("request_ids = %v, want [req_1]", group["request_ids"])
	}
}

// doerFunc adapts a function to an openai.HTTPDoer.
type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

// clientConfig returns an openai.ClientConfig whose HTTPDoer always returns body as a 200
// response, so CreateChatCompletionStream builds a real stream without a network call.
func clientConfig(t *testing.T, body string) openai.ClientConfig {
	t.Helper()
	cfg := openai.DefaultConfig("test-key")
	cfg.HTTPClient = doerFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})
	return cfg
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
		"write":     {got.CacheWriteInputTokens, want.CacheWriteInputTokens},
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
