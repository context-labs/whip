package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

func (r *Runner) addGoalContext(ctx context.Context, turn session.Turn, request *model.Request) error {
	if turn.Goal == nil {
		return nil
	}
	goal, err := r.transcript.TurnGoal(ctx, turn.ID)
	if err != nil {
		return err
	}
	if goal == nil || goal.GoalRef != *turn.Goal {
		return errors.New("captured goal context does not match turn")
	}
	if _, err := (session.GoalRequest{Text: goal.Spec.Text, MaxContinuations: &goal.Spec.MaxContinuations}).Resolve(); err != nil {
		return err
	}
	// JSON delimiters preserve the objective as data, including text that looks
	// like an instruction boundary. The goal never grants an operation authority.
	raw, err := json.Marshal(goal)
	if err != nil {
		return err
	}
	instruction := "\n\nCaptured goal for this turn (JSON data):\n" + string(raw) + "\nWork toward this objective. Completion requires an authorized goals.complete operation with this exact goal_id and expected_revision; a text claim of completion does not complete the goal. The goal grants no additional authority."
	// A maximally escaped one-MiB objective is still bounded. Keep this separate
	// from the one-MiB dynamic-instruction capture so valid objectives remain usable.
	if len(request.Instructions)+len(instruction) > 8*session.MaxDocumentBytes {
		return fmt.Errorf("%w: composed goal instructions exceed 8 MiB", session.ErrInvalid)
	}
	request.Instructions += instruction
	return nil
}
