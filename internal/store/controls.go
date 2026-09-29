package store

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"path/filepath"
	"slices"

	"github.com/context-labs/whip/internal/session"
)

func controlRetry(ctx context.Context, q querier, id, digest string) (result session.ControlEdit, err error) {
	var previous string
	result.ID = id
	if err := q.QueryRowContext(ctx, "SELECT session_id,revision,digest FROM session_control_edits WHERE id=?", id).Scan(&result.SessionID, &result.Revision, &previous); err != nil {
		return result, found(err)
	}
	if previous != digest {
		return result, ErrConflict
	}
	current, err := readSession(ctx, q, result.SessionID)
	if errors.Is(err, ErrNotFound) {
		result.Deleted = true
		return result, nil
	}
	if err != nil {
		return result, err
	}
	current, err = capturedSession(ctx, q, current, result.Revision)
	if err == nil {
		result.Session = &current
	}
	return result, err
}

// WorkspaceSetRetry checks the stable request before mutable filesystem checks.
func (s *Store) WorkspaceSetRetry(ctx context.Context, request session.WorkspaceSetRequest) (session.ControlEdit, error) {
	if err := request.Validate(); err != nil {
		return session.ControlEdit{}, err
	}
	digest, err := requestDigest("workspace.set", request)
	if err != nil {
		return session.ControlEdit{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return session.ControlEdit{}, err
	}
	defer func() { _ = tx.Rollback() }()
	return controlRetry(ctx, tx, request.ID, digest)
}

const maxControlOwners = 4096

func treeOwners(ctx context.Context, q querier, tree session.TreeID) ([]session.SessionID, error) {
	rows, err := q.QueryContext(ctx, "SELECT id FROM sessions WHERE tree_id=? ORDER BY id LIMIT ?", tree, maxControlOwners+1)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	owners := []session.SessionID{}
	for rows.Next() {
		var id session.SessionID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		owners = append(owners, id)
		if len(owners) > maxControlOwners {
			return nil, ErrLimit
		}
	}
	return owners, rows.Err()
}

// ControlOwners is the exact owner set whose live shell reservations must be
// held idle through SetWorkingDirectory's commit. SQL rechecks the set itself.
func (s *Store) ControlOwners(ctx context.Context, tree session.TreeID) ([]session.SessionID, error) {
	return treeOwners(ctx, s.db, tree)
}

func controlIdle(ctx context.Context, q querier, tree session.TreeID) error {
	var busy bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM turns t JOIN sessions s ON s.id=t.session_id WHERE s.tree_id=? AND t.state IN('running','cancelling'))
 OR EXISTS(SELECT 1 FROM inputs i JOIN sessions s ON s.id=i.session_id WHERE s.tree_id=? AND i.turn_id IS NULL AND i.steered_turn_id IS NULL AND i.cancelled_at IS NULL)
 OR EXISTS(SELECT 1 FROM workspace_actions a JOIN sessions s ON s.id=a.session_id WHERE s.tree_id=? AND a.state='claimed')`, tree, tree, tree).Scan(&busy)
	if err != nil {
		return err
	}
	if busy {
		return ErrBusy
	}
	return nil
}

// SetWorkingDirectory changes only the next configuration snapshot. The caller
// resolves the human path, then holds shell reservations for exactly owners.
func (s *Store) SetWorkingDirectory(ctx context.Context, request session.WorkspaceSetRequest, canonical string, owners []session.SessionID) (result session.ControlEdit, err error) {
	if err = request.Validate(); err != nil {
		return
	}
	digest, err := requestDigest("workspace.set", request)
	if err != nil {
		return
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		result, err = controlRetry(ctx, tx, request.ID, digest)
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		current, err := readSession(ctx, tx, request.SessionID)
		if err != nil {
			return err
		}
		if current.ConfigRevision != request.ExpectedRevision || current.ConfigRevision == math.MaxInt64 {
			return ErrConflict
		}
		if !filepath.IsAbs(canonical) || filepath.Clean(canonical) != canonical || session.ValidateText(canonical, 4096) != nil {
			return session.ErrInvalid
		}
		actual, err := treeOwners(ctx, tx, current.TreeID)
		if err != nil {
			return err
		}
		if !slices.Equal(actual, owners) {
			return ErrConflict
		}
		if err := controlIdle(ctx, tx, current.TreeID); err != nil {
			return err
		}
		var pinned bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM workspace_snapshots WHERE session_id=? AND released_at IS NULL)", current.ID).Scan(&pinned); err != nil {
			return err
		}
		if pinned && canonical != current.WorkingDirectory {
			return ErrBusy
		}
		current.WorkingDirectory = canonical
		result, err = commitControl(ctx, tx, request.ID, "workspace", digest, current)
		if err == nil && current.ParentID == nil {
			err = bumpTreeCatalog(ctx, tx)
		}
		return err
	})
	return
}

func (s *Store) ConfigureRun(ctx context.Context, request session.RunConfigureRequest) (result session.ControlEdit, err error) {
	if err = request.Validate(); err != nil {
		return
	}
	digest, err := requestDigest("run.configure", request)
	if err != nil {
		return
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		result, err = controlRetry(ctx, tx, request.ID, digest)
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		current, err := readSession(ctx, tx, request.SessionID)
		if err != nil {
			return err
		}
		if current.ParentID != nil {
			return session.ErrInvalid
		}
		if current.ConfigRevision != request.ExpectedRevision || current.ConfigRevision == math.MaxInt64 {
			return ErrConflict
		}
		if err := controlIdle(ctx, tx, current.TreeID); err != nil {
			return err
		}
		current.Config.Run = new(request.Configuration)
		result, err = commitControl(ctx, tx, request.ID, "run", digest, current)
		return err
	})
	return
}

func commitControl(ctx context.Context, tx *sql.Tx, id, kind, digest string, current session.Session) (session.ControlEdit, error) {
	raw, err := encode(current.Config)
	if err != nil {
		return session.ControlEdit{}, err
	}
	current.ConfigRevision++
	if _, err := tx.ExecContext(ctx, "INSERT INTO session_configurations VALUES (?,?,?,?,?)", current.ID, current.ConfigRevision, raw, current.WorkingDirectory, now()); err != nil {
		return session.ControlEdit{}, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO session_control_edits VALUES (?,?,?,?,?,?)", id, current.ID, kind, digest, current.ConfigRevision, now()); err != nil {
		return session.ControlEdit{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE sessions SET config_revision=? WHERE id=?", current.ConfigRevision, current.ID); err != nil {
		return session.ControlEdit{}, err
	}
	return session.ControlEdit{ID: id, SessionID: current.ID, Revision: current.ConfigRevision, Session: &current}, nil
}

func rootHeadless(ctx context.Context, q querier, owner session.SessionID) (bool, error) {
	var headless bool
	err := q.QueryRowContext(ctx, `SELECT COALESCE(json_extract(c.configuration,'$.run.headless'),0) FROM sessions root JOIN sessions owner ON owner.tree_id=root.tree_id
 JOIN session_configurations c ON c.session_id=root.id AND c.revision=root.config_revision WHERE owner.id=? AND root.parent_id IS NULL`, owner).Scan(&headless)
	return headless, found(err)
}
