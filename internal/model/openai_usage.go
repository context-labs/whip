package model

import (
	"bytes"
	"encoding/json"
	"math/big"
	"strconv"

	"github.com/context-labs/whip/internal/session"
)

// decodeChatUsage retains missing counts and validates accounting independently
// from the completion. Bad accounting must not discard usable assistant output.
func decodeChatUsage(raw json.RawMessage) (session.ModelUsage, *int64, *string) {
	var fields map[string]json.RawMessage
	if len(raw) == 0 || string(raw) == "null" {
		return session.ModelUsage{}, nil, nil
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return session.ModelUsage{}, nil, new("provider usage was not an object; accounting unavailable")
	}
	var note *string
	count := func(value json.RawMessage) *int64 {
		if len(value) == 0 || string(value) == "null" {
			return nil
		}
		var number int64
		if err := json.Unmarshal(value, &number); err != nil || number < 0 {
			note = new("provider returned invalid token counts; affected counts unavailable")
			return nil
		}
		return &number
	}
	details := func(key string) map[string]json.RawMessage {
		var values map[string]json.RawMessage
		if value := fields[key]; len(value) > 0 {
			if err := json.Unmarshal(value, &values); err != nil {
				note = new("provider returned invalid token details; details unavailable")
			}
		}
		return values
	}
	input, output := details("prompt_tokens_details"), details("completion_tokens_details")
	usage := session.ModelUsage{
		Input: count(fields["prompt_tokens"]), Output: count(fields["completion_tokens"]),
		CachedInput: count(input["cached_tokens"]), Reasoning: count(output["reasoning_tokens"]),
		CachedOutput: count(output["cached_tokens"]),
	}
	if err := usage.Validate(); err != nil {
		usage = session.ModelUsage{}
		note = new("provider token totals contradict their details; counts unavailable")
	}
	var cost *int64
	if value := fields["cost"]; len(value) > 0 && string(value) != "null" {
		var ok bool
		cost, ok = usdToNano(value)
		if !ok {
			note = new("provider returned invalid accounting; reported cost unavailable")
		}
	}
	return usage, cost, note
}

// usdToNano parses a provider USD charge exactly, rounding upward only at the
// final nano-USD boundary. Numeric bounds also prevent expensive huge exponents.
func usdToNano(raw []byte) (*int64, bool) {
	if len(raw) == 0 || len(raw) > 64 || raw[0] < '0' || raw[0] > '9' {
		return nil, false
	}
	if exponent := bytes.IndexAny(raw, "eE"); exponent >= 0 {
		value, err := strconv.Atoi(string(raw[exponent+1:]))
		if err != nil || value < -128 || value > 128 {
			return nil, false
		}
	}
	value, ok := new(big.Rat).SetString(string(raw))
	if !ok || value.Sign() < 0 {
		return nil, false
	}
	value.Mul(value, big.NewRat(1000000000, 1))
	numerator := new(big.Int).Add(value.Num(), new(big.Int).Sub(value.Denom(), big.NewInt(1)))
	result := new(big.Int).Quo(numerator, value.Denom())
	if !result.IsInt64() {
		return nil, false
	}
	return new(result.Int64()), true
}
