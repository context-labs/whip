package session

import (
	"context"
	"errors"
)

var ErrCommandTarget = errors.New("cancellation target is not an active input command in this root")

// CancelQueuedInput atomically removes exactly one queued command from model
// admission. A running result lets its owning actor cancel that command's turn.
func (s *Store) CancelQueuedInput(ctx context.Context, rootID, clientID, commandID string, outcome []byte) (CommandRecord, bool, error) {
	if len(outcome) > InlineValueLimit {
		return CommandRecord{}, false, errors.New("cancellation outcome exceeds inline limit")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CommandRecord{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	record, found, err := loadCommandTx(ctx, tx, clientID, commandID)
	if err != nil {
		return record, false, err
	}
	if !found || record.Scope != CommandScopeRoot || record.RootID != rootID || record.IngressSeq <= 0 || (record.Operation != "submit" && record.Operation != "steer") {
		return record, false, ErrCommandTarget
	}
	if record.Status == "running" || record.Status == "waiting" {
		return record, false, nil
	}
	if record.Status != "queued" {
		return record, false, ErrCommandTarget
	}
	result, err := tx.ExecContext(ctx, `UPDATE inbox SET status='cancelled' WHERE root_id=? AND agent_id=? AND seq=? AND status='queued'`, rootID, rootID, record.IngressSeq)
	if err != nil {
		return record, false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return record, false, err
	}
	if n != 1 {
		return record, false, ErrCommandTarget
	}
	stamp := now()
	if _, err = tx.ExecContext(ctx, `UPDATE commands SET status='cancelled',outcome_inline=?,outcome_ref=NULL,updated_at=? WHERE client_id=? AND command_id=? AND status='queued'`, outcome, stamp, clientID, commandID); err != nil {
		return record, false, err
	}
	if _, err = s.insertActorEventTx(ctx, tx, rootID, "command.cancelled", actorEvent{AgentID: rootID, InboxSeq: record.IngressSeq, Status: "cancelled", CommandClientID: clientID, CommandID: commandID}, stamp); err != nil {
		return record, false, err
	}
	if err = tx.Commit(); err != nil {
		return record, false, err
	}
	record.Status = "cancelled"
	record.Outcome = RuntimeValue{Inline: outcome}
	return record, true, nil
}
