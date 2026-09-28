package runtime

import (
	"context"

	"github.com/context-labs/whip/internal/session"
)

// AutomaticTitleDecision reads immutable initialization evidence without
// admitting or restarting naming. The selected title remains tree metadata.
func (r *Runtime) AutomaticTitleDecision(ctx context.Context, tree session.TreeID) (session.AutomaticTitleDecision, error) {
	return r.store.AutomaticTitleDecision(ctx, tree)
}

func (r *Runtime) AutomaticTitleResult(ctx context.Context, tree session.TreeID, attempt session.ModelAttemptID) (session.AutomaticTitleResult, error) {
	return r.store.AutomaticTitleResult(ctx, tree, attempt)
}
