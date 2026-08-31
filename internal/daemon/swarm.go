package daemon

import (
	"context"

	sessionstore "github.com/context-labs/whip/internal/session"
)

func (s *Session) AdmitChild(ctx context.Context, parentAgentID, childAgentID, executionID string, budgets ...sessionstore.BudgetLimit) error {
	return s.routeControl(ctx, func(actorCtx context.Context) error {
		_, err := s.store.AdmitChild(actorCtx, sessionstore.ChildAdmission{
			RootID: s.meta.ID, ParentAgentID: parentAgentID, ChildAgentID: childAgentID, ExecutionID: executionID,
			Budgets: budgets,
		})
		return err
	})
}

func (s *Session) StartChildTurn(ctx context.Context, callerAgentID, executionID string) error {
	return s.routeControl(ctx, func(actorCtx context.Context) error {
		_, err := s.store.StartChildTurn(actorCtx, s.meta.ID, callerAgentID, executionID)
		return err
	})
}

func (s *Session) FinishChildTurn(ctx context.Context, callerAgentID, executionID, status string) error {
	return s.routeControl(ctx, func(actorCtx context.Context) error {
		_, err := s.store.FinishChildTurn(actorCtx, s.meta.ID, callerAgentID, executionID, status)
		return err
	})
}

func (s *Session) ListAgentRelatives(ctx context.Context, callerAgentID string) (relatives sessionstore.AgentRelatives, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		relatives, err = s.store.ListAgentRelatives(actorCtx, s.meta.ID, callerAgentID)
		return err
	})
	return relatives, err
}

func (s *Session) SendAgentMessage(ctx context.Context, senderAgentID, recipientAgentID string, message sessionstore.AgentMessage) (sequence sessionstore.InboxSequence, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		sequence, err = s.store.SendAgentMessage(actorCtx, s.meta.ID, senderAgentID, recipientAgentID, message)
		return err
	})
	return sequence, err
}

func (s *Session) TerminalizeSubtree(ctx context.Context, callerAgentID, targetAgentID, status string) error {
	return s.routeControl(ctx, func(actorCtx context.Context) error {
		_, err := s.store.TerminalizeSubtree(actorCtx, s.meta.ID, callerAgentID, targetAgentID, status)
		return err
	})
}

// routeControl admits one reply-bearing operation to the existing actor queue.
// Once admitted, it waits for the durable outcome rather than returning an
// uncertain cancellation to the caller.
func (s *Session) routeControl(ctx context.Context, control func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.admitMu.RLock()
	if s.stopping {
		s.admitMu.RUnlock()
		return ErrStopped
	}
	reply := make(chan error, 1)
	s.supervisor.post(workerEnvelope{kind: workerControl, control: control, reply: reply})
	s.admitMu.RUnlock()
	return <-reply
}
