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

// TestLLM_CAT2_TokenInvariants proves the record's cache fields are subsets of the input
// count, that the event carries the OTel key set, and that a caller who reports more cache
// than input still gets a coherent total instead of a negative one.
func TestLLM_CAT2_TokenInvariants(t *testing.T) {
	log, rec := wlogtest.New(t)
	ctx, end := wlog.Start(log.WithContext(context.Background()), "agent.run")
	llm.Set(ctx, llm.Record{
		Model:                 "claude-sonnet-5",
		InputTokens:           1_050,
		CachedInputTokens:     1_000,
		CacheWriteInputTokens: 50,
		OutputTokens:          200,
		ReasoningTokens:       20,
	})
	end()

	group, _ := rec.Last()["llm"].(map[string]any)
	for _, key := range []string{
		"input_tokens", "output_tokens", "total_tokens",
		"cached_input_tokens", "cache_write_input_tokens", "reasoning_tokens",
	} {
		if _, ok := group[key]; !ok {
			t.Errorf("llm.%s is missing from the event: %v", key, group)
		}
	}
	if got := intOf(group["total_tokens"]); got != 1_250 {
		t.Errorf("total_tokens = %v, want input + output", group["total_tokens"])
	}

	// A caller that reports more cache than input cannot turn a cost negative.
	prices := llm.NewPrices(map[string]llm.Price{"m": {InputPerMillion: 1_000_000, CachedInputPerMillion: 100_000}})
	cost, ok := prices.Cost(llm.Record{Model: "m", InputTokens: 10, CachedInputTokens: 500})
	if !ok {
		t.Fatal("Cost refused a known model")
	}
	if cost.TotalMicros < 0 {
		t.Errorf("Cost = %d, want no negative part", cost.TotalMicros)
	}
}

// TestLLM_CAT2_CostParts proves each token part is priced at its own rate, including both
// cache kinds, so a cache-heavy call is not billed as fresh input.
func TestLLM_CAT2_CostParts(t *testing.T) {
	prices := llm.NewPrices(map[string]llm.Price{
		"claude-sonnet-5": {
			InputPerMillion:       10_000_000,
			OutputPerMillion:      50_000_000,
			CachedInputPerMillion: 1_000_000,
			CacheWritePerMillion:  12_500_000,
		},
	})

	cost, ok := prices.Cost(llm.Record{
		Model:                 "claude-sonnet-5",
		InputTokens:           3_000_000,
		CachedInputTokens:     1_000_000,
		CacheWriteInputTokens: 1_000_000,
		OutputTokens:          1_000_000,
	})
	if !ok {
		t.Fatal("Cost refused a known model")
	}

	// One million uncached input, one million read, one million written, one million out.
	if cost.InputMicros != 10_000_000 {
		t.Errorf("InputMicros = %d, want 10 million for the uncached part", cost.InputMicros)
	}
	if cost.CacheReadMicros != 1_000_000 {
		t.Errorf("CacheReadMicros = %d, want 1 million", cost.CacheReadMicros)
	}
	if cost.CacheWriteMicros != 12_500_000 {
		t.Errorf("CacheWriteMicros = %d, want 12.5 million", cost.CacheWriteMicros)
	}
	if cost.OutputMicros != 50_000_000 {
		t.Errorf("OutputMicros = %d, want 50 million", cost.OutputMicros)
	}
	if want := int64(73_500_000); cost.TotalMicros != want {
		t.Errorf("TotalMicros = %d, want %d", cost.TotalMicros, want)
	}

	// The Anthropic example from the research: 50 fresh tokens and 100,000 cache reads, at
	// the old $3 input and $0.30 cache-hit rates, is 30,150 micros and not 15.
	old := llm.NewPrices(map[string]llm.Price{
		"claude": {InputPerMillion: 3_000_000, OutputPerMillion: 15_000_000, CachedInputPerMillion: 300_000},
	})
	cost, ok = old.Cost(llm.Record{Model: "claude", InputTokens: 100_050, CachedInputTokens: 100_000})
	if !ok {
		t.Fatal("Cost refused the prefix model")
	}
	if cost.TotalMicros != 30_150 {
		t.Errorf("TotalMicros = %d, want 30150", cost.TotalMicros)
	}
}

// TestLLM_CAT3_SnapshotPrefix proves a dated snapshot prices as its model, that the longest
// matching prefix wins, and that the table holds only the rows the pricing pages list.
func TestLLM_CAT3_SnapshotPrefix(t *testing.T) {
	prices := llm.DefaultPrices()

	cases := []struct {
		model string
		want  int64
		ok    bool
	}{
		// A dated snapshot prices as its model: gpt-4o-2024-08-06 is $2.50 per MTok in.
		{"gpt-4o-2024-08-06", 2_500_000, true},
		// The longest prefix wins: the mini's snapshot must not use the gpt-4o row.
		{"gpt-4o-mini-2024-07-18", 150_000, true},
		// An exact row wins over any prefix.
		{"gpt-4o", 2_500_000, true},
		// A suffix prices as its base model, which is how a dated snapshot is priced. A model
		// that is not even a suffix of a row is unknown instead.
		{"gpt-4o-omni", 2_500_000, true},
		{"text-embedding-3", 0, false},
		{"llama-3-70b", 0, false},
	}
	for _, tc := range cases {
		price, ok := prices.Price(tc.model)
		if ok != tc.ok {
			t.Errorf("Price(%q) ok = %v, want %v", tc.model, ok, tc.ok)
			continue
		}
		if ok && price.InputPerMillion != tc.want {
			t.Errorf("Price(%q).InputPerMillion = %d, want %d", tc.model, price.InputPerMillion, tc.want)
		}
	}

	// The ids the pricing pages do list are present.
	for _, model := range []string{
		"gpt-6-astra", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-5.5", "gpt-5.4",
		"gpt-4.1", "gpt-4o", "gpt-4o-mini", "text-embedding-3-small", "text-embedding-3-large",
		"claude-sonnet-5", "claude-sonnet-4-6", "claude-opus-5", "claude-haiku-4-5",
	} {
		if _, ok := prices.Price(model); !ok {
			t.Errorf("DefaultPrices holds no row for %q", model)
		}
	}
	// The rows the research corrected are gone: an id with a dot, and a family name that is
	// no model id.
	for _, stale := range []string{"claude-haiku-4.5", "text-embedding-3"} {
		if _, ok := prices.Price(stale); ok {
			t.Errorf("DefaultPrices still holds a row for %q", stale)
		}
	}
}
