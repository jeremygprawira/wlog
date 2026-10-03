// Package wlogopenai turns an OpenAI SDK response or stream into an llm.Record. It maps
// both the Chat Completions shape and the Responses shape.
//
// FromChatCompletion and FromResponse map a whole response. ObserveChat and
// ObserveResponses wrap a stream, so the caller still reads every chunk and still builds the
// same Record. Middleware records the request id.
//
// No helper keeps the prompt, the completion, or the tool payload unless the caller passes
// WithContent. Core redacts those values like any other.
package wlogopenai

import (
	"net/http"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/responses"

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

// FromChatCompletion turns one Chat Completions response into an llm.Record. OpenAI reports
// the prompt count with its cache parts included, so the cache counts map across as subsets.
func FromChatCompletion(r *openai.ChatCompletion, opts ...Option) llm.Record {
	if r == nil {
		return llm.Record{}
	}
	record := llm.Record{
		Provider:              provider,
		Model:                 r.Model,
		Operation:             "chat",
		ResponseID:            r.ID,
		InputTokens:           int(r.Usage.PromptTokens),
		CachedInputTokens:     int(r.Usage.PromptTokensDetails.CachedTokens),
		CacheWriteInputTokens: int(r.Usage.PromptTokensDetails.CacheWriteTokens),
		OutputTokens:          int(r.Usage.CompletionTokens),
		ReasoningTokens:       int(r.Usage.CompletionTokensDetails.ReasoningTokens),
	}
	if len(r.Choices) > 0 {
		record.FinishReason = r.Choices[0].FinishReason
		for _, call := range r.Choices[0].Message.ToolCalls {
			record.ToolCalls = append(record.ToolCalls, llm.ToolCall{Name: call.Function.Name})
		}
	}
	if resolve(opts...).content {
		record.Content = chatContentOf(r)
	}
	return record
}

// FromResponse turns one Responses response into an llm.Record.
func FromResponse(r *responses.Response, opts ...Option) llm.Record {
	if r == nil {
		return llm.Record{}
	}
	record := llm.Record{
		Provider:              provider,
		Model:                 r.Model,
		Operation:             "responses",
		ResponseID:            r.ID,
		InputTokens:           int(r.Usage.InputTokens),
		CachedInputTokens:     int(r.Usage.InputTokensDetails.CachedTokens),
		CacheWriteInputTokens: int(r.Usage.InputTokensDetails.CacheWriteTokens),
		OutputTokens:          int(r.Usage.OutputTokens),
		ReasoningTokens:       int(r.Usage.OutputTokensDetails.ReasoningTokens),
	}
	for _, item := range r.Output {
		if item.Type == "function_call" && item.Name != "" {
			record.ToolCalls = append(record.ToolCalls, llm.ToolCall{Name: item.Name})
		}
	}
	if resolve(opts...).content {
		record.Content = responseContentOf(r)
	}
	return record
}

// chatContentOf maps the assistant message to the opt-in gen_ai shape.
func chatContentOf(r *openai.ChatCompletion) *llm.Content {
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
func responseContentOf(r *responses.Response) *llm.Content {
	content := &llm.Content{}
	for _, item := range r.Output {
		if len(item.Content) == 0 {
			continue
		}
		text := item.Content[0].Text
		if text == "" {
			continue
		}
		content.OutputMessages = append(content.OutputMessages, llm.Message{
			Role:  "assistant",
			Parts: []llm.Part{{Type: "text", Content: text}},
		})
	}
	if len(content.OutputMessages) == 0 {
		return nil
	}
	return content
}

// ChatObserver wraps a Chat Completions stream and builds a Record as the caller reads it.
// It never reads ahead, so the caller keeps every chunk.
type ChatObserver struct {
	stream *ssestream.Stream[openai.ChatCompletionChunk]
	record llm.Record
}

// ObserveChat wraps stream. The caller drives it with Next and Current, and reads Record
// once Next reports false.
func ObserveChat(stream *ssestream.Stream[openai.ChatCompletionChunk], opts ...Option) *ChatObserver {
	_ = resolve(opts...)
	return &ChatObserver{stream: stream, record: llm.Record{Provider: provider, Operation: "chat"}}
}

// Next reads the next chunk and folds it into the record.
func (o *ChatObserver) Next() bool {
	if !o.stream.Next() {
		return false
	}
	o.consume(o.stream.Current())
	return true
}

// Current returns the chunk the caller just read.
func (o *ChatObserver) Current() openai.ChatCompletionChunk { return o.stream.Current() }

// Record returns the record built so far.
func (o *ChatObserver) Record() llm.Record { return o.record }

// Err returns the stream error, if any.
func (o *ChatObserver) Err() error { return o.stream.Err() }

// consume folds one chunk into the record: the identity, the token counts, the finish
// reason, and the tool names. It never reads the text.
func (o *ChatObserver) consume(chunk openai.ChatCompletionChunk) {
	if chunk.ID != "" {
		o.record.ResponseID = chunk.ID
	}
	if chunk.Model != "" {
		o.record.Model = chunk.Model
	}
	if chunk.Usage.PromptTokens > 0 {
		o.record.InputTokens = int(chunk.Usage.PromptTokens)
		o.record.CachedInputTokens = int(chunk.Usage.PromptTokensDetails.CachedTokens)
		o.record.CacheWriteInputTokens = int(chunk.Usage.PromptTokensDetails.CacheWriteTokens)
	}
	if chunk.Usage.CompletionTokens > 0 {
		o.record.OutputTokens = int(chunk.Usage.CompletionTokens)
		o.record.ReasoningTokens = int(chunk.Usage.CompletionTokensDetails.ReasoningTokens)
	}
	for _, choice := range chunk.Choices {
		if choice.FinishReason != "" {
			o.record.FinishReason = choice.FinishReason
		}
		for _, call := range choice.Delta.ToolCalls {
			if call.Function.Name != "" {
				o.record.ToolCalls = append(o.record.ToolCalls, llm.ToolCall{Name: call.Function.Name})
			}
		}
	}
}

// ResponsesObserver wraps a Responses stream. The terminal response.completed event carries
// the whole response, so the observer maps that event.
type ResponsesObserver struct {
	stream *ssestream.Stream[responses.ResponseStreamEventUnion]
	record llm.Record
	opts   []Option
}

// ObserveResponses wraps stream. The caller drives it with Next and Current, and reads
// Record once Next reports false.
func ObserveResponses(stream *ssestream.Stream[responses.ResponseStreamEventUnion], opts ...Option) *ResponsesObserver {
	return &ResponsesObserver{
		stream: stream,
		record: llm.Record{Provider: provider, Operation: "responses"},
		opts:   opts,
	}
}

// Next reads the next event. The terminal event fills the record.
func (o *ResponsesObserver) Next() bool {
	if !o.stream.Next() {
		return false
	}
	event := o.stream.Current()
	switch event.Type {
	case "response.completed", "response.incomplete", "response.failed":
		response := event.Response
		o.record = FromResponse(&response, o.opts...)
	}
	return true
}

// Current returns the event the caller just read.
func (o *ResponsesObserver) Current() responses.ResponseStreamEventUnion { return o.stream.Current() }

// Record returns the record built so far.
func (o *ResponsesObserver) Record() llm.Record { return o.record }

// Err returns the stream error, if any.
func (o *ResponsesObserver) Err() error { return o.stream.Err() }

// Middleware returns an option.Middleware that records the request id from the response
// headers onto the current event. Pass it with option.WithMiddleware(Middleware()).
func Middleware() option.Middleware {
	return func(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
		resp, err := next(req)
		if resp != nil {
			if id := resp.Header.Get("x-request-id"); id != "" {
				wlog.SetGroup(req.Context(), "llm", map[string]any{"request_ids": []any{id}})
			}
		}
		return resp, err
	}
}
