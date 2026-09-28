package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/context-labs/whip/internal/session"
)

const contentSelect = `SELECT r.id,r.session_id,r.digest,b.size,r.media_type,r.created_at
 FROM content_references r JOIN content_bodies b ON b.digest=r.digest`

// RegisterContent follows durable body publication. Retrying the same reference
// is idempotent; SQL failure may leave an unreferenced file, never a missing body.
func (s *Store) RegisterContent(ctx context.Context, reference session.ContentReference) (result session.ContentReference, err error) {
	if err := reference.Validate(); err != nil {
		return result, err
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		var exists bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM content_references WHERE id=?)", reference.ID).Scan(&exists); err != nil {
			return err
		}
		result, err = registerContent(ctx, tx, reference)
		if err == nil && !exists {
			err = chargeWrite(ctx, tx, reference.SessionID, "content", reference.ID, 0, reference.Size)
		}
		return err
	})
	return result, err
}

func registerContent(ctx context.Context, tx *sql.Tx, reference session.ContentReference) (result session.ContentReference, err error) {
	existing, err := scanContent(tx.QueryRowContext(ctx, contentSelect+" WHERE r.id=?", reference.ID))
	if err == nil {
		if existing.SessionID != reference.SessionID || existing.Digest != reference.Digest || existing.Size != reference.Size || existing.MediaType != reference.MediaType {
			return result, ErrConflict
		}
		result = existing
		return result, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return result, err
	}
	if _, err := readSession(ctx, tx, reference.SessionID); err != nil {
		return result, err
	}
	var count int
	var size int64
	if err := tx.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(b.size),0)
 FROM content_references r JOIN content_bodies b ON b.digest=r.digest WHERE r.session_id=?`, reference.SessionID).Scan(&count, &size); err != nil {
		return result, err
	}
	if count >= session.MaxContentReferences || size+reference.Size > session.MaxSessionContentBytes {
		return result, ErrLimit
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO content_bodies VALUES (?,?) ON CONFLICT DO NOTHING", reference.Digest, reference.Size); err != nil {
		return result, err
	}
	var storedSize int64
	if err := tx.QueryRowContext(ctx, "SELECT size FROM content_bodies WHERE digest=?", reference.Digest).Scan(&storedSize); err != nil {
		return result, err
	}
	if storedSize != reference.Size {
		return result, ErrConflict
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO content_references VALUES (?,?,?,?,?)", reference.ID, reference.SessionID, reference.Digest, reference.MediaType, now()); err != nil {
		return result, err
	}
	result, err = readContent(ctx, tx, reference.SessionID, reference.ID)
	return result, err
}

func scanContent(row *sql.Row) (session.ContentReference, error) {
	var result session.ContentReference
	var created int64
	err := row.Scan(&result.ID, &result.SessionID, &result.Digest, &result.Size, &result.MediaType, &created)
	result.CreatedAt = timestamp(created)
	return result, found(err)
}

func readContent(ctx context.Context, q querier, owner session.SessionID, id string) (session.ContentReference, error) {
	return scanContent(q.QueryRowContext(ctx, contentSelect+" WHERE r.session_id=? AND r.id=?", owner, id))
}

func (s *Store) ContentReference(ctx context.Context, owner session.SessionID, id string) (session.ContentReference, error) {
	return readContent(ctx, s.db, owner, id)
}

func validateContentReferences(ctx context.Context, q querier, owner session.SessionID, parts []session.Part) error {
	for _, part := range parts {
		if part.Type == "content" {
			if _, err := readContent(ctx, q, owner, part.ReferenceID); err != nil {
				return err
			}
		}
	}
	return nil
}

// The parent reference authorizes sharing. Each distinct reference gets a new
// child-owned identity; body metadata remains immutable and shared by digest.
func shareChildContent(ctx context.Context, tx *sql.Tx, parent, child session.SessionID, parts []session.Part) ([]session.Part, error) {
	result := append([]session.Part(nil), parts...)
	shared := make(map[string]string)
	for i, part := range parts {
		if part.Type != "content" {
			continue
		}
		id, exists := shared[part.ReferenceID]
		if !exists {
			reference, err := readContent(ctx, tx, parent, part.ReferenceID)
			if err != nil {
				return nil, err
			}
			reference.ID, reference.SessionID = newID("content"), child
			if _, err := registerContent(ctx, tx, reference); err != nil {
				return nil, err
			}
			id = reference.ID
			shared[part.ReferenceID] = id
		}
		result[i].ReferenceID = id
	}
	return result, nil
}

// ContentReferenced is used only by exclusive startup collection, before any
// upload can race the gap between publishing a file and registering its reference.
func (s *Store) ContentReferenced(ctx context.Context, digest string) (bool, error) {
	var referenced bool
	err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM content_references WHERE digest=?) OR EXISTS(SELECT 1 FROM cells WHERE json_extract(checkpoint,'$.digest')=?) OR EXISTS(SELECT 1 FROM state_versions WHERE digest=?)", digest, digest, digest).Scan(&referenced)
	return referenced, err
}

func (s *Store) PruneUnusedContent(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM content_bodies
 WHERE NOT EXISTS(SELECT 1 FROM content_references r WHERE r.digest=content_bodies.digest)
 AND NOT EXISTS(SELECT 1 FROM state_versions v WHERE v.digest=content_bodies.digest)`)
	return err
}
