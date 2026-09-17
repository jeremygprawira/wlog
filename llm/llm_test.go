package llm_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/llm"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestLLM_CAT1_CallsCapped proves the two call arrays stop at 200 entries and that every
// entry past the cap is counted in wlog.dropped_fields, so an agent loop cannot grow one
// event without bound and cannot lose the fact that it tried (gate G4).
func TestLLM_CAT1_CallsCapped(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "agent.run")

	const attempts = 250
	for i := 0; i < attempts; i++ {
		llm.Add(ctx, llm.Record{
			Model:       "gpt-4o",
			InputTokens: 1,
			ToolCalls:   []llm.ToolCall{{Name: "search"}, {Name: "refund"}},
		})
	}
	end()

	group, _ := rec.Last()["llm"].(map[string]any)
	calls, _ := group["calls"].([]any)
	if len(calls) != 200 {
		t.Errorf("llm.calls holds %d entries, want the cap of 200", len(calls))
	}
	toolCalls, _ := group["tool_calls"].([]any)
	if len(toolCalls) != 200 {
		t.Errorf("llm.tool_calls holds %d entries, want the cap of 200", len(toolCalls))
	}
	if got := intOf(rec.Last()["wlog.dropped_fields"]); got != 350 {
		t.Errorf("wlog.dropped_fields = %v, want 350 (50 calls and 300 tool calls)", got)
	}

	// The totals still count every attempt, so a reader sees the work that happened.
	if got := intOf(group["input_tokens"]); got != attempts {
		t.Errorf("input_tokens = %v, want every attempt", got)
	}
	if got := intOf(group["tool_call_count"]); got != attempts*2 {
		t.Errorf("tool_call_count = %v, want every tool call", got)
	}
}

// TestLLM_CAT4_KeepsCallerCost proves the enricher never replaces a cost the caller set,
// and that a record written by Set before Add adds up instead of being overwritten.
func TestLLM_CAT4_KeepsCallerCost(t *testing.T) {
	prices := llm.NewPrices(map[string]llm.Price{
		"gpt-4o": {InputPerMillion: 1_000_000, OutputPerMillion: 2_000_000},
	})
	log, rec := wlogtest.New(t, wlog.WithEnrichers(llm.Enricher(prices)))

	// A caller who knows the real cost keeps it, even when the table could price the call.
	ctx, end := wlog.Start(log.WithContext(context.Background()), "agent.run")
	llm.Set(ctx, llm.Record{Model: "gpt-4o", InputTokens: 100, Cost: &llm.Cost{TotalMicros: 777}})
	end()
	group, _ := rec.Last()["llm"].(map[string]any)
	if got := intOf(group["cost_micros"]); got != 777 {
		t.Errorf("cost_micros = %v, want the caller's 777", group["cost_micros"])
	}
	if got := intOf(group["input_tokens"]); got != 100 {
		t.Errorf("input_tokens = %v, want the caller's record left alone", group["input_tokens"])
	}

	// Set writes one record, Add folds another in: the two costs add up.
	ctx, end = wlog.Start(log.WithContext(context.Background()), "agent.run")
	llm.Set(ctx, llm.Record{Model: "gpt-4o", InputTokens: 100, Cost: &llm.Cost{TotalMicros: 700}})
	llm.Add(ctx, llm.Record{Model: "gpt-4o", InputTokens: 100, Cost: &llm.Cost{TotalMicros: 300}})
	end()
	group, _ = rec.Last()["llm"].(map[string]any)
	if got := intOf(group["cost_micros"]); got != 1000 {
		t.Errorf("cost_micros = %v, want 700 + 300", group["cost_micros"])
	}

	// With no caller cost, the table still prices the call.
	ctx, end = wlog.Start(log.WithContext(context.Background()), "agent.run")
	llm.Set(ctx, llm.Record{Model: "gpt-4o", InputTokens: 1_000_000, OutputTokens: 1_000_000})
	end()
	group, _ = rec.Last()["llm"].(map[string]any)
	if got := intOf(group["cost_micros"]); got != 3_000_000 {
		t.Errorf("cost_micros = %v, want the table's 3000000", group["cost_micros"])
	}
}

// TestLLM_CAT11_NoCostUSD proves money never reaches the event as a float: the record and
// the priced event carry integer micros only.
func TestLLM_CAT11_NoCostUSD(t *testing.T) {
	log, rec := wlogtest.New(t, wlog.WithEnrichers(llm.Enricher(llm.DefaultPrices())))
	ctx, end := wlog.Start(log.WithContext(context.Background()), "agent.run")
	llm.Set(ctx, llm.Record{
		Model:        "gpt-4o",
		InputTokens:  1_000_000,
		OutputTokens: 1_000_000,
		Cost:         &llm.Cost{TotalMicros: 5},
	})
	end()
	assertNoCostUSD(t, "Set", rec.Last())

	ctx, end = wlog.Start(log.WithContext(context.Background()), "agent.run")
	llm.Add(ctx, llm.Record{Model: "gpt-4o", InputTokens: 1_000_000, ToolCalls: []llm.ToolCall{{Name: "search"}}})
	end()
	assertNoCostUSD(t, "Add", rec.Last())
}

// assertNoCostUSD fails when an event carries a float dollar amount anywhere.
func assertNoCostUSD(t *testing.T, how string, event map[string]any) {
	t.Helper()
	if strings.Contains(strings.Join(keysOf(event), " "), "cost_usd") {
		t.Errorf("%s put cost_usd on the event: %v", how, event)
	}
	group, _ := event["llm"].(map[string]any)
	if group == nil {
		t.Fatalf("%s produced no llm group: %v", how, event)
	}
	if _, ok := group["cost_usd"]; ok {
		t.Errorf("%s put llm.cost_usd on the event: %v", how, group)
	}
	if _, ok := group["cost_micros"]; !ok {
		t.Errorf("%s left no llm.cost_micros: %v", how, group)
	}
	if calls, ok := group["calls"].([]any); ok {
		for _, entry := range calls {
			call, _ := entry.(map[string]any)
			if _, ok := call["cost_usd"]; ok {
				t.Errorf("%s put cost_usd on a call: %v", how, call)
			}
		}
	}
}

// keysOf returns the top-level keys of an event, including a reserved key that carries a
// dot such as wlog.dropped_fields.
func keysOf(event map[string]any) []string {
	keys := make([]string, 0, len(event))
	for key := range event {
		keys = append(keys, key)
	}
	return keys
}

// intOf reads an integer field whatever integer width it arrived as.
func intOf(value any) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	default:
		return 0
	}
}
