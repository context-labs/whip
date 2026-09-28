package session

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"time"
)

type (
	ModelAttemptID    string
	ModelAttemptState string
)

var ErrCostOverflow = errors.New("model cost exceeds the supported nano-USD range")

const (
	AttemptReserved   ModelAttemptState = "reserved"
	AttemptDispatched ModelAttemptState = "dispatched"
	AttemptSucceeded  ModelAttemptState = "succeeded"
	AttemptFailed     ModelAttemptState = "failed"
	AttemptCancelled  ModelAttemptState = "cancelled"
	AttemptUncertain  ModelAttemptState = "uncertain"
)

// ModelUsage retains presence independently for each reported count. Input and
// output are totals; cached input is a subset of input. Reasoning and cached
// output are disjoint subsets of output, never additional total tokens.
type ModelUsage struct {
	Input        *int64 `json:"input"`
	Output       *int64 `json:"output"`
	Reasoning    *int64 `json:"reasoning"`
	CachedInput  *int64 `json:"cached_input"`
	CachedOutput *int64 `json:"cached_output"`
}

// ModelPrices uses nano-USD per million tokens. Nil means the rate is unknown,
// including an unknown special-category rate; zero is explicitly free.
type ModelPrices struct {
	Input        *int64 `json:"input"`
	Output       *int64 `json:"output"`
	Reasoning    *int64 `json:"reasoning"`
	CachedInput  *int64 `json:"cached_input"`
	CachedOutput *int64 `json:"cached_output"`
}

func (p ModelPrices) Clone() ModelPrices {
	copyValue := func(value *int64) *int64 {
		if value == nil {
			return nil
		}
		return new(*value)
	}
	return ModelPrices{
		Input: copyValue(p.Input), Output: copyValue(p.Output), Reasoning: copyValue(p.Reasoning),
		CachedInput: copyValue(p.CachedInput), CachedOutput: copyValue(p.CachedOutput),
	}
}

func (u ModelUsage) Validate() error {
	for _, value := range []*int64{u.Input, u.Output, u.Reasoning, u.CachedInput, u.CachedOutput} {
		if value != nil && *value < 0 {
			return fmt.Errorf("%w: negative model usage", ErrInvalid)
		}
	}
	if u.Input != nil && u.CachedInput != nil && *u.CachedInput > *u.Input {
		return fmt.Errorf("%w: cached input exceeds total input", ErrInvalid)
	}
	if u.Output != nil {
		remaining := *u.Output
		for _, value := range []*int64{u.Reasoning, u.CachedOutput} {
			if value != nil {
				if *value > remaining {
					return fmt.Errorf("%w: output details exceed total output", ErrInvalid)
				}
				remaining -= *value
			}
		}
	}
	return nil
}

func (p ModelPrices) Validate() error {
	for _, value := range []*int64{p.Input, p.Output, p.Reasoning, p.CachedInput, p.CachedOutput} {
		if value != nil && *value < 0 {
			return fmt.Errorf("%w: negative model price", ErrInvalid)
		}
	}
	return nil
}

// Cost returns nil when available evidence cannot determine the price. Exact
// integer arithmetic rounds the final nano-USD upward once and rejects overflow.
//
//nolint:nilnil // An absent cost is a valid unknown value, distinct from both zero and invalid evidence.
func (p ModelPrices) Cost(u ModelUsage) (*int64, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if err := u.Validate(); err != nil {
		return nil, err
	}
	free := true
	for _, rate := range []*int64{p.Input, p.Output, p.Reasoning, p.CachedInput, p.CachedOutput} {
		if rate == nil || *rate != 0 {
			free = false
			break
		}
	}
	if free {
		return new(int64(0)), nil
	}
	sum := new(big.Int)
	add := func(count int64, rate *int64) bool {
		if count == 0 {
			return true
		}
		if rate == nil {
			return false
		}
		sum.Add(sum, new(big.Int).Mul(big.NewInt(count), big.NewInt(*rate)))
		return true
	}
	// When the special rate equals the base rate, missing detail counts do not
	// affect cost. Otherwise those counts must be explicitly known, including 0.
	detail := func(total int64, count, rate, base *int64) (int64, bool) {
		if total == 0 {
			return 0, true
		}
		if count != nil {
			return *count, true
		}
		if rate == nil && base == nil || rate != nil && base != nil && *rate == *base {
			return 0, true
		}
		return 0, false
	}
	if u.Input == nil || u.Output == nil {
		return nil, nil
	}
	cachedInput, knownInput := detail(*u.Input, u.CachedInput, p.CachedInput, p.Input)
	reasoning, knownReasoning := detail(*u.Output, u.Reasoning, p.Reasoning, p.Output)
	cachedOutput, knownOutput := detail(*u.Output, u.CachedOutput, p.CachedOutput, p.Output)
	if !knownInput || !knownReasoning || !knownOutput {
		return nil, nil
	}
	if !add(*u.Input-cachedInput, p.Input) || !add(cachedInput, p.CachedInput) || !add(*u.Output-reasoning-cachedOutput, p.Output) || !add(reasoning, p.Reasoning) || !add(cachedOutput, p.CachedOutput) {
		return nil, nil
	}
	sum.Add(sum, big.NewInt(999999))
	sum.Quo(sum, big.NewInt(1000000))
	if !sum.IsInt64() {
		return nil, fmt.Errorf("%w: %w", ErrInvalid, ErrCostOverflow)
	}
	value := sum.Int64()
	return &value, nil
}

