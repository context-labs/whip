package llm

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"
)

// CallAccounting fixes the owner and price snapshot for one logical model call.
// It is request-local because a client can serve several agents concurrently.
type CallAccounting struct {
	Budget            ModelCallBudget
	Purpose, Provider string
	Pricing           Pricing
}

// ModelCallBudget admits and settles each actual provider attempt separately.
type ModelCallBudget interface {
	BeginModelAttempt(context.Context, ModelAttempt) (ModelPermit, error)
}

type ModelAttempt struct {
	LogicalID                string
	Number                   int
	Model, Provider, Purpose string
	Pricing                  Pricing
	InputTokens              int64
	MaxTokens                int
	Timeout                  time.Duration
}

// ModelPermit limits this attempt, and owns its durable settlement identity.
type ModelPermit struct {
	ID        string
	MaxTokens int
	Timeout   time.Duration
	Settle    func(ModelAttemptResult) error
}

type ModelAttemptResult struct {
	Usage      Usage
	Dispatched bool
	Elapsed    time.Duration
	Failed     bool
}

// AccountingError means admission or settlement could not complete. A returned
// provider response remains valid and must be retained, but its tools must not run.
type AccountingError struct {
	Err              error
	ResponseComplete bool
}

func (e *AccountingError) Error() string { return "model accounting: " + e.Err.Error() }
func (e *AccountingError) Unwrap() error { return e.Err }
func IsAccountingError(err error) bool {
	_, ok := errors.AsType[*AccountingError](err)
	return ok
}

// IsCompletedAccountingError identifies a complete response whose accounting
// failed, as distinct from an interrupted response with a settlement failure.
func IsCompletedAccountingError(err error) bool {
	e, ok := errors.AsType[*AccountingError](err)
	return ok && e.ResponseComplete
}

// HasUsage distinguishes complete token usage, including zero, from absent or
// partial counts. Nonzero internal literals count until decoded from the wire.
func (u Usage) HasUsage() bool {
	return u.Reported || !u.decoded && (u.PromptTokens != 0 || u.CompletionTokens != 0 || u.PromptTokensDetails != nil)
}

func (u Usage) Validate() error {
	if u.PromptTokens < 0 || u.CompletionTokens < 0 || u.Cached() < 0 || u.Cached() > u.PromptTokens {
		return errors.New("invalid model token usage")
	}
	if !u.HasUsage() && (u.PromptTokens != 0 || u.CompletionTokens != 0 || u.PromptTokensDetails != nil || u.CompletionTokensDetails != nil) {
		return errors.New("incomplete model token usage")
	}
	if int64(u.PromptTokens) > math.MaxInt64-int64(u.CompletionTokens) {
		return errors.New("model token usage overflows int64")
	}
	if u.CompletionTokensDetails != nil && (u.CompletionTokensDetails.ReasoningTokens < 0 || u.CompletionTokensDetails.ReasoningTokens > u.CompletionTokens) {
		return errors.New("invalid model reasoning token usage")
	}
	if u.Cost != nil && (math.IsNaN(*u.Cost) || math.IsInf(*u.Cost, 0) || *u.Cost < 0) {
		return errors.New("invalid provider model cost")
	}
	return nil
}

func (u *Usage) UnmarshalJSON(data []byte) error {
	type plain Usage
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*u = Usage(decoded)
	u.decoded = true
	var presence struct {
		Reported         *bool `json:"reported"`
		PromptTokens     *int  `json:"prompt_tokens"`
		CompletionTokens *int  `json:"completion_tokens"`
	}
	if err := json.Unmarshal(data, &presence); err != nil {
		return err
	}
	if presence.Reported == nil {
		u.Reported = presence.PromptTokens != nil && presence.CompletionTokens != nil
	}
	return nil
}

func (u Usage) MarshalJSON() ([]byte, error) {
	type plain Usage
	u.Reported = u.HasUsage()
	return json.Marshal(plain(u))
}

