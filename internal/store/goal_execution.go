package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/context-labs/whip/internal/session"
)

// TurnGoal reads the immutable objective selected by Claim, never the current
// goal state. The caller can freeze this value for the entire provider loop.
//
//nolint:nilnil // Ordinary turns without an eligible goal and maintenance turns have no goal context.
func (s *Store) TurnGoal(ctx context.Context, id session.TurnID) (*session.GoalContext, error) {
	var goalID *session.GoalID
	var revision, limit sql.NullInt64
	var text sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT t.goal_id,t.goal_revision,g.text,g.max_continuations FROM turns t LEFT JOIN goals g ON g.id=t.goal_id AND g.session_id=t.session_id WHERE t.id=?`, id).Scan(&goalID, &revision, &text, &limit)
	if err != nil {
		return nil, found(err)
	}
	if goalID == nil {
		return nil, nil
	}
	if !text.Valid || !limit.Valid {
		return nil, ErrConflict
	}
	return &session.GoalContext{ID: *goalID, Revision: revision.Int64, Spec: session.GoalSpec{Text: text.String, MaxContinuations: limit.Int64}}, nil
}

// claimGoal may retire one stale queued goal input. Its caller commits that
// cleanup before returning ErrNoWork so a disabled input cannot block the queue.
func claimGoal(ctx context.Context, tx *sql.Tx, owner session.Session, input *session.Input) (*session.GoalRef, bool, error) {
	if input != nil && input.Kind != session.PromptInput {
		return nil, false, nil
	}
	goal, err := currentGoal(ctx, tx, owner.ID)
	if err != nil {
		return nil, false, err
	}
	eligible := goal != nil && goal.State == session.GoalArmed && owner.Config.GoalsEnabled
	if input != nil && input.Goal != nil && (!eligible || goal.GoalRef != *input.Goal) {
		if _, err := tx.ExecContext(ctx, "UPDATE inputs SET cancelled_at=? WHERE id=?", now(), input.ID); err != nil {
			return nil, false, err
		}
		if goal != nil && goal.ID == input.Goal.ID && goal.State == session.GoalArmed && !owner.Config.GoalsEnabled {
			if err := pauseGoal(ctx, tx, *goal, "disabled"); err != nil {
				return nil, false, err
			}
		}
		return nil, true, nil
	}
	if !eligible {
		return nil, false, nil
	}
	return new(goal.GoalRef), false, nil
}

func pauseGoal(ctx context.Context, tx *sql.Tx, goal session.Goal, reason string) error {
	if _, err := tx.ExecContext(ctx, "UPDATE goals SET state='paused',revision=revision+1,stop_reason=? WHERE id=?", reason, goal.ID); err != nil {
		return err
	}
	return cancelQueuedGoalInputs(ctx, tx, goal.ID)
}

func finishGoal(ctx context.Context, tx *sql.Tx, turn session.Turn) error {
	if turn.Goal == nil || turn.Kind != session.PromptInput {
		return nil
	}
	goal, err := currentGoal(ctx, tx, turn.SessionID)
	if err != nil {
		return err
	}
	if goal == nil || goal.GoalRef != *turn.Goal || goal.State != session.GoalArmed {
		return nil
	}
	if turn.State != session.Succeeded {
		return pauseGoal(ctx, tx, *goal, "turn_"+string(turn.State))
	}
	owner, err := readSession(ctx, tx, turn.SessionID)
	if err != nil {
		return err
	}
	if owner.Lifecycle != session.Active {
		return pauseGoal(ctx, tx, *goal, "stopped")
	}
	if !owner.Config.GoalsEnabled {
		return pauseGoal(ctx, tx, *goal, "disabled")
	}
	var uncertain bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM model_attempts WHERE turn_id=? AND state='uncertain') OR EXISTS(SELECT 1 FROM cells WHERE turn_id=? AND state='uncertain') OR EXISTS(SELECT 1 FROM operations o JOIN cells c ON c.id=o.cell_id WHERE c.turn_id=? AND o.state='uncertain')`, turn.ID, turn.ID, turn.ID).Scan(&uncertain); err != nil {
		return err
	}
	if uncertain {
		return pauseGoal(ctx, tx, *goal, "uncertain_execution")
	}
	if _, err := turnOutput(ctx, tx, turn.ID); err != nil {
		if errors.Is(err, session.ErrInvalid) {
			return pauseGoal(ctx, tx, *goal, "output_invalid")
		}
		return err
	}
	intent, err := completionIntent(ctx, tx, turn, owner.TreeID)
	if err != nil {
		return err
	}
	if intent != nil {
		if _, err := tx.ExecContext(ctx, "UPDATE goals SET state='completed',revision=revision+1,stop_reason=NULL,completion_turn_id=?,completion_operation_id=? WHERE id=?", turn.ID, *intent, goal.ID); err != nil {
			return err
		}
		return cancelQueuedGoalInputs(ctx, tx, goal.ID)
	}
	var outstanding bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM inputs WHERE goal_id=? AND turn_id IS NULL AND cancelled_at IS NULL)`, goal.ID).Scan(&outstanding); err != nil {
		return err
	}
	if outstanding {
		return nil
	}
	if goal.ContinuationsUsed >= goal.Spec.MaxContinuations {
		return pauseGoal(ctx, tx, *goal, "round_limit")
	}
	// Admission checks run after writes. Roll back only this proposed continuation
	// on semantic pressure, retaining the already completed turn and its accounting.
	if _, err := tx.ExecContext(ctx, "SAVEPOINT goal_continuation"); err != nil {
		return err
	}
	admissionErr := continueGoal(ctx, tx, turn, *goal)
	if admissionErr != nil {
		if !errors.Is(admissionErr, ErrLimit) {
			return admissionErr
		}
		if _, err := tx.ExecContext(ctx, "ROLLBACK TO goal_continuation"); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "RELEASE goal_continuation"); err != nil {
		return err
	}
	if admissionErr != nil {
		return pauseGoal(ctx, tx, *goal, "admission_limit")
	}
	return nil
}

func continueGoal(ctx context.Context, tx *sql.Tx, turn session.Turn, goal session.Goal) error {
	if _, err := tx.ExecContext(ctx, "UPDATE goals SET revision=revision+1,continuations_used=continuations_used+1,stop_reason=NULL WHERE id=?", goal.ID); err != nil {
		return err
	}
	ref := session.GoalRef{ID: goal.ID, Revision: goal.Revision + 1}
	identity := session.RequestIdentity{ClientID: "goal", RequestID: fmt.Sprintf("continuation_%x", sha256.Sum256([]byte(turn.ID)))}
	digest, err := requestDigest("goal_continuation", struct {
		Turn session.TurnID
		Goal session.GoalRef
	}{turn.ID, ref})
	if err != nil {
		return err
	}
	_, err = admitGoalInput(ctx, tx, identity, digest, turn.SessionID, ref)
	return err
}

// ApplyGoalCompletion dispatches and records an authorized intent atomically.
// It deliberately does not mutate the goal before successful turn settlement.
func (s *Store) ApplyGoalCompletion(ctx context.Context, id session.OperationID) (result json.RawMessage, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		op, err := readOperation(ctx, tx, id)
		if err != nil {
			return err
		}
		if op.Capability != "goals.complete" {
			return ErrConflict
		}
		if op.State == session.OperationSucceeded && op.Result != nil {
			result = op.Result.Value
			return nil
		}
		var request session.GoalCompletion
		decoder := json.NewDecoder(bytes.NewReader(op.Arguments))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			return fmt.Errorf("%w: invalid goal completion: %w", session.ErrInvalid, err)
		}
		if err := request.Validate(); err != nil {
			return err
		}
		turn, err := readTurn(ctx, tx, op.TurnID)
		if err != nil {
			return err
		}
		ref := session.GoalRef{ID: request.GoalID, Revision: request.ExpectedRevision}
		if turn.Goal == nil || *turn.Goal != ref || turn.Kind != session.PromptInput {
			return ErrConflict
		}
		owner, err := readSession(ctx, tx, op.SessionID)
		if err != nil {
			return err
		}
		if op.Resource != string(owner.TreeID) || !owner.Config.GoalsEnabled {
			return ErrConflict
		}
		var enabled bool
		if err := tx.QueryRowContext(ctx, `SELECT json_extract(configuration,'$.goals_enabled') FROM session_configurations WHERE session_id=? AND revision=?`, turn.SessionID, turn.ConfigRevision).Scan(&enabled); err != nil {
			return err
		}
		if !enabled {
			return ErrConflict
		}
		selected, err := currentGoal(ctx, tx, owner.ID)
		if err != nil {
			return err
		}
		if selected == nil || selected.GoalRef != ref || selected.State != session.GoalArmed {
			return ErrConflict
		}
		dispatch, err := dispatchOperation(ctx, tx, id)
		if err != nil {
			return err
		}
		if !dispatch {
			return ErrConflict
		}
		result, err = json.Marshal(struct {
			GoalID           session.GoalID `json:"goal_id"`
			ExpectedRevision int64          `json:"expected_revision,string"`
			Accepted         bool           `json:"accepted"`
		}{request.GoalID, request.ExpectedRevision, true})
		if err != nil {
			return err
		}
		op.State = session.OperationDispatched
		_, err = settleOperation(ctx, tx, op, session.OperationResult{State: session.OperationSucceeded, Value: result})
		return err
	})
	return
}

//nolint:nilnil // No authorized completion intent is the ordinary continuation case.
func completionIntent(ctx context.Context, q querier, turn session.Turn, tree session.TreeID) (*session.OperationID, error) {
	var id session.OperationID
	err := q.QueryRowContext(ctx, `SELECT o.id FROM operations o JOIN cells c ON c.id=o.cell_id WHERE c.turn_id=? AND o.capability='goals.complete' AND o.resource=? AND o.state='succeeded' AND json_extract(o.arguments,'$.goal_id')=? AND json_extract(o.arguments,'$.expected_revision')=? ORDER BY o.created_at,o.id LIMIT 1`, turn.ID, tree, turn.Goal.ID, strconv.FormatInt(turn.Goal.Revision, 10)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}