// ModelRequestSnapshot contains dispatch provenance without credentials, provider
// objects or copied transcript bodies. The digest identifies the composed request.
type ModelRequestSnapshot struct {
	Purpose         string         `json:"purpose"`
	Model           ModelSelection `json:"model"`
	Route           string         `json:"route"`
	Adapter         string         `json:"adapter"`
	RequestDigest   string         `json:"request_digest"`
	Prices          ModelPrices    `json:"prices"`
	InputTokenBound *int64         `json:"input_token_bound"`
	MaxOutputTokens int64          `json:"max_output_tokens"`
	TimeoutMillis   int64          `json:"timeout_millis"`
}

func (s ModelRequestSnapshot) Validate() error {
	if err := ValidateID(s.Purpose); err != nil {
		return err
	}
	if err := ValidateID(s.Adapter); err != nil {
		return err
	}
	if err := s.Model.Validate(); err != nil {
		return err
	}
	digest, err := hex.DecodeString(s.RequestDigest)
	if err != nil || len(digest) != sha256.Size || strings.ToLower(s.RequestDigest) != s.RequestDigest {
		return fmt.Errorf("%w: invalid model request digest", ErrInvalid)
	}
	route, err := url.Parse(s.Route)
	if err != nil || route.User != nil || route.RawQuery != "" || route.Fragment != "" || route.Hostname() == "" || (route.Scheme != "https" && route.Scheme != "http" && route.Scheme != "scripted") || len(s.Route) > 4096 {
		return fmt.Errorf("%w: invalid model route snapshot", ErrInvalid)
	}
	if s.InputTokenBound != nil && *s.InputTokenBound < 0 {
		return fmt.Errorf("%w: negative input token bound", ErrInvalid)
	}
	if s.MaxOutputTokens < 1 || s.MaxOutputTokens > 1000000 || s.TimeoutMillis < 1 || s.TimeoutMillis > 600000 {
		return fmt.Errorf("%w: invalid model request limits", ErrInvalid)
	}
	return s.Prices.Validate()
}

type ModelAttemptResult struct {
	State               ModelAttemptState `json:"state"`
	ElapsedMillis       *int64            `json:"elapsed_millis"`
	Usage               ModelUsage        `json:"usage"`
	ReportedCostNanoUSD *int64            `json:"reported_cost_nano_usd"`
	Failure             *string           `json:"failure"`
	UsageNote           *string           `json:"usage_note"`
}

func (r ModelAttemptResult) Validate() error {
	if r.ElapsedMillis != nil && *r.ElapsedMillis < 0 {
		return fmt.Errorf("%w: negative model elapsed time", ErrInvalid)
	}
	if r.UsageNote != nil {
		if err := ValidateText(*r.UsageNote, 1024); err != nil {
			return err
		}
	}
	switch r.State {
	case AttemptSucceeded, AttemptFailed, AttemptCancelled, AttemptUncertain:
	default:
		return fmt.Errorf("%w: nonterminal attempt result", ErrInvalid)
	}
	if err := r.Usage.Validate(); err != nil {
		return err
	}
	if r.ReportedCostNanoUSD != nil && *r.ReportedCostNanoUSD < 0 {
		return fmt.Errorf("%w: negative provider cost", ErrInvalid)
	}
	if r.State == AttemptSucceeded && r.Failure != nil {
		return fmt.Errorf("%w: successful model attempt has failure", ErrInvalid)
	}
	if r.Failure != nil {
		return ValidateText(*r.Failure, 16384)
	}
	return nil
}

type ModelAttemptSpec struct {
	ID        ModelAttemptID
	TurnID    TurnID
	LogicalID string
	Number    int
	Request   ModelRequestSnapshot
}

type ModelAttempt struct {
	ID           ModelAttemptID
	TurnID       TurnID
	LogicalID    string
	Number       int
	Request      ModelRequestSnapshot
	State        ModelAttemptState
	Result       *ModelAttemptResult
	CostNanoUSD  *int64
	CostSource   string
	CostNote     *string
	MessageID    *MessageID
	CreatedAt    time.Time
	DispatchedAt *time.Time
	FinishedAt   *time.Time
}
