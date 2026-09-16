package llm_test

import (
	"context"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/llm"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// testPrices is a fixed table, so a later change to DefaultPrices cannot break a test.
func testPrices() *llm.Prices {
	return llm.NewPrices(map[string]llm.Price{
		"model-a": {InputPerMillion: 3_000_000, OutputPerMillion: 15_000_000, CachedInputPerMillion: 300_000},
	})
}

// TestLLM_Cost_PricesTokens proves input, output, and cached tokens are priced, with the
// cached token at the cached rate.
func TestLLM_Cost_PricesTokens(t *testing.T) {
	cost, ok := testPrices().Cost(llm.Record{
		Model: "model-a", InputTokens: 10, CachedInputTokens: 4, OutputTokens: 2,
	})
	if !ok {
		t.Fatal("Cost reported false for a known model")
	}
	// 6 regular input at 3 micros each = 18, 4 cached at 0.3 micros each = 1.2 -> 1
	// with half-up rounding, 2 output at 15 each = 30.
	if cost.InputMicros != 19 || cost.OutputMicros != 30 || cost.TotalMicros != 49 {
		t.Errorf("cost = %+v, want input 19, output 30, total 49", cost)
	}
}

// TestLLM_Cost_ExactMicros proves money stays in whole micros: a dollar value with no
// exact float form is still counted exactly.
func TestLLM_Cost_ExactMicros(t *testing.T) {
	prices := llm.NewPrices(map[string]llm.Price{"m": {InputPerMillion: 3_000_000}})
	cost, ok := prices.Cost(llm.Record{Model: "m", InputTokens: 1})
	if !ok {
		t.Fatal("Cost reported false")
	}
	if cost.TotalMicros != 3 {
		t.Errorf("TotalMicros = %d, want exactly 3", cost.TotalMicros)
	}
	if cost.USD() != 0.000003 {
		t.Logf("USD() = %.12f (a float, informational only)", cost.USD())
	}
	total := int64(0)
	for i := 0; i < 10; i++ {
		c, _ := prices.Cost(llm.Record{Model: "m", InputTokens: 1})
		total += c.TotalMicros
	}
	if total != 30 {
		t.Errorf("ten calls summed to %d micros, want 30", total)
	}
}

// TestLLM_Cost_UnknownModel proves an unknown model reports false.
func TestLLM_Cost_UnknownModel(t *testing.T) {
	if _, ok := testPrices().Cost(llm.Record{Model: "nope", InputTokens: 1}); ok {
		t.Error("Cost reported true for an unknown model")
	}
}

// TestLLM_Prices_WithIsImmutable proves With returns a new table and the original is
// unchanged, under -race.
func TestLLM_Prices_WithIsImmutable(t *testing.T) {
	original := testPrices()
	extended := original.With("model-b", llm.Price{InputPerMillion: 1_000_000})

	if _, ok := original.Cost(llm.Record{Model: "model-b"}); ok {
		t.Error("the original table learned the new model")
	}
	if _, ok := extended.Cost(llm.Record{Model: "model-b", InputTokens: 1}); !ok {
		t.Error("the new table lost the added model")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			_ = original.With("model-c", llm.Price{InputPerMillion: 2})
		}
	}()
	for i := 0; i < 100; i++ {
		_, _ = original.Cost(llm.Record{Model: "model-a", InputTokens: 1})
	}
	<-done
}

// TestLLM_Enricher_PricesCalls proves the enricher prices every call and folds the
// total.
func TestLLM_Enricher_PricesCalls(t *testing.T) {
	log, rec := wlogtest.New(t, wlog.WithEnrichers(llm.Enricher(testPrices())))
	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "chat")
	llm.Add(ctx, llm.Record{Model: "model-a", InputTokens: 1})
	llm.Add(ctx, llm.Record{Model: "model-a", InputTokens: 1})
	end()

	group := llmGroup(t, rec)
	if group["cost_micros"] != int64(6) {
		t.Errorf("cost_micros = %v, want 6", group["cost_micros"])
	}
	calls, _ := group["calls"].([]any)
	for i, entry := range calls {
		call, _ := entry.(map[string]any)
		if call["cost_micros"] != int64(3) {
			t.Errorf("call %d cost_micros = %v, want 3", i, call["cost_micros"])
		}
	}
}

// TestLLM_Enricher_UnknownModel proves an unknown model marks the event instead of
// pricing it as zero.
func TestLLM_Enricher_UnknownModel(t *testing.T) {
	log, rec := wlogtest.New(t, wlog.WithEnrichers(llm.Enricher(testPrices())))
	ctx := log.WithContext(context.Background())
	ctx, end := wlog.Start(ctx, "chat")
	llm.Set(ctx, llm.Record{Model: "mystery-model", InputTokens: 5})
	end()

	group := llmGroup(t, rec)
	if group["cost_unknown"] != true {
		t.Errorf("cost_unknown = %v, want true", group["cost_unknown"])
	}
	if _, present := group["cost_micros"]; present {
		t.Errorf("cost_micros present for an unknown model: %v", group["cost_micros"])
	}
}
