package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
)

// ChildControlResult carries post-commit process cleanup separately from the
// durable guest outcome. CancelTurns may be repeated safely on operation retry.
type ChildControlResult struct {
	Value       json.RawMessage
	CancelTurns []session.TurnID
	Deleted     bool
}

// ApplyChildControl authorizes the source and target, applies the action, and
// settles its operation in one transaction. No process work occurs in the TX.
func (s *Store) ApplyChildControl(ctx context.Context, id session.OperationID) (result ChildControlResult, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		op, err := readOperation(ctx, tx, id)
		if err != nil {
			return err
		}
		switch op.Capability {
		case "agents.submit", "agents.inspect", "agents.list", "agents.stop", "agents.delete":
		default:
			return ErrConflict
		}
		if op.State == session.OperationSucceeded && op.Result != nil {
			result.Value = op.Result.Value
			result.Deleted = op.Capability == "agents.delete"
			if op.Capability == "agents.stop" {
				var target session.ChildTarget
				if err := json.Unmarshal(op.Arguments, &target); err != nil {
					return err
				}
				result.CancelTurns, err = subtreeCancellingTurns(ctx, tx, target.SessionID)
			}
			return err
		}
		owner, err := readSession(ctx, tx, op.SessionID)
		if err != nil {
			return err
		}
		if op.Resource != string(owner.TreeID) {
			return ErrConflict
		}
		dispatch, err := dispatchOperation(ctx, tx, id)
		if err != nil {
			return err
		}
		if !dispatch {
			return ErrConflict
		}
		result, err = applyChildControl(ctx, tx, owner, op)
		if err != nil {
			return err
		}
		op.State = session.OperationDispatched
		_, err = settleOperation(ctx, tx, op, session.OperationResult{State: session.OperationSucceeded, Value: result.Value})
		return err
	})
	return
}

func applyChildControl(ctx context.Context, tx *sql.Tx, owner session.Session, op session.Operation) (ChildControlResult, error) {
	var result ChildControlResult
	var value any
	var err error
	switch op.Capability {
	case "agents.submit":
		var request session.ChildSubmit
		if err := json.Unmarshal(op.Arguments, &request); err != nil {
			return result, err
		}
		value, err = submitChild(ctx, tx, owner.ID, op.ID, request)
	case "agents.inspect":
		var request session.ChildInspect
		if err := json.Unmarshal(op.Arguments, &request); err != nil {
			return result, err
		}
		value, err = inspectChild(ctx, tx, owner.ID, request)
	case "agents.list":
		var request session.ChildList
		if err := json.Unmarshal(op.Arguments, &request); err != nil {
			return result, err
		}
		var items []session.RelativeMetadata
		items, err = listRelatives(ctx, tx, owner, request)
		value = struct {
			Items []session.RelativeMetadata `json:"items"`
		}{items}
	case "agents.stop", "agents.delete":
		var request session.ChildTarget
		if err := json.Unmarshal(op.Arguments, &request); err != nil {
			return result, err
		}
		if _, err := controlDescendant(ctx, tx, owner.ID, request.SessionID); err != nil {
			return result, err
		}
		if op.Capability == "agents.stop" {
			value, result.CancelTurns, err = stopChildSubtree(ctx, tx, request.SessionID)
		} else {
			err = deleteSubtree(ctx, tx, request.SessionID)
			result.Deleted = err == nil
			value = struct {
				SessionID session.SessionID `json:"session_id"`
				Deleted   bool              `json:"deleted"`
			}{request.SessionID, true}
		}
	}
	if err != nil {
		return result, err
	}
	result.Value, err = json.Marshal(value)
	return result, err
}

func controlDescendant(ctx context.Context, q querier, owner, target session.SessionID) (session.Session, error) {
	if err := session.ValidateID(string(target)); err != nil {
		return session.Session{}, err
	}
	current, err := readSession(ctx, q, target)
	if err != nil {
		return current, err
	}
	var descendant bool
	err = q.QueryRowContext(ctx, subtree+" SELECT EXISTS(SELECT 1 FROM subtree WHERE id=? AND id<>?)", owner, target, owner).Scan(&descendant)
	if err != nil {
		return current, err
	}
	if !descendant {
		return current, fmt.Errorf("%w: target must be a proper descendant", session.ErrInvalid)
	}
	return current, nil
}

func submitChild(ctx context.Context, tx *sql.Tx, owner session.SessionID, operation session.OperationID, request session.ChildSubmit) (session.ChildSubmission, error) {
	var result session.ChildSubmission
	target, err := controlDescendant(ctx, tx, owner, request.SessionID)
	if err != nil {
		return result, err
	}
	if target.ParentID == nil || *target.ParentID != owner {
		return result, fmt.Errorf("%w: submission requires a direct child", session.ErrInvalid)
	}
	if err := session.ValidateInputParts(request.Parts); err != nil {
		return result, err
	}
	digest, err := requestDigest("child_submit", request)
	if err != nil {
		return result, err
	}
	parts, err := shareChildContent(ctx, tx, owner, target.ID, request.Parts)
	if err != nil {
		return result, err
	}
	if err := session.ValidateInputParts(parts); err != nil {
		return result, err
	}
	admitted, err := admitInput(ctx, tx, session.RequestIdentity{ClientID: "operation", RequestID: string(operation)}, digest, Submission{SessionID: target.ID, Source: session.AgentInput, Parts: parts})
	if err != nil {
		return result, err
	}
	if err := chargeWrite(ctx, tx, owner, "input", string(admitted.Input.ID), 0, inputWriteBytes(request.Parts)); err != nil {
		return result, err
	}
	return session.ChildSubmission{SessionID: target.ID, InputID: admitted.Input.ID}, nil
}

