package llm

import (
	"context"

	"github.com/jeremygprawira/wlog"
)

// Price is one model's rate, in micros per million tokens.
type Price struct {
	InputPerMillion       int64
	OutputPerMillion      int64
	CachedInputPerMillion int64
}

// Prices is an immutable price table. NewPrices copies the input map, and With returns
// a new table, so a caller can read one table from many goroutines.
type Prices struct {
	byModel map[string]Price
}

// NewPrices builds a table from a model-to-price map.
func NewPrices(byModel map[string]Price) *Prices {
	copied := make(map[string]Price, len(byModel))
	for model, price := range byModel {
		copied[model] = price
	}
	return &Prices{byModel: copied}
}

// With returns a new table with one model added or replaced. The original is unchanged.
func (p *Prices) With(model string, price Price) *Prices {
	copied := make(map[string]Price, len(p.byModel)+1)
	for name, existing := range p.byModel {
		copied[name] = existing
	}
	copied[model] = price
	return &Prices{byModel: copied}
}

// Cost prices a record from its token counts. It reports false for a model the table
// does not hold. A cached input token is priced at the cached rate, and the rest of the
// input tokens at the input rate.
func (p *Prices) Cost(r Record) (Cost, bool) {
	price, ok := p.byModel[r.Model]
	if !ok {
		return Cost{}, false
	}
	cached := r.CachedInputTokens
	if cached > r.InputTokens {
		cached = r.InputTokens
	}
	regular := r.InputTokens - cached
	input := micros(regular, price.InputPerMillion) + micros(cached, price.CachedInputPerMillion)
	output := micros(r.OutputTokens, price.OutputPerMillion)
	return Cost{InputMicros: input, OutputMicros: output, TotalMicros: input + output}, true
}

// micros prices tokens at a rate of micros per million tokens, with half-up rounding.
// It never uses a float, so the money stays exact.
func micros(tokens int, perMillion int64) int64 {
	if tokens <= 0 || perMillion <= 0 {
		return 0
	}
	return (int64(tokens)*perMillion + 500_000) / 1_000_000
}

// DefaultPrices is a small convenience table, checked 2026-09-16. It is not a source
// of truth: a caller overrides any row with With, and prices change often.
func DefaultPrices() *Prices {
	return NewPrices(map[string]Price{
		"gpt-4o":           {InputPerMillion: 2_500_000, OutputPerMillion: 10_000_000, CachedInputPerMillion: 1_250_000},
		"gpt-4o-mini":      {InputPerMillion: 150_000, OutputPerMillion: 600_000, CachedInputPerMillion: 75_000},
		"claude-sonnet-5":  {InputPerMillion: 3_000_000, OutputPerMillion: 15_000_000, CachedInputPerMillion: 300_000},
		"claude-haiku-4.5": {InputPerMillion: 1_000_000, OutputPerMillion: 5_000_000, CachedInputPerMillion: 100_000},
		"text-embedding-3": {InputPerMillion: 20_000, OutputPerMillion: 0},
	})
}

// Enricher prices every model call the handler already put on the event. It writes
// llm.cost_micros and llm.cost_usd, and marks a call whose model the table does not
// hold with llm.cost_unknown, so a dashboard can count what it failed to price.
func Enricher(p *Prices) wlog.Enricher {
	return wlog.EnricherFunc(func(_ context.Context, event map[string]any) {
		e := enricher{prices: p}
		e.Enrich(event)
	})
}

// enricher holds the price table behind the returned wlog.Enricher.
type enricher struct{ prices *Prices }

// Enrich prices the llm group, or each call in llm.calls[] when the handler used Add.
func (e enricher) Enrich(event map[string]any) {
	group, _ := event["llm"].(map[string]any)
	if group == nil {
		return
	}
	if calls, ok := group["calls"].([]any); ok && len(calls) > 0 {
		e.priceCalls(group, calls)
		return
	}
	e.priceRecord(group, group)
}

// priceCalls prices every call and folds the results into the group totals.
func (e enricher) priceCalls(group map[string]any, calls []any) {
	var total int64
	hasUnknown := false
	hasCost := false
	for _, entry := range calls {
		call, _ := entry.(map[string]any)
		if call == nil {
			continue
		}
		cost, ok := e.prices.Cost(recordFrom(call))
		if !ok {
			if modelOf(call) != "" {
				hasUnknown = true
			}
			continue
		}
		call["cost_micros"] = cost.TotalMicros
		call["cost_usd"] = cost.USD()
		total += cost.TotalMicros
		hasCost = true
	}
	if hasCost {
		group["cost_micros"] = total
		group["cost_usd"] = Cost{TotalMicros: total}.USD()
	}
	if hasUnknown {
		group["cost_unknown"] = true
	}
}

// priceRecord prices one record in place.
func (e enricher) priceRecord(group, record map[string]any) {
	if modelOf(record) == "" {
		return
	}
	cost, ok := e.prices.Cost(recordFrom(record))
	if !ok {
		group["cost_unknown"] = true
		return
	}
	group["cost_micros"] = cost.TotalMicros
	group["cost_usd"] = cost.USD()
}

// recordFrom reads the priceable fields from one event map.
func recordFrom(m map[string]any) Record {
	return Record{
		Model:             modelOf(m),
		InputTokens:       intOf(m["input_tokens"]),
		OutputTokens:      intOf(m["output_tokens"]),
		CachedInputTokens: intOf(m["cached_input_tokens"]),
	}
}

// modelOf reads the model name.
func modelOf(m map[string]any) string {
	model, _ := m["model"].(string)
	return model
}
