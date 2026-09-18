package llm

import (
	"context"
	"strings"

	"github.com/jeremygprawira/wlog"
)

// Price is one model's rate, in micros per million tokens.
type Price struct {
	// CacheWritePerMillion prices the tokens written to a prompt cache. It is above the
	// input rate for every provider that bills writes, so a writer that leaves it out prices
	// writes at the input rate and under-counts.
	CacheWritePerMillion  int64
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

// Cost prices a record from its token counts. It reports false for a model the table does
// not hold.
//
// Each part of the input carries its own rate: the cache reads at the cached rate, the cache
// writes at the write rate, and the rest at the input rate. A part the row does not price
// falls back to the input rate, which is what a provider charges when it has no cache rate,
// and never to zero.
func (p *Prices) Cost(r Record) (Cost, bool) {
	price, ok := p.Price(r.Model)
	if !ok {
		return Cost{}, false
	}

	// The cache parts are subsets of the input count. A caller that reports more cache than
	// input is clamped, so no part can go negative and no token is billed twice.
	cached := r.CachedInputTokens
	if cached > r.InputTokens {
		cached = r.InputTokens
	}
	written := r.CacheWriteInputTokens
	if written > r.InputTokens-cached {
		written = r.InputTokens - cached
	}
	regular := r.InputTokens - cached - written

	inputMicros := micros(regular, price.InputPerMillion)
	readMicros := micros(cached, firstRate(price.CachedInputPerMillion, price.InputPerMillion))
	writeMicros := micros(written, firstRate(price.CacheWritePerMillion, price.InputPerMillion))
	outputMicros := micros(r.OutputTokens, price.OutputPerMillion)

	return Cost{
		InputMicros:      inputMicros,
		CacheReadMicros:  readMicros,
		CacheWriteMicros: writeMicros,
		OutputMicros:     outputMicros,
		TotalMicros:      inputMicros + readMicros + writeMicros + outputMicros,
	}, true
}

// firstRate returns the part's own rate, or the fallback when the row leaves it out.
func firstRate(rate, fallback int64) int64 {
	if rate > 0 {
		return rate
	}
	return fallback
}

// Price returns the row for a model. An exact row wins; otherwise the longest key that the
// model id starts with, at a separator, wins, so a dated snapshot such as
// "gpt-4o-2024-08-06" prices as gpt-4o and "gpt-4o-mini-2024-07-18" prices as gpt-4o-mini
// rather than as gpt-4o.
func (p *Prices) Price(model string) (Price, bool) {
	if price, ok := p.byModel[model]; ok {
		return price, true
	}
	best := ""
	for key := range p.byModel {
		if len(key) <= len(best) || !strings.HasPrefix(model, key) {
			continue
		}
		// The boundary keeps "gpt-4o" from matching "gpt-4omini" while still matching a
		// dated or tagged snapshot.
		if rest := model[len(key):]; rest[0] != '-' && rest[0] != '.' && rest[0] != ':' && rest[0] != '@' {
			continue
		}
		best = key
	}
	if best == "" {
		return Price{}, false
	}
	return p.byModel[best], true
}

// micros prices tokens at a rate of micros per million tokens, with half-up rounding.
// It never uses a float, so the money stays exact.
func micros(tokens int, perMillion int64) int64 {
	if tokens <= 0 || perMillion <= 0 {
		return 0
	}
	return (int64(tokens)*perMillion + 500_000) / 1_000_000
}

// Enricher prices every model call the handler already put on the event. It fills
// llm.cost_micros, and marks a call whose model the table does not hold with
// llm.cost_unknown, so a dashboard can count what it failed to price.
//
// It never replaces a cost the caller already set: a caller that paid a different price, or
// that priced a model the table does not hold, keeps its own number.
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

// priceCalls prices every call and folds the results into the group total.
//
// A call the caller already priced keeps its own number and is never re-priced. Add folds
// each call's cost into the group total as it appends, so a group total that is already
// there counts those call costs; only a hand-built event whose calls carry costs and whose
// group total is still zero needs the sum of the calls instead.
func (e enricher) priceCalls(group map[string]any, calls []any) {
	groupCost := int64Of(group["cost_micros"])
	callCosts := int64(0)
	tableCosts := int64(0)
	hasUnknown := false
	hasAny := groupCost > 0

	for _, entry := range calls {
		call, _ := entry.(map[string]any)
		if call == nil {
			continue
		}
		if set := int64Of(call["cost_micros"]); set > 0 {
			callCosts += set
			hasAny = true
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
		tableCosts += cost.TotalMicros
		hasAny = true
	}

	if hasAny {
		total := groupCost + tableCosts
		if groupCost == 0 {
			total = callCosts + tableCosts
		}
		group["cost_micros"] = total
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
	if int64Of(record["cost_micros"]) > 0 {
		// The caller priced this call, so the table must not argue with it.
		return
	}
	cost, ok := e.prices.Cost(recordFrom(record))
	if !ok {
		group["cost_unknown"] = true
		return
	}
	group["cost_micros"] = cost.TotalMicros
}

// recordFrom reads the priceable fields from one event map.
func recordFrom(m map[string]any) Record {
	return Record{
		Model:                 modelOf(m),
		InputTokens:           intOf(m["input_tokens"]),
		OutputTokens:          intOf(m["output_tokens"]),
		CachedInputTokens:     intOf(m["cache_read_input_tokens"]),
		CacheWriteInputTokens: intOf(m["cache_write_input_tokens"]),
	}
}

// modelOf reads the request model name.
func modelOf(m map[string]any) string {
	model, _ := m["request_model"].(string)
	return model
}