func inspectChild(ctx context.Context, q querier, owner session.SessionID, request session.ChildInspect) (session.ChildOutcome, error) {
	var result session.ChildOutcome
	if request.Offset < 0 || request.Offset > session.MaxDocumentBytes {
		return result, session.ErrInvalid
	}
	if _, err := controlDescendant(ctx, q, owner, request.SessionID); err != nil {
		return result, err
	}
	input, err := readInput(ctx, q, request.InputID)
	if err != nil {
		return result, err
	}
	if input.SessionID != request.SessionID {
		return result, fmt.Errorf("%w: input belongs to another session", session.ErrInvalid)
	}
	result = session.ChildOutcome{SessionID: input.SessionID, InputID: input.ID, InputState: input.State, TurnID: input.TurnID, Offset: request.Offset}
	if input.TurnID == nil {
		if request.Offset != 0 {
			return result, session.ErrInvalid
		}
		return result, nil
	}
	turn, err := readTurn(ctx, q, *input.TurnID)
	if err != nil {
		return result, err
	}
	result.TurnState = &turn.State
	result.Failure = turn.Failure
	var raw string
	var messageID session.MessageID
	err = q.QueryRowContext(ctx, "SELECT id,parts FROM messages WHERE turn_id=? AND role='assistant' ORDER BY sequence DESC LIMIT 1", turn.ID).Scan(&messageID, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		if request.Offset != 0 {
			return result, session.ErrInvalid
		}
		return result, nil
	}
	if err != nil {
		return result, err
	}
	result.MessageID = &messageID
	var parts []session.Part
	if err := json.Unmarshal([]byte(raw), &parts); err != nil {
		return result, err
	}
	var full strings.Builder
	for _, part := range parts {
		if part.Type != "text" {
			result.OmittedParts++
			continue
		}
		full.WriteString(part.Text)
	}
	text := full.String()
	result.TotalBytes = int64(len(text))
	if request.Offset > result.TotalBytes || (request.Offset < result.TotalBytes && !utf8.RuneStart(text[request.Offset])) {
		return result, fmt.Errorf("%w: offset must be a UTF-8 boundary within result text", session.ErrInvalid)
	}
	result.Text, result.Truncated = childText(text[request.Offset:], 16*1024)
	if result.Truncated {
		next := request.Offset + int64(len(result.Text))
		result.NextOffset = &next
	}

	return result, nil
}

func childText(text string, limit int) (string, bool) {
	if len(text) <= limit {
		return text, false
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit], true
}

func listRelatives(ctx context.Context, q querier, owner session.Session, request session.ChildList) ([]session.RelativeMetadata, error) {
	if request.Limit == 0 {
		request.Limit = 20
	}
	if request.Limit < 1 || request.Limit > 100 {
		return nil, session.ErrInvalid
	}
	if request.After != "" && session.ValidateID(string(request.After)) != nil {
		return nil, session.ErrInvalid
	}
	condition, target := "parent_id=?", owner.ID
	switch request.Relation {
	case "", "children":
	case "siblings":
		if owner.ParentID == nil {
			return []session.RelativeMetadata{}, nil
		}
		target = *owner.ParentID
	case "parent":
		if owner.ParentID == nil {
			return []session.RelativeMetadata{}, nil
		}
		condition, target = "id=?", *owner.ParentID
	default:
		return nil, session.ErrInvalid
	}
	rows, err := q.QueryContext(ctx, "SELECT id,parent_id,lifecycle FROM sessions WHERE "+condition+" AND id<>? AND id>? ORDER BY id LIMIT ?", target, owner.ID, request.After, request.Limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []session.RelativeMetadata{}
	for rows.Next() {
		var item session.RelativeMetadata
		if err := rows.Scan(&item.SessionID, &item.ParentID, &item.Lifecycle); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func subtreeCancellingTurns(ctx context.Context, q querier, id session.SessionID) ([]session.TurnID, error) {
	rows, err := q.QueryContext(ctx, subtree+" SELECT id FROM turns WHERE session_id IN (SELECT id FROM subtree) AND state='cancelling' ORDER BY id", id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []session.TurnID{}
	for rows.Next() {
		var turn session.TurnID
		if err := rows.Scan(&turn); err != nil {
			return nil, err
		}
		result = append(result, turn)
	}
	return result, rows.Err()
}

func stopChildSubtree(ctx context.Context, tx *sql.Tx, id session.SessionID) (session.ChildStopResult, []session.TurnID, error) {
	result := session.ChildStopResult{SessionID: id}
	stopped, err := tx.ExecContext(ctx, subtree+" UPDATE sessions SET lifecycle='stopped' WHERE id IN (SELECT id FROM subtree)", id)
	if err != nil {
		return result, nil, err
	}
	result.StoppedSessions, err = stopped.RowsAffected()
	if err != nil {
		return result, nil, err
	}
	if _, err := tx.ExecContext(ctx, subtree+" UPDATE turns SET state='cancelling' WHERE session_id IN (SELECT id FROM subtree) AND state='running'", id); err != nil {
		return result, nil, err
	}
	turns, err := subtreeCancellingTurns(ctx, tx, id)
	if err != nil {
		return result, nil, err
	}
	result.CancellingTurns = int64(len(turns))
	return result, turns, err
}
