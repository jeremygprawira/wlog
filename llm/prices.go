package llm

// DefaultPrices is the price table from the vendor pages the project reviewed on
// 2026-09-16. It is a convenience, not a source of truth: a provider changes a price with
// no notice, so a team that spends real money prices its own models with
// DefaultPrices().With(model, price) or builds its own table.
//
// Rates are micros per million tokens, so $2.50 per million tokens is 2_500_000.
//
// Two provider quirks are worth knowing:
//
//   - An Anthropic cache write is billed above the input rate, and the rate depends on the
//     cache's lifetime: 1.25x the input rate for the 5 minute cache and 2x for the 1 hour
//     cache. One field cannot hold both, so this table carries the 5 minute rate. A service
//     that uses the 1 hour cache must price those writes itself.
//   - An embedding model has no output, so its output rate is zero and a record with no
//     output tokens costs only its input.
func DefaultPrices() *Prices {
	return NewPrices(map[string]Price{
		// OpenAI, standard tier, short context.
		"gpt-6-astra":            {InputPerMillion: 10_000_000, CachedInputPerMillion: 1_000_000, CacheWritePerMillion: 12_500_000, OutputPerMillion: 50_000_000},
		"gpt-5.6-sol":            {InputPerMillion: 4_000_000, CachedInputPerMillion: 400_000, CacheWritePerMillion: 5_000_000, OutputPerMillion: 20_000_000},
		"gpt-5.6-terra":          {InputPerMillion: 2_000_000, CachedInputPerMillion: 200_000, CacheWritePerMillion: 2_500_000, OutputPerMillion: 12_000_000},
		"gpt-5.6-luna":           {InputPerMillion: 200_000, CachedInputPerMillion: 20_000, CacheWritePerMillion: 250_000, OutputPerMillion: 1_200_000},
		"gpt-5.5":                {InputPerMillion: 5_000_000, CachedInputPerMillion: 500_000, OutputPerMillion: 30_000_000},
		"gpt-5.4":                {InputPerMillion: 2_500_000, CachedInputPerMillion: 250_000, OutputPerMillion: 15_000_000},
		"gpt-4.1":                {InputPerMillion: 2_000_000, CachedInputPerMillion: 500_000, OutputPerMillion: 8_000_000},
		"gpt-4o":                 {InputPerMillion: 2_500_000, CachedInputPerMillion: 1_250_000, OutputPerMillion: 10_000_000},
		"gpt-4o-mini":            {InputPerMillion: 150_000, CachedInputPerMillion: 75_000, OutputPerMillion: 600_000},
		"text-embedding-3-small": {InputPerMillion: 20_000},
		"text-embedding-3-large": {InputPerMillion: 130_000},

		// Anthropic, standard tier. The cache-write rate is the 5 minute cache.
		"claude-fable-5-1":  {InputPerMillion: 10_000_000, CacheWritePerMillion: 12_500_000, CachedInputPerMillion: 250_000, OutputPerMillion: 50_000_000},
		"claude-mythos-5-1": {InputPerMillion: 10_000_000, CacheWritePerMillion: 12_500_000, CachedInputPerMillion: 250_000, OutputPerMillion: 50_000_000},
		"claude-fable-5":    {InputPerMillion: 10_000_000, CacheWritePerMillion: 12_500_000, CachedInputPerMillion: 1_000_000, OutputPerMillion: 50_000_000},
		"claude-mythos-5":   {InputPerMillion: 10_000_000, CacheWritePerMillion: 12_500_000, CachedInputPerMillion: 1_000_000, OutputPerMillion: 50_000_000},
		"claude-opus-5":     {InputPerMillion: 5_000_000, CacheWritePerMillion: 6_250_000, CachedInputPerMillion: 500_000, OutputPerMillion: 25_000_000},
		"claude-opus-4-8":   {InputPerMillion: 5_000_000, CacheWritePerMillion: 6_250_000, CachedInputPerMillion: 500_000, OutputPerMillion: 25_000_000},
		"claude-opus-4-7":   {InputPerMillion: 5_000_000, CacheWritePerMillion: 6_250_000, CachedInputPerMillion: 500_000, OutputPerMillion: 25_000_000},
		"claude-opus-4-6":   {InputPerMillion: 5_000_000, CacheWritePerMillion: 6_250_000, CachedInputPerMillion: 500_000, OutputPerMillion: 25_000_000},
		"claude-opus-4-5":   {InputPerMillion: 5_000_000, CacheWritePerMillion: 6_250_000, CachedInputPerMillion: 500_000, OutputPerMillion: 25_000_000},
		"claude-sonnet-5":   {InputPerMillion: 2_000_000, CacheWritePerMillion: 2_500_000, CachedInputPerMillion: 200_000, OutputPerMillion: 10_000_000},
		"claude-sonnet-4-6": {InputPerMillion: 3_000_000, CacheWritePerMillion: 3_750_000, CachedInputPerMillion: 300_000, OutputPerMillion: 15_000_000},
		"claude-sonnet-4-5": {InputPerMillion: 3_000_000, CacheWritePerMillion: 3_750_000, CachedInputPerMillion: 300_000, OutputPerMillion: 15_000_000},
		"claude-haiku-4-5":  {InputPerMillion: 1_000_000, CacheWritePerMillion: 1_250_000, CachedInputPerMillion: 100_000, OutputPerMillion: 5_000_000},
	})
}
