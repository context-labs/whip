package daemon

import (
	"context"
	"errors"
	sessionstore "github.com/context-labs/whip/internal/session"
)

func (s *Session) checkTurnTarget(ctx context.Context, agentID, turnID string) error {
	if agentID == "" || turnID == "" {
		return rpcFailure(-32602, "cancellation requires a specific agent and turn ID")
	}
	active, err := s.store.ActiveTurn(ctx, s.meta.ID, agentID)
	if err != nil {
		return err
	}
	if active != turnID {
		return rpcFailure(-32009, "cancellation target is no longer active")
	}
	return nil
}

func (s *Session) cancelInputCommand(ctx context.Context, clientID, commandID string) error {
	record, cancelled, err := s.store.CancelQueuedInput(ctx, s.meta.ID, clientID, commandID, encodeCommandOutcome("cancel", "", context.Canceled))
	if errors.Is(err, sessionstore.ErrCommandTarget) {
		return rpcFailure(-32009, err.Error())
	}
	if err != nil {
		return err
	}
	if cancelled {
		s.settle(record.IngressSeq, Completion{Sequence: record.IngressSeq, Err: context.Canceled})
		s.notify()
		return nil
	}
	if s.running == nil || s.turnCancel == nil {
		return rpcFailure(-32009, "target command no longer has an active turn")
	}
	// The root actor owns one live model turn. Running root inbox rows are
	// either its initial input or steering already claimed by that same turn.
	turnID, err := s.store.ActiveTurn(ctx, s.meta.ID, s.authority.AgentID)
	if err != nil {
		return err
	}
	if turnID == "" {
		return rpcFailure(-32009, "target command no longer has an active turn")
	}
	s.turnCancel()
	return nil
}
