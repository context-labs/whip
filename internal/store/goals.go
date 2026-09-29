package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/context-labs/whip/internal/session"
)

// GoalAdmission projects the original creation, even when that goal is no longer
// current. Initial refers only to the optional input admitted by this creation.
type GoalAdmission struct {
	ID        session.GoalID
	Goal      *session.Goal
	Current   bool
	Initial   *Admission
	DeletedAt *time.Time
}

type GoalChange struct {
	Goal         session.Goal
	CancelTurnID *session.TurnID
}

const goalSelect = `SELECT id,revision,session_id,text,max_continuations,state,continuations_used,stop_reason,created_at,completion_turn_id,completion_operation_id,origin_formulation_attempt_id FROM goals`

func scanGoal(row scanner) (value session.Goal, err error) {
	var created int64
	err = row.Scan(&value.ID, &value.Revision, &value.SessionID, &value.Spec.Text, &value.Spec.MaxContinuations, &value.State, &value.ContinuationsUsed, &value.StopReason, &created, &value.CompletionTurnID, &value.CompletionOperationID, &value.OriginFormulationAttemptID)
	if err != nil {
		return value, found(err)
	}
	value.CreatedAt = timestamp(created)
	return value, nil
}

func readGoal(ctx context.Context, q querier, owner session.SessionID, id session.GoalID) (session.Goal, error) {
	return scanGoal(q.QueryRowContext(ctx, goalSelect+" WHERE session_id=? AND id=? AND deleted_at IS NULL", owner, id))
}

//nolint:nilnil // No goal has ever been selected for an existing session.
func currentGoal(ctx context.Context, q querier, owner session.SessionID) (*session.Goal, error) {
	value, err := scanGoal(q.QueryRowContext(ctx, goalSelect+" WHERE session_id=? AND deleted_at IS NULL ORDER BY ordinal DESC LIMIT 1", owner))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func (s *Store) Goal(ctx context.Context, owner session.SessionID, id session.GoalID) (session.Goal, error) {
	return readGoal(ctx, s.db, owner, id)
}

func (s *Store) CurrentGoal(ctx context.Context, owner session.SessionID) (_ *session.Goal, err error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("rollback goal inspection: %w", rollbackErr))
		}
	}()
	if _, err := readSession(ctx, tx, owner); err != nil {
		return nil, err
	}
	result, err := currentGoal(ctx, tx, owner)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func goalInitialIdentity(id session.GoalID) session.RequestIdentity {
	return session.RequestIdentity{ClientID: "goal", RequestID: fmt.Sprintf("initial_%x", sha256.Sum256([]byte(id)))}
}

func goalAdmission(ctx context.Context, q querier, owner session.SessionID, id session.GoalID) (result GoalAdmission, err error) {
	result.ID = id
	var deleted sql.NullInt64
	if err = q.QueryRowContext(ctx, "SELECT deleted_at FROM goals WHERE id=? AND session_id=?", id, owner).Scan(&deleted); err != nil {
		return result, found(err)
	}
	result.DeletedAt = optionalTime(deleted)
	if !deleted.Valid {
		value, e := readGoal(ctx, q, owner, id)
		if e != nil {
			return result, e
		}
		result.Goal = &value
		selected, e := currentGoal(ctx, q, owner)
		if e != nil {
			return result, e
		}
		result.Current = selected != nil && selected.ID == id
	}
	initial, e := readAdmission(ctx, q, goalInitialIdentity(id))
	if e == nil {
		result.Initial = &initial
	} else if !errors.Is(e, ErrNotFound) {
		return result, e
	}
	return result, nil
}

func goalEligible(ctx context.Context, tx *sql.Tx, owner session.SessionID, except session.TurnID) error {
	if err := requireNoDirectWork(ctx, tx, owner); err != nil {
		return err
	}
	value, err := readSession(ctx, tx, owner)
	if err != nil {
		return err
	}
	if value.Lifecycle != session.Active {
		return ErrStopped
	}
	if !value.Config.GoalsEnabled {
		return fmt.Errorf("%w: goals are disabled for session", session.ErrInvalid)
	}
	var busy bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM turns WHERE session_id=? AND id<>? AND state IN ('running','cancelling'))", owner, except).Scan(&busy); err != nil {
		return err
	}
	if busy {
		return ErrBusy
	}
	return nil
}

