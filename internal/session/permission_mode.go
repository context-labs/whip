package session

import (
	"fmt"
	"time"
)

// PermissionMode controls root approval prompts, never intrinsic validation or
// a child's exact delegated grants. Children display their tree's saved policy.
type PermissionMode string

type PermissionModeEditID string

const (
	PermissionPrompt    PermissionMode = "prompt"
	PermissionAutomatic PermissionMode = "automatic"
)

func (m PermissionMode) Validate() error {
	if m != PermissionPrompt && m != PermissionAutomatic {
		return fmt.Errorf("%w: permission mode must be prompt or automatic", ErrInvalid)
	}
	return nil
}

// ResolvePermissionMode preserves omission as the safe Ask default. Explicit
// edits use Validate and cannot reset policy through an empty value.
func ResolvePermissionMode(m PermissionMode) (PermissionMode, error) {
	if m == "" {
		return PermissionPrompt, nil
	}
	return m, m.Validate()
}

type PermissionPolicy struct {
	TreeID    TreeID
	Mode      PermissionMode
	Revision  Revision
	UpdatedAt time.Time
}

type PermissionModeRequest struct {
	ID               PermissionModeEditID `json:"id"`
	SessionID        SessionID            `json:"session_id"`
	ExpectedRevision Revision             `json:"expected_revision,string"`
	Mode             PermissionMode       `json:"mode"`
}

func (r PermissionModeRequest) Validate() error {
	for _, id := range []string{string(r.ID), string(r.SessionID)} {
		if err := ValidateID(id); err != nil {
			return err
		}
	}
	if r.ExpectedRevision < 1 {
		return fmt.Errorf("%w: permission revision must be positive", ErrInvalid)
	}
	return r.Mode.Validate()
}

// PermissionModeEdit is immutable evidence, retained after deletion. Policy is
// the original result, not a projection of later edits to the same tree.
type PermissionModeEdit struct {
	PermissionModeRequest
	Policy       PermissionPolicy
	PreviousMode PermissionMode
	CreatedAt    time.Time
}
