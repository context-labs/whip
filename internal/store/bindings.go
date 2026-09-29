package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/context-labs/whip/internal/session"
)

func validateBindingSources(ctx context.Context, q querier, config session.Configuration) error {
	for _, family := range []struct {
		ref   *session.DefinitionRef
		hooks bool
	}{{config.ToolsDefinition, false}, {config.HooksDefinition, true}} {
		if family.ref == nil {
			// Host/override declarations may advertise syntax without an executor owner.
			// Only resolution from a registered definition can attach that authority.
			continue
		}
		declared, err := definition(ctx, q, *family.ref)
		if err != nil {
			return err
		}
		if family.hooks {
			for name, contract := range config.Hooks {
				original, ok := declared.Document.Defaults.Hooks[name]
				if !ok || !session.SameContract(original, contract) {
					return fmt.Errorf("%w: hook differs from its definition source", session.ErrInvalid)
				}
			}
		} else {
			for name, contract := range config.Tools {
				original, ok := declared.Document.Defaults.Tools[name]
				if !ok || !session.SameContract(original, contract) {
					return fmt.Errorf("%w: tool differs from its definition source", session.ErrInvalid)
				}
			}
		}
	}
	return nil
}

func readConfiguration(ctx context.Context, q querier, id session.SessionID, revision session.Revision) (session.Configuration, error) {
	var config session.Configuration
	var raw string
	if err := q.QueryRowContext(ctx, "SELECT configuration FROM session_configurations WHERE session_id=? AND revision=?", id, revision).Scan(&raw); err != nil {
		return config, found(err)
	}
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return config, err
	}
	return config, nil
}

// CellSession returns only the session/configuration captured by the owning
// active cell. Mutable session configuration cannot change a running host call.
func (s *Store) CellSession(ctx context.Context, id session.SessionID, cell session.CellID) (result session.Session, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		if err := operationLive(ctx, tx, cell); err != nil {
			return err
		}
		var owner session.SessionID
		var revision session.Revision
		if err := tx.QueryRowContext(ctx, "SELECT t.session_id,t.config_revision FROM cells c JOIN turns t ON t.id=c.turn_id WHERE c.id=?", cell).Scan(&owner, &revision); err != nil {
			return found(err)
		}
		if owner != id {
			return ErrConflict
		}
		current, err := readSession(ctx, tx, owner)
		if err != nil {
			return err
		}
		current, err = capturedSession(ctx, tx, current, revision)
		if err != nil {
			return err
		}
		current.ConfigRevision = revision
		result = current
		return nil
	})
	return
}

// ConfigurationSession projects an immutable configuration and working directory
// using the existing session identity. It does not claim execution authority.
func (s *Store) ConfigurationSession(ctx context.Context, id session.SessionID, revision session.Revision) (session.Session, error) {
	current, err := readSession(ctx, s.db, id)
	if err != nil {
		return current, err
	}
	return capturedSession(ctx, s.db, current, revision)
}

func capturedSession(ctx context.Context, q querier, current session.Session, revision session.Revision) (session.Session, error) {
	var raw string
	if err := q.QueryRowContext(ctx, "SELECT configuration,working_directory FROM session_configurations WHERE session_id=? AND revision=?", current.ID, revision).Scan(&raw, &current.WorkingDirectory); err != nil {
		return current, found(err)
	}
	current.ConfigRevision = revision
	current.Config = session.Configuration{}
	return current, json.Unmarshal([]byte(raw), &current.Config)
}
