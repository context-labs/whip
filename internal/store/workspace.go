package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/context-labs/whip/internal/session"
)

const workspaceActionColumns = "id,snapshot_id,session_id,kind,state,failure,created_at,finished_at"

func scanWorkspaceAction(row scanner) (value session.WorkspaceAction, err error) {
	var created int64
	var finished sql.NullInt64
	err = row.Scan(&value.ID, &value.SnapshotID, &value.SessionID, &value.Kind, &value.State, &value.Failure, &created, &finished)
	value.CreatedAt = timestamp(created)
	value.FinishedAt = optionalTime(finished)
	return value, found(err)
}

func workspaceDigest(kind session.WorkspaceActionKind, request session.WorkspaceRequest) (string, error) {
	if err := session.ValidateID(string(request.ID)); err != nil {
		return "", err
	}
	return requestDigest("workspace_"+string(kind), request)
}

func readWorkspaceRetry(ctx context.Context, q querier, kind session.WorkspaceActionKind, request session.WorkspaceRequest, digest string) (session.WorkspaceResult, error) {
	var previous string
	if err := q.QueryRowContext(ctx, "SELECT digest FROM workspace_actions WHERE id=?", request.ID).Scan(&previous); err != nil {
		return session.WorkspaceResult{}, found(err)
	}
	if digest != previous {
		return session.WorkspaceResult{}, ErrConflict
	}
	return readWorkspaceResult(ctx, q, request.ID)
}

// WorkspaceRetry checks immutable request identity before inspecting a current
// owner, path, snapshot pin or release state. Claimed actions are never dispatch.
func (s *Store) WorkspaceRetry(ctx context.Context, kind session.WorkspaceActionKind, request session.WorkspaceRequest) (result session.WorkspaceResult, err error) {
	digest, err := workspaceDigest(kind, request)
	if err != nil {
		return result, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error { result, err = readWorkspaceRetry(ctx, tx, kind, request, digest); return err })
	return
}

func readWorkspaceSnapshot(ctx context.Context, q querier, owner session.SessionID, id session.WorkspaceSnapshotID) (value session.WorkspaceSnapshot, err error) {
	var binding string
	var created int64
	var released sql.NullInt64
	err = q.QueryRowContext(ctx, `SELECT s.id,s.session_id,s.capture_id,s.binding,s.object_id,s.created_at,s.released_at,a.state
 FROM workspace_snapshots s JOIN workspace_actions a ON a.id=s.capture_id WHERE s.id=? AND s.session_id=?`, id, owner).
		Scan(&value.ID, &value.SessionID, &value.CaptureID, &binding, &value.ObjectID, &created, &released, &value.State)
	if err != nil {
		return value, found(err)
	}
	value.CreatedAt = timestamp(created)
	value.ReleasedAt = optionalTime(released)
	value.Semantics = session.WorkspaceSemantics
	err = json.Unmarshal([]byte(binding), &value.Binding)
	return
}

func readWorkspaceResult(ctx context.Context, q querier, id session.WorkspaceActionID) (result session.WorkspaceResult, err error) {
	result.Action, err = scanWorkspaceAction(q.QueryRowContext(ctx, "SELECT "+workspaceActionColumns+" FROM workspace_actions WHERE id=?", id))
	if err == nil {
		result.Snapshot, err = readWorkspaceSnapshot(ctx, q, result.Action.SessionID, result.Action.SnapshotID)
	}
	return
}

func (s *Store) WorkspaceSnapshot(ctx context.Context, owner session.SessionID, id session.WorkspaceSnapshotID) (session.WorkspaceSnapshot, error) {
	return readWorkspaceSnapshot(ctx, s.db, owner, id)
}

// WorkspaceAction observes a durable action without inspecting or changing Git.
// It remains available after explicit release and owner deletion.
func (s *Store) WorkspaceAction(ctx context.Context, owner session.SessionID, id session.WorkspaceActionID) (session.WorkspaceAction, error) {
	return scanWorkspaceAction(s.db.QueryRowContext(ctx, "SELECT "+workspaceActionColumns+" FROM workspace_actions WHERE id=? AND session_id=?", id, owner))
}

