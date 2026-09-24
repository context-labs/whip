package session

import (
	"context"
	"errors"
)

// BeginDaemonGeneration records one exclusive daemon start. Callers must hold
// the cross-process owner lock before opening the Store and invoking it.
func (s *Store) BeginDaemonGeneration(ctx context.Context, buildID string) (int64, error) {
	if buildID == "" {
		return 0, errors.New("daemon build ID is required")
	}
	var generation int64
	err := s.db.QueryRowContext(ctx, `INSERT INTO daemon_state(id,generation,build_id,status,updated_at)
		VALUES(1,1,?,'running',?) ON CONFLICT(id) DO UPDATE SET
		generation=daemon_state.generation+1,build_id=excluded.build_id,status='running',updated_at=excluded.updated_at
		RETURNING generation`, buildID, now()).Scan(&generation)
	return generation, err
}

func (s *Store) DaemonGeneration(ctx context.Context) (generation int64, buildID, status string, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT generation,build_id,status FROM daemon_state WHERE id=1`).Scan(&generation, &buildID, &status)
	return
}

func (s *Store) SetDaemonStatus(ctx context.Context, generation int64, status string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE daemon_state SET status=?,updated_at=? WHERE id=1 AND generation=?`, status, now(), generation)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		if err != nil {
			return err
		}
		return errors.New("daemon generation is no longer current")
	}
	return nil
}

// RuntimeID identifies this database independently of daemon generations or builds.
func (s *Store) RuntimeID(ctx context.Context) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT runtime_id FROM runtime_schema WHERE id=1`).Scan(&id)
	return id, err
}
