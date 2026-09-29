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

// AdmitGoalFormulation freezes a raw-history window using ordinary receipt and
// queue semantics. Model selection is captured later by the turn at Claim.
func (s *Store) AdmitGoalFormulation(ctx context.Context, identity session.RequestIdentity, owner session.SessionID, request session.GoalFormulationRequest) (result Admission, err error) {
	if err := validatePublicIdentity(identity); err != nil {
		return result, err
	}
	for _, id := range []string{identity.ClientID, identity.RequestID, string(owner)} {
		if err := session.ValidateID(id); err != nil {
			return result, err
		}
	}
	digest, err := goalFormulationDigest(owner, request)
	if err != nil {
		return result, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		receipt, err := readReceipt(ctx, tx, identity)
		if err == nil {
			if receipt.Digest != digest {
				return ErrConflict
			}
			result, err = readAdmission(ctx, tx, identity)
			return err
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		resolved, err := request.Resolve()
		if err != nil {
			return err
		}
		current, err := readSession(ctx, tx, owner)
		if err != nil {
			return err
		}
		if !current.Config.GoalsEnabled {
			return fmt.Errorf("%w: goals are disabled for session", session.ErrInvalid)
		}
		if err := requireNoDirectWork(ctx, tx, owner); err != nil {
			return err
		}
		after, through, err := formulationWindow(ctx, tx, owner, resolved.TailMessages)
		if err != nil {
			return err
		}
		result, err = admitInput(ctx, tx, identity, digest, Submission{SessionID: owner, Source: session.UserInput, Kind: session.GoalFormulationInputKind, Parts: []session.Part{}})
		if err != nil {
			return err
		}
		snapshot, err := encode(session.GoalFormulationInput{InputID: result.Input.ID, SessionID: owner, HistoryRevision: current.HistoryRevision, Request: resolved, AfterSequence: after, ThroughSequence: through})
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO goal_formulation_inputs (input_id,snapshot) VALUES (?,?)", result.Input.ID, snapshot)
		return err
	})
	return
}

