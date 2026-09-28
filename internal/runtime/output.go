package runtime

import (
	"context"

	"github.com/context-labs/whip/internal/session"
)

// TurnOutput reads the validated final output of a completed turn.
func (r *Runtime) TurnOutput(ctx context.Context, id session.TurnID) (*session.StructuredOutput, error) {
	return r.store.TurnOutput(ctx, id)
}
