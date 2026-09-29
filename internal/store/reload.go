package store

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strings"

	"github.com/context-labs/whip/internal/session"
)

const reloadSelect = `SELECT id,digest,session_id,tree_id,expected_revision,host_revision,configuration,state,revision,created_at,settled_at FROM session_reloads`

func reloadOverrides(ctx context.Context, q querier, owner session.SessionID, revision session.Revision) (fields session.ReloadOverrides, err error) {
	err = q.QueryRowContext(ctx, "SELECT override_fields FROM session_configurations WHERE session_id=? AND revision=?", owner, revision).Scan(&fields)
	return fields, found(err)
}

func scanReload(row scanner) (edit session.ReloadEdit, digest string, err error) {
	var raw string
	var created int64
	var settled sql.NullInt64
	err = row.Scan(&edit.ID, &digest, &edit.SessionID, &edit.TreeID, &edit.ExpectedRevision, &edit.HostRevision, &raw, &edit.State, &edit.Revision, &created, &settled)
	if err != nil {
		return edit, digest, found(err)
	}
	edit.CreatedAt = timestamp(created)
	if settled.Valid {
		edit.SettledAt = new(timestamp(settled.Int64))
	}
	err = json.Unmarshal([]byte(raw), &edit.Configuration)
	return edit, digest, err
}

func reloadRetry(ctx context.Context, q querier, request session.ReloadRequest, digest string) (session.ReloadEdit, error) {
	edit, previous, err := scanReload(q.QueryRowContext(ctx, reloadSelect+" WHERE id=?", request.ID))
	if err == nil && previous != digest {
		return session.ReloadEdit{}, ErrConflict
	}
	return edit, err
}

// ReloadRetry resolves accepted payloads without consulting live host settings.
func (s *Store) ReloadRetry(ctx context.Context, request session.ReloadRequest) (session.ReloadEdit, error) {
	if err := request.Validate(); err != nil {
		return session.ReloadEdit{}, err
	}
	digest, err := requestDigest("sessions.reload", request)
	if err != nil {
		return session.ReloadEdit{}, err
	}
	return reloadRetry(ctx, s.db, request, digest)
}

func (s *Store) ReloadEdit(ctx context.Context, owner session.SessionID, id string) (session.ReloadEdit, error) {
	edit, _, err := scanReload(s.db.QueryRowContext(ctx, reloadSelect+" WHERE id=? AND session_id=?", id, owner))
	return edit, err
}

// AdmitReload freezes the resolved safe configuration and the host revision in
// the same transaction as admission. Nothing external starts here.
func (s *Store) AdmitReload(ctx context.Context, request session.ReloadRequest, host session.Configuration, hostRevision string) (result session.ReloadEdit, err error) {
	if err := request.Validate(); err != nil {
		return result, err
	}
	digest, err := requestDigest("sessions.reload", request)
	if err != nil {
		return result, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		result, err = reloadRetry(ctx, tx, request, digest)
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		hash, err := hex.DecodeString(hostRevision)
		if err != nil || len(hash) != 32 || strings.ToLower(hostRevision) != hostRevision {
			return session.ErrInvalid
		}
		current, err := readSession(ctx, tx, request.SessionID)
		if err != nil {
			return err
		}
		if current.ParentID != nil || current.ConfigRevision != request.ExpectedRevision || current.ConfigRevision == math.MaxInt64 {
			return ErrConflict
		}
		var pending bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM session_reloads WHERE tree_id=? AND state='pending')", current.TreeID).Scan(&pending); err != nil {
			return err
		}
		if pending {
			return ErrBusy
		}
		if _, err := treeOwners(ctx, tx, current.TreeID); err != nil {
			return err
		}
		fields, err := reloadOverrides(ctx, tx, current.ID, current.ConfigRevision)
		if err != nil {
			return err
		}
		definition, err := definition(ctx, tx, current.Definition)
		if err != nil {
			return err
		}
		captured, err := session.RefreshConfiguration(current.Config, host, definition.Document, fields)
		if err != nil {
			return err
		}
		raw, err := encode(captured)
		if err != nil {
			return err
		}
		if len(raw) > session.MaxDocumentBytes {
			return ErrLimit
		}
		var count, pendingCount, ownCount, used int64
		if err := tx.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(state='pending'),0),coalesce(sum(session_id=?),0),coalesce(sum(length(CAST(configuration AS BLOB))),0) FROM session_reloads`, current.ID).Scan(&count, &pendingCount, &ownCount, &used); err != nil {
			return err
		}
		if count >= 1024 || pendingCount >= 128 || ownCount >= 64 || used+int64(len(raw)) > 128<<20 {
			return ErrLimit
		}
		created := now()
		if _, err := tx.ExecContext(ctx, `INSERT INTO session_reloads (id,digest,session_id,tree_id,expected_revision,host_revision,configuration,state,created_at) VALUES (?,?,?,?,?,?,?,'pending',?)`, request.ID, digest, current.ID, current.TreeID, current.ConfigRevision, hostRevision, raw, created); err != nil {
			return err
		}
		result = session.ReloadEdit{ReloadRequest: request, TreeID: current.TreeID, HostRevision: hostRevision, Configuration: captured, State: session.ReloadPending, CreatedAt: timestamp(created)}
		return nil
	})
	if err != nil {
		return session.ReloadEdit{}, err
	}
	return result, nil
}

type PendingReload struct {
	ID        string
	SessionID session.SessionID
	TreeID    session.TreeID
}

// NextReload is bounded scheduler metadata; the cursor is disposable.
func (s *Store) NextReload(ctx context.Context, after string) (next PendingReload, err error) {
	err = s.db.QueryRowContext(ctx, "SELECT id,session_id,tree_id FROM session_reloads WHERE state='pending' AND id>? ORDER BY id LIMIT 1", after).Scan(&next.ID, &next.SessionID, &next.TreeID)
	return next, found(err)
}

func reloadPending(ctx context.Context, q querier, tree session.TreeID) error {
	var pending bool
	if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM session_reloads WHERE tree_id=? AND state='pending') AND NOT EXISTS(SELECT 1 FROM turns t JOIN sessions s ON s.id=t.session_id WHERE s.tree_id=? AND t.state IN('running','cancelling'))`, tree, tree).Scan(&pending); err != nil {
		return err
	}
	if pending {
		return ErrBusy
	}
	return nil
}

