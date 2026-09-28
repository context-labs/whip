package session

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

type (
	WorkspaceSnapshotID  string
	WorkspaceActionID    string
	WorkspaceActionKind  string
	WorkspaceActionState string
)

const (
	WorkspaceCapture             WorkspaceActionKind  = "capture"
	WorkspaceRestore             WorkspaceActionKind  = "restore"
	WorkspaceRelease             WorkspaceActionKind  = "release"
	WorkspaceClaimed             WorkspaceActionState = "claimed"
	WorkspaceSucceeded           WorkspaceActionState = "succeeded"
	WorkspaceUncertain           WorkspaceActionState = "uncertain"
	MaxWorkspaceSnapshots                             = 128
	MaxRuntimeWorkspaceSnapshots                      = 1024
	WorkspaceSemantics                                = "Tracked files under the session working directory are overlaid. Untracked and later files may remain; staging is not preserved. Other writers are not frozen."
)

type WorkspaceRequest struct {
	ID         WorkspaceActionID   `json:"id"`
	SnapshotID WorkspaceSnapshotID `json:"snapshot_id"`
	SessionID  SessionID           `json:"session_id"`
}

func (r WorkspaceRequest) Validate() error {
	for _, id := range []string{string(r.ID), string(r.SnapshotID), string(r.SessionID)} {
		if err := ValidateID(id); err != nil {
			return err
		}
	}
	return nil
}

// WorkspaceBinding is host-private filesystem identity. It is never authority
// supplied by a client; the Git adapter derives it from the session directory.
type WorkspaceBinding struct {
	Worktree         string
	GitDirectory     string
	CommonDirectory  string
	Scope            string
	WorktreeIdentity string
	GitIdentity      string
	CommonIdentity   string
	ScopeIdentity    string
}

func (b WorkspaceBinding) Validate() error {
	for _, path := range []string{b.Worktree, b.GitDirectory, b.CommonDirectory} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || ValidateText(path, 4096) != nil {
			return fmt.Errorf("%w: invalid workspace binding", ErrInvalid)
		}
	}
	if !filepath.IsLocal(b.Scope) || filepath.Clean(b.Scope) != b.Scope || ValidateText(b.Scope, 4096) != nil {
		return fmt.Errorf("%w: invalid workspace scope", ErrInvalid)
	}
	for _, identity := range []string{b.WorktreeIdentity, b.GitIdentity, b.CommonIdentity, b.ScopeIdentity} {
		if ValidateText(identity, 128) != nil {
			return fmt.Errorf("%w: missing filesystem identity", ErrInvalid)
		}
	}
	return nil
}

func ValidateWorkspaceObject(id string) error {
	if len(id) != 40 && len(id) != 64 {
		return fmt.Errorf("%w: invalid workspace object", ErrInvalid)
	}
	if strings.IndexFunc(id, func(c rune) bool { return c < '0' || c > '9' && c < 'a' || c > 'f' }) >= 0 {
		return fmt.Errorf("%w: invalid workspace object", ErrInvalid)
	}
	return nil
}

type WorkspaceSnapshot struct {
	ID         WorkspaceSnapshotID  `json:"id"`
	SessionID  SessionID            `json:"session_id"`
	CaptureID  WorkspaceActionID    `json:"capture_id"`
	State      WorkspaceActionState `json:"state"`
	CreatedAt  time.Time            `json:"created_at"`
	ReleasedAt *time.Time           `json:"released_at"`
	Semantics  string               `json:"semantics"`
	Binding    WorkspaceBinding     `json:"-"`
	ObjectID   *string              `json:"-"`
}

type WorkspaceAction struct {
	WorkspaceRequest
	Kind       WorkspaceActionKind  `json:"kind"`
	State      WorkspaceActionState `json:"state"`
	Failure    *string              `json:"failure"`
	CreatedAt  time.Time            `json:"created_at"`
	FinishedAt *time.Time           `json:"finished_at"`
}

type WorkspaceResult struct {
	Action   WorkspaceAction   `json:"action"`
	Snapshot WorkspaceSnapshot `json:"snapshot"`
}
