package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/context-labs/whip/internal/session"
)

// ModelCaptureCapacity bounds automatic body publication before filesystem I/O.
// Reservation rechecks the same owner quota atomically before registering refs.
func (s *Store) ModelCaptureCapacity(ctx context.Context, owner session.SessionID, capture session.ModelCapture) (bool, error) {
	return modelCaptureCapacity(ctx, s.db, owner, capture)
}

func modelCaptureCapacity(ctx context.Context, q querier, owner session.SessionID, capture session.ModelCapture) (bool, error) {
	var count, size int64
	if err := q.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(b.size),0) FROM content_references r JOIN content_bodies b ON b.digest=r.digest WHERE r.owner_session_id=?`, owner).Scan(&count, &size); err != nil {
		return false, err
	}
	seen := map[string]bool{}
	for _, body := range []session.CapturedText{capture.Instructions, capture.Notices} {
		for _, chunk := range body.Chunks {
			if seen[chunk.ID] {
				continue
			}
			seen[chunk.ID] = true
			var exists bool
			if err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM content_references WHERE owner_session_id=? AND reference_id=?)`, owner, chunk.ID).Scan(&exists); err != nil {
				return false, err
			}
			if !exists {
				count++
				size += chunk.Size
			}
		}
	}
	return count <= session.MaxContentReferences && size <= session.MaxSessionContentBytes, nil
}

func sameModelCapture(ctx context.Context, tx *sql.Tx, id session.ModelAttemptID, capture *session.ModelCapture) error {
	if capture != nil {
		sealed := *capture
		sealed.Seal()
		if sealed.SourceDigest != capture.SourceDigest {
			return ErrConflict
		}
	}
	var digest, requestDigest string
	err := tx.QueryRowContext(ctx, `SELECT source_digest,json_extract(capture,'$.request_digest') FROM model_captures WHERE attempt_id=?`, id).Scan(&digest, &requestDigest)
	if errors.Is(err, sql.ErrNoRows) && capture == nil {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if capture == nil || digest != capture.SourceDigest || requestDigest != capture.RequestDigest {
		return ErrConflict
	}
	return nil
}

func registerModelCapture(ctx context.Context, tx *sql.Tx, owner session.SessionID, id session.ModelAttemptID, capture *session.ModelCapture) error {
	if capture == nil {
		return nil
	}
	value := *capture
	if err := value.Validate(owner); err != nil {
		return err
	}
	capacity, err := modelCaptureCapacity(ctx, tx, owner, value)
	if err != nil {
		return err
	}
	for _, body := range []*session.CapturedText{&value.Instructions, &value.Notices} {
		if !capacity && body.Status == "available" && body.Bytes > 0 {
			body.Status, body.Chunks = "quota", []session.ContentReference{}
		}
		for i, chunk := range body.Chunks {
			reference, err := registerContent(ctx, tx, chunk)
			if err != nil {
				return err
			}
			body.Chunks[i] = reference
		}
	}
	raw, err := encode(value)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO model_captures(attempt_id,source_digest,capture) VALUES (?,?,?)`, id, value.SourceDigest, raw)
	return err
}

// ModelInspection reads only exact-attempt evidence; absence is historical
// absence, never permission to reconstruct a prompt from today's configuration.
func (s *Store) ModelInspection(ctx context.Context, owner session.SessionID, id session.ModelAttemptID) (result session.ModelInspection, err error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback() }()
	var raw *string
	err = tx.QueryRowContext(ctx, `SELECT t.session_id,a.id,a.turn_id,json_extract(a.request,'$.request_digest'),c.capture FROM model_attempts a JOIN turns t ON t.id=a.turn_id LEFT JOIN model_captures c ON c.attempt_id=a.id WHERE a.id=? AND t.session_id=?`, id, owner).Scan(&result.SessionID, &result.AttemptID, &result.TurnID, &result.RequestDigest, &raw)
	if err != nil {
		return result, found(err)
	}
	if raw != nil {
		result.Capture = &session.ModelCapture{}
		if err := json.Unmarshal([]byte(*raw), result.Capture); err != nil {
			return result, err
		}
	}
	summary, err := scanCompaction(tx.QueryRowContext(ctx, "SELECT "+compactionColumns+" FROM compactions WHERE session_id=? AND attempt_id=?", owner, id), false)
	if err == nil {
		result.Compaction = &summary.CompactionMetadata
	} else if !errors.Is(err, ErrNotFound) {
		return result, err
	}
	return result, tx.Commit()
}
