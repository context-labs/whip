package runtime

import (
	"context"

	"github.com/context-labs/whip/internal/session"
)

// Rewind returns the immutable accepted edit, not a current session snapshot.
// Every new edit, including one keeping all messages, resets the REPL. External
// files and other session subsystems remain independent of conversation history.
func (r *Runtime) Rewind(ctx context.Context, request session.RewindRequest) (session.HistoryEdit, error) {
	if err := r.Err(); err != nil {
		return session.HistoryEdit{}, err
	}
	edit, err := r.store.Rewind(ctx, request)
	if err != nil {
		return edit, err
	}
	r.applyHistoryEdit(edit)
	return edit, nil
}

func (r *Runtime) applyHistoryEdit(edit session.HistoryEdit) {
	r.mu.Lock()
	entry := r.kernels[edit.SessionID]
	if entry != nil && entry.historyRevision < edit.Revision {
		delete(r.kernels, edit.SessionID)
	} else {
		entry = nil
	}
	r.mu.Unlock()
	// A resumed turn may already have installed a new-revision kernel. An old
	// acknowledgement may dispose only the cache made stale by that exact edit.
	if entry != nil {
		entry.kernel.Close()
	}
}
