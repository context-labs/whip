package session

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var (
	ErrHumanAlreadyPaired = errors.New("a human client is already paired")
	ErrIdentityExists     = errors.New("client identity already exists")
)

type ClientIdentity struct {
	ClientID  string
	Kind      string
	PublicKey []byte
	PairedBy  string
	CreatedAt time.Time
}

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

// PairFirstHuman atomically admits exactly one initial human identity.
func (s *Store) PairFirstHuman(ctx context.Context, identity ClientIdentity) error {
	if err := validateClientIdentity(identity); err != nil {
		return err
	}
	if identity.Kind == "automation" {
		return errors.New("automation cannot consume human enrollment")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var humans int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM client_identities WHERE kind<>'automation'`).Scan(&humans); err != nil {
		return err
	}
	if humans != 0 {
		return ErrHumanAlreadyPaired
	}
	if err := insertClientIdentity(ctx, tx, identity); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) PairClient(ctx context.Context, identity ClientIdentity) error {
	if err := validateClientIdentity(identity); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO client_identities(client_id,kind,public_key,paired_by,created_at) VALUES(?,?,?,?,?)`,
		identity.ClientID, identity.Kind, identity.PublicKey, identity.PairedBy, now())
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		if err != nil {
			return err
		}
		return ErrIdentityExists
	}
	return nil
}

func (s *Store) LoadClientIdentity(ctx context.Context, clientID string) (ClientIdentity, error) {
	var identity ClientIdentity
	var created string
	err := s.db.QueryRowContext(ctx, `SELECT client_id,kind,public_key,paired_by,created_at FROM client_identities WHERE client_id=?`, clientID).Scan(
		&identity.ClientID, &identity.Kind, &identity.PublicKey, &identity.PairedBy, &created)
	identity.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return identity, err
}

func (s *Store) HumanIdentityCount(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM client_identities WHERE kind<>'automation'`).Scan(&count)
	return count, err
}

type identityInserter interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertClientIdentity(ctx context.Context, tx identityInserter, identity ClientIdentity) error {
	result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO client_identities(client_id,kind,public_key,paired_by,created_at) VALUES(?,?,?,?,?)`,
		identity.ClientID, identity.Kind, identity.PublicKey, identity.PairedBy, now())
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		if err != nil {
			return err
		}
		return ErrIdentityExists
	}
	return nil
}

func validateClientIdentity(identity ClientIdentity) error {
	if identity.ClientID == "" || identity.Kind == "" || len(identity.PublicKey) != 32 {
		return errors.New("client identity requires ID, kind, and Ed25519 public key")
	}
	return nil
}
