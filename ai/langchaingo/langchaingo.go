// Package wloglangchaingo turns a langchaingo llms.ContentResponse into an llm.Record, and
// turns its callbacks.Handler tool and chain events into call records.
//
// FromContentResponse maps a whole response. The Anthropic provider in langchaingo
// v0.1.14 never calls HandleLLMGenerateContentEnd on success, only HandleLLMStart and
// HandleLLMError, so a caller using it must call FromContentResponse directly on the
// result instead of relying on a Handler to do it. Handler covers what a typed helper
// cannot: it turns every tool call and every chain step into one entry under calls[],
// through wlog.StartCall.
//
// No helper keeps the prompt, the completion, or the tool payload unless the caller passes
// WithContent. Core redacts those values like any other.
package wloglangchaingo

import (
	"context"
	"sync"

	"github.com/tmc/langchaingo/callbacks"
	"github.com/tmc/langchaingo/llms"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/llm"
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

// FromContentResponse turns one llms.ContentResponse into an llm.Record. provider and
// model name the call, because ContentResponse carries neither: langchaingo folds every
// provider's usage into GenerationInfo under its own key names, so provider picks how to
// read them. Only "anthropic" and "openai" are mapped; any other provider's record holds
// the shape of the call with its token counts at 0.
//
// The Anthropic provider gives one ContentChoice per content block of one message, each
// repeating the same usage, so only the first choice with a non-empty GenerationInfo
// fills the token counts. Every choice can still hold its own tool call.
func FromContentResponse(resp *llms.ContentResponse, provider, model string, opts ...Option) llm.Record {
	if resp == nil || len(resp.Choices) == 0 {
		return llm.Record{}
	}
	r := llm.Record{Provider: provider, Model: model, Operation: "chat"}
	usageApplied := false
	for _, choice := range resp.Choices {
		if choice.StopReason != "" && r.FinishReason == "" {
			r.FinishReason = choice.StopReason
		}
		if !usageApplied && len(choice.GenerationInfo) > 0 {
			applyGenerationInfo(&r, provider, choice.GenerationInfo)
			usageApplied = true
		}
		if choice.FuncCall != nil {
			r.ToolCalls = append(r.ToolCalls, llm.ToolCall{Name: choice.FuncCall.Name})
		}
		for _, call := range choice.ToolCalls {
			if call.FunctionCall != nil {
				r.ToolCalls = append(r.ToolCalls, llm.ToolCall{Name: call.FunctionCall.Name})
			}
		}
	}
	if resolve(opts...).content {
		r.Content = contentOf(resp.Choices)
	}
	return r
}

// applyGenerationInfo reads one usage block by the key names the named provider writes.
func applyGenerationInfo(r *llm.Record, provider string, info map[string]any) {
	switch provider {
	case "anthropic":
		read := intOf(info["CacheReadInputTokens"])
		written := intOf(info["CacheCreationInputTokens"])
		r.InputTokens = intOf(info["InputTokens"]) + read + written
		r.CachedInputTokens = read
		r.CacheWriteInputTokens = written
		r.OutputTokens = intOf(info["OutputTokens"])
	case "openai":
		r.InputTokens = intOf(info["PromptTokens"])
		r.CachedInputTokens = intOf(info["PromptCachedTokens"])
		r.OutputTokens = intOf(info["CompletionTokens"])
		r.ReasoningTokens = intOf(info["ReasoningTokens"])
	}
}

// intOf reads a stored integer of any numeric width, treating anything else as 0.
func intOf(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

// contentOf maps every choice's text and tool calls to the opt-in gen_ai shape.
func contentOf(choices []*llms.ContentChoice) *llm.Content {
	content := &llm.Content{}
	for _, choice := range choices {
		if choice.Content != "" {
			content.OutputMessages = append(content.OutputMessages, llm.Message{
				Role:  "assistant",
				Parts: []llm.Part{{Type: "text", Content: choice.Content}},
			})
		}
		if choice.FuncCall != nil {
			content.OutputMessages = append(content.OutputMessages, llm.Message{
				Role:  "assistant",
				Parts: []llm.Part{{Type: "tool_call", Name: choice.FuncCall.Name, Arguments: wlog.JSONTree(choice.FuncCall.Arguments)}},
			})
		}
		for _, call := range choice.ToolCalls {
			if call.FunctionCall == nil {
				continue
			}
			content.OutputMessages = append(content.OutputMessages, llm.Message{
				Role:  "assistant",
				Parts: []llm.Part{{Type: "tool_call", ID: call.ID, Name: call.FunctionCall.Name, Arguments: wlog.JSONTree(call.FunctionCall.Arguments)}},
			})
		}
	}
	if len(content.OutputMessages) == 0 {
		return nil
	}
	return content
}

// handler turns tool and chain callback pairs into call records.
//
// ponytail: HandleToolStart/End and HandleChainStart/End carry no call id or name, so a
// pair correlates by the identity of the ctx value the SDK hands back unchanged between
// the two calls. Two tool calls sharing one ctx concurrently would collide; upgrade to a
// real id if langchaingo ever adds one to these signatures.
type handler struct {
	callbacks.SimpleHandler
	tools  sync.Map // context.Context -> func(wlog.CallResult)
	chains sync.Map // context.Context -> func(wlog.CallResult)
}

// Handler returns a callbacks.Handler that turns every tool call and every chain step into
// one entry under calls[], through wlog.StartCall. It never touches the LLM call itself;
// call FromContentResponse on the result for that.
func Handler() callbacks.Handler {
	return &handler{}
}

// HandleToolStart starts one call of kind "agent", operation "tool".
func (h *handler) HandleToolStart(ctx context.Context, _ string) {
	_, end := wlog.StartCall(ctx, wlog.Call{Kind: "agent", System: "langchaingo", Operation: "tool"})
	h.tools.Store(ctx, end)
}

// HandleToolEnd ends the call HandleToolStart began on the same ctx.
func (h *handler) HandleToolEnd(ctx context.Context, _ string) {
	endCall(&h.tools, ctx, wlog.CallResult{})
}

// HandleToolError ends the call HandleToolStart began on the same ctx, as a failure.
func (h *handler) HandleToolError(ctx context.Context, err error) {
	endCall(&h.tools, ctx, wlog.CallResult{Err: err})
}

// HandleChainStart starts one call of kind "agent", operation "chain".
func (h *handler) HandleChainStart(ctx context.Context, _ map[string]any) {
	_, end := wlog.StartCall(ctx, wlog.Call{Kind: "agent", System: "langchaingo", Operation: "chain"})
	h.chains.Store(ctx, end)
}

// HandleChainEnd ends the call HandleChainStart began on the same ctx.
func (h *handler) HandleChainEnd(ctx context.Context, _ map[string]any) {
	endCall(&h.chains, ctx, wlog.CallResult{})
}

// HandleChainError ends the call HandleChainStart began on the same ctx, as a failure.
func (h *handler) HandleChainError(ctx context.Context, err error) {
	endCall(&h.chains, ctx, wlog.CallResult{Err: err})
}

// endCall looks up the end func a start stored under ctx and calls it once. A ctx with no
// matching start is left alone, never a panic.
func endCall(m *sync.Map, ctx context.Context, result wlog.CallResult) {
	v, ok := m.LoadAndDelete(ctx)
	if !ok {
		return
	}
	v.(func(wlog.CallResult))(result)
}