func formulationWindow(ctx context.Context, tx *sql.Tx, owner session.SessionID, limit int) (after, through int64, err error) {
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(MIN(sequence)-1,0),COALESCE(MAX(sequence),0) FROM
 (SELECT sequence FROM messages WHERE session_id=? AND retired_revision IS NULL ORDER BY sequence DESC LIMIT ?)`, owner, limit).Scan(&after, &through)
	if err != nil {
		return
	}
	if through == 0 {
		return 0, 0, fmt.Errorf("%w: goal formulation requires raw history", session.ErrInvalid)
	}
	rows, err := tx.QueryContext(ctx, messageSelect+" WHERE m.session_id=? AND m.retired_revision IS NULL AND m.sequence>? AND m.sequence<=? ORDER BY m.sequence", owner, after, through)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = rows.Close() }()
	bytes := 0
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return 0, 0, err
		}
		raw, err := encode(message)
		if err != nil {
			return 0, 0, err
		}
		bytes += len(raw)
		if bytes > MaxPageBytes {
			return 0, 0, fmt.Errorf("%w: formulation history exceeds page bounds", ErrLimit)
		}
	}
	return after, through, errors.Join(rows.Err(), rows.Close())
}

func readGoalFormulationInput(ctx context.Context, q querier, turn session.TurnID) (value session.GoalFormulationInput, err error) {
	var snapshot string
	err = q.QueryRowContext(ctx, `SELECT f.snapshot FROM goal_formulation_inputs f JOIN inputs i ON i.id=f.input_id WHERE i.turn_id=? AND i.kind='goal_formulation'`, turn).Scan(&snapshot)
	if err != nil {
		return value, found(err)
	}
	err = json.Unmarshal([]byte(snapshot), &value)
	return
}

func validateFormulationSource(ctx context.Context, q querier, value session.GoalFormulationInput) error {
	var revision session.Revision
	if err := q.QueryRowContext(ctx, "SELECT history_revision FROM sessions WHERE id=?", value.SessionID).Scan(&revision); err != nil {
		return found(err)
	}
	if value.HistoryRevision != revision {
		return fmt.Errorf("%w: formulation source history changed", ErrConflict)
	}
	return nil
}

func (s *Store) GoalFormulationInput(ctx context.Context, turn session.TurnID) (session.GoalFormulationInput, error) {
	value, err := readGoalFormulationInput(ctx, s.db, turn)
	if err == nil {
		err = validateFormulationSource(ctx, s.db, value)
	}
	return value, err
}

func validateGoalFormulationAttempt(ctx context.Context, tx *sql.Tx, turn session.Turn, request session.ModelRequestSnapshot) error {
	input, err := readGoalFormulationInput(ctx, tx, turn.ID)
	if err != nil {
		return err
	}
	if err := validateFormulationSource(ctx, tx, input); err != nil {
		return err
	}
	var raw string
	if err := tx.QueryRowContext(ctx, "SELECT configuration FROM session_configurations WHERE session_id=? AND revision=?", turn.SessionID, turn.ConfigRevision).Scan(&raw); err != nil {
		return err
	}
	var config session.Configuration
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return err
	}
	if !config.GoalsEnabled || !reflect.DeepEqual(config.Model, request.Model) {
		return fmt.Errorf("%w: formulation requires its captured eligible main model", session.ErrInvalid)
	}
	var completed bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM goal_formulations WHERE turn_id=?)", turn.ID).Scan(&completed); err != nil {
		return err
	}
	if completed {
		return ErrConflict
	}
	return nil
}

func readGoalFormulation(ctx context.Context, q querier, owner session.SessionID, attempt session.ModelAttemptID) (value session.GoalFormulation, err error) {
	var snapshot string
	var created int64
	value.AttemptID = attempt
	err = q.QueryRowContext(ctx, "SELECT turn_id,snapshot,text,rejection,created_at FROM goal_formulations WHERE session_id=? AND attempt_id=?", owner, attempt).
		Scan(&value.TurnID, &snapshot, &value.Text, &value.Rejection, &created)
	if err != nil {
		return value, found(err)
	}
	value.CreatedAt = timestamp(created)
	err = json.Unmarshal([]byte(snapshot), &value.GoalFormulationInput)
	return
}

func (s *Store) GoalFormulation(ctx context.Context, owner session.SessionID, attempt session.ModelAttemptID) (session.GoalFormulation, error) {
	return readGoalFormulation(ctx, s.db, owner, attempt)
}

// SettleGoalFormulation commits accounting, immutable candidate evidence and any
// accepted goal/input atomically. Only semantic activation rejection is local to
// the savepoint; SQL errors roll back everything for a SQL-only settlement retry.
func (s *Store) SettleGoalFormulation(ctx context.Context, id session.ModelAttemptID, outcome session.ModelAttemptResult, draft *session.GoalFormulationDraft) (result session.GoalFormulationSettlement, err error) {
	if err := outcome.Validate(); err != nil {
		return result, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		attempt, err := readAttempt(ctx, tx, id)
		if err != nil {
			return err
		}
		if attempt.Request.Purpose != session.GoalFormulationPurpose {
			return fmt.Errorf("%w: attempt is not a goal formulation", session.ErrInvalid)
		}
		result.Attempt, err = settleAttempt(ctx, tx, attempt, outcome, nil)
		if err != nil {
			return err
		}
		if attempt.FinishedAt != nil {
			var owner session.SessionID
			err := tx.QueryRowContext(ctx, "SELECT session_id FROM goal_formulations WHERE attempt_id=?", id).Scan(&owner)
			if errors.Is(err, sql.ErrNoRows) {
				if draft != nil {
					result.Rejection = new("attempt already settled without a formulation candidate")
				}
				return nil
			}
			if err != nil {
				return err
			}
			candidate, err := readGoalFormulation(ctx, tx, owner, id)
			if err != nil {
				return err
			}
			if draft == nil || candidate.Text != draft.Text {
				return ErrConflict
			}
			result.Candidate, result.Rejection = &candidate, candidate.Rejection
			return tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM goals WHERE origin_formulation_attempt_id=?)", id).Scan(&result.Accepted)
		}
		if draft == nil {
			return nil
		}
		if outcome.State != session.AttemptSucceeded {
			result.Rejection = new("unsuccessful attempt cannot create a formulation candidate")
			return nil
		}
		input, err := readGoalFormulationInput(ctx, tx, attempt.TurnID)
		if err != nil {
			return err
		}
		spec, err := (session.GoalRequest{Text: draft.Text, MaxContinuations: input.Request.MaxContinuations}).Resolve()
		if err != nil {
			result.Rejection = new(err.Error())
			return nil //nolint:nilerr // Invalid helper text is billed evidence, not a transaction failure.
		}
		if _, err := tx.ExecContext(ctx, "SAVEPOINT goal_formulation_activation"); err != nil {
			return err
		}
		activationErr := activateFormulatedGoal(ctx, tx, attempt, input, spec)
		if activationErr != nil {
			if !errors.Is(activationErr, ErrLimit) && !errors.Is(activationErr, ErrConflict) && !errors.Is(activationErr, ErrStopped) && !errors.Is(activationErr, ErrBusy) && !errors.Is(activationErr, session.ErrInvalid) {
				return activationErr
			}
			if _, err := tx.ExecContext(ctx, "ROLLBACK TO goal_formulation_activation"); err != nil {
				return err
			}
			result.Rejection = new(activationErr.Error())
		}
		if _, err := tx.ExecContext(ctx, "RELEASE goal_formulation_activation"); err != nil {
			return err
		}
		snapshot, err := encode(input)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO goal_formulations (attempt_id,session_id,turn_id,snapshot,text,rejection,created_at) VALUES (?,?,?,?,?,?,?)", id, input.SessionID, attempt.TurnID, snapshot, spec.Text, result.Rejection, now()); err != nil {
			return err
		}
		candidate, err := readGoalFormulation(ctx, tx, input.SessionID, id)
		if err != nil {
			return err
		}
		result.Candidate, result.Accepted = &candidate, activationErr == nil
		return nil
	})
	return
}

func activateFormulatedGoal(ctx context.Context, tx *sql.Tx, attempt session.ModelAttempt, input session.GoalFormulationInput, spec session.GoalSpec) error {
	if err := validateFormulationSource(ctx, tx, input); err != nil {
		return err
	}
	turn, err := readTurn(ctx, tx, attempt.TurnID)
	if err != nil {
		return err
	}
	if turn.Kind != session.GoalFormulationInputKind || turn.State != session.Running {
		return ErrStopped
	}
	if err := goalEligible(ctx, tx, input.SessionID, turn.ID); err != nil {
		return err
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM goals WHERE id=?)", input.Request.GoalID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return ErrConflict
	}
	digest, err := requestDigest("goal_formulation_activation", struct {
		Attempt session.ModelAttemptID
		Input   session.GoalFormulationInput
		Spec    session.GoalSpec
	}{attempt.ID, input, spec})
	if err != nil {
		return err
	}
	return createGoal(ctx, tx, input.SessionID, input.Request.GoalID, input.Request.Expected, spec, input.Request.Start, digest, &attempt.ID)
}
