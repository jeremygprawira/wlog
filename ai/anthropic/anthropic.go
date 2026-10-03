// Package wloganthropic turns an Anthropic SDK response or stream into an llm.Record.
//
// FromMessage and FromBetaMessage map a whole response. Observe wraps a stream, so the
// caller still reads every chunk and still builds the same Record. Middleware records the
// request id and the retry count from the response headers.
//
// No helper keeps the prompt, the completion, or the tool payload unless the caller passes
// WithContent. Core redacts those values like any other.
package wloganthropic

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/llm"
)

// The values the OpenTelemetry gen_ai conventions expect.
const (
	provider  = "anthropic"
	operation = "chat"
	toolUse   = "tool_use"
)

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

// tokens holds one usage block, so the message and the beta message share one mapping.
type tokens struct {
	input           int64
	cacheRead       int64
	cacheWriteTotal int64
	cacheWrite5m    int64
	cacheWrite1h    int64
	output          int64
	reasoning       int64
}

// FromMessage turns one Anthropic Message into an llm.Record.
func FromMessage(m *anthropic.Message, opts ...Option) llm.Record {
	if m == nil {
		return llm.Record{}
	}
	r := recordOf(m.ID, m.Model, string(m.StopReason), tokens{
		input:           m.Usage.InputTokens,
		cacheRead:       m.Usage.CacheReadInputTokens,
		cacheWriteTotal: m.Usage.CacheCreationInputTokens,
		cacheWrite5m:    m.Usage.CacheCreation.Ephemeral5mInputTokens,
		cacheWrite1h:    m.Usage.CacheCreation.Ephemeral1hInputTokens,
		output:          m.Usage.OutputTokens,
		reasoning:       m.Usage.OutputTokensDetails.ThinkingTokens,
	})
	for _, block := range m.Content {
		if block.Type == toolUse {
			r.ToolCalls = append(r.ToolCalls, llm.ToolCall{Name: block.Name})
		}
	}
	if resolve(opts...).content {
		r.Content = contentOf(m.Content)
	}
	return r
}

// FromBetaMessage turns one Anthropic BetaMessage into an llm.Record.
func FromBetaMessage(m *anthropic.BetaMessage, opts ...Option) llm.Record {
	if m == nil {
		return llm.Record{}
	}
	r := recordOf(m.ID, m.Model, string(m.StopReason), tokens{
		input:           m.Usage.InputTokens,
		cacheRead:       m.Usage.CacheReadInputTokens,
		cacheWriteTotal: m.Usage.CacheCreationInputTokens,
		cacheWrite5m:    m.Usage.CacheCreation.Ephemeral5mInputTokens,
		cacheWrite1h:    m.Usage.CacheCreation.Ephemeral1hInputTokens,
		output:          m.Usage.OutputTokens,
		reasoning:       m.Usage.OutputTokensDetails.ThinkingTokens,
	})
	for _, block := range m.Content {
		if block.Type == toolUse {
			r.ToolCalls = append(r.ToolCalls, llm.ToolCall{Name: block.Name})
		}
	}
	if resolve(opts...).content {
		r.Content = betaContentOf(m.Content)
	}
	return r
}

// recordOf maps the shared fields. Anthropic reports the input without its cache parts, so
// the cache parts are added back into the whole input count, as the token table says.
func recordOf(id, model, stopReason string, t tokens) llm.Record {
	write5m, write1h := t.cacheWrite5m, t.cacheWrite1h
	if write5m == 0 && write1h == 0 {
		// The breakdown is absent, so the total is the five minute write.
		write5m = t.cacheWriteTotal
	}
	return llm.Record{
		Provider:                provider,
		Model:                   model,
		Operation:               operation,
		ResponseID:              id,
		InputTokens:             int(t.input + t.cacheWriteTotal + t.cacheRead),
		CachedInputTokens:       int(t.cacheRead),
		CacheWriteInputTokens:   int(write5m),
		CacheWrite1hInputTokens: int(write1h),
		OutputTokens:            int(t.output),
		ReasoningTokens:         int(t.reasoning),
		FinishReason:            stopReason,
	}
}

// contentOf maps the content blocks of one message to the opt-in gen_ai shape.
func contentOf(blocks []anthropic.ContentBlockUnion) *llm.Content {
	content := &llm.Content{}
	for _, block := range blocks {
		switch block.Type {
		case "text":
			content.OutputMessages = append(content.OutputMessages, llm.Message{
				Role:  "assistant",
				Parts: []llm.Part{{Type: "text", Content: block.Text}},
			})
		case toolUse:
			content.OutputMessages = append(content.OutputMessages, llm.Message{
				Role:  "assistant",
				Parts: []llm.Part{{Type: "tool_call", ID: block.ID, Name: block.Name, Arguments: block.Input}},
			})
		}
	}
	if len(content.OutputMessages) == 0 {
		return nil
	}
	return content
}

// betaContentOf maps the content blocks of the beta message, which shares the block shape.
func betaContentOf(blocks []anthropic.BetaContentBlockUnion) *llm.Content {
	content := &llm.Content{}
	for _, block := range blocks {
		switch block.Type {
		case "text":
			content.OutputMessages = append(content.OutputMessages, llm.Message{
				Role:  "assistant",
				Parts: []llm.Part{{Type: "text", Content: block.Text}},
			})
		case toolUse:
			content.OutputMessages = append(content.OutputMessages, llm.Message{
				Role:  "assistant",
				Parts: []llm.Part{{Type: "tool_call", ID: block.ID, Name: block.Name}},
			})
		}
	}
	if len(content.OutputMessages) == 0 {
		return nil
	}
	return content
}

