package llm

import (
	"context"

	"github.com/jeremygprawira/wlog"
)

// group is the event group every field lives under.
const group = "llm"

// maxCalls caps the two call arrays on one event, as SPEC-llm says. An entry past the cap is
// dropped and counted in wlog.dropped_fields, so an agent loop cannot grow one event with no
// bound and cannot lose the fact that it tried (gate G4).
const maxCalls = 200

// Set writes r onto the current event under the "llm" group. Outside a wlog.Start, it
// does nothing, the same as wlog.Set. A zero field stays off the event.
func Set(ctx context.Context, r Record) {
	wlog.SetGroup(ctx, group, fieldsFor(r))
}

// Add folds r into the current event's llm totals and appends it to llm.calls[]. Use it
// when one request makes several model calls. It is a no-op outside a wlog.Start.
//
// Add is not safe to call concurrently on one event, because it reads and writes the
// group. A request's model calls are sequential in practice.
func Add(ctx context.Context, r Record) {
	// The decode speed follows from the output count and the call duration, so a caller
	// does not compute it.
	if r.OutputTokensPerSecond == 0 && r.OutputTokens > 0 && r.Duration > 0 {
		r.OutputTokensPerSecond = float64(r.OutputTokens) / r.Duration.Seconds()
	}
	current, _ := wlog.Field(ctx, group)
	existing, _ := current.(map[string]any)

	fields := fieldsFor(r)
	fields["input_tokens"] = intOf(existing["input_tokens"]) + r.InputTokens
	fields["output_tokens"] = intOf(existing["output_tokens"]) + r.OutputTokens
	if total := fields["input_tokens"].(int) + fields["output_tokens"].(int); total > 0 {
		fields["total_tokens"] = total
	}
	if cached := intOf(existing["cache_read_input_tokens"]) + r.CachedInputTokens; cached > 0 {
		fields["cache_read_input_tokens"] = cached
	}
	if written := intOf(existing["cache_write_input_tokens"]) + r.CacheWriteInputTokens; written > 0 {
		fields["cache_write_input_tokens"] = written
	}
	if written := intOf(existing["cache_write_1h_input_tokens"]) + r.CacheWrite1hInputTokens; written > 0 {
		fields["cache_write_1h_input_tokens"] = written
	}
	if steps := intOf(existing["steps"]) + r.Steps; steps > 0 {
		fields["steps"] = steps
	}
	if reasoning := intOf(existing["reasoning_tokens"]) + r.ReasoningTokens; reasoning > 0 {
		fields["reasoning_tokens"] = reasoning
	}

	calls, _ := existing["calls"].([]any)
	toolCalls, _ := existing["tool_calls"].([]any)
	dropped := 0

	if len(calls) < maxCalls {
		fields["calls"] = append(calls, callMap(r))
	} else {
		dropped++
	}
	for _, call := range toolCallMaps(r.ToolCalls) {
		if len(toolCalls) >= maxCalls {
			dropped++
			continue
		}
		toolCalls = append(toolCalls, call)
	}
	if len(toolCalls) > 0 {
		fields["tool_calls"] = toolCalls
	}
	fields["tool_call_count"] = intOf(existing["tool_call_count"]) + len(r.ToolCalls)
	if failures := intOf(existing["tool_call_failures"]) + failedCalls(r.ToolCalls); failures > 0 {
		fields["tool_call_failures"] = failures
	}

	if r.Cost != nil {
		// A record written by Set before Add is folded in, not replaced, so the total covers
		// every call the request made.
		total := int64Of(existing["cost_micros"]) + r.Cost.TotalMicros
		fields["cost_micros"] = total
	}
	// One entry per call, appended, because a request may finish more than one way.
	if r.FinishReason != "" {
		reasons, _ := existing["finish_reasons"].([]any)
		fields["finish_reasons"] = append(reasons, r.FinishReason)
	}

	wlog.SetGroup(ctx, group, fields)
	// The count goes through core, so an entry that hit llm's own cap appears in
	// wlog.dropped_fields beside every other capped write.
	wlog.CountDropped(ctx, dropped)
}