func cancelQueuedGoalInputs(ctx context.Context, tx *sql.Tx, id session.GoalID) error {
	_, err := tx.ExecContext(ctx, "UPDATE inputs SET cancelled_at=? WHERE goal_id=? AND turn_id IS NULL AND steered_turn_id IS NULL AND cancelled_at IS NULL", now(), id)
	return err
}

// CreateGoal resolves a stable ID before inspecting the owner's current state.
// Replaying this request never selects an old goal or admits another input.
func (s *Store) CreateGoal(ctx context.Context, owner session.SessionID, id session.GoalID, expected *session.GoalRef, request session.GoalRequest, start bool) (GoalAdmission, error) {
	return s.CreateGoalWithDefault(ctx, owner, id, expected, request, start, 100)
}

// CreateGoalWithDefault captures an injected host allowance only for a new goal
// with an omitted allowance. The unresolved request remains the retry identity.
func (s *Store) CreateGoalWithDefault(ctx context.Context, owner session.SessionID, id session.GoalID, expected *session.GoalRef, request session.GoalRequest, start bool, defaultContinuations int64) (result GoalAdmission, err error) {
	for _, value := range []string{string(owner), string(id)} {
		if err := session.ValidateID(value); err != nil {
			return result, err
		}
	}
	digest, err := goalCreationDigest(owner, expected, request, start)
	if err != nil {
		return result, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		var previous string
		err := tx.QueryRowContext(ctx, "SELECT initial_digest FROM goals WHERE id=?", id).Scan(&previous)
		if err == nil {
			if previous != digest {
				return ErrConflict
			}
			result, err = goalAdmission(ctx, tx, owner, id)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if expected != nil {
			if err := expected.Validate(); err != nil {
				return err
			}
		}
		resolved := request
		if resolved.MaxContinuations == nil {
			resolved.MaxContinuations = new(defaultContinuations)
		}
		spec, err := resolved.Resolve()
		if err != nil {
			return err
		}
		if err := goalEligible(ctx, tx, owner, ""); err != nil {
			return err
		}
		if err := createGoal(ctx, tx, owner, id, expected, spec, start, digest, nil); err != nil {
			return err
		}
		result, err = goalAdmission(ctx, tx, owner, id)
		return err
	})
	return
}

