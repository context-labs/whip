package protocol

import (
	"time"

	"github.com/context-labs/whip/internal/session"
)

type PermissionPolicy struct {
	TreeID    ID      `json:"tree_id"`
	Mode      string  `json:"mode" enum:"prompt,automatic"`
	Revision  Counter `json:"revision" pattern:"^[1-9][0-9]{0,18}$"`
	UpdatedAt string  `json:"updated_at"`
}

type SetPermissionModeParams struct {
	EditID           ID      `json:"edit_id"`
	SessionID        ID      `json:"session_id"`
	ExpectedRevision Counter `json:"expected_revision" pattern:"^[1-9][0-9]{0,18}$"`
	Mode             string  `json:"mode" enum:"prompt,automatic"`
}

type PermissionModeEditParams struct {
	SessionID ID `json:"session_id"`
	EditID    ID `json:"edit_id"`
}

type PermissionModeEdit struct {
	ID               ID               `json:"id"`
	SessionID        ID               `json:"session_id"`
	ExpectedRevision Counter          `json:"expected_revision" pattern:"^[1-9][0-9]{0,18}$"`
	Mode             string           `json:"mode" enum:"prompt,automatic"`
	PreviousMode     string           `json:"previous_mode" enum:"prompt,automatic"`
	Policy           PermissionPolicy `json:"policy"`
	CreatedAt        string           `json:"created_at"`
}

// DefaultPermissionMode exposes only the effective future-root default and the
// current host-file revision. It contains no provider or credential declaration.
type DefaultPermissionMode struct {
	Mode     string `json:"mode" enum:"prompt,automatic"`
	Revision string `json:"revision" pattern:"^[a-f0-9]{64}$"`
}

type SetDefaultPermissionModeParams struct {
	ExpectedRevision string `json:"expected_revision" pattern:"^[a-f0-9]{64}$"`
	Mode             string `json:"mode" enum:"prompt,automatic"`
}

func PermissionPolicyFromDomain(value session.PermissionPolicy) PermissionPolicy {
	return PermissionPolicy{TreeID: ID(value.TreeID), Mode: string(value.Mode), Revision: Counter(value.Revision), UpdatedAt: value.UpdatedAt.Format(time.RFC3339Nano)}
}

func PermissionModeEditFromDomain(value session.PermissionModeEdit) PermissionModeEdit {
	return PermissionModeEdit{ID: ID(value.ID), SessionID: ID(value.SessionID), ExpectedRevision: Counter(value.ExpectedRevision), Mode: string(value.Mode), PreviousMode: string(value.PreviousMode), Policy: PermissionPolicyFromDomain(value.Policy), CreatedAt: value.CreatedAt.Format(time.RFC3339Nano)}
}

func (p CreateTreeParams) DomainPermissionMode() *session.PermissionMode {
	if p.PermissionMode == nil {
		return nil
	}
	return new(session.PermissionMode(*p.PermissionMode))
}
