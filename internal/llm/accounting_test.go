package llm

import (
	"math"
	"testing"
)

func TestTokenPricesPresence(t *testing.T) {
	for _, tc := range []struct {
		name              string
		pricing           Pricing
		known, cacheKnown bool
		cache             float64
	}{
		{name: "missing"},
		{name: "free", pricing: Pricing{Prompt: "0", Completion: "0"}, known: true},
		{name: "free cache", pricing: Pricing{Prompt: "0.01", Completion: "0.02", InputCacheRead: "0"}, known: true, cacheKnown: true},
		{name: "cache fallback", pricing: Pricing{Prompt: "0.01", Completion: "0.02"}, known: true, cache: 0.01},
		{name: "invalid", pricing: Pricing{Prompt: "NaN", Completion: "0.02"}},
		{name: "negative", pricing: Pricing{Prompt: "-1", Completion: "0.02"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.pricing.TokenPrices()
			if got.Known != tc.known || got.CacheReadKnown != tc.cacheKnown || got.CacheRate() != tc.cache {
				t.Fatalf("prices=%+v", got)
			}
		})
	}
	if err := (TokenPrices{Input: math.Inf(1)}).Validate(); err == nil {
		t.Fatal("infinite price accepted")
	}
}

func TestUsageAggregateDoesNotOverflow(t *testing.T) {
	usage := Usage{PromptTokens: math.MaxInt - 3, Reported: true}
	usage.add(Usage{PromptTokens: 2, CompletionTokens: 5, Reported: true})
	if err := usage.validate(); err != nil {
		t.Fatal(err)
	}
	if usage.PromptTokens+usage.CompletionTokens != math.MaxInt {
		t.Fatalf("aggregate=%+v", usage)
	}
	usage.add(Usage{CompletionTokens: 5, Reported: true})
	if err := usage.validate(); err != nil {
		t.Fatal(err)
	}
}
