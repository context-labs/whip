package session

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// DefinitionRecord is one registered agent definition revision. The store keeps
// the document bytes; the daemon owns their meaning and validation.
type DefinitionRecord struct {
	ID           string    `json:"id"`
	Revision     string    `json:"revision"`
	Body         []byte    `json:"body"`
	RegisteredBy string    `json:"registered_by"`
	CreatedAt    time.Time `json:"created_at"`
}

// ErrNoDefinition means no registered definition matches the id and revision.
var ErrNoDefinition = errors.New("registered agent definition not found")

// RegisterDefinition stores one revision of a definition. Registering the same
// id and revision again is a no-op that reports created=false, so retries and
// redeploys of an unchanged agent are idempotent.
func (s *Store) RegisterDefinition(ctx context.Context, id, revision string, body []byte, registeredBy string) (bool, error) {
	if id == "" || revision == "" || len(body) == 0 {
		return false, errors.New("definition registration requires id, revision, and body")
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO definitions(id,revision,body,registered_by,created_at) VALUES(?,?,?,?,?)
		ON CONFLICT(id,revision) DO NOTHING`, id, revision, body, registeredBy, now())
	if err != nil {
		return false, err
	}
	inserted, err := result.RowsAffected()
	return inserted == 1, err
}

// LoadDefinition returns one exact revision.
func (s *Store) LoadDefinition(ctx context.Context, id, revision string) (DefinitionRecord, error) {
	return scanDefinition(s.db.QueryRowContext(ctx, `SELECT id,revision,body,registered_by,created_at FROM definitions WHERE id=? AND revision=?`, id, revision))
}

// LatestDefinition returns the most recently registered revision of an id.
func (s *Store) LatestDefinition(ctx context.Context, id string) (DefinitionRecord, error) {
	return scanDefinition(s.db.QueryRowContext(ctx, `SELECT id,revision,body,registered_by,created_at FROM definitions WHERE id=? ORDER BY created_at DESC, revision DESC LIMIT 1`, id))
}

// ListDefinitions returns the latest revision of every registered id.
func (s *Store) ListDefinitions(ctx context.Context) ([]DefinitionRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT d.id,d.revision,d.body,d.registered_by,d.created_at FROM definitions d
		WHERE d.created_at||d.revision = (SELECT MAX(o.created_at||o.revision) FROM definitions o WHERE o.id=d.id) ORDER BY d.id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var records []DefinitionRecord
	for rows.Next() {
		record, err := scanDefinition(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

type rowScanner interface{ Scan(...any) error }

func scanDefinition(row rowScanner) (DefinitionRecord, error) {
	var record DefinitionRecord
	var created string
	err := row.Scan(&record.ID, &record.Revision, &record.Body, &record.RegisteredBy, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return DefinitionRecord{}, ErrNoDefinition
	}
	if err != nil {
		return DefinitionRecord{}, err
	}
	record.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return record, nil
}