// fieldsFor maps one record to the group fields. Zero values stay off.
func fieldsFor(r Record) map[string]any {
	fields := map[string]any{}
	if r.Provider != "" {
		fields["provider"] = r.Provider
	}
	if r.Model != "" {
		fields["request_model"] = r.Model
	}
	if r.Operation != "" {
		fields["operation"] = r.Operation
	}
	if r.InputTokens > 0 {
		fields["input_tokens"] = r.InputTokens
	}
	if r.OutputTokens > 0 {
		fields["output_tokens"] = r.OutputTokens
	}
	if total := r.InputTokens + r.OutputTokens; total > 0 {
		fields["total_tokens"] = total
	}
	if r.CachedInputTokens > 0 {
		fields["cache_read_input_tokens"] = r.CachedInputTokens
	}
	if r.CacheWriteInputTokens > 0 {
		fields["cache_write_input_tokens"] = r.CacheWriteInputTokens
	}
	if r.CacheWrite1hInputTokens > 0 {
		fields["cache_write_1h_input_tokens"] = r.CacheWrite1hInputTokens
	}
	if r.ReasoningTokens > 0 {
		fields["reasoning_tokens"] = r.ReasoningTokens
	}
	if len(r.ToolCalls) > 0 {
		fields["tool_calls"] = toolCallMaps(r.ToolCalls)
		fields["tool_call_count"] = len(r.ToolCalls)
		if failures := failedCalls(r.ToolCalls); failures > 0 {
			fields["tool_call_failures"] = failures
		}
	}
	if r.TimeToFirstToken > 0 {
		fields["time_to_first_chunk_ms"] = r.TimeToFirstToken.Milliseconds()
	}
	if r.Duration > 0 {
		fields["duration_ms"] = r.Duration.Milliseconds()
	}
	if r.Streamed {
		fields["streamed"] = true
	}
	if r.Steps > 0 {
		fields["steps"] = r.Steps
	}
	if r.OutputTokensPerSecond > 0 {
		fields["output_tokens_per_second"] = r.OutputTokensPerSecond
	}
	if r.FinishReason != "" {
		// finish_reasons is an array, because a request may finish more than one way.
		fields["finish_reasons"] = []any{r.FinishReason}
	}
	if r.Cost != nil {
		// Money stays in whole micros: a float on the event would round, and SPEC-llm says
		// money never passes through one.
		fields["cost_micros"] = r.Cost.TotalMicros
	}
	return fields
}

// callMap is one entry for llm.calls[].
func callMap(r Record) map[string]any {
	return fieldsFor(r)
}

// toolCallMaps maps tool calls to plain maps for the event.
func toolCallMaps(calls []ToolCall) []any {
	out := make([]any, 0, len(calls))
	for _, call := range calls {
		entry := map[string]any{"name": call.Name}
		if call.Duration > 0 {
			entry["duration_ms"] = call.Duration.Milliseconds()
		}
		if call.Failed {
			entry["failed"] = true
		}
		out = append(out, entry)
	}
	return out
}

// failedCalls counts the failed tool calls.
func failedCalls(calls []ToolCall) int {
	failures := 0
	for _, call := range calls {
		if call.Failed {
			failures++
		}
	}
	return failures
}

// intOf reads a stored integer, treating a missing value as 0.
func intOf(value any) int {
	switch number := value.(type) {
	case int:
		return number
	case int64:
		return int(number)
	case float64:
		return int(number)
	default:
		return 0
	}
}

// int64Of reads a stored integer as int64, treating a missing value as 0.
func int64Of(value any) int64 {
	switch number := value.(type) {
	case int:
		return int64(number)
	case int64:
		return number
	case float64:
		return int64(number)
	default:
		return 0
	}
}
