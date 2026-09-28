// Package inferenceauth owns the execution host's private Inference.net account
// record. It performs local persistence only; it never contacts a provider.
package inferenceauth

import (
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	fileName       = "inference-net.json"
	maxRecordBytes = 64 << 10
	maxTokenBytes  = 16 << 10
)

var (
	ErrInvalid        = errors.New("invalid Inference.net credential record")
	ErrStorage        = errors.New("inference.net credential storage is unavailable or unsafe")
	ErrStoragePending = errors.New("inference.net credential persistence is unresolved; retry local persistence")
	ErrChanged        = errors.New("inference.net credentials changed; capture current authorization")
	ErrKeyRequired    = errors.New("an Inference.net machine key is required")
	ErrClosed         = errors.New("inference.net credential manager is closed")
)

// Management is private control-plane authorization, independent of inference.
// Nil ExpiresAt means unknown; an expired session does not expire a machine key.
type Management struct {
	Token     string     `json:"token"`
	UserID    string     `json:"user_id"`
	Email     string     `json:"email"`
	ExpiresAt *time.Time `json:"expires_at"`
}

type Scope struct {
	TeamID      string `json:"team_id"`
	TeamName    string `json:"team_name"`
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name"`
}

type MachineKey struct {
	ID    string `json:"id"`
	Value string `json:"value"`
	Name  string `json:"name"`
}

// Credentials is private host state, never a public status or request snapshot.
// Management-only and machine-key-only records are both supported.
type Credentials struct {
	Management Management `json:"management"`
	Scope      Scope      `json:"scope"`
	MachineKey MachineKey `json:"machine_key"`
}

func (c Credentials) clone() Credentials {
	if c.Management.ExpiresAt != nil {
		c.Management.ExpiresAt = new(*c.Management.ExpiresAt)
	}
	return c
}

func (c Credentials) validate() error {
	management := c.Management.Token != ""
	key := c.MachineKey.Value != ""
	if !management && !key {
		return ErrInvalid
	}
	if management {
		if !token(c.Management.Token) || !text(c.Management.UserID, 256, false) || !text(c.Management.Email, 1024, true) {
			return ErrInvalid
		}
		if expiry := c.Management.ExpiresAt; expiry != nil && (expiry.IsZero() || expiry.Year() < 1 || expiry.Year() > 9999) {
			return ErrInvalid
		}
	} else if c.Management.UserID != "" || c.Management.Email != "" || c.Management.ExpiresAt != nil {
		return ErrInvalid
	}
	if c.Scope != (Scope{}) {
		if !text(c.Scope.TeamID, 256, false) || !text(c.Scope.ProjectID, 256, false) || !text(c.Scope.TeamName, 512, true) || !text(c.Scope.ProjectName, 512, true) {
			return ErrInvalid
		}
	}
	if key {
		if !token(c.MachineKey.Value) || !text(c.MachineKey.ID, 256, false) || !text(c.MachineKey.Name, 512, true) || c.Scope == (Scope{}) {
			return ErrInvalid
		}
	} else if c.MachineKey != (MachineKey{}) {
		return ErrInvalid
	}
	return nil
}

func token(value string) bool {
	if value == "" || len(value) > maxTokenBytes {
		return false
	}
	for _, b := range []byte(value) {
		if b < 0x21 || b > 0x7e {
			return false
		}
	}
	return true
}

func text(value string, limit int, empty bool) bool {
	return (empty || value != "") && len(value) <= limit && utf8.ValidString(value) && strings.IndexFunc(value, unicode.IsControl) == -1
}
