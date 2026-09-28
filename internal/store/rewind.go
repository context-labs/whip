package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"

	"github.com/context-labs/whip/internal/session"
)

// Rewind retires a transcript suffix and resets its REPL revision atomically.
// Exact retries return the original edit even after new execution or edits. Mail,
// goals, state, accounting and external effects retain their independent facts.
func (s *Store) Rewind(ctx context.Context, request session.RewindRequest) (result session.HistoryEdit, err error) {
	if err := request.Validate(); err != nil {
		return result, err
	}
	digest, err := requestDigest("history_rewind", request)
	if err != nil {
		return result, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		var previous string
		err := tx.QueryRowContext(ctx, "SELECT digest FROM history_edits WHERE id=?", request.ID).Scan(&previous)
		if err == nil {
			if previous != digest {
				return ErrConflict
			}
			result, err = readHistoryEdit(ctx, tx, request.SessionID, request.ID)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := validateRewind(ctx, tx, request); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO history_edits (id,session_id,digest,expected_revision,revision,observed_through,keep_through,created_at)
 VALUES (?,?,?,?,?,?,?,?)`, request.ID, request.SessionID, digest, request.ExpectedRevision, request.ExpectedRevision+1, request.ObservedThrough, request.KeepThrough, now()); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE sessions SET history_revision=history_revision+1 WHERE id=?", request.SessionID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE messages SET retired_by=?,retired_revision=?
 WHERE session_id=? AND sequence>? AND retired_revision IS NULL`, request.ID, request.ExpectedRevision+1, request.SessionID, request.KeepThrough); err != nil {
			return err
		}
		if err := retainRewindContext(ctx, tx, request.SessionID); err != nil {
			return err
		}
		result, err = readHistoryEdit(ctx, tx, request.SessionID, request.ID)
		return err
	})
	return
}

func validateRewind(ctx context.Context, tx *sql.Tx, request session.RewindRequest) error {
	var revision session.Revision
	var lifecycle session.Lifecycle
	var through int64
	if err := tx.QueryRowContext(ctx, `SELECT history_revision,lifecycle,
 (SELECT COALESCE(MAX(sequence),0) FROM messages WHERE session_id=s.id AND retired_revision IS NULL)
 FROM sessions s WHERE id=?`, request.SessionID).Scan(&revision, &lifecycle, &through); err != nil {
		return found(err)
	}
	if revision != request.ExpectedRevision || through != request.ObservedThrough {
		return ErrConflict
	}
	if revision == math.MaxInt64 {
		return ErrLimit
	}
	if lifecycle != session.Stopped {
		return fmt.Errorf("%w: rewind requires a stopped session", ErrConflict)
	}
	var busy bool
	if err := tx.QueryRowContext(ctx, `SELECT
 EXISTS(SELECT 1 FROM turns WHERE session_id=? AND state IN ('running','cancelling')) OR
 EXISTS(SELECT 1 FROM inputs WHERE session_id=? AND turn_id IS NULL AND cancelled_at IS NULL)`, request.SessionID, request.SessionID).Scan(&busy); err != nil {
		return err
	}
	if busy {
		return ErrBusy
	}
	if request.KeepThrough == 0 {
		return nil
	}
	var boundary bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM messages m
 LEFT JOIN turns t ON t.id=m.turn_id WHERE m.session_id=? AND m.sequence=? AND m.retired_revision IS NULL
 AND (t.id IS NULL OR t.finished_at IS NOT NULL))`, request.SessionID, request.KeepThrough).Scan(&boundary); err != nil {
		return err
	}
	if !boundary {
		return fmt.Errorf("%w: rewind must keep a terminal history group boundary", session.ErrInvalid)
	}
	// A copied group may have no turn. Validate group identity directly, including
	// any interleaved copied groups, instead of inferring grouping from local work.
	var split bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM messages later
 WHERE later.session_id=? AND later.retired_revision IS NULL AND later.sequence>? AND EXISTS(
  SELECT 1 FROM messages earlier WHERE earlier.group_id=later.group_id
  AND earlier.retired_revision IS NULL AND earlier.sequence<=?))`, request.SessionID, request.KeepThrough, request.KeepThrough).Scan(&split); err != nil {
		return err
	}
	if split {
		return fmt.Errorf("%w: rewind cannot split a history group", session.ErrInvalid)
	}
	return nil
}

func retainRewindContext(ctx context.Context, tx *sql.Tx, owner session.SessionID) error {
	head, err := readContextHead(ctx, tx, owner)
	if err != nil || head.CompactionID == nil {
		return err
	}
	value, err := readCompaction(ctx, tx, owner, *head.CompactionID)
	if err != nil {
		return err
	}
	if err := activeCompaction(ctx, tx, value); err == nil {
		return nil
	} else if !errors.Is(err, ErrConflict) {
		return err
	}
	if head.Revision == math.MaxInt64 {
		return ErrLimit
	}
	_, err = selectCompaction(ctx, tx, head, nil)
	return err
}
