package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/context-labs/whip/internal/session"
)

// InstructionReadGrant admits a turn's workspace instruction reads against one
// transactional authority snapshot. It never consumes one-use approval. Reads
// happen after this transaction; later revocation cannot erase captured bytes.
func (s *Store) InstructionReadGrant(ctx context.Context, id session.TurnID) (*session.Grant, error) {
	var result *session.Grant
	err := s.write(ctx, func(tx *sql.Tx) error {
		owner, err := instructionTurn(ctx, tx, id)
		if err != nil {
			return err
		}
		grant, err := matchingGrant(ctx, tx, owner.ID, "files.read", owner.WorkingDirectory)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		result = &grant
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func instructionTurn(ctx context.Context, q querier, id session.TurnID) (session.Session, error) {
	turn, err := readTurn(ctx, q, id)
	if err != nil {
		return session.Session{}, err
	}
	if turn.Kind != session.PromptInput {
		return session.Session{}, ErrConflict
	}
	if turn.State != session.Running {
		return session.Session{}, ErrStopped
	}
	owner, err := readSession(ctx, q, turn.SessionID)
	if err != nil {
		return session.Session{}, err
	}
	if owner.Lifecycle != session.Active {
		return session.Session{}, ErrStopped
	}
	if err := requireTurnPermit(ctx, q, id); err != nil {
		return session.Session{}, err
	}
	return owner, nil
}

// SaveInstructionManifest records audit metadata before provider dispatch. An
// exact retry succeeds after cancellation or settlement; it never replaces the
// original snapshot or rechecks authority for filesystem reads already made.
func (s *Store) SaveInstructionManifest(ctx context.Context, id session.TurnID, manifest session.InstructionManifest) error {
	if err := manifest.Validate(); err != nil {
		return err
	}
	raw, err := encode(manifest)
	if err != nil {
		return err
	}
	return s.write(ctx, func(tx *sql.Tx) error {
		var existing string
		err := tx.QueryRowContext(ctx, "SELECT manifest FROM turn_instruction_manifests WHERE turn_id=?", id).Scan(&existing)
		if err == nil {
			if existing != raw {
				return ErrConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := instructionTurn(ctx, tx, id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO turn_instruction_manifests (turn_id,manifest) VALUES (?,?)", id, raw)
		return err
	})
}

// InstructionManifest returns nil for an existing turn that captured no
// instructions. Missing turns return ErrNotFound, including after deletion.
func (s *Store) InstructionManifest(ctx context.Context, id session.TurnID) (*session.InstructionManifest, error) {
	var raw sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT m.manifest FROM turns t
 LEFT JOIN turn_instruction_manifests m ON m.turn_id=t.id WHERE t.id=?`, id).Scan(&raw)
	if err != nil {
		return nil, found(err)
	}
	if !raw.Valid {
		return nil, nil //nolint:nilnil // An existing turn may have no captured manifest.
	}
	var result session.InstructionManifest
	if err := json.Unmarshal([]byte(raw.String), &result); err != nil {
		return nil, err
	}
	return &result, nil
}
