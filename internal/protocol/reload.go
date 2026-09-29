package protocol

import (
	"time"

	"github.com/context-labs/whip/internal/session"
)

type ReloadSessionParams struct {
	EditID           ID      `json:"edit_id"`
	SessionID        ID      `json:"session_id"`
	ExpectedRevision Counter `json:"expected_revision" pattern:"^[1-9][0-9]{0,18}$"`
}

type ReloadEditParams struct {
	SessionID ID `json:"session_id"`
	EditID    ID `json:"edit_id"`
}

type ReloadEdit struct {
	ID               ID            `json:"id"`
	SessionID        ID            `json:"session_id"`
	TreeID           ID            `json:"tree_id"`
	ExpectedRevision Counter       `json:"expected_revision" pattern:"^[1-9][0-9]{0,18}$"`
	HostRevision     string        `json:"host_revision" pattern:"^[a-f0-9]{64}$"`
	Configuration    Configuration `json:"configuration"`
	State            string        `json:"state" enum:"pending,applied,conflicted,interrupted,unavailable"`
	Revision         *Counter      `json:"revision"`
	CreatedAt        string        `json:"created_at"`
	SettledAt        *string       `json:"settled_at"`
}

func ReloadEditFromDomain(value session.ReloadEdit) (ReloadEdit, error) {
	result := ReloadEdit{ID: ID(value.ID), SessionID: ID(value.SessionID), TreeID: ID(value.TreeID), ExpectedRevision: Counter(value.ExpectedRevision), HostRevision: value.HostRevision, State: string(value.State), CreatedAt: value.CreatedAt.Format(time.RFC3339Nano)}
	if value.Revision != nil {
		result.Revision = new(Counter(*value.Revision))
	}
	if value.SettledAt != nil {
		result.SettledAt = new(value.SettledAt.Format(time.RFC3339Nano))
	}
	configuration, err := ConfigurationFromDomain(value.Configuration)
	result.Configuration = configuration
	return result, err
}
