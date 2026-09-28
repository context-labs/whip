// Package openaiauth owns the execution host's ChatGPT subscription credentials.
// Credentials never belong in configuration, protocol replies, or model history.
package openaiauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	Provider = "openai-codex"
	BaseURL  = "https://chatgpt.com/backend-api/codex"
	maxBytes = 256 << 10
)

var (
	ErrSignInRequired = errors.New("sign in to OpenAI (ChatGPT subscription) on the execution host")
	ErrLoginChanged   = errors.New("OpenAI login changed; retry using the current account")
	ErrPersistence    = errors.New("OpenAI credential storage needs attention")
)

// Credentials is private host state. Never serialize it into a public response.
type Credentials struct {
	AccessToken      string    `json:"accessToken"`
	RefreshToken     string    `json:"refreshToken"`
	ExpiresAt        time.Time `json:"expiresAt"`
	AccountID        string    `json:"accountId"`
	Email            string    `json:"email,omitempty"`
	Plan             string    `json:"plan,omitempty"`
	ComputeResidency string    `json:"computeResidency,omitempty"`
}

func (c Credentials) validate() error {
	if !headerValue(c.AccessToken) || !headerValue(c.RefreshToken) || !headerValue(c.AccountID) || c.ExpiresAt.IsZero() {
		return errors.New("invalid OpenAI credentials; sign in again")
	}
	if c.ComputeResidency != "" && !headerValue(c.ComputeResidency) {
		return errors.New("invalid OpenAI compute residency")
	}
	return nil
}

func headerValue(value string) bool {
	return value != "" && len(value) <= 32<<10 && !strings.ContainsAny(value, "\r\n\x00")
}

func readCredentials(path string) (Credentials, error) {
	// #nosec G304 -- Manager constructs this private host path with the fixed name openai-codex.json.
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return Credentials{}, nil
	}
	if err != nil {
		return Credentials{}, errors.New("could not read OpenAI credentials on the execution host")
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil || len(data) > maxBytes {
		return Credentials{}, errors.New("could not read bounded OpenAI credentials on the execution host")
	}
	var credentials Credentials
	if json.Unmarshal(data, &credentials) != nil {
		return Credentials{}, errors.New("malformed OpenAI credential file; repair or remove it before signing in")
	}
	if err := credentials.validate(); err != nil {
		return Credentials{}, err
	}
	return credentials, nil
}

// The publication result distinguishes a failed write from credentials that are
// already visible but whose directory entry has not been durably confirmed.
func saveCredentials(path string, credentials Credentials, syncDirectory func(*os.File) error) (published bool, err error) {
	if err := credentials.validate(); err != nil {
		return false, err
	}
	// #nosec G117 -- Credentials are intentionally persisted only to the 0600 file created below.
	data, err := json.Marshal(credentials)
	if err != nil || len(data) > maxBytes {
		return false, errors.New("could not encode OpenAI credentials")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, fmt.Errorf("%w: could not create credential directory", ErrPersistence)
	}
	// #nosec G304 -- The manager owns this fixed credential directory.
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return false, fmt.Errorf("%w: could not open credential directory", ErrPersistence)
	}
	defer func() { _ = directory.Close() }()
	f, err := os.CreateTemp(filepath.Dir(path), ".openai-codex-*")
	if err != nil {
		return false, fmt.Errorf("%w: could not create temporary credential file", ErrPersistence)
	}
	defer func() { _ = os.Remove(f.Name()) }()
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if errors.Join(writeErr, syncErr, closeErr) != nil {
		return false, fmt.Errorf("%w: could not save credential file", ErrPersistence)
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return false, fmt.Errorf("%w: could not replace credential file", ErrPersistence)
	}
	if err := syncDirectory(directory); err != nil {
		return true, fmt.Errorf("%w: credentials published but durability is unconfirmed", ErrPersistence)
	}
	return true, nil
}

func removeCredentials(path string, remove func(string) error, syncDirectory func(*os.File) error) error {
	// #nosec G304 -- The manager owns this fixed credential directory.
	directory, err := os.Open(filepath.Dir(path))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: could not open credential directory for logout", ErrPersistence)
	}
	defer func() { _ = directory.Close() }()
	if err := remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: could not remove credentials; retry logout", ErrPersistence)
	}
	// A previous unlink may have succeeded before directory sync failed. An
	// already absent file still needs this sync before logout can report success.
	if err := syncDirectory(directory); err != nil {
		return fmt.Errorf("%w: logout durability is unconfirmed; retry logout", ErrPersistence)
	}
	return nil
}