// add retains known token usage from retries for display, saturating instead of
// wrapping. Monetary accounting settles the original attempts independently.
// The aggregate charge is only present when every attempt reported one.
func (u *Usage) add(other Usage) {
	if !u.aggregated {
		*u = other
		if other.PromptTokensDetails != nil {
			details := *other.PromptTokensDetails
			u.PromptTokensDetails = &details
		}
		if other.CompletionTokensDetails != nil {
			details := *other.CompletionTokensDetails
			u.CompletionTokensDetails = &details
		}
		u.aggregated = true
		return
	}
	if u.Cost != nil && other.Cost != nil {
		sum := *u.Cost + *other.Cost
		if !math.IsInf(sum, 0) && !math.IsNaN(sum) {
			u.Cost = &sum
		} else {
			u.Cost = nil
		}
	} else {
		u.Cost = nil
	}
	if !other.HasUsage() {
		return
	}
	u.Reported = true
	remaining := math.MaxInt - u.PromptTokens - u.CompletionTokens
	prompt := min(other.PromptTokens, remaining)
	completion := min(other.CompletionTokens, remaining-prompt)
	u.PromptTokens += prompt
	u.CompletionTokens += completion
	if other.PromptTokensDetails != nil {
		if u.PromptTokensDetails == nil {
			u.PromptTokensDetails = &struct {
				CachedTokens int `json:"cached_tokens"`
			}{}
		}
		u.PromptTokensDetails.CachedTokens += min(other.Cached(), prompt)
	}
	if other.CompletionTokensDetails != nil {
		if u.CompletionTokensDetails == nil {
			u.CompletionTokensDetails = &struct {
				ReasoningTokens int `json:"reasoning_tokens"`
			}{}
		}
		u.CompletionTokensDetails.ReasoningTokens += min(other.CompletionTokensDetails.ReasoningTokens, completion)
	}
}

// Known reports whether valid input and output prices were explicitly provided.
// Zero is a known free rate; an empty field is unavailable.
func (p Pricing) Known() bool {
	_, _, _, known := p.ratesExact()
	return known
}

func priceRate(s string) (*big.Rat, error) {
	if len(s) > 128 {
		return nil, errors.New("invalid model price")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return nil, fmt.Errorf("invalid model price %q", s)
	}
	// Bound decimal exponents before math/big allocates their powers of ten.
	if index := strings.IndexAny(s, "eE"); index >= 0 {
		exponent, err := strconv.Atoi(s[index+1:])
		if err != nil || exponent < -324 || exponent > 308 {
			return nil, errors.New("model price exponent out of range")
		}
	}
	rate, ok := new(big.Rat).SetString(s)
	if !ok {
		return nil, errors.New("invalid decimal model price")
	}
	return rate, nil
}

func (p Pricing) ratesExact() (input, output, cache *big.Rat, known bool) {
	if p.Prompt == "" || p.Completion == "" {
		return nil, nil, nil, false
	}
	var err error
	input, err = priceRate(p.Prompt)
	if err != nil {
		return nil, nil, nil, false
	}
	output, err = priceRate(p.Completion)
	if err != nil {
		return nil, nil, nil, false
	}
	cache = input
	if p.InputCacheRead != "" {
		cache, err = priceRate(p.InputCacheRead)
		if err != nil {
			return nil, nil, nil, false
		}
	}
	return input, output, cache, true
}

func micros(cost *big.Rat) (int64, error) {
	scaled := new(big.Rat).Mul(cost, big.NewRat(1_000_000, 1))
	whole, remainder := new(big.Int), new(big.Int)
	whole.QuoRem(scaled.Num(), scaled.Denom(), remainder)
	if remainder.Sign() > 0 {
		whole.Add(whole, big.NewInt(1))
	}
	if whole.Sign() < 0 || !whole.IsInt64() {
		return 0, errors.New("model cost overflows int64")
	}
	return whole.Int64(), nil
}

// ReserveCost returns a conservative cost in millionths of a dollar. Without
// known prices it returns zero; callers must use Known to distinguish that case.
func (p Pricing) ReserveCost(inputTokens, outputTokens int64) (int64, error) {
	if inputTokens < 0 || outputTokens < 0 {
		return 0, errors.New("negative model token reservation")
	}
	input, output, cache, known := p.ratesExact()
	if !known {
		return 0, nil
	}
	if cache.Cmp(input) > 0 {
		input = cache
	}
	cost := new(big.Rat).Mul(input, big.NewRat(inputTokens, 1))
	cost.Add(cost, new(big.Rat).Mul(output, big.NewRat(outputTokens, 1)))
	return micros(cost)
}

// ActualCost prefers the provider's charge, including an explicit zero. Token
// pricing is only a fallback, and cached/reasoning tokens are never added twice.
func (p Pricing) ActualCost(u Usage) (int64, bool, error) {
	if err := u.Validate(); err != nil {
		return 0, false, err
	}
	if u.Cost != nil {
		cost, err := priceRate(strconv.FormatFloat(*u.Cost, 'g', -1, 64))
		if err != nil {
			return 0, false, err
		}
		amount, err := micros(cost)
		return amount, true, err
	}
	input, output, cache, known := p.ratesExact()
	if !known {
		return 0, false, nil
	}
	if !u.HasUsage() {
		// All effective rates being zero proves the token-priced cost even
		// when the provider omitted token counts. Token usage stays unknown.
		free := input.Sign() == 0 && output.Sign() == 0 && cache.Sign() == 0
		return 0, free, nil
	}
	cost := new(big.Rat).Mul(input, big.NewRat(int64(u.PromptTokens-u.Cached()), 1))
	cost.Add(cost, new(big.Rat).Mul(cache, big.NewRat(int64(u.Cached()), 1)))
	cost.Add(cost, new(big.Rat).Mul(output, big.NewRat(int64(u.CompletionTokens), 1)))
	amount, err := micros(cost)
	return amount, true, err
}

