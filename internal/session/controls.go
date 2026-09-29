package session

import (
	"fmt"
	"unicode/utf8"
)

// RunConfiguration is a human root control, never a definition or agent override.
// Nil preserves ordinary interactive limits; an explicit zero MaxTurns is uncapped.
type RunConfiguration struct {
	System   string `json:"system"`
	MaxTurns int    `json:"max_turns"`
	Headless bool   `json:"headless"`
	CacheKey string `json:"cache_key"`
}

func (c RunConfiguration) Validate() error {
	if !utf8.ValidString(c.System) || !utf8.ValidString(c.CacheKey) {
		return ErrInvalid
	}
	if c.MaxTurns < 0 || c.MaxTurns > 1_000_000 {
		return fmt.Errorf("%w: max_turns must be between 0 and 1000000", ErrInvalid)
	}
	if c.System != "" {
		if err := ValidateText(c.System, MaxInstructionBytes/2); err != nil {
			return err
		}
	}
	if c.CacheKey != "" {
		return ValidateText(c.CacheKey, 4096)
	}
	return nil
}

type WorkspaceSetRequest struct {
	ID               string    `json:"id"`
	SessionID        SessionID `json:"session_id"`
	ExpectedRevision Revision  `json:"expected_revision"`
	Path             string    `json:"path"`
}

func (r WorkspaceSetRequest) Validate() error {
	if !utf8.ValidString(r.Path) {
		return ErrInvalid
	}
	if err := validateControl(r.ID, r.SessionID, r.ExpectedRevision); err != nil {
		return err
	}
	if r.Path != "" {
		return ValidateText(r.Path, 4096)
	}
	return nil
}

type RunConfigureRequest struct {
	ID               string           `json:"id"`
	SessionID        SessionID        `json:"session_id"`
	ExpectedRevision Revision         `json:"expected_revision"`
	Configuration    RunConfiguration `json:"configuration"`
}

func (r RunConfigureRequest) Validate() error {
	if err := validateControl(r.ID, r.SessionID, r.ExpectedRevision); err != nil {
		return err
	}
	return r.Configuration.Validate()
}

func validateControl(id string, owner SessionID, expected Revision) error {
	if err := ValidateID(id); err != nil {
		return err
	}
	if err := ValidateID(string(owner)); err != nil {
		return err
	}
	if expected <= 0 {
		return fmt.Errorf("%w: configuration revision must be positive", ErrInvalid)
	}
	return nil
}

// ControlEdit returns the captured result even after later edits. A deleted owner
// leaves a receipt without recreating the session or its configuration.
type ControlEdit struct {
	ID        string
	SessionID SessionID
	Revision  Revision
	Deleted   bool
	Session   *Session
}