// createGoal applies the shared goal CAS, replacement, charges and initial input.
// Public creation checks idle eligibility; formulation permits only its own turn.
func createGoal(ctx context.Context, tx *sql.Tx, owner session.SessionID, id session.GoalID, expected *session.GoalRef, spec session.GoalSpec, start bool, digest string, origin *session.ModelAttemptID) error {
	selected, err := currentGoal(ctx, tx, owner)
	if err != nil {
		return err
	}
	if (selected == nil) != (expected == nil) || selected != nil && selected.GoalRef != *expected {
		return ErrConflict
	}
	if selected != nil && selected.State.Open() {
		if selected.Revision == math.MaxInt64 {
			return ErrLimit
		}
		if _, err := tx.ExecContext(ctx, "UPDATE goals SET state='superseded',revision=revision+1,stop_reason=NULL WHERE id=?", selected.ID); err != nil {
			return err
		}
		if err := cancelQueuedGoalInputs(ctx, tx, selected.ID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO goals (id,session_id,initial_digest,text,max_continuations,revision,state,continuations_used,created_at,origin_formulation_attempt_id) VALUES (?,?,?,?,?,1,'armed',0,?,?)`, id, owner, digest, spec.Text, spec.MaxContinuations, now(), origin); err != nil {
		return err
	}
	if err := chargeWrite(ctx, tx, owner, "goal", string(id), 0, int64(len(spec.Text))); err != nil {
		return err
	}
	if start {
		if _, err := admitGoalInput(ctx, tx, goalInitialIdentity(id), digest, owner, session.GoalRef{ID: id, Revision: 1}); err != nil {
			return err
		}
	}
	return nil
}

// Claim captures the immutable goal through this input provenance; the objective
// is not copied into each admitted input.
func admitGoalInput(ctx context.Context, tx *sql.Tx, identity session.RequestIdentity, digest string, owner session.SessionID, ref session.GoalRef) (Admission, error) {
	parts := []session.Part{{Type: "text", Text: "Work on the goal."}}
	result, err := admitInput(ctx, tx, identity, digest, Submission{SessionID: owner, Source: session.GoalInput, Goal: &ref, Parts: parts})
	if err != nil {
		return result, err
	}
	err = chargeWrite(ctx, tx, owner, "input", string(result.Input.ID), 0, inputWriteBytes(parts))
	return result, err
}

// ResumeGoal admits a caller-identified input. Receipt replay precedes revision,
// eligibility and allowance checks, and never resets continuation usage.
func (s *Store) ResumeGoal(ctx context.Context, identity session.RequestIdentity, owner session.SessionID, ref session.GoalRef) (result Admission, err error) {
	if err := validatePublicIdentity(identity); err != nil {
		return result, err
	}
	for _, value := range []string{identity.ClientID, identity.RequestID, string(owner)} {
		if err := session.ValidateID(value); err != nil {
			return result, err
		}
	}
	digest, err := goalResumeDigest(owner, ref)
	if err != nil {
		return result, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		receipt, e := readReceipt(ctx, tx, identity)
		if e == nil {
			if receipt.Digest != digest {
				return ErrConflict
			}
			result, err = readAdmission(ctx, tx, identity)
			return err
		}
		if !errors.Is(e, ErrNotFound) {
			return e
		}
		if err := ref.Validate(); err != nil {
			return err
		}
		if err := goalEligible(ctx, tx, owner, ""); err != nil {
			return err
		}
		goal, err := readGoal(ctx, tx, owner, ref.ID)
		if err != nil {
			return err
		}
		selected, err := currentGoal(ctx, tx, owner)
		if err != nil {
			return err
		}
		if selected == nil || selected.GoalRef != ref || !goal.State.Open() {
			return ErrConflict
		}
		var outstanding, started bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM inputs i LEFT JOIN turns t ON t.id=i.turn_id WHERE i.goal_id=? AND ((i.turn_id IS NULL AND i.steered_turn_id IS NULL AND i.cancelled_at IS NULL) OR t.state IN ('running','cancelling'))),(EXISTS(SELECT 1 FROM inputs WHERE goal_id=?) OR EXISTS(SELECT 1 FROM turns WHERE goal_id=?))`, ref.ID, ref.ID, ref.ID).Scan(&outstanding, &started); err != nil {
			return err
		}
		if outstanding {
			return ErrBusy
		}
		if started && goal.ContinuationsUsed >= goal.Spec.MaxContinuations {
			return ErrLimit
		}
		if _, err := tx.ExecContext(ctx, "UPDATE goals SET state='armed',revision=revision+1,stop_reason=NULL WHERE id=?", ref.ID); err != nil {
			return err
		}
		ref.Revision++
		result, err = admitGoalInput(ctx, tx, identity, digest, owner, ref)
		return err
	})
	return
}

func (s *Store) CancelGoal(ctx context.Context, owner session.SessionID, id session.GoalID) (result GoalChange, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		value, err := readGoal(ctx, tx, owner, id)
		if err != nil {
			return err
		}
		if value.State.Open() {
			if _, err := tx.ExecContext(ctx, "UPDATE goals SET state='cancelled',revision=revision+1,stop_reason=NULL WHERE id=?", id); err != nil {
				return err
			}
			if err := cancelQueuedGoalInputs(ctx, tx, id); err != nil {
				return err
			}
		}
		// A terminal retry can still identify its previously cancelled turn, but it
		// never chooses a newer human turn by the session's mutable active state.
		if value.State.Open() || value.State == session.GoalCancelled {
			var turn session.TurnID
			err = tx.QueryRowContext(ctx, `SELECT t.id FROM inputs i JOIN turns t ON t.id=i.turn_id WHERE i.goal_id=? AND t.state IN ('running','cancelling')`, id).Scan(&turn)
			if err == nil {
				if _, err := cancelTurn(ctx, tx, turn); err != nil {
					return err
				}
				result.CancelTurnID = &turn
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		result.Goal, err = readGoal(ctx, tx, owner, id)
		return err
	})
	return
}
