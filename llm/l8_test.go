package llm_test

import (
	"context"
	"testing"

	"github.com/jeremygprawira/wlog"
	"github.com/jeremygprawira/wlog/llm"
	"github.com/jeremygprawira/wlog/wlogtest"
)

// TestLLM_L8_EnricherPricesOneHourCacheWrites proves the Enricher prices one hour
// cache writes at twice the input rate, the same way Cost does.
func TestLLM_L8_EnricherPricesOneHourCacheWrites(t *testing.T) {
	log, rec := wlogtest.New(t, wlog.WithEnrichers(llm.Enricher(llm.DefaultPrices())))
	ctx, end := wlog.Start(log.WithContext(context.Background()), "chat")
	llm.Set(ctx, llm.Record{
		Model:                   "claude-sonnet-4-6",
		InputTokens:             10_000,
		CacheWrite1hInputTokens: 1_000,
		OutputTokens:            100,
	})
	end()

	group := llmGroup(t, rec)
	if group["cost_micros"] != int64(34_500) {
		t.Fatalf("cost_micros = %v, want 34500", group["cost_micros"])
	}
}
