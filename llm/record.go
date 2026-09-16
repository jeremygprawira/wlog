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

	InputTokens       int
	OutputTokens      int
	CachedInputTokens int // tokens served from a prompt cache, billed at a lower rate
	ReasoningTokens   int

	ToolCalls []ToolCall

	TimeToFirstToken time.Duration // stream only, zero for a whole-response call
	Duration         time.Duration
	Streamed         bool

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
	InputMicros  int64
	OutputMicros int64
	TotalMicros  int64
}

// USD returns the total as dollars.
func (c Cost) USD() float64 { return float64(c.TotalMicros) / 1e6 }
