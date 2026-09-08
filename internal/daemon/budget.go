package daemon

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
	sessionstore "github.com/context-labs/whip/internal/session"
)

func (s *Session) consumeBudgets(ctx context.Context, agentID string, reservations []capability.Reservation, action func() error) error {
	if err := s.store.ReserveBudget(ctx, s.meta.ID, agentID, reservations); err != nil {
		return err
	}
	if err := action(); err != nil {
		return errors.Join(err, s.store.ReleaseBudget(context.WithoutCancel(ctx), s.meta.ID, agentID, reservations))
	}
	return s.store.ReconcileBudget(context.WithoutCancel(ctx), s.meta.ID, agentID, reservations, nil)
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

// ReserveModelCall admits one root transport attempt.
func (s *Session) ReserveModelCall(ctx context.Context, estimate llm.CallEstimate) (func(llm.Usage) error, error) {
	return s.ReserveAgentModelCall(ctx, s.authority.AgentID, estimate)
}

// ReserveAgentModelCall accounts with the immutable prices of this particular
// model route, including child overrides, compaction and stateless helpers.
func (s *Session) ReserveAgentModelCall(ctx context.Context, agentID string, estimate llm.CallEstimate) (func(llm.Usage) error, error) {
	const maxCallElapsed = int64((30 * time.Minute) / time.Millisecond)
	if estimate.PromptTokens < 0 || estimate.OutputTokens < 1 || estimate.PromptTokens > math.MaxInt64-estimate.OutputTokens {
		return nil, errors.New("invalid model token estimate")
	}
	pricing := estimate.Prices
	if err := pricing.Validate(); err != nil {
		return nil, err
	}
	cost := int64(0)
	if pricing.Known {
		cost = dollarsToMicros(float64(estimate.PromptTokens)*max(pricing.Input, pricing.CacheRate()) + float64(estimate.OutputTokens)*pricing.Output)
	}
	reservation := []capability.Reservation{
		{Kind: string(sessionstore.BudgetTokens), Amount: estimate.PromptTokens + estimate.OutputTokens},
		{Kind: string(sessionstore.BudgetCost), Amount: cost},
		{Kind: string(sessionstore.BudgetElapsed), Amount: maxCallElapsed},
		{Kind: string(sessionstore.BudgetActiveOperations), Amount: 1},
	}
	if _, err := routeControlOwnedValue(s, ctx, func(actorCtx context.Context) (struct{}, error) {
		return struct{}{}, s.store.ReserveModelBudget(actorCtx, s.meta.ID, agentID, reservation, !pricing.Known)
	}); err != nil {
		return nil, fmt.Errorf("reserve model budget: %w", err)
	}
	started := time.Now()
	var settleMu sync.Mutex
	settled := false
	return func(usage llm.Usage) error {
		settleMu.Lock()
		defer settleMu.Unlock()
		if settled {
			return nil
		}
		actual := []capability.Usage{{Kind: string(sessionstore.BudgetElapsed), Amount: max(time.Since(started).Milliseconds(), 0)}}
		if usage.Reported || !usage.Dispatched {
			if usage.PromptTokens < 0 || usage.CompletionTokens < 0 || int64(usage.PromptTokens) > math.MaxInt64-int64(usage.CompletionTokens) {
				return errors.New("invalid model usage")
			}
			actual = append(actual, capability.Usage{Kind: string(sessionstore.BudgetTokens), Amount: int64(usage.PromptTokens) + int64(usage.CompletionTokens)})
			if pricing.Known || !usage.Dispatched {
				actual = append(actual, capability.Usage{Kind: string(sessionstore.BudgetCost), Amount: actualCostMicros(usage, pricing)})
			}
		}
		// Settlement must outlive cancellation without routing back through the actor:
		// shutdown can be waiting for this worker to finish.
		err := s.store.ReconcileModelBudget(context.Background(), s.meta.ID, agentID, reservation, actual)
		if err == nil {
			settled = true
		}
		return err
	}, nil
}

func actualCostMicros(usage llm.Usage, pricing llm.TokenPrices) int64 {
	prompt := max(usage.PromptTokens, 0)
	cached := min(max(usage.Cached(), 0), prompt)
	completion := max(usage.CompletionTokens, 0)
	return dollarsToMicros(float64(prompt-cached)*pricing.Input + float64(cached)*pricing.CacheRate() + float64(completion)*pricing.Output)
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

func (s *Session) InspectBudgets(ctx context.Context, callerAgentID, targetAgentID string) ([]sessionstore.BudgetState, error) {
	return routeControlValue(s, ctx, func(actorCtx context.Context) ([]sessionstore.BudgetState, error) {
		return s.store.InspectBudgetsFor(actorCtx, s.meta.ID, callerAgentID, targetAgentID)
	})
}

func (s *Session) CapBudget(ctx context.Context, callerAgentID, targetAgentID string, kind sessionstore.BudgetKind, limit int64) (sessionstore.BudgetState, error) {
	return routeControlValue(s, ctx, func(actorCtx context.Context) (sessionstore.BudgetState, error) {
		return s.store.CapBudget(actorCtx, s.meta.ID, callerAgentID, targetAgentID, kind, limit)
	})
}
