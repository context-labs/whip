package runtime

import (
	"context"
	"slices"
	"strings"

	"github.com/context-labs/whip/internal/session"
)

type (
	HookDecision     struct{ InvocationID, Hook, Operation, Decision, Reason string }
	ExecutorProgress struct {
		InvocationID string
		OperationID  session.OperationID
		Text         string
	}
)

// ExecutorActivity is bounded process-local observation. It disappears with the
// owning turn; durable history retains executed arguments, results and failures,
// not a fabricated hook audit or a recoverable executor invocation queue.
type ExecutorActivity struct {
	Epoch     string
	TurnID    session.TurnID
	Revision  int64
	Decisions []HookDecision
	Truncated bool
	Progress  *ExecutorProgress
}

func (r *Runtime) ExecutorActivity(ctx context.Context, owner session.SessionID) (*ExecutorActivity, error) {
	if _, err := r.store.Session(ctx, owner); err != nil {
		return nil, err
	}
	r.mu.Lock()
	live := r.active[owner]
	if live == nil {
		r.mu.Unlock()
		return nil, nil //nolint:nilnil // No live activity is a successful empty observation.
	}
	result := live.executorActivity
	result.Epoch, result.TurnID = r.epoch, live.turn
	result.Decisions = slices.Clone(result.Decisions)
	if result.Decisions == nil {
		result.Decisions = []HookDecision{}
	}
	if result.Progress != nil {
		result.Progress = new(*result.Progress)
	}
	r.mu.Unlock()
	turn, err := r.store.Turn(ctx, result.TurnID)
	if err != nil {
		return nil, err
	}
	if turn.State.Terminal() {
		return nil, nil //nolint:nilnil // No live activity is a successful empty observation.
	}
	return &result, nil
}

func (r *Runtime) hookDecision(owner session.SessionID, turn session.TurnID, decision HookDecision) {
	r.mu.Lock()
	defer r.mu.Unlock()
	live := r.active[owner]
	if live == nil || live.turn != turn || r.closed {
		return
	}
	activity := &live.executorActivity
	activity.Revision++
	if len(activity.Decisions) == 8 {
		activity.Truncated = true
		return
	}
	decision.Reason = boundedHookText(decision.Reason, 512)
	activity.Decisions = append(activity.Decisions, decision)
}

func (r *Runtime) hookNotice(owner session.SessionID, turn session.TurnID, text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	live := r.active[owner]
	if live == nil || live.turn != turn || r.closed || len(live.hookNotices) == 8 {
		return
	}
	used := 0
	for _, text := range live.hookNotices {
		used += len(text) + 1
	}
	if used >= 2048 {
		return
	}
	live.hookNotices = append(live.hookNotices, boundedHookText(text, 2048-used))
}

// TurnNotices is read by the runner before each model round. It cannot attach a
// late callback to a subsequent turn or become a second transcript authority.
func (r *Runtime) TurnNotices(turn session.Turn) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	live := r.active[turn.SessionID]
	if live == nil || live.turn != turn.ID {
		return ""
	}
	return strings.Join(live.hookNotices, "\n")
}

func (r *Runtime) executorProgress(owner session.SessionID, turn session.TurnID, progress *ExecutorProgress) {
	r.mu.Lock()
	defer r.mu.Unlock()
	live := r.active[owner]
	if live == nil || live.turn != turn || r.closed {
		return
	}
	live.executorActivity.Revision++
	live.executorActivity.Progress = progress
}
