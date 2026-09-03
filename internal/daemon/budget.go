package daemon

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
	sessionstore "github.com/context-labs/whip/internal/session"
)

type modelPricing struct {
	input, output, cacheRead float64
}

func (s *Session) consumeBudgets(ctx context.Context, agentID string, reservations []capability.Reservation, action func() error) error {
	if err := s.store.ReserveBudget(ctx, s.meta.ID, agentID, reservations); err != nil {
		return err
	}
	if err := action(); err != nil {
		return errors.Join(err, s.store.ReleaseBudget(ctx, s.meta.ID, agentID, reservations))
	}
	return s.store.ReconcileBudget(ctx, s.meta.ID, agentID, reservations, nil)
}

func durableReservations(bytes int) []capability.Reservation {
	reservations := []capability.Reservation{
		{Kind: string(sessionstore.BudgetRecordCount), Amount: 1, Consume: true},
	}
	if bytes > 0 {
		reservations = append(reservations, capability.Reservation{Kind: string(sessionstore.BudgetDurableBytes), Amount: int64(bytes), Consume: true})
	}
	return reservations
}

// ConfigureModelPricing supplies the provider-advertised per-token USD rates
// used for durable monetary budget accounting. Missing pricing remains
// unpriced rather than being reported as zero-cost usage.
func (s *Session) ConfigureModelPricing(input, output, cacheRead float64) error {
	for _, rate := range []float64{input, output, cacheRead} {
		if rate < 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
			return errors.New("model pricing must be finite and nonnegative")
		}
	}
	s.pricingMu.Lock()
	s.pricing = modelPricing{input: input, output: output, cacheRead: cacheRead}
	s.pricingMu.Unlock()
	return nil
}

func (s *Session) modelPricing() modelPricing {
	s.pricingMu.RLock()
	defer s.pricingMu.RUnlock()
	return s.pricing
}

// ReserveModelCall makes the root session itself an agent.ModelCallBudget.
// Root, Classic, RLM, and stateless calls therefore share durable accounting.
func (s *Session) ReserveModelCall(ctx context.Context, amount int64) (func(llm.Usage) error, error) {
	return s.reserveModelCall(ctx, s.authority.AgentID, amount)
}

func (s *Session) reserveModelCall(ctx context.Context, agentID string, amount int64) (func(llm.Usage) error, error) {
	const maxCallElapsed = int64((30 * time.Minute) / time.Millisecond)
	if amount < 1 {
		return nil, errors.New("model call reservation must be positive")
	}
	pricing := s.modelPricing()
	reservation := []capability.Reservation{
		{Kind: string(sessionstore.BudgetTokens), Amount: amount},
		{Kind: string(sessionstore.BudgetElapsed), Amount: maxCallElapsed},
		{Kind: string(sessionstore.BudgetActiveOperations), Amount: 1},
	}
	if cost := maximumCostMicros(amount, pricing); cost > 0 {
		reservation = append(reservation, capability.Reservation{Kind: string(sessionstore.BudgetCost), Amount: cost})
	}
	started := time.Now()
	if err := s.routeControl(ctx, func(actorCtx context.Context) error {
		return s.store.ReserveBudget(actorCtx, s.meta.ID, agentID, reservation)
	}); err != nil {
		return nil, err
	}
	return func(usage llm.Usage) error {
		actual := make([]capability.Usage, 0, 3)
		if tokens := usage.PromptTokens + usage.CompletionTokens; tokens > 0 {
			actual = append(actual, capability.Usage{Kind: string(sessionstore.BudgetTokens), Amount: int64(tokens)})
		}
		if cost := actualCostMicros(usage, pricing); cost > 0 {
			actual = append(actual, capability.Usage{Kind: string(sessionstore.BudgetCost), Amount: cost})
		}
		actual = append(actual, capability.Usage{Kind: string(sessionstore.BudgetElapsed), Amount: max(time.Since(started).Milliseconds(), 1)})
		return s.routeControl(context.Background(), func(actorCtx context.Context) error {
			return s.store.ReconcileBudget(actorCtx, s.meta.ID, agentID, reservation, actual)
		})
	}, nil
}

func maximumCostMicros(tokens int64, pricing modelPricing) int64 {
	rate := max(pricing.input, pricing.output, pricing.cacheRead)
	return dollarsToMicros(float64(tokens) * rate)
}

func actualCostMicros(usage llm.Usage, pricing modelPricing) int64 {
	prompt := max(usage.PromptTokens, 0)
	cached := min(max(usage.Cached(), 0), prompt)
	completion := max(usage.CompletionTokens, 0)
	cacheRead := pricing.cacheRead
	if cacheRead == 0 {
		cacheRead = pricing.input
	}
	return dollarsToMicros(float64(prompt-cached)*pricing.input + float64(cached)*cacheRead + float64(completion)*pricing.output)
}

func dollarsToMicros(value float64) int64 {
	if value <= 0 {
		return 0
	}
	if value >= float64(math.MaxInt64)/1_000_000 {
		return math.MaxInt64
	}
	return int64(math.Ceil(value * 1_000_000))
}

func (s *Session) InspectBudgets(ctx context.Context, callerAgentID, targetAgentID string) (states []sessionstore.BudgetState, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		states, err = s.store.InspectBudgetsFor(actorCtx, s.meta.ID, callerAgentID, targetAgentID)
		return err
	})
	return states, err
}

func (s *Session) CapBudget(ctx context.Context, callerAgentID, targetAgentID string, kind sessionstore.BudgetKind, limit int64) (state sessionstore.BudgetState, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		state, err = s.store.CapBudget(actorCtx, s.meta.ID, callerAgentID, targetAgentID, kind, limit)
		return err
	})
	return state, err
}
