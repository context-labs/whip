package llm

import (
	"context"
	"errors"
	"math"
	"strconv"
)

// TokenPrices is an immutable snapshot of advertised USD rates per token.
// Known distinguishes a free model from one with no usable advertised prices.
type TokenPrices struct {
	Input, Output, CacheRead float64
	Known, CacheReadKnown    bool
}

func (p TokenPrices) Validate() error {
	for _, value := range []float64{p.Input, p.Output, p.CacheRead} {
		if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return errors.New("model pricing must be finite and nonnegative")
		}
	}
	return nil
}

func (p TokenPrices) CacheRate() float64 {
	if p.CacheReadKnown {
		return p.CacheRead
	}
	return p.Input
}

// TokenPrices preserves the presence of zero prices from the provider catalog.
func (p Pricing) TokenPrices() TokenPrices {
	input, inErr := strconv.ParseFloat(p.Prompt, 64)
	output, outErr := strconv.ParseFloat(p.Completion, 64)
	cached, cacheErr := strconv.ParseFloat(p.InputCacheRead, 64)
	value := TokenPrices{Input: input, Output: output, CacheRead: cached,
		Known: inErr == nil && outErr == nil, CacheReadKnown: cacheErr == nil}
	if err := value.Validate(); err != nil {
		return TokenPrices{}
	}
	if p.InputCacheRead != "" && cacheErr != nil {
		return TokenPrices{}
	}
	return value
}

// CallEstimate separates input and output so reservations use the route's rates.
type CallEstimate struct {
	PromptTokens, OutputTokens int64
	Prices                     TokenPrices
}

func beginAttempt(ctx context.Context, request Request) (func(Usage) error, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if request.BeforeAttempt != nil {
		return request.BeforeAttempt(ctx, request)
	}
	return nil, nil //nolint:nilnil // no hook means this client does not enforce application budgets
}

func (u Usage) validate() error {
	if u.PromptTokens < 0 || u.CompletionTokens < 0 || u.PromptTokens > math.MaxInt-u.CompletionTokens || u.Cached() < 0 || u.Cached() > u.PromptTokens {
		return errors.New("provider returned invalid token usage")
	}
	return nil
}

// add retains known usage from every attempt, including failed attempts. Saturate
// the display aggregate rather than wrapping; each attempt settles independently.
func (u *Usage) add(other Usage) {
	u.Dispatched = u.Dispatched || other.Dispatched
	if !other.Reported {
		return
	}
	u.Reported = true
	remaining := math.MaxInt - u.PromptTokens - u.CompletionTokens
	prompt := min(other.PromptTokens, remaining)
	u.PromptTokens += prompt
	u.CompletionTokens += min(other.CompletionTokens, remaining-prompt)
	if other.PromptTokensDetails != nil {
		if u.PromptTokensDetails == nil {
			u.PromptTokensDetails = &struct {
				CachedTokens int `json:"cached_tokens"`
			}{}
		}
		u.PromptTokensDetails.CachedTokens += min(other.Cached(), prompt)
	}
}