// EstimateTokens approximates input with message framing, tool calls and images.
func EstimateTokens(messages []Message) int {
	total := 0
	add := func(tokens int) {
		if tokens > math.MaxInt-total {
			total = math.MaxInt
		} else {
			total += tokens
		}
	}
	for _, message := range messages {
		add(4)
		add((len(message.Content) + 3) / 4)
		for _, part := range message.Parts {
			add(PartTokens(part))
		}
		for _, call := range message.ToolCalls {
			add(8)
			add((len(call.Function.Name) + len(call.Function.Arguments) + 3) / 4)
		}
	}
	return total
}

func requestTokens(req Request) (int64, error) {
	input := int64(EstimateTokens(req.Messages))
	if len(req.Tools) == 0 {
		return input, nil
	}
	definitions, err := json.Marshal(req.Tools)
	if err != nil {
		return 0, err
	}
	toolTokens := int64((len(definitions) + 3) / 4)
	if input > math.MaxInt64-toolTokens {
		return 0, errors.New("model input estimate overflows int64")
	}
	return input + toolTokens, nil
}

const defaultCallTimeout = 10 * time.Minute

func (c *Client) callContext(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := defaultCallTimeout
	if c.HTTP != nil && c.HTTP.Timeout > 0 {
		timeout = c.HTTP.Timeout
	}
	return context.WithTimeout(ctx, timeout)
}

func (c *Client) runAttempt(ctx context.Context, req Request, logicalID string, number int, invoke func(context.Context, []byte) (Message, Usage, error)) (Message, Usage, error) {
	if err := ctx.Err(); err != nil {
		return Message{}, Usage{}, err
	}
	deadline, _ := ctx.Deadline()
	requestedMaxTokens := max(req.MaxTokens, 1)
	permit := ModelPermit{MaxTokens: req.MaxTokens, Timeout: time.Until(deadline)}
	if req.Accounting != nil && req.Accounting.Budget != nil {
		input, err := requestTokens(req)
		if err != nil {
			return Message{}, Usage{}, err
		}
		accounting := req.Accounting
		permit, err = accounting.Budget.BeginModelAttempt(ctx, ModelAttempt{
			LogicalID: logicalID, Number: number, Model: req.Model, Provider: accounting.Provider,
			Purpose: accounting.Purpose, Pricing: accounting.Pricing, InputTokens: input,
			MaxTokens: requestedMaxTokens, Timeout: permit.Timeout,
		})
		if err != nil {
			return Message{}, Usage{}, &AccountingError{Err: err}
		}
		req.MaxTokens = permit.MaxTokens
	}
	settle := func(result ModelAttemptResult, err error) error {
		if permit.Settle != nil {
			if settlementErr := permit.Settle(result); settlementErr != nil {
				return &AccountingError{Err: errors.Join(err, settlementErr), ResponseComplete: err == nil && result.Dispatched && !result.Failed}
			}
		}
		return err
	}
	if req.Accounting != nil && req.Accounting.Budget != nil && (permit.MaxTokens <= 0 || permit.MaxTokens > requestedMaxTokens || permit.Settle == nil) {
		err := &AccountingError{Err: errors.New("invalid model call permit")}
		return Message{}, Usage{}, settle(ModelAttemptResult{Failed: true}, err)
	}
	req.Messages = stripAuthored(req.Messages)
	body, err := json.Marshal(req)
	if err != nil {
		return Message{}, Usage{}, settle(ModelAttemptResult{Failed: true}, nonRetryable{err})
	}
	if _, err := httpRequest(ctx, c.BaseURL, body); err != nil {
		return Message{}, Usage{}, settle(ModelAttemptResult{Failed: true}, nonRetryable{err})
	}
	if err := ctx.Err(); err != nil {
		return Message{}, Usage{}, settle(ModelAttemptResult{Failed: true}, err)
	}
	if permit.Timeout <= 0 {
		return Message{}, Usage{}, settle(ModelAttemptResult{Failed: true}, context.DeadlineExceeded)
	}
	attemptCtx, cancel := context.WithTimeout(ctx, permit.Timeout)
	started := time.Now()
	message, usage, err := invoke(attemptCtx, body)
	elapsed := time.Since(started)
	cancel()
	return message, usage, settle(ModelAttemptResult{Usage: usage, Dispatched: true, Elapsed: elapsed, Failed: err != nil}, err)
}

func logicalCallID() string { return rand.Text() }
