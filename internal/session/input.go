package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
)

// ResolveInboxPayload reconstructs a complete input under its recipient's
// authority. RuntimeValue.Size is not trusted: content metadata owns the size.
func (s *Store) ResolveInboxPayload(ctx context.Context, item InboxItem) ([]byte, error) {
	if item.Payload.ReferenceID == "" {
		if len(item.Payload.Inline) > InlineValueLimit {
			return nil, fmt.Errorf("%w: oversized inline payload", ErrInvalidInput)
		}
		return append([]byte(nil), item.Payload.Inline...), nil
	}
	chunk, meta, err := s.ReadContent(ctx, item.Payload.ReferenceID, item.RootID, item.AgentID, 0, MaxContentRead)
	if err != nil {
		return nil, inputReadError(err)
	}
	if meta.Size < 0 || meta.Size > MaxInputPayloadBytes {
		return nil, fmt.Errorf("%w: payload exceeds %d bytes", ErrInvalidInput, MaxInputPayloadBytes)
	}
	data := make([]byte, 0, int(meta.Size))
	for {
		if int64(len(data)+len(chunk)) > meta.Size || len(chunk) == 0 && int64(len(data)) < meta.Size {
			return nil, fmt.Errorf("%w: payload size does not match stored content", ErrInvalidInput)
		}
		data = append(data, chunk...)
		if int64(len(data)) == meta.Size {
			return data, nil
		}
		chunk, _, err = s.ReadContent(ctx, item.Payload.ReferenceID, item.RootID, item.AgentID, int64(len(data)), min(MaxContentRead, int(meta.Size)-len(data)))
		if err != nil {
			return nil, inputReadError(err)
		}
	}
}

// Invalid payloads are terminal; a database or transient I/O failure retains
// normal execution retry semantics rather than discarding valid child input.
func inputReadError(err error) error {
	if errors.Is(err, ErrContentAccess) || errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
		return fmt.Errorf("%w: cannot read payload: %w", ErrInvalidInput, err)
	}
	return fmt.Errorf("read turn input: %w", err)
}

// startInputCommandTx validates correlation before any model can receive the
// input. Internal input has no command; a correlated terminal command is a bug.
func startInputCommandTx(ctx context.Context, tx *sql.Tx, rootID string, seq int64, stamp string) error {
	var status string
	err := tx.QueryRowContext(ctx, `SELECT status FROM commands WHERE root_id=? AND scope='root' AND ingress_seq=? AND ingress_seq>0`, rootID, seq).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if status != "queued" {
		return errors.New("root turn command is not queued")
	}
	_, err = tx.ExecContext(ctx, `UPDATE commands SET status='running',updated_at=? WHERE root_id=? AND scope='root' AND ingress_seq=?`, stamp, rootID, seq)
	return err
}

func interruptCommandsTx(ctx context.Context, tx *sql.Tx, rootID, stamp string, preserveQueuedInput bool) error {
	query := `UPDATE commands SET status='interrupted',updated_at=? WHERE status IN ('queued','running','waiting')`
	args := []any{stamp}
	if rootID != "" {
		query += ` AND root_id=?`
		args = append(args, rootID)
	}
	if preserveQueuedInput {
		query += ` AND NOT(status='queued' AND scope='root' AND ingress_seq>0 AND EXISTS(
			SELECT 1 FROM inbox i JOIN agents a ON a.root_id=i.root_id AND a.id=i.agent_id
			WHERE i.root_id=commands.root_id AND i.seq=commands.ingress_seq AND a.parent_id IS NULL AND i.status='queued'))`
	}
	_, err := tx.ExecContext(ctx, query, args...)
	return err
}

// RunningTurnID is captured once by a worker; boundary claims then validate
// that same turn, so a late boundary cannot claim work for a subsequent turn.
func (s *Store) RunningTurnID(ctx context.Context, rootID, agentID string) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM turns WHERE root_id=? AND agent_id=? AND status='running'`, rootID, agentID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil // detached runners have no durable turn or boundary input
	}
	return id, err
}

func validateRunningTurnTx(ctx context.Context, tx *sql.Tx, rootID, agentID, turnID string) error {
	var running bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM turns WHERE root_id=? AND agent_id=? AND id=? AND status='running')`, rootID, agentID, turnID).Scan(&running); err != nil {
		return err
	}
	if !running {
		return errors.New("agent turn is not running")
	}
	return nil
}

// ClaimSteers atomically claims queued human steers before a boundary exposes
// them. Mail remains pending until the successful turn acknowledges revisions.
func (s *Store) ClaimSteers(ctx context.Context, rootID, agentID, turnID string) ([]InboxItem, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := validateRunningTurnTx(ctx, tx, rootID, agentID, turnID); err != nil {
		return nil, err
	}
	agent, err := loadAgentTx(ctx, tx, rootID, agentID)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT i.seq,i.kind,i.status,substr(i.payload_inline,1,?),COALESCE(i.payload_ref,''),
		COALESCE(r.digest,''),COALESCE(r.size,0),COALESCE(r.media_type,''),COALESCE(r.source,'')
		FROM inbox i LEFT JOIN content_references r ON r.id=i.payload_ref
		WHERE i.root_id=? AND i.agent_id=? AND i.status='queued' AND i.kind IN ('steer','steer.parts') ORDER BY i.seq LIMIT ?`,
		InlineValueLimit+1, rootID, agentID, MaxInboxBatch)
	if err != nil {
		return nil, err
	}
	items, scanErr := scanInboxRows(rows, rootID, agentID)
	if err := errors.Join(scanErr, rows.Close()); err != nil {
		return nil, err
	}
	stamp := now()
	for i := range items {
		if agent.ParentID == "" {
			if err := startInputCommandTx(ctx, tx, rootID, items[i].Seq, stamp); err != nil {
				return nil, err
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE inbox SET status='running' WHERE root_id=? AND agent_id=? AND seq=?`, rootID, agentID, items[i].Seq); err != nil {
			return nil, err
		}
		items[i].Status = "running"
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return items, nil
}

// RejectTurnInput settles malformed boundary input without terminating the
// surrounding turn or leaving an endlessly retried steer in the queue.
func (s *Store) RejectTurnInput(ctx context.Context, rootID, agentID, turnID string, seq int64, reason string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := validateRunningTurnTx(ctx, tx, rootID, agentID, turnID); err != nil {
		return err
	}
	agent, err := loadAgentTx(ctx, tx, rootID, agentID)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE inbox SET status='interrupted' WHERE root_id=? AND agent_id=? AND seq=? AND status='running'`, rootID, agentID, seq)
	if err != nil {
		return err
	}
	if count, err := result.RowsAffected(); err != nil || count != 1 {
		if err != nil {
			return err
		}
		return ErrInboxTerminal
	}
	stamp := now()
	if agent.ParentID == "" {
		outcome, _ := json.Marshal(struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}{Code: -32602, Message: reason})
		if _, err := tx.ExecContext(ctx, `UPDATE commands SET status='failed',outcome_inline=?,updated_at=?
			WHERE root_id=? AND scope='root' AND ingress_seq=? AND ingress_seq>0 AND status='running'`, outcome, stamp, rootID, seq); err != nil {
			return err
		}
	}
	if _, err := s.insertActorEventTx(ctx, tx, rootID, "inbox.failed", actorEvent{AgentID: agentID, InboxSeq: seq, Status: "failed", Error: reason}, stamp); err != nil {
		return err
	}
	return tx.Commit()
}
