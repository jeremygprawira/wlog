package wloganthropic_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"

	"github.com/jeremygprawira/wlog"
	wloganthropic "github.com/jeremygprawira/wlog/ai/anthropic"
	"github.com/jeremygprawira/wlog/llm"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// message returns the recorded response the tests map.
func message(t *testing.T) *anthropic.Message {
	t.Helper()
	body, err := os.ReadFile("testdata/message.json")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var m anthropic.Message
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	return &m
}

// want returns the hand-written golden record for message().
func want() llm.Record {
	return llm.Record{
		Provider:                "anthropic",
		Model:                   "claude-sonnet-4-6",
		Operation:               "chat",
		ResponseID:              "msg_01",
		InputTokens:             101_250,
		CachedInputTokens:       100_000,
		CacheWriteInputTokens:   1_000,
		CacheWrite1hInputTokens: 200,
		OutputTokens:            200,
		ReasoningTokens:         40,
		FinishReason:            "tool_use",
		ToolCalls:               []llm.ToolCall{{Name: "get_weather"}},
	}
}

// TestAnthropic_FromMessage proves the typed helper matches the golden and the token table.
func TestAnthropic_FromMessage(t *testing.T) {
	got := wloganthropic.FromMessage(message(t))
	check(t, got, want())
}

// TestAnthropic_ContentOptIn proves no text reaches the record by default, and WithContent
// adds it.
func TestAnthropic_ContentOptIn(t *testing.T) {
	if got := wloganthropic.FromMessage(message(t)); got.Content != nil {
		t.Error("content reached the record without WithContent")
	}
	got := wloganthropic.FromMessage(message(t), wloganthropic.WithContent())
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

// TestAnthropic_Observe proves a stream gives the same record as the full response, and the
// caller still reads every chunk.
func TestAnthropic_Observe(t *testing.T) {
	events := []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_01\",\"model\":\"claude-sonnet-4-6\",\"usage\":{\"input_tokens\":50,\"cache_read_input_tokens\":100000,\"cache_creation_input_tokens\":1200,\"cache_creation\":{\"ephemeral_5m_input_tokens\":1000,\"ephemeral_1h_input_tokens\":200},\"output_tokens\":1}}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"get_weather\"}}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":200,\"output_tokens_details\":{\"thinking_tokens\":40}}}\n\n",
	}
	observer := wloganthropic.Observe(streamOf(t, events...))
	chunks := 0
	for observer.Next() {
		chunks++
		_ = observer.Current()
	}
	if err := observer.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if chunks != len(events) {
		t.Errorf("the caller read %d chunks, want %d", chunks, len(events))
	}
	check(t, observer.Record(), want())
}

// TestAnthropic_Middleware proves the middleware reads the request id and the retry count
// onto the current event.
func TestAnthropic_Middleware(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "op")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://example.invalid", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("X-Stainless-Retry-Count", "2")
	resp := &http.Response{
		Header: http.Header{"Request-Id": []string{"req_1"}},
		Body:   io.NopCloser(strings.NewReader("")),
	}
	next := func(*http.Request) (*http.Response, error) { return resp, nil }
	if _, err := wloganthropic.Middleware()(req, next); err != nil {
		t.Fatalf("middleware: %v", err)
	}
	end()

	group, _ := rec.Last()["llm"].(map[string]any)
	ids, _ := group["request_ids"].([]any)
	if len(ids) != 1 || ids[0] != "req_1" {
		t.Errorf("request_ids = %v, want [req_1]", group["request_ids"])
	}
	if group["attempts"] != int64(3) {
		t.Errorf("attempts = %v, want 3", group["attempts"])
	}
}

// streamOf builds a message stream from SSE event text.
func streamOf(t *testing.T, events ...string) *ssestream.Stream[anthropic.MessageStreamEventUnion] {
	t.Helper()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(strings.Join(events, ""))),
	}
	return ssestream.NewStream[anthropic.MessageStreamEventUnion](ssestream.NewDecoder(resp), nil)
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
		"write1h":   {got.CacheWrite1hInputTokens, want.CacheWrite1hInputTokens},
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

// TestAnthropic_FromBetaMessage proves the beta shape maps the same record.
func TestAnthropic_FromBetaMessage(t *testing.T) {
	body, err := os.ReadFile("testdata/beta_message.json")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var m anthropic.BetaMessage
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	got := wloganthropic.FromBetaMessage(&m, wloganthropic.WithContent())
	check(t, got, want())
	if got.Content == nil {
		t.Error("WithContent wrote no content")
	}
}

// TestAnthropic_CacheWriteFallback proves a usage block with no breakdown bills its whole
// cache write at the five minute rate.
func TestAnthropic_CacheWriteFallback(t *testing.T) {
	got := wloganthropic.FromMessage(&anthropic.Message{
		Usage: anthropic.Usage{InputTokens: 10, CacheCreationInputTokens: 500},
	})
	if got.CacheWriteInputTokens != 500 {
		t.Errorf("CacheWriteInputTokens = %d, want 500", got.CacheWriteInputTokens)
	}
	if got.InputTokens != 510 {
		t.Errorf("InputTokens = %d, want 510", got.InputTokens)
	}
}

// TestAnthropic_ObserveTextBlock proves a text content block is not read as a tool call.
func TestAnthropic_ObserveTextBlock(t *testing.T) {
	events := []string{
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n",
	}
	observer := wloganthropic.Observe(streamOf(t, events...))
	for observer.Next() {
	}
	if len(observer.Record().ToolCalls) != 0 {
		t.Errorf("ToolCalls = %v, want none", observer.Record().ToolCalls)
	}
}