// ApplyReload settles one captured control. The runtime holds the tree's existing
// claim gate and idle shell reservations through SQL and joined cache retirement.
func (s *Store) ApplyReload(ctx context.Context, id string) (result session.ReloadEdit, changed bool, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		result, _, err = scanReload(tx.QueryRowContext(ctx, reloadSelect+" WHERE id=?", id))
		if err != nil {
			return err
		}
		if result.State != session.ReloadPending {
			return nil
		}
		current, readErr := readSession(ctx, tx, result.SessionID)
		switch {
		case errors.Is(readErr, ErrNotFound):
			result.State = session.ReloadUnavailable
		case readErr != nil:
			return readErr
		case current.ConfigRevision != result.ExpectedRevision:
			result.State = session.ReloadConflicted
		default:
			var busy bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM turns t JOIN sessions s ON s.id=t.session_id WHERE s.tree_id=? AND t.state IN('running','cancelling'))
 OR EXISTS(SELECT 1 FROM workspace_actions a JOIN sessions s ON s.id=a.session_id WHERE s.tree_id=? AND a.state='claimed')`, current.TreeID, current.TreeID).Scan(&busy); err != nil {
				return err
			}
			if busy {
				return ErrBusy
			}
			raw, err := encode(result.Configuration)
			if err != nil {
				return err
			}
			revision := current.ConfigRevision + 1
			if _, err := tx.ExecContext(ctx, `INSERT INTO session_configurations VALUES(?,?,?,?,?,(SELECT override_fields FROM session_configurations WHERE session_id=? AND revision=?))`, current.ID, revision, raw, current.WorkingDirectory, now(), current.ID, current.ConfigRevision); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "UPDATE sessions SET config_revision=? WHERE id=?", revision, current.ID); err != nil {
				return err
			}
			result.State, result.Revision, changed = session.ReloadApplied, &revision, true
		}
		settled := now()
		result.SettledAt = new(timestamp(settled))
		_, err := tx.ExecContext(ctx, "UPDATE session_reloads SET state=?,revision=?,settled_at=? WHERE id=?", result.State, result.Revision, settled, result.ID)
		return err
	})
	if err != nil {
		return session.ReloadEdit{}, false, err
	}
	return result, changed, nil
}

// CancelReload only withdraws still-pending work. Applying and cancelling share
// the same transaction owner; a terminal result cannot be reinterpreted.
func (s *Store) CancelReload(ctx context.Context, owner session.SessionID, id string) (result session.ReloadEdit, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		result, _, err = scanReload(tx.QueryRowContext(ctx, reloadSelect+" WHERE id=? AND session_id=?", id, owner))
		if err != nil {
			return err
		}
		if result.State != session.ReloadPending {
			return nil
		}
		settled := now()
		if _, err := tx.ExecContext(ctx, "UPDATE session_reloads SET state='interrupted',settled_at=? WHERE id=?", settled, id); err != nil {
			return err
		}
		result.State, result.SettledAt = session.ReloadInterrupted, new(timestamp(settled))
		return nil
	})
	return
}
