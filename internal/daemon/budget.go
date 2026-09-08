package daemon

import (
	"context"
	"errors"
	"fmt"
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

// BeginModelAttempt implements the root agent's model budget. Every retry has
// its own durable identity and immutable route snapshot.
func (s *Session) BeginModelAttempt(ctx context.Context, attempt llm.ModelAttempt) (llm.ModelPermit, error) {
	return s.beginAgentModelAttempt(ctx, s.authority.AgentID, attempt)
}

func (s *Session) beginAgentModelAttempt(ctx context.Context, agentID string, attempt llm.ModelAttempt) (llm.ModelPermit, error) {
	reservation, err := routeControlOwnedValue(s, ctx, func(actorCtx context.Context) (sessionstore.ModelCallReservation, error) {
		// Never hold accountingMu while waiting for the actor: stopping a
		// child can make that actor wait for a host call using the same lock.
		s.accountingMu.Lock()
		defer s.accountingMu.Unlock()
		// Repair the exact saved result, never by repeating a provider call.
		if err := s.flushPendingAccountingLocked(); err != nil {
			return sessionstore.ModelCallReservation{}, errors.New("model accounting is unresolved; further calls are paused")
		}
		return s.store.AdmitModelCall(actorCtx, s.meta.ID, agentID, attempt)
	})
	if err != nil {
		return llm.ModelPermit{}, err
	}
	s.publishModelAccounting(ctx)
	return llm.ModelPermit{
		ID: reservation.ID, MaxTokens: reservation.MaxTokens, Timeout: reservation.Timeout,
		Settle: func(result llm.ModelAttemptResult) error {
			s.accountingMu.Lock()
			defer s.accountingMu.Unlock()
			// Repair the first retained result before considering another
			// callback for the same attempt. A conflicting callback must not
			// replace an outcome merely because its first write failed.
			if _, pending := s.pendingAccounting[reservation.ID]; pending {
				if err := s.flushPendingAccountingLocked(); err != nil {
					return err
				}
			}
			// A cancelled turn still owes accounting. This bounded, independent
			// lifetime also avoids routing settlement through a stopping actor.
			settleCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			settled, err := s.store.SettleModelCall(settleCtx, s.meta.ID, reservation.ID, result)
			if err != nil {
				if errors.Is(err, sessionstore.ErrModelCallConflict) {
					return err
				}
				if s.pendingAccounting == nil {
					s.pendingAccounting = make(map[string]llm.ModelAttemptResult)
				}
				s.pendingAccounting[reservation.ID] = result
				s.modelAccountingNotice(agentID, "Model response retained; accounting is unresolved. Further calls are paused.")
				return err
			}
			delete(s.pendingAccounting, reservation.ID)
			s.publishModelAccounting(settleCtx)
			if settled.Exhausted {
				s.modelAccountingNotice(agentID, "Model budget exhausted. The response is retained; further tools and model calls are paused.")
				return fmt.Errorf("model budget exhausted: %w", capability.ErrDenied)
			}
			return nil
		},
	}, nil
}

func (s *Session) flushPendingAccounting() error {
	s.accountingMu.Lock()
	defer s.accountingMu.Unlock()
	return s.flushPendingAccountingLocked()
}

func (s *Session) flushPendingAccountingLocked() error {
	if len(s.pendingAccounting) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for id, result := range s.pendingAccounting {
		if _, err := s.store.SettleModelCall(ctx, s.meta.ID, id, result); err != nil {
			return err
		}
		delete(s.pendingAccounting, id)
	}
	return nil
}

func (s *Session) publishModelAccounting(ctx context.Context) {
	accounting, err := s.store.ModelAccounting(ctx, s.meta.ID, "", true)
	if err == nil {
		s.supervisor.post(workerEnvelope{kind: workerStream, stream: &streamEnvelope{
			kind: "stream.accounting", event: StreamEvent{Accounting: &accounting},
		}})
	}
}

func (s *Session) modelAccountingNotice(agentID, text string) {
	s.supervisor.post(workerEnvelope{kind: workerStream, stream: &streamEnvelope{
		kind: "stream.notice", event: StreamEvent{AgentID: agentID, Text: text},
	}})
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