func (s *Store) WorkspaceSnapshots(ctx context.Context, owner session.SessionID, after session.WorkspaceSnapshotID, limit int) ([]session.WorkspaceSnapshot, error) {
	if err := pageLimit(limit); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM workspace_snapshots WHERE session_id=? AND id>? ORDER BY id LIMIT ?", owner, after, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	ids := []session.WorkspaceSnapshotID{}
	for rows.Next() {
		var id session.WorkspaceSnapshotID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	values := make([]session.WorkspaceSnapshot, 0, len(ids))
	for _, id := range ids {
		value, err := readWorkspaceSnapshot(ctx, s.db, owner, id)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

// ClaimWorkspace is the one-use external-effect gate. A successful retry returns
// claimed=false even if the earlier action is still running or uncertain.
func (s *Store) ClaimWorkspace(ctx context.Context, kind session.WorkspaceActionKind, request session.WorkspaceRequest, binding session.WorkspaceBinding) (result session.WorkspaceResult, claimed bool, err error) {
	digest, err := workspaceDigest(kind, request)
	if err != nil {
		return result, false, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		previous, err := readWorkspaceRetry(ctx, tx, kind, request, digest)
		if err == nil {
			result = previous
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		if err := request.Validate(); err != nil {
			return err
		}
		if kind != session.WorkspaceCapture && kind != session.WorkspaceRestore && kind != session.WorkspaceRelease {
			return session.ErrInvalid
		}
		if err := binding.Validate(); err != nil {
			return err
		}
		if _, err := readSession(ctx, tx, request.SessionID); err != nil {
			return err
		}
		var busy bool
		if err := tx.QueryRowContext(ctx, `SELECT
 EXISTS(SELECT 1 FROM turns WHERE session_id=? AND finished_at IS NULL) OR
 EXISTS(SELECT 1 FROM inputs WHERE session_id=? AND turn_id IS NULL AND steered_turn_id IS NULL AND cancelled_at IS NULL) OR
 EXISTS(SELECT 1 FROM workspace_actions WHERE session_id=? AND state='claimed')`, request.SessionID, request.SessionID, request.SessionID).Scan(&busy); err != nil {
			return err
		}
		if busy {
			return ErrBusy
		}
		ready, err := mailReady(ctx, tx, request.SessionID)
		if err != nil {
			return err
		}
		if ready {
			return ErrBusy
		}
		if kind == session.WorkspaceCapture {
			var exists bool
			var ownerCount, total int
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM workspace_snapshots WHERE id=?),
 (SELECT COUNT(*) FROM workspace_snapshots WHERE session_id=? AND released_at IS NULL),
 (SELECT COUNT(*) FROM workspace_snapshots WHERE released_at IS NULL)`, request.SnapshotID, request.SessionID).Scan(&exists, &ownerCount, &total); err != nil {
				return err
			}
			if exists {
				return ErrConflict
			}
			if ownerCount >= session.MaxWorkspaceSnapshots || total >= session.MaxRuntimeWorkspaceSnapshots {
				return ErrLimit
			}
			raw, err := encode(binding)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO workspace_snapshots(id,session_id,capture_id,binding,created_at) VALUES(?,?,?,?,?)", request.SnapshotID, request.SessionID, request.ID, raw, now()); err != nil {
				return err
			}
		} else {
			snapshot, err := readWorkspaceSnapshot(ctx, tx, request.SessionID, request.SnapshotID)
			if err != nil {
				return err
			}
			if snapshot.Binding != binding || snapshot.ReleasedAt != nil {
				return ErrConflict
			}
			if kind == session.WorkspaceRestore && (snapshot.State != session.WorkspaceSucceeded || snapshot.ObjectID == nil) {
				return ErrConflict
			}
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO workspace_actions(id,snapshot_id,session_id,kind,digest,state,created_at) VALUES(?,?,?,?,?,'claimed',?)", request.ID, request.SnapshotID, request.SessionID, kind, digest, now()); err != nil {
			return err
		}
		result, err = readWorkspaceResult(ctx, tx, request.ID)
		claimed = err == nil
		return err
	})
	if err != nil {
		return session.WorkspaceResult{}, false, err
	}
	return
}

// StageWorkspaceSnapshot records the immutable commit before the pin is written.
// A crash after Git updates the ref therefore leaves a releasable known target.
func (s *Store) StageWorkspaceSnapshot(ctx context.Context, id session.WorkspaceActionID, object string) error {
	if err := session.ValidateWorkspaceObject(object); err != nil {
		return err
	}
	return s.write(ctx, func(tx *sql.Tx) error {
		value, err := readWorkspaceResult(ctx, tx, id)
		if err != nil {
			return err
		}
		if value.Action.Kind != session.WorkspaceCapture || value.Action.State != session.WorkspaceClaimed {
			return ErrConflict
		}
		if value.Snapshot.ObjectID != nil {
			if *value.Snapshot.ObjectID == object {
				return nil
			}
			return ErrConflict
		}
		_, err = tx.ExecContext(ctx, "UPDATE workspace_snapshots SET object_id=? WHERE id=?", object, value.Snapshot.ID)
		return err
	})
}

// SettleWorkspace never converts an uncertain external effect into failure or
// success on replay. Only the first claimed attempt may publish an outcome.
func (s *Store) SettleWorkspace(ctx context.Context, id session.WorkspaceActionID, success bool) (result session.WorkspaceResult, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		value, err := readWorkspaceResult(ctx, tx, id)
		if err != nil {
			return err
		}
		state := session.WorkspaceUncertain
		var failure *string
		if success {
			state = session.WorkspaceSucceeded
		} else {
			failure = new("Workspace action was interrupted or failed; its filesystem effects may be partial. It will not run again automatically.")
		}
		if value.Action.State != session.WorkspaceClaimed {
			if value.Action.State != state {
				return ErrConflict
			}
			result = value
			return nil
		}
		if success && value.Action.Kind == session.WorkspaceCapture && value.Snapshot.ObjectID == nil {
			return ErrConflict
		}
		if success && value.Action.Kind == session.WorkspaceRelease {
			if _, err := tx.ExecContext(ctx, "UPDATE workspace_snapshots SET released_at=? WHERE id=?", now(), value.Snapshot.ID); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, "UPDATE workspace_actions SET state=?,failure=?,finished_at=? WHERE id=?", state, failure, now(), id); err != nil {
			return err
		}
		result, err = readWorkspaceResult(ctx, tx, id)
		return err
	})
	return
}

func recoverWorkspace(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, "UPDATE workspace_actions SET state='uncertain',failure='Runtime restarted; workspace effects may be partial and will not be replayed.',finished_at=? WHERE state='claimed'", now())
	return err
}

func workspaceDeletionCheck(ctx context.Context, tx *sql.Tx, id session.SessionID) error {
	var pinned bool
	if err := tx.QueryRowContext(ctx, subtree+" SELECT EXISTS(SELECT 1 FROM workspace_snapshots WHERE session_id IN(SELECT id FROM subtree) AND released_at IS NULL)", id).Scan(&pinned); err != nil {
		return err
	}
	if pinned {
		return fmt.Errorf("%w: release retained workspace snapshots before deletion", ErrBusy)
	}
	return nil
}
