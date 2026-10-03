// Package wlogeino turns a cloudwego/eino chat model callback into an llm.Record.
//
// Handler is the only entry point: eino ships no chat model implementation of its own, and
// every provider lives in its own eino-ext module reporting through this same
// callbacks.Handler shape, so there is no single response type to map directly. It reads
// token usage from CallbackOutput.TokenUsage, and falls back to Message.ResponseMeta.Usage
// when a provider implementation fills only that. A streamed call is read on an
// independent stream copy, drained in its own goroutine so the real consumer is never
// blocked, and closed when done.
//
// No helper keeps the prompt, the completion, or the tool payload unless the caller passes
// WithContent. Core redacts those values like any other.
package wlogeino

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	template "github.com/cloudwego/eino/utils/callbacks"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/llm"
)

// Option configures Handler.
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

// Handler returns a callbacks.Handler that turns every chat model call into an llm.Record
// and folds it into the current event with llm.Add.
func Handler(opts ...Option) callbacks.Handler {
	content := resolve(opts...).content
	return template.NewHandlerHelper().
		ChatModel(&template.ModelCallbackHandler{
			OnEnd: func(ctx context.Context, info *callbacks.RunInfo, output *model.CallbackOutput) context.Context {
				r := llm.Record{Provider: providerOf(info), Operation: "chat"}
				applyOutput(&r, output, content)
				llm.Add(ctx, r)
				return ctx
			},
			OnEndWithStreamOutput: func(ctx context.Context, info *callbacks.RunInfo, output *schema.StreamReader[*model.CallbackOutput]) context.Context {
				// The caller may end the event before the stream does. The record
				// lives on a detached child that this goroutine ends. Cancel still
				// uses the caller's context, because Detach does not.
				child, end := wlog.Detach(ctx, "eino")
				go func() {
					defer end()
					drain(ctx, child, output, providerOf(info), content)
				}()
				return ctx
			},
			OnError: func(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
				r := llm.Record{Provider: providerOf(info), Operation: "chat", FinishReason: "error"}
				llm.Add(ctx, r)
				wlog.Error(ctx, err)
				return ctx
			},
		}).
		Handler()
}

// providerOf names the call from the RunInfo eino gives every callback. info or its Type
// can be empty for a component that never set one, so "eino" is the fallback.
func providerOf(info *callbacks.RunInfo) string {
	if info == nil || info.Type == "" {
		return "eino"
	}
	return info.Type
}

// streamItem is one read from the model stream.
type streamItem struct {
	chunk *model.CallbackOutput
	err   error
	panic any
}

// drain reads a streamed chat model call on its own copy of the stream, so the real
// consumer is never blocked, and folds the result into ctx once done. callCtx is the
// caller's context. A cancel ends the read. A stream error or a recovered panic is
// written on ctx.
func drain(callCtx, ctx context.Context, stream *schema.StreamReader[*model.CallbackOutput], provider string, content bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			wlog.Error(ctx, fmt.Errorf("panic: %v", recovered))
		}
	}()
	if stream == nil {
		panic("nil stream")
	}
	defer stream.Close()

	// Recv blocks until the source sends or closes. The read runs aside so this
	// call can return when the caller cancels.
	items := make(chan streamItem, 1)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				items <- streamItem{panic: recovered}
			}
		}()
		for {
			chunk, err := stream.Recv()
			items <- streamItem{chunk: chunk, err: err}
			if err != nil {
				return
			}
		}
	}()

	r := llm.Record{Provider: provider, Operation: "chat", Streamed: true}
	var messages []*schema.Message
	var fail error
	for {
		select {
		case <-callCtx.Done():
			fail = callCtx.Err()
		case it := <-items:
			if it.panic != nil {
				panic(it.panic)
			}
			if it.chunk != nil {
				if it.chunk.Config != nil && it.chunk.Config.Model != "" {
					r.Model = it.chunk.Config.Model
				}
				applyUsage(&r, it.chunk)
				if it.chunk.Message != nil {
					messages = append(messages, it.chunk.Message)
				}
			}
			if it.err == nil || errors.Is(it.err, io.EOF) {
				if it.err != nil {
					goto done
				}
				continue
			}
			fail = it.err
		}
		if fail != nil {
			break
		}
	}
done:
	if len(messages) > 0 {
		// Deltas of one tool call share an index. Concat merges them, and it merges
		// the text too, so a streamed call counts each tool call once.
		merged, err := schema.ConcatMessages(messages)
		if err == nil {
			applyMessage(&r, merged, content)
		}
	}
	if fail != nil {
		r.FinishReason = "error"
		wlog.Error(ctx, fail)
	}
	llm.Add(ctx, r)
}

// applyOutput folds one whole CallbackOutput into r: the model name, the usage, the
// finish reason, and the tool calls.
func applyOutput(r *llm.Record, output *model.CallbackOutput, content bool) {
	if output == nil {
		return
	}
	if output.Config != nil && output.Config.Model != "" {
		r.Model = output.Config.Model
	}
	applyUsage(r, output)
	applyMessage(r, output.Message, content)
}

// applyMessage folds one message into r. A stream calls it once, on the merged message.
func applyMessage(r *llm.Record, message *schema.Message, content bool) {
	if message == nil {
		return
	}
	if message.ResponseMeta != nil && message.ResponseMeta.FinishReason != "" {
		r.FinishReason = message.ResponseMeta.FinishReason
	}
	for _, call := range message.ToolCalls {
		r.ToolCalls = append(r.ToolCalls, llm.ToolCall{Name: call.Function.Name})
	}
	if content {
		r.Content = mergeContent(r.Content, message)
	}
}

// applyUsage reads CallbackOutput.TokenUsage, and falls back to the usage the message
// itself carries when a provider implementation only fills that.
func applyUsage(r *llm.Record, output *model.CallbackOutput) {
	if u := output.TokenUsage; u != nil {
		r.InputTokens = u.PromptTokens
		r.CachedInputTokens = u.PromptTokenDetails.CachedTokens
		r.OutputTokens = u.CompletionTokens
		r.ReasoningTokens = u.CompletionTokensDetails.ReasoningTokens
		return
	}
	if output.Message == nil || output.Message.ResponseMeta == nil || output.Message.ResponseMeta.Usage == nil {
		return
	}
	u := output.Message.ResponseMeta.Usage
	r.InputTokens = u.PromptTokens
	r.CachedInputTokens = u.PromptTokenDetails.CachedTokens
	r.OutputTokens = u.CompletionTokens
	r.ReasoningTokens = u.CompletionTokensDetails.ReasoningTokens
}

// mergeContent appends one message's text and tool calls to the opt-in gen_ai shape. A
// message with neither leaves content unchanged.
func mergeContent(content *llm.Content, message *schema.Message) *llm.Content {
	var added []llm.Message
	if message.Content != "" {
		added = append(added, llm.Message{
			Role:  "assistant",
			Parts: []llm.Part{{Type: "text", Content: message.Content}},
		})
	}
	for _, call := range message.ToolCalls {
		added = append(added, llm.Message{
			Role:  "assistant",
			Parts: []llm.Part{{Type: "tool_call", ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments}},
		})
	}
	if len(added) == 0 {
		return content
	}
	if content == nil {
		content = &llm.Content{}
	}
	content.OutputMessages = append(content.OutputMessages, added...)
	return content
}
