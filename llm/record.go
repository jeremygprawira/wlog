// Package llm records one model call on the wide event under the "llm" group. It holds
// the model, the token counts, the tool calls, the stream timings, and the money the
// call cost, so a team can answer "what did this request spend, and where" from the
// same event that holds the request's status and duration.
//
// The package depends on no LLM SDK. A caller fills a Record from whatever client it
// uses, which keeps this package in the root module and free of a vendor's release
// cycle. It records the shape of a call, never its content.
package llm

import "time"

// Record is one model call. A zero field is left off the event, so a caller fills only
// what its own client reports.
type Record struct {
	Provider  string // "openai", "anthropic", "google", or any string
	Model     string // the exact model id billed, such as "claude-sonnet-5"
	Operation string // "chat", "embedding", "rerank", or any string

	// InputTokens is the whole input count, the way the providers report it: it includes
	// the cache reads and the cache writes. CachedInputTokens and CacheWriteInputTokens are
	// subsets of it, so a caller maps each provider onto the same shape.
	//
	// Anthropic reports input_tokens WITHOUT its cache parts, so its adapter adds
	// input_tokens + cache_creation_input_tokens + cache_read_input_tokens here. OpenAI and
	// Gemini already include them in their prompt count, so their adapters map it across.
	InputTokens int
	// CachedInputTokens is the part of the input served from a prompt cache, billed at the
	// lower cache-read rate.
	CachedInputTokens int
	// CacheWriteInputTokens is the part of the input written to a prompt cache, billed above
	// the plain input rate. Anthropic bills this at the five minute cache rate.
	CacheWriteInputTokens int
	// CacheWrite1hInputTokens is the part of the input written to a one hour prompt cache,
	// which Anthropic bills at twice the input rate.
	CacheWrite1hInputTokens int

	OutputTokens    int
	ReasoningTokens int // a subset of OutputTokens

	ToolCalls []ToolCall

	TimeToFirstToken time.Duration // stream only, zero for a whole-response call
	Duration         time.Duration
	Streamed         bool

	// OutputTokensPerSecond is the decode speed. Add sets it from OutputTokens and Duration
	// when the caller leaves it zero.
	OutputTokensPerSecond float64
	// Steps is the number of agent or chain steps, when the framework reports them.
	Steps int

	FinishReason string // "stop", "length", "tool_calls", "content_filter", or any string
	Cost         *Cost  // nil until Price fills it
}

// ToolCall is one tool invocation inside a call.
type ToolCall struct {
	Name     string
	Duration time.Duration
	Failed   bool
}

// Cost is money, in whole millionths of a US dollar, so no float rounding reaches the
// event. USD renders it for a human.
type Cost struct {
	// InputMicros prices only the uncached input: the part of the input that is neither a
	// cache read nor a cache write. Each part carries its own rate, so a cache-heavy call is
	// never billed as fresh input.
	InputMicros      int64
	CacheReadMicros  int64
	CacheWriteMicros int64
	// CacheWrite1hMicros prices the one hour cache writes, which cost more than the five
	// minute writes.
	CacheWrite1hMicros int64
	OutputMicros       int64
	TotalMicros        int64
}

// USD returns the total as dollars.
func (c Cost) USD() float64 { return float64(c.TotalMicros) / 1e6 }
