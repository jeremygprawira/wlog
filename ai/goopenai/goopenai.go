// Package wlogopenai turns a github.com/sashabaranov/go-openai response or stream into an
// llm.Record. It maps both the Chat Completions shape and the Responses shape.
//
// FromChatCompletionResponse and FromResponse map a whole response. ObserveChat wraps a
// stream, so the caller still reads every chunk and still builds the same Record. Doer
// records the request id from the response headers.
//
// No helper keeps the prompt, the completion, or the tool payload unless the caller passes
// WithContent. Core redacts those values like any other.
package wlogopenai

import (
	"encoding/json"
	"io"
	"net/http"

	openai "github.com/sashabaranov/go-openai"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/llm"
)

// provider is the OpenTelemetry gen_ai.provider.name value.
const provider = "openai"

// Option configures a helper.
type Option func(*config)

// config holds the resolved options.
type config struct {
	content bool
}

// WithContent opts into the prompt, the completion, and the tool payload. Without it, the
// record holds the shape of the call and no text. Core redacts the values.
func WithContent() Option { return func(c *config) { c.content = true } }

// resolve applies the options.
func resolve(opts ...Option) config {
	c := config{}
	for _, o := range opts {
		o(&c)
	}
	return c
}

// FromChatCompletionResponse turns one Chat Completions response into an llm.Record. This
// SDK's PromptTokensDetails has no cache write count, so CacheWriteInputTokens stays 0 for
// this shape.
func FromChatCompletionResponse(r *openai.ChatCompletionResponse, opts ...Option) llm.Record {
	if r == nil {
		return llm.Record{}
	}
	record := llm.Record{
		Provider:     provider,
		Model:        r.Model,
		Operation:    "chat",
		ResponseID:   r.ID,
		InputTokens:  r.Usage.PromptTokens,
		OutputTokens: r.Usage.CompletionTokens,
	}
	if r.Usage.PromptTokensDetails != nil {
		record.CachedInputTokens = r.Usage.PromptTokensDetails.CachedTokens
	}
	if r.Usage.CompletionTokensDetails != nil {
		record.ReasoningTokens = r.Usage.CompletionTokensDetails.ReasoningTokens
	}
	if len(r.Choices) > 0 {
		record.FinishReason = string(r.Choices[0].FinishReason)
		for _, call := range r.Choices[0].Message.ToolCalls {
			record.ToolCalls = append(record.ToolCalls, llm.ToolCall{Name: call.Function.Name})
		}
	}
	if resolve(opts...).content {
		record.Content = chatContentOf(r)
	}
	return record
}

// FromResponse turns one Responses API response into an llm.Record. The Responses API
// reports no finish reason, so FinishReason stays empty for this shape.
func FromResponse(r *openai.CreateResponseResponse, opts ...Option) llm.Record {
	if r == nil {
		return llm.Record{}
	}
	record := llm.Record{
		Provider:   provider,
		Model:      r.Model,
		Operation:  "responses",
		ResponseID: r.ID,
	}
	if r.Usage != nil {
		record.InputTokens = r.Usage.InputTokens
		record.OutputTokens = r.Usage.OutputTokens
		if r.Usage.InputTokensDetails != nil {
			record.CachedInputTokens = r.Usage.InputTokensDetails.CachedTokens
			record.CacheWriteInputTokens = r.Usage.InputTokensDetails.CacheWriteTokens
		}
		if r.Usage.OutputTokensDetails != nil {
			record.ReasoningTokens = r.Usage.OutputTokensDetails.ReasoningTokens
		}
	}
	for _, item := range outputItemsOf(r.Output) {
		if item.Type == "function_call" && item.Name != "" {
			record.ToolCalls = append(record.ToolCalls, llm.ToolCall{Name: item.Name})
		}
	}
	if resolve(opts...).content {
		record.Content = responseContentOf(r.Output)
	}
	return record
}

// outputItemsOf decodes the Responses API's untyped Output slice into ResponseOutputItem,
// the way CreateResponseResponse.GetOutputText does. An item that fails to decode is
// skipped, never turned into a partial or a wrong tool call.
func outputItemsOf(raw []any) []openai.ResponseOutputItem {
	items := make([]openai.ResponseOutputItem, 0, len(raw))
	for _, entry := range raw {
		data, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		var item openai.ResponseOutputItem
		if err := json.Unmarshal(data, &item); err != nil {
			continue
		}
		items = append(items, item)
	}
	return items
}

