package session

import (
	"fmt"
	"time"
)

type ForkID string

const (
	MaxForkMessages          = 10_000
	MaxForkGroups            = 1_000
	MaxForkHistoryBytes      = 64 << 20
	MaxForkContinuationBytes = 4 << 20
	MaxForkCompactions       = 128
)

// ForkRequest selects an exact, whole-group prefix. The working directory is
// reused; forking does not create a Git worktree or copy filesystem state.
type ForkRequest struct {
	ID                      ForkID    `json:"id"`
	SessionID               SessionID `json:"session_id"`
	ExpectedHistoryRevision Revision  `json:"expected_history_revision,string"`
	ExpectedConfigRevision  Revision  `json:"expected_config_revision,string"`
	ObservedThrough         int64     `json:"observed_through,string"`
	KeepThrough             int64     `json:"keep_through,string"`
	Title                   *string   `json:"title"`
}

func (r ForkRequest) Validate() error {
	for _, id := range []string{string(r.ID), string(r.SessionID)} {
		if err := ValidateID(id); err != nil {
			return err
		}
	}
	if r.ExpectedHistoryRevision < 1 || r.ExpectedConfigRevision < 1 || r.KeepThrough < 0 || r.ObservedThrough < r.KeepThrough {
		return fmt.Errorf("%w: invalid fork revisions or boundary", ErrInvalid)
	}
	if r.Title != nil {
		return ValidateText(*r.Title, 1024)
	}
	return nil
}

// ForkDefaults are current host limits, applied once to a fresh root. They are
// excluded from request identity so a retry survives host configuration changes.
// Both logical-write budgets must be supplied with finite limits.
type ForkDefaults struct {
	Resources []ResourceLimit
	Budgets   []BudgetLimit
}

// Fork is an immutable admission receipt. IDs remain after either tree is
// deleted, preventing an exact retry from recreating its destination.
type Fork struct {
	ForkRequest
	TreeID    TreeID    `json:"tree_id"`
	RootID    SessionID `json:"root_id"`
	CreatedAt time.Time `json:"created_at"`
}

type ForkResult struct {
	Fork    Fork
	Tree    *Tree
	Root    *Session
	Deleted bool
}
