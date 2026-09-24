package inferencenet

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/context-labs/whip/internal/config"
)

// Auth is whip's Inference.net sign-in state, persisted to
// ~/.whipcode/inference-net.json (0600). The session token drives the control
// plane (device flow, key minting); the machine key is what the provider
// entry resolves to for inference calls.
type Auth struct {
	SessionToken   string `json:"sessionToken,omitempty"`
	UserEmail      string `json:"userEmail,omitempty"`
	TeamID         string `json:"teamId,omitempty"`
	TeamName       string `json:"teamName,omitempty"`
	ProjectID      string `json:"projectId,omitempty"`
	ProjectName    string `json:"projectName,omitempty"`
	MachineKeyID   string `json:"machineKeyId,omitempty"`
	MachineKey     string `json:"machineKey,omitempty"`
	MachineKeyName string `json:"machineKeyName,omitempty"`
}

// SignedIn reports whether a device-flow session token is stored.
func (a Auth) SignedIn() bool { return a.SessionToken != "" }

// HasMachineKey reports whether a provisioned machine API key is stored.
func (a Auth) HasMachineKey() bool { return a.MachineKey != "" }

func authPath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "inference-net.json"), nil
}

// LoadAuth reads the stored auth state; a missing file yields the zero value.
func LoadAuth() (Auth, error) {
	p, err := authPath()
	if err != nil {
		return Auth{}, err
	}
	data, err := os.ReadFile(p) //nolint:gosec // G304: p is whip's own auth-state path
	if errors.Is(err, os.ErrNotExist) {
		return Auth{}, nil
	}
	if err != nil {
		return Auth{}, err
	}
	var a Auth
	if err := json.Unmarshal(data, &a); err != nil {
		return Auth{}, err
	}
	return a, nil
}

// SaveAuth writes the auth state atomically with owner-only perms.
func SaveAuth(a Auth) error {
	p, err := authPath()
	if err != nil {
		return err
	}
	//nolint:gosec // G117: persisting the session token + machine key is the whole point
	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ClearAuth removes the stored auth state (logout).
func ClearAuth() error {
	p, err := authPath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
