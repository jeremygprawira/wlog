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
	"time"

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
		// The OpenAI provider copies ToolCalls[0] into FuncCall. Read FuncCall only
		// when ToolCalls is empty, so that copy is not counted twice.
		if choice.FuncCall != nil && len(choice.ToolCalls) == 0 {
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
		if choice.FuncCall != nil && len(choice.ToolCalls) == 0 {
			content.OutputMessages = append(content.OutputMessages, llm.Message{
				Role:  "assistant",
				Parts: []llm.Part{{Type: "tool_call", Name: choice.FuncCall.Name, Arguments: choice.FuncCall.Arguments}},
			})
		}
		for _, call := range choice.ToolCalls {
			if call.FunctionCall == nil {
				continue
			}
			content.OutputMessages = append(content.OutputMessages, llm.Message{
				Role:  "assistant",
				Parts: []llm.Part{{Type: "tool_call", ID: call.ID, Name: call.FunctionCall.Name, Arguments: call.FunctionCall.Arguments}},
			})
		}
	}
	if len(content.OutputMessages) == 0 {
		return nil
	}
	return content
}

// callTTL and callCap bound a start that never receives an end. The library calculator
// tool returns with no end callback, so an unbounded map would keep one entry per call.
const (
	callTTL = 5 * time.Minute
	callCap = 10_000
)

// storedCall is one open start. at is the time it was stored, so a later start can drop
// it once it is older than callTTL.
type storedCall struct {
	end func(wlog.CallResult)
	at  time.Time
}

// handler turns tool and chain callback pairs into call records.
//
// A context holds a stack of starts, not one slot. An end closes the latest open
// start on that context, so nested pairs do not overwrite each other.
type handler struct {
	callbacks.SimpleHandler
	log    *wlog.Logger
	mu     sync.Mutex
	tools  map[context.Context][]storedCall
	chains map[context.Context][]storedCall
}

// Handler returns a callbacks.Handler that turns every tool call and every chain step into
// one entry under calls[], through wlog.StartCall. It never records the LLM call itself.
// Call FromContentResponse on the result for that. log is told when a start is dropped.
// A nil log still drops the start.
func Handler(log *wlog.Logger) callbacks.Handler {
	return &handler{
		log:    log,
		tools:  map[context.Context][]storedCall{},
		chains: map[context.Context][]storedCall{},
	}
}

// HandleToolStart starts one call of kind "other", operation "tool".
func (h *handler) HandleToolStart(ctx context.Context, _ string) {
	h.begin(h.tools, ctx, "tool")
}

// HandleToolEnd ends the call HandleToolStart began on the same ctx.
func (h *handler) HandleToolEnd(ctx context.Context, _ string) {
	h.finish(h.tools, ctx, wlog.CallResult{})
}

// HandleToolError ends the call HandleToolStart began on the same ctx, as a failure.
func (h *handler) HandleToolError(ctx context.Context, err error) {
	h.finish(h.tools, ctx, wlog.CallResult{Err: err})
}

// HandleChainStart starts one call of kind "other", operation "chain".
func (h *handler) HandleChainStart(ctx context.Context, _ map[string]any) {
	h.begin(h.chains, ctx, "chain")
}

// HandleChainEnd ends the call HandleChainStart began on the same ctx.
func (h *handler) HandleChainEnd(ctx context.Context, _ map[string]any) {
	h.finish(h.chains, ctx, wlog.CallResult{})
}

// HandleChainError ends the call HandleChainStart began on the same ctx, as a failure.
func (h *handler) HandleChainError(ctx context.Context, err error) {
	h.finish(h.chains, ctx, wlog.CallResult{Err: err})
}

// begin stores one start. It first drops every start older than callTTL. At the cap it
// drops the new start instead of growing. Each drop is reported.
func (h *handler) begin(m map[context.Context][]storedCall, ctx context.Context, operation string) {
	h.mu.Lock()
	dropped := h.prune(m)
	if openCalls(m) >= callCap {
		dropped++
		h.mu.Unlock()
		h.report(dropped)
		return
	}
	_, end := wlog.StartCall(ctx, wlog.Call{Kind: "other", System: "langchaingo", Operation: operation})
	m[ctx] = append(m[ctx], storedCall{end: end, at: time.Now()})
	h.mu.Unlock()
	h.report(dropped)
}

// openCalls counts every start still held, across every context.
func openCalls(m map[context.Context][]storedCall) int {
	n := 0
	for _, stack := range m {
		n += len(stack)
	}
	return n
}

// prune drops every start older than callTTL. The caller holds h.mu.
func (h *handler) prune(m map[context.Context][]storedCall) int {
	now := time.Now()
	dropped := 0
	for key, stack := range m {
		kept := stack[:0]
		for _, slot := range stack {
			if now.Sub(slot.at) > callTTL {
				dropped++
				continue
			}
			kept = append(kept, slot)
		}
		if len(kept) == 0 {
			delete(m, key)
			continue
		}
		m[key] = kept
	}
	return dropped
}

// report tells log about each dropped start. A nil log stays quiet.
func (h *handler) report(n int) {
	if h.log == nil || n == 0 {
		return
	}
	for i := 0; i < n; i++ {
		h.log.Report(wlog.Problem{Code: "WLOG_CAP_REACHED", Source: "langchaingo"})
	}
}

// finish ends the latest start stored under ctx. A ctx with no start is left alone.
func (h *handler) finish(m map[context.Context][]storedCall, ctx context.Context, result wlog.CallResult) {
	h.mu.Lock()
	stack := m[ctx]
	if len(stack) == 0 {
		h.mu.Unlock()
		return
	}
	slot := stack[len(stack)-1]
	stack = stack[:len(stack)-1]
	if len(stack) == 0 {
		delete(m, ctx)
	} else {
		m[ctx] = stack
	}
	h.mu.Unlock()
	slot.end(result)
}