// Observer wraps a message stream and builds a Record as the caller reads it. Observe
// never reads ahead, so the caller keeps every chunk and no text reaches the record.
type Observer struct {
	stream   *ssestream.Stream[anthropic.MessageStreamEventUnion]
	record   llm.Record
	rawInput int
	started  time.Time
	sawChunk bool
}

// Observe wraps stream. The caller drives it with Next and Current, and reads Record once
// Next reports false.
func Observe(stream *ssestream.Stream[anthropic.MessageStreamEventUnion], opts ...Option) *Observer {
	_ = resolve(opts...)
	return &Observer{
		stream:  stream,
		record:  llm.Record{Provider: provider, Operation: operation, Streamed: true},
		started: time.Now(),
	}
}

// Next reads the next event and folds it into the record. It reports false at the end.
func (o *Observer) Next() bool {
	if !o.stream.Next() {
		o.record.Streamed = true
		o.record.Duration = time.Since(o.started)
		return false
	}
	if !o.sawChunk {
		o.sawChunk = true
		o.record.TimeToFirstToken = positiveSince(o.started)
	}
	o.consume(o.stream.Current())
	return true
}

// Current returns the event the caller just read.
func (o *Observer) Current() anthropic.MessageStreamEventUnion { return o.stream.Current() }

// Record returns the record built so far. Read it after Next reports false.
func (o *Observer) Record() llm.Record { return o.record }

// Err returns the stream error, if any.
func (o *Observer) Err() error { return o.stream.Err() }

// consume folds one stream event into the record. It reads the token counts and the names,
// and never the text.
func (o *Observer) consume(event anthropic.MessageStreamEventUnion) {
	switch event.Type {
	case "message_start":
		usage := event.Message.Usage
		o.record.Model = event.Message.Model
		o.record.ResponseID = event.Message.ID
		o.rawInput = int(usage.InputTokens)
		o.record.InputTokens = int(usage.InputTokens + usage.CacheCreationInputTokens + usage.CacheReadInputTokens)
		o.record.CachedInputTokens = int(usage.CacheReadInputTokens)
		write5m, write1h := usage.CacheCreation.Ephemeral5mInputTokens, usage.CacheCreation.Ephemeral1hInputTokens
		if write5m == 0 && write1h == 0 {
			write5m = usage.CacheCreationInputTokens
		}
		o.record.CacheWriteInputTokens = int(write5m)
		o.record.CacheWrite1hInputTokens = int(write1h)
		o.record.OutputTokens = int(usage.OutputTokens)
		for _, block := range event.Message.Content {
			if block.Type == toolUse {
				o.record.ToolCalls = append(o.record.ToolCalls, llm.ToolCall{Name: block.Name})
			}
		}
	case "message_delta":
		usage := event.Usage
		changed := false
		if usage.JSON.InputTokens.Valid() {
			o.rawInput = int(usage.InputTokens)
			changed = true
		}
		if usage.JSON.CacheReadInputTokens.Valid() {
			o.record.CachedInputTokens = int(usage.CacheReadInputTokens)
			changed = true
		}
		if usage.JSON.CacheCreationInputTokens.Valid() {
			o.record.CacheWriteInputTokens = int(usage.CacheCreationInputTokens)
			o.record.CacheWrite1hInputTokens = 0
			changed = true
		}
		if changed {
			o.record.InputTokens = o.rawInput + o.record.CachedInputTokens + o.record.CacheWriteInputTokens + o.record.CacheWrite1hInputTokens
		}
		if usage.JSON.OutputTokens.Valid() {
			o.record.OutputTokens = int(usage.OutputTokens)
		}
		if usage.JSON.OutputTokensDetails.Valid() {
			o.record.ReasoningTokens = int(usage.OutputTokensDetails.ThinkingTokens)
		}
		if reason := string(event.Delta.StopReason); reason != "" {
			o.record.FinishReason = reason
		}
	case "content_block_start":
		if event.ContentBlock.Type == toolUse {
			o.record.ToolCalls = append(o.record.ToolCalls, llm.ToolCall{Name: event.ContentBlock.Name})
		}
	}
}

// Middleware returns an option.Middleware that records the request id from the response
// and the retry count from the request. Pass it with
// option.WithMiddleware(Middleware()).
func Middleware() option.Middleware {
	return func(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
		resp, err := next(req)
		if resp != nil {
			recordHeaders(req, resp)
		}
		return resp, err
	}
}

// recordHeaders writes the request id and the attempt count onto the event of req.
func recordHeaders(req *http.Request, resp *http.Response) {
	ctx := req.Context()
	if id := resp.Header.Get("request-id"); id != "" {
		appendRequestID(ctx, id)
	}
	if count := req.Header.Get("X-Stainless-Retry-Count"); count != "" {
		if n, err := strconv.Atoi(count); err == nil {
			wlog.SetGroup(ctx, "llm", map[string]any{"attempts": n + 1})
		}
	}
}

// appendRequestID keeps every id. SetGroup would replace the list with the last one.
func appendRequestID(ctx context.Context, id string) {
	wlog.UpdateGroup(ctx, "llm", func(fields map[string]any) {
		ids, _ := fields["request_ids"].([]any)
		next := make([]any, len(ids)+1)
		copy(next, ids)
		next[len(ids)] = id
		fields["request_ids"] = next
	})
}

// positiveSince reports the time since started. A clock that has not moved still
// counts as one nanosecond, so a first chunk is never stored as zero.
func positiveSince(started time.Time) time.Duration {
	d := time.Since(started)
	if d <= 0 {
		return time.Nanosecond
	}
	return d
}
