package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/context-labs/whip/internal/session"
)

func validateInputDelivery(delivery session.InputDelivery, target *session.TurnID) error {
	if delivery != "" && delivery != session.DeliveryQueued && delivery != session.DeliverySteer {
		return fmt.Errorf("%w: delivery must be queued or steer", session.ErrInvalid)
	}
	if target != nil {
		if delivery != session.DeliverySteer {
			return fmt.Errorf("%w: only steering can target a turn", session.ErrInvalid)
		}
		return session.ValidateID(string(*target))
	}
	return nil
}

func steeringTarget(ctx context.Context, q querier, request Submission) (*session.TurnID, error) {
	if err := validateInputDelivery(request.Delivery, request.TargetTurnID); err != nil {
		return nil, err
	}
	if request.Delivery != session.DeliverySteer {
		return nil, nil //nolint:nilnil // Ordinary queued admission has no target.
	}
	if request.Kind != session.PromptInput || (request.Source != session.UserInput && request.Source != session.AgentInput) {
		return nil, session.ErrInvalid
	}
	var id session.TurnID
	err := q.QueryRowContext(ctx, `SELECT t.id FROM turns t WHERE t.session_id=? AND t.state='running'
 AND COALESCE((SELECT kind FROM inputs WHERE turn_id=t.id),'prompt')='prompt'`, request.SessionID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		if request.TargetTurnID != nil {
			return nil, ErrConflict
		}
		return nil, nil //nolint:nilnil // Idle, direct, or maintenance work retains normal queued delivery.
	}
	if err != nil {
		return nil, err
	}
	if request.TargetTurnID != nil && *request.TargetTurnID != id {
		return nil, ErrConflict
	}
	return &id, nil
}

func insertInputSteering(ctx context.Context, tx *sql.Tx, request session.SteerInputRequest, digest string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO input_steering VALUES (?,?,?,?,?,?)", request.ID, digest, request.SessionID, request.InputID, request.TurnID, now())
	return err
}

func readInputSteering(ctx context.Context, q querier, id session.InputSteeringID) (result session.InputSteeringResult, digest string, err error) {
	var created int64
	v := &result.Steering
	err = q.QueryRowContext(ctx, "SELECT id,digest,session_id,input_id,turn_id,created_at FROM input_steering WHERE id=?", id).
		Scan(&v.ID, &digest, &v.SessionID, &v.InputID, &v.TurnID, &created)
	if err != nil {
		return result, digest, found(err)
	}
	v.CreatedAt = timestamp(created)
	input, err := readInput(ctx, q, v.InputID)
	if errors.Is(err, ErrNotFound) {
		result.Deleted = true
		return result, digest, nil
	}
	if err == nil {
		result.Input = &input
	}
	return result, digest, err
}

func (s *Store) InputSteering(ctx context.Context, owner session.SessionID, id session.InputSteeringID) (result session.InputSteeringResult, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		result, _, err = readInputSteering(ctx, tx, id)
		if err == nil && result.Steering.SessionID != owner {
			return ErrNotFound
		}
		return err
	})
	return
}

// SteerInput promotes an existing queued prompt without copying its parts or
// changing its original receipt. Human controls, like human admission, do not
// charge agent write budgets. The one-target-per-input guard bounds live intent.
func (s *Store) SteerInput(ctx context.Context, request session.SteerInputRequest) (result session.InputSteeringResult, err error) {
	if err := session.ValidateID(string(request.ID)); err != nil {
		return result, err
	}
	digest, err := requestDigest("input_steer", request)
	if err != nil {
		return result, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		previous, previousDigest, err := readInputSteering(ctx, tx, request.ID)
		if err == nil {
			if previousDigest != digest {
				return ErrConflict
			}
			result = previous
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		for _, id := range []string{string(request.SessionID), string(request.InputID), string(request.TurnID)} {
			if err := session.ValidateID(id); err != nil {
				return err
			}
		}
		input, err := readInput(ctx, tx, request.InputID)
		if err != nil {
			return err
		}
		if input.SessionID != request.SessionID {
			return ErrNotFound
		}
		if input.State != session.Queued || input.Steering != nil {
			return ErrConflict
		}
		owner, err := readSession(ctx, tx, request.SessionID)
		if err != nil {
			return err
		}
		if owner.Lifecycle != session.Active {
			return ErrStopped
		}
		if _, err := steeringTarget(ctx, tx, Submission{SessionID: request.SessionID, Kind: input.Kind, Source: input.Source, Delivery: session.DeliverySteer, TargetTurnID: &request.TurnID}); err != nil {
			return err
		}
		if err := insertInputSteering(ctx, tx, request, digest); err != nil {
			return err
		}
		result, _, err = readInputSteering(ctx, tx, request.ID)
		return err
	})
	return
}

// observeInputSteers commits whole authored messages only between completed
// model/tool batches. Queue admission already bounds each input and reference.
func observeInputSteers(ctx context.Context, tx *sql.Tx, turn session.Turn) ([]session.Message, error) {
	if turn.State != session.Running {
		return nil, ErrStopped
	}
	if turn.Kind != session.PromptInput {
		return []session.Message{}, nil
	}
	var activeModel bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM model_attempts WHERE turn_id=? AND state IN ('reserved','dispatched'))", turn.ID).Scan(&activeModel); err != nil {
		return nil, err
	}
	_, pending, err := pendingCalls(ctx, tx, turn.ID)
	if err != nil {
		return nil, err
	}
	if activeModel || len(pending) != 0 {
		return nil, ErrBusy
	}
	result := []session.Message{}
	size := 0
	for range 20 {
		var id session.InputID
		var bytes int
		err := tx.QueryRowContext(ctx, `SELECT i.id,length(CAST(i.parts AS BLOB)) FROM inputs i JOIN input_steering s ON s.input_id=i.id
 WHERE s.turn_id=? AND i.turn_id IS NULL AND i.steered_turn_id IS NULL AND i.cancelled_at IS NULL
 ORDER BY i.ordinal LIMIT 1`, turn.ID).Scan(&id, &bytes)
		if errors.Is(err, sql.ErrNoRows) {
			break
		}
		if err != nil {
			return nil, err
		}
		// Leave whole messages pending once this bounded batch is full.
		if bytes > MaxPageBytes/2-size {
			break
		}
		size += bytes
		if _, err := tx.ExecContext(ctx, "UPDATE inputs SET steered_turn_id=? WHERE id=?", turn.ID, id); err != nil {
			return nil, err
		}
		messageID := session.MessageID("message_" + string(id))
		if _, err := tx.ExecContext(ctx, `INSERT INTO messages (id,session_id,turn_id,group_id,sequence,role,input_id,created_at)
 SELECT ?,?,?,?,COALESCE(MAX(sequence),0)+1,'user',?,? FROM messages WHERE session_id=?`, messageID, turn.SessionID, turn.ID, turn.ID, id, now(), turn.SessionID); err != nil {
			return nil, err
		}
		message, err := scanMessage(tx.QueryRowContext(ctx, messageSelect+" WHERE m.id=?", messageID))
		if err != nil {
			return nil, err
		}
		result = append(result, message)
	}
	return result, nil
}
