package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/context-labs/whip/internal/session"
)

const cellSelect = `SELECT c.id,t.session_id,c.turn_id,c.call_message_id,c.call_id,c.state,
 c.result_message_id,c.checkpoint,c.created_at,c.finished_at FROM cells c JOIN turns t ON t.id=c.turn_id`

func scanCell(row scanner) (result session.Cell, err error) {
	var checkpoint sql.NullString
	var created int64
	var finished sql.NullInt64
	err = row.Scan(&result.ID, &result.SessionID, &result.TurnID, &result.CallMessageID, &result.CallID, &result.State, &result.ResultMessageID, &checkpoint, &created, &finished)
	if err != nil {
		return result, found(err)
	}
	result.CreatedAt, result.FinishedAt = timestamp(created), optionalTime(finished)
	if checkpoint.Valid {
		err = json.Unmarshal([]byte(checkpoint.String), &result.Checkpoint)
	}
	return
}

func (s *Store) Cell(ctx context.Context, id session.CellID) (session.Cell, error) {
	return scanCell(s.db.QueryRowContext(ctx, cellSelect+" WHERE c.id=?", id))
}

func (s *Store) Cells(ctx context.Context, turn session.TurnID, after session.CellID, limit int) ([]session.Cell, error) {
	if err := pageLimit(limit); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, cellSelect+" WHERE c.turn_id=? AND c.id>? ORDER BY c.id LIMIT ?", turn, after, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []session.Cell{}
	size := 0
	for rows.Next() {
		value, err := scanCell(rows)
		if err != nil {
			return nil, err
		}
		raw, err := encode(value)
		if err != nil {
			return nil, err
		}
		size += len(raw)
		if size > MaxPageBytes {
			break
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

// LatestCell is the authoritative REPL boundary, including unavailable and
// uncertain boundaries. Selecting the latest usable checkpoint instead would
// silently forget execution that happened after that checkpoint.
func (s *Store) LatestCell(ctx context.Context, id session.SessionID) (*session.Cell, error) {
	cell, err := scanCell(s.db.QueryRowContext(ctx, cellSelect+" WHERE t.session_id=? ORDER BY c.ordinal DESC LIMIT 1", id))
	if errors.Is(err, ErrNotFound) {
		return nil, nil //nolint:nilnil // No prior cell is a valid empty REPL boundary, distinct from a cell with a missing checkpoint.
	}
	return &cell, err
}

// BeginCell is a single-dispatch gate. A retry can inspect the same row but must
// not run it again, even when the first transaction's acknowledgement was lost.
func (s *Store) BeginCell(ctx context.Context, spec session.CellSpec) (result session.Cell, dispatch bool, err error) {
	for _, id := range []string{string(spec.ID), string(spec.TurnID), string(spec.CallMessageID), spec.CallID} {
		if err := session.ValidateID(id); err != nil {
			return result, false, err
		}
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		existing, err := scanCell(tx.QueryRowContext(ctx, cellSelect+" WHERE c.id=?", spec.ID))
		if err == nil {
			if existing.TurnID != spec.TurnID || existing.CallMessageID != spec.CallMessageID || existing.CallID != spec.CallID {
				return ErrConflict
			}
			result = existing
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		turn, err := readTurn(ctx, tx, spec.TurnID)
		if err != nil {
			return err
		}
		if turn.State != session.Running {
			return ErrConflict
		}
		if err := requireTurnPermit(ctx, tx, turn.ID); err != nil {
			return err
		}
		message, pending, err := pendingCalls(ctx, tx, turn.ID)
		if err != nil {
			return err
		}
		call, ok := pending[spec.CallID]
		if message.ID != spec.CallMessageID || !ok || call.Name != "execute" {
			return ErrConflict
		}
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM cells WHERE turn_id=?", turn.ID).Scan(&count); err != nil {
			return err
		}
		if count >= 64 {
			return ErrLimit
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO cells (id,turn_id,call_message_id,call_id,state,created_at) VALUES (?,?,?,?,'running',?)`, spec.ID, spec.TurnID, spec.CallMessageID, spec.CallID, now())
		if err != nil {
			return err
		}
		result, err = scanCell(tx.QueryRowContext(ctx, cellSelect+" WHERE c.id=?", spec.ID))
		dispatch = err == nil
		return err
	})
	// A failed COMMIT is ambiguous; it never grants execution.
	return result, dispatch && err == nil, err
}

func (s *Store) SettleCell(ctx context.Context, id session.CellID, state session.CellState, output session.ToolResult, checkpoint *session.Checkpoint) (result session.Cell, err error) {
	if state != session.CellSucceeded && state != session.CellFailed && state != session.CellUncertain {
		return result, fmt.Errorf("%w: invalid cell outcome", session.ErrInvalid)
	}
	if output.IsError != (state != session.CellSucceeded) || (state == session.CellUncertain && checkpoint != nil) {
		return result, fmt.Errorf("%w: inconsistent cell result", session.ErrInvalid)
	}
	if checkpoint != nil {
		if err := checkpoint.Validate(); err != nil {
			return result, err
		}
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		cell, err := scanCell(tx.QueryRowContext(ctx, cellSelect+" WHERE c.id=?", id))
		if err != nil {
			return err
		}
		if cell.CallID != output.CallID {
			return ErrConflict
		}
		draft := session.MessageDraft{ID: session.MessageID(string(id) + "_result"), Role: session.Tool, Parts: []session.Part{{Type: "tool_result", Result: &output}}}
		if cell.State != session.CellRunning {
			if cell.State != state || !reflect.DeepEqual(cell.Checkpoint, checkpoint) {
				return ErrConflict
			}
			saved, err := scanMessage(tx.QueryRowContext(ctx, messageSelect+" WHERE m.id=?", draft.ID))
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(saved.Parts, draft.Parts) {
				return ErrConflict
			}
			result = cell
			return nil
		}
		var pending int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM operations WHERE cell_id=? AND finished_at IS NULL", id).Scan(&pending); err != nil {
			return err
		}
		if pending != 0 {
			return ErrBusy
		}
		turn, err := readTurn(ctx, tx, cell.TurnID)
		if err != nil {
			return err
		}
		if checkpoint != nil {
			var engine session.Engine
			if err := tx.QueryRowContext(ctx, "SELECT t.engine FROM session_trees t JOIN sessions s ON s.tree_id=t.id WHERE s.id=?", cell.SessionID).Scan(&engine); err != nil {
				return err
			}
			if checkpoint.Engine != engine {
				return ErrConflict
			}
		}
		if _, err := appendMessage(ctx, tx, turn, draft); err != nil {
			return err
		}
		var raw any
		if checkpoint != nil {
			raw, err = encode(checkpoint)
			if err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, "UPDATE cells SET state=?,result_message_id=?,checkpoint=?,finished_at=? WHERE id=?", state, draft.ID, raw, now(), id); err != nil {
			return err
		}
		result, err = scanCell(tx.QueryRowContext(ctx, cellSelect+" WHERE c.id=?", id))
		return err
	})
	return
}

// Only the most recent assistant message may have outstanding calls: appending
// the next assistant message requires all previous calls to have results.
func pendingCalls(ctx context.Context, tx *sql.Tx, turn session.TurnID) (session.Message, map[string]session.ToolCall, error) {
	message, err := scanMessage(tx.QueryRowContext(ctx, messageSelect+" WHERE m.turn_id=? AND m.role='assistant' ORDER BY m.sequence DESC LIMIT 1", turn))
	pending := map[string]session.ToolCall{}
	if errors.Is(err, ErrNotFound) {
		return session.Message{}, pending, nil
	}
	if err != nil {
		return message, nil, err
	}
	for _, part := range message.Parts {
		if part.Call != nil {
			pending[part.Call.ID] = *part.Call
		}
	}
	rows, err := tx.QueryContext(ctx, messageSelect+" WHERE m.turn_id=? AND m.sequence>? AND m.role='tool' ORDER BY m.sequence", turn, message.Sequence)
	if err != nil {
		return message, nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		result, err := scanMessage(rows)
		if err != nil {
			return message, nil, err
		}
		for _, part := range result.Parts {
			if part.Result != nil {
				delete(pending, part.Result.CallID)
			}
		}
	}
	return message, pending, rows.Err()
}

func validateCallOrder(ctx context.Context, tx *sql.Tx, turn session.TurnID, draft session.MessageDraft) error {
	if draft.Role != session.Assistant && draft.Role != session.Tool {
		return nil
	}
	_, pending, err := pendingCalls(ctx, tx, turn)
	if err != nil {
		return err
	}
	if draft.Role == session.Assistant {
		if len(pending) != 0 {
			return fmt.Errorf("%w: unanswered tool calls", ErrConflict)
		}
		return nil
	}
	if len(draft.Parts) != 1 || draft.Parts[0].Result == nil {
		return fmt.Errorf("%w: tool result required", session.ErrInvalid)
	}
	if _, ok := pending[draft.Parts[0].Result.CallID]; !ok {
		return fmt.Errorf("%w: no pending tool call", ErrConflict)
	}
	return nil
}

// Reconcile unexecuted calls on terminal failure so the next ordinary turn has
// a valid transcript. This records absence/uncertainty; it never executes a cell.
func reconcileCalls(ctx context.Context, tx *sql.Tx, turn session.Turn, reason string) error {
	message, pending, err := pendingCalls(ctx, tx, turn.ID)
	if err != nil {
		return err
	}
	for _, part := range message.Parts {
		if part.Call == nil {
			continue
		}
		call, ok := pending[part.Call.ID]
		if !ok {
			continue
		}
		cell, err := scanCell(tx.QueryRowContext(ctx, cellSelect+" WHERE c.call_message_id=? AND c.call_id=?", message.ID, call.ID))
		text := reason + "; call was not dispatched"
		id := session.MessageID(fmt.Sprintf("%s_unexecuted_%d", message.ID, len(pending)))
		if err == nil {
			if cell.State != session.CellRunning {
				return ErrConflict
			}
			text = reason + "; execution outcome is unknown. Do not replay effects."
			id = session.MessageID(string(cell.ID) + "_result")
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		draft := session.MessageDraft{ID: id, Role: session.Tool, Parts: []session.Part{{Type: "tool_result", Result: &session.ToolResult{CallID: call.ID, Output: text, IsError: true}}}}
		if _, err := appendMessage(ctx, tx, turn, draft); err != nil {
			return err
		}
		if cell.ID != "" {
			if _, err := tx.ExecContext(ctx, "UPDATE cells SET state='uncertain',result_message_id=?,finished_at=? WHERE id=?", id, now(), cell.ID); err != nil {
				return err
			}
		}
		delete(pending, call.ID)
	}
	return nil
}

func recoverCells(ctx context.Context, tx *sql.Tx) error {
	// One active turn at a time keeps recovery memory bounded by one message.
	var after session.TurnID
	for {
		var id session.TurnID
		err := tx.QueryRowContext(ctx, "SELECT id FROM turns WHERE state IN ('running','cancelling') AND id>? ORDER BY id LIMIT 1", after).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		turn, err := readTurn(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := reconcileCalls(ctx, tx, turn, "runtime restarted"); err != nil {
			return err
		}
		after = id
	}
}
