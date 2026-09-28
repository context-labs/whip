package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/context-labs/whip/internal/session"
)

// TurnInput returns the exact canonical input claimed by this turn. Mail-only
// turns return nil; missing turns return ErrNotFound. Inspection never expands
// skill references or rewrites the accepted input or transcript.
func (s *Store) TurnInput(ctx context.Context, id session.TurnID) (*session.Input, error) {
	var inputID *session.InputID
	var result session.Input
	var raw string
	var created int64
	err := s.db.QueryRowContext(ctx, `SELECT i.id,COALESCE(i.session_id,''),COALESCE(i.source,''),
 COALESCE(i.kind,''),COALESCE(i.parts,'null'),i.turn_id,COALESCE(i.created_at,0)
 FROM turns t LEFT JOIN inputs i ON i.turn_id=t.id WHERE t.id=?`, id).
		Scan(&inputID, &result.SessionID, &result.Source, &result.Kind, &raw, &result.TurnID, &created)
	if err != nil {
		return nil, found(err)
	}
	if inputID == nil {
		return nil, nil //nolint:nilnil // Mail-only turns have no canonical input.
	}
	result.ID, result.State, result.CreatedAt = *inputID, session.Claimed, timestamp(created)
	if err := json.Unmarshal([]byte(raw), &result.Parts); err != nil {
		return nil, err
	}
	return &result, nil
}

// SessionInstructions inspects current copied policy and standing workspace
// authority in one read-only snapshot. Stopped sessions and stopped issuers keep
// valid grants until revoked; inspection neither starts work nor requires a permit.
func (s *Store) SessionInstructions(ctx context.Context, id session.SessionID) (_ session.Instructions, _ *session.Grant, err error) {
	// ReadOnly selects a deferred BEGIN with the pinned SQLite driver, avoiding
	// the writer reservation used by ordinary store execution transactions.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return session.Instructions{}, nil, err
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("rollback instruction inspection: %w", rollbackErr))
		}
	}()
	owner, err := readSession(ctx, tx, id)
	if err != nil {
		return session.Instructions{}, nil, err
	}
	var result *session.Grant
	grant, err := matchingGrant(ctx, tx, owner.ID, "files.read", owner.WorkingDirectory)
	if err == nil {
		result = &grant
	} else if !errors.Is(err, ErrNotFound) {
		return session.Instructions{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return session.Instructions{}, nil, err
	}
	return owner.Config.Instructions, result, nil
}

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