// chatContentOf maps the assistant message to the opt-in gen_ai shape.
func chatContentOf(r *openai.ChatCompletionResponse) *llm.Content {
	content := &llm.Content{}
	for _, choice := range r.Choices {
		if choice.Message.Content != "" {
			content.OutputMessages = append(content.OutputMessages, llm.Message{
				Role:  "assistant",
				Parts: []llm.Part{{Type: "text", Content: choice.Message.Content}},
			})
		}
		for _, call := range choice.Message.ToolCalls {
			content.OutputMessages = append(content.OutputMessages, llm.Message{
				Role:  "assistant",
				Parts: []llm.Part{{Type: "tool_call", ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments}},
			})
		}
	}
	if len(content.OutputMessages) == 0 {
		return nil
	}
	return content
}

// responseContentOf maps the output items to the opt-in gen_ai shape.
func responseContentOf(raw []any) *llm.Content {
	content := &llm.Content{}
	for _, item := range outputItemsOf(raw) {
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				if part.Type == "output_text" && part.Text != "" {
					content.OutputMessages = append(content.OutputMessages, llm.Message{
						Role:  "assistant",
						Parts: []llm.Part{{Type: "text", Content: part.Text}},
					})
				}
			}
		case "function_call":
			content.OutputMessages = append(content.OutputMessages, llm.Message{
				Role:  "assistant",
				Parts: []llm.Part{{Type: "tool_call", ID: item.CallID, Name: item.Name, Arguments: item.Arguments}},
			})
		}
	}
	if len(content.OutputMessages) == 0 {
		return nil
	}
	return content
}

// ChatObserver wraps a Chat Completions stream and builds a Record as the caller reads it.
// It never reads ahead, so the caller keeps every chunk.
type ChatObserver struct {
	stream  *openai.ChatCompletionStream
	current openai.ChatCompletionStreamResponse
	record  llm.Record
	err     error
}

// ObserveChat wraps stream. The caller drives it with Next and Current, and reads Record
// once Next reports false.
func ObserveChat(stream *openai.ChatCompletionStream, opts ...Option) *ChatObserver {
	_ = resolve(opts...)
	return &ChatObserver{stream: stream, record: llm.Record{Provider: provider, Operation: "chat"}}
}

// Next reads the next chunk and folds it into the record. It reports false at the end of
// the stream or on a stream error, which Err then reports.
func (o *ChatObserver) Next() bool {
	resp, err := o.stream.Recv()
	if err != nil {
		if err != io.EOF {
			o.err = err
		}
		return false
	}
	o.current = resp
	o.consume(resp)
	return true
}

// Current returns the chunk the caller just read.
func (o *ChatObserver) Current() openai.ChatCompletionStreamResponse { return o.current }

// Record returns the record built so far.
func (o *ChatObserver) Record() llm.Record { return o.record }

// Err returns the stream error, if any. The end of stream is not an error.
func (o *ChatObserver) Err() error { return o.err }

// consume folds one chunk into the record: the identity, the token counts, the finish
// reason, and the tool names. It never reads the text.
func (o *ChatObserver) consume(chunk openai.ChatCompletionStreamResponse) {
	if chunk.ID != "" {
		o.record.ResponseID = chunk.ID
	}
	if chunk.Model != "" {
		o.record.Model = chunk.Model
	}
	if chunk.Usage != nil {
		o.record.InputTokens = chunk.Usage.PromptTokens
		o.record.OutputTokens = chunk.Usage.CompletionTokens
		if chunk.Usage.PromptTokensDetails != nil {
			o.record.CachedInputTokens = chunk.Usage.PromptTokensDetails.CachedTokens
		}
		if chunk.Usage.CompletionTokensDetails != nil {
			o.record.ReasoningTokens = chunk.Usage.CompletionTokensDetails.ReasoningTokens
		}
	}
	for _, choice := range chunk.Choices {
		if choice.FinishReason != "" {
			o.record.FinishReason = string(choice.FinishReason)
		}
		for _, call := range choice.Delta.ToolCalls {
			if call.Function.Name != "" {
				o.record.ToolCalls = append(o.record.ToolCalls, llm.ToolCall{Name: call.Function.Name})
			}
		}
	}
}

// Doer wraps next, so every call records the request id from the response headers onto
// the llm group of the current event. A nil next means &http.Client{}. Pass the result as
// openai.ClientConfig.HTTPClient.
func Doer(next openai.HTTPDoer) openai.HTTPDoer {
	if next == nil {
		next = &http.Client{}
	}
	return &doer{next: next}
}

// doer records the request id around the wrapped HTTPDoer.
type doer struct{ next openai.HTTPDoer }

// Do records the request id and returns exactly what the wrapped HTTPDoer returned.
func (d *doer) Do(req *http.Request) (*http.Response, error) {
	resp, err := d.next.Do(req)
	if resp != nil {
		if id := resp.Header.Get("x-request-id"); id != "" {
			wlog.SetGroup(req.Context(), "llm", map[string]any{"request_ids": []any{id}})
		}
	}
	return resp, err
}
