package llm_test

import (
	"testing"

	"github.com/jeremygprawira/wlog/llm"
)

// TestLLM_L19_PrefixOnlyDateOrLatest proves a prefix prices a date or -latest only.
func TestLLM_L19_PrefixOnlyDateOrLatest(t *testing.T) {
	prices := llm.DefaultPrices()
	for _, model := range []string{"gpt-4.1-mini", "gpt-4.1-nano", "gpt-5.5-pro"} {
		if _, ok := prices.Price(model); ok {
			t.Errorf("Price(%q) priced a different model", model)
		}
	}
	for _, model := range []string{"gpt-4o-2024-08-06", "gpt-4o-latest", "gpt-4o-mini-2024-07-18"} {
		if _, ok := prices.Price(model); !ok {
			t.Errorf("Price(%q) missed a dated snapshot", model)
		}
	}
}
