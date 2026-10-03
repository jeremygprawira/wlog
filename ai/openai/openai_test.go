package wlogopenai_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/responses"

	"github.com/jeremygprawira/wlog"
	wlogopenai "github.com/jeremygprawira/wlog/ai/openai"
	"github.com/jeremygprawira/wlog/llm"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// chatCompletion returns the recorded Chat Completions response.
func chatCompletion(t *testing.T) *openai.ChatCompletion {
	t.Helper()
	body, err := os.ReadFile("testdata/chat_completion.json")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var r openai.ChatCompletion
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	return &r
}

// response returns the recorded Responses response.
func response(t *testing.T) *responses.Response {
	t.Helper()
	body, err := os.ReadFile("testdata/response.json")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var r responses.Response
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	return &r
}

// TestOpenAI_FromChatCompletion proves the typed helper matches the golden.
func TestOpenAI_FromChatCompletion(t *testing.T) {
	got := wlogopenai.FromChatCompletion(chatCompletion(t))
	check(t, got, llm.Record{
		Provider:              "openai",
		Model:                 "gpt-4o",
		Operation:             "chat",
		ResponseID:            "chatcmpl_01",
		InputTokens:           1000,
		CachedInputTokens:     500,
		CacheWriteInputTokens: 100,
		OutputTokens:          200,
		ReasoningTokens:       40,
		FinishReason:          "tool_calls",
		ToolCalls:             []llm.ToolCall{{Name: "get_weather"}},
	})
}

// TestOpenAI_FromResponse proves the Responses helper matches the golden.
func TestOpenAI_FromResponse(t *testing.T) {
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

// TestOpenAI_ContentOptIn proves no text reaches the record by default.
func TestOpenAI_ContentOptIn(t *testing.T) {
	if got := wlogopenai.FromChatCompletion(chatCompletion(t)); got.Content != nil {
		t.Error("content reached the record without WithContent")
	}
	got := wlogopenai.FromChatCompletion(chatCompletion(t), wlogopenai.WithContent())
	if got.Content == nil {
		t.Fatal("WithContent wrote no content")
	}
	if len(got.Content.OutputMessages) != 2 {
		t.Fatalf("output messages = %d, want 2", len(got.Content.OutputMessages))
	}

	chat := wlogopenai.FromResponse(response(t), wlogopenai.WithContent())
	if chat.Content == nil {
		t.Fatal("WithContent wrote no content for the Responses shape")
	}
}

// TestOpenAI_ObserveChat proves the chat stream gives the same record as the response.
func TestOpenAI_ObserveChat(t *testing.T) {
	chunks := []string{
		"data: {\"id\":\"chatcmpl_01\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"get_weather\"}}]},\"finish_reason\":\"\"}]}\n\n",
		"data: {\"id\":\"chatcmpl_01\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":1000,\"completion_tokens\":200,\"prompt_tokens_details\":{\"cached_tokens\":500,\"cache_write_tokens\":100},\"completion_tokens_details\":{\"reasoning_tokens\":40}}}\n\n",
		"data: [DONE]\n\n",
	}
	observer := wlogopenai.ObserveChat(chatStream(t, chunks...))
	read := 0
	for observer.Next() {
		read++
		_ = observer.Current()
	}
	if err := observer.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if read != 2 {
		t.Errorf("the caller read %d chunks, want 2", read)
	}
	check(t, observer.Record(), llm.Record{
		Provider:              "openai",
		Model:                 "gpt-4o",
		Operation:             "chat",
		ResponseID:            "chatcmpl_01",
		InputTokens:           1000,
		CachedInputTokens:     500,
		CacheWriteInputTokens: 100,
		OutputTokens:          200,
		ReasoningTokens:       40,
		FinishReason:          "tool_calls",
		ToolCalls:             []llm.ToolCall{{Name: "get_weather"}},
	})
}

// TestOpenAI_ObserveResponses proves the terminal event gives the same record as the
// response.
func TestOpenAI_ObserveResponses(t *testing.T) {
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
	event := "data: {\"type\":\"response.completed\",\"response\":" + string(compact) + "}\n\n"
	observer := wlogopenai.ObserveResponses(responseStream(t, event))
	for observer.Next() {
		_ = observer.Current()
	}
	if err := observer.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}
	check(t, observer.Record(), llm.Record{
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

// TestOpenAI_Middleware proves the middleware records the request id on the event.
func TestOpenAI_Middleware(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://example.invalid", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp := &http.Response{
		Header: http.Header{"X-Request-Id": []string{"req_1"}},
		Body:   io.NopCloser(strings.NewReader("")),
	}
	next := func(*http.Request) (*http.Response, error) { return resp, nil }
	if _, err := wlogopenai.Middleware()(req, next); err != nil {
		t.Fatalf("middleware: %v", err)
	}
	end()

	group, _ := rec.Last()["llm"].(map[string]any)
	ids, _ := group["request_ids"].([]any)
	if len(ids) != 1 || ids[0] != "req_1" {
		t.Errorf("request_ids = %v, want [req_1]", group["request_ids"])
	}
}

// chatStream builds a Chat Completions stream from SSE event text.
func chatStream(t *testing.T, chunks ...string) *ssestream.Stream[openai.ChatCompletionChunk] {
	t.Helper()
	return ssestream.NewStream[openai.ChatCompletionChunk](decoder(strings.Join(chunks, "")), nil)
}

// responseStream builds a Responses stream from SSE event text.
func responseStream(t *testing.T, events ...string) *ssestream.Stream[responses.ResponseStreamEventUnion] {
	t.Helper()
	return ssestream.NewStream[responses.ResponseStreamEventUnion](decoder(strings.Join(events, "")), nil)
}

// decoder returns an SSE decoder over one body.
func decoder(body string) ssestream.Decoder {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	return ssestream.NewDecoder(resp)
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
