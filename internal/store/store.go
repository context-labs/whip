// Package store persists the session domain. Opening it owns only a database;
// execution, recovery scheduling, credentials and resource managers live elsewhere.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/context-labs/whip/internal/session"
	"modernc.org/sqlite"
)

const (
	applicationID = 0x57504834
	schemaVersion = 40
)

// MaxPageBytes bounds hydrated collection responses as well as their row count.
// Cursors resume after the last returned item when the byte limit ends a page.
const MaxPageBytes = 4 << 20

//go:embed schema.sql
var schema string

var (
	ErrNotFound = errors.New("record not found")
	ErrConflict = errors.New("conflicting revision or request")
	ErrBusy     = errors.New("session has an active turn")
	ErrNoWork   = errors.New("no queued input")
	ErrStopped  = errors.New("session is stopped")
	ErrLimit    = errors.New("admission limit reached")
	ErrSchema   = errors.New("unsupported database identity or version")
)

type Store struct {
	db       *sql.DB
	identity session.RuntimeID
}

// Open accepts a dedicated database path. It rejects existing foreign schemas,
// initializes fresh storage atomically, and never starts or recovers execution.
func Open(ctx context.Context, path string) (_ *Store, err error) {
	if path == "" {
		return nil, fmt.Errorf("%w: database path is required", session.ErrInvalid)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	//nolint:gosec // The host explicitly selects its dedicated database path; this is not a remote request path.
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err == nil {
		if err := file.Close(); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	uri := url.URL{Scheme: "file", Path: path}
	query := uri.Query()
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "synchronous(FULL)")
	query.Set("_txlock", "immediate")
	uri.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	// One connection per handle bounds memory and avoids in-process writer
	// contention. SQLite still arbitrates independent handles/processes.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{db: db}
	defer func() {
		if err != nil {
			err = errors.Join(err, db.Close())
		}
	}()
	err = s.write(ctx, func(tx *sql.Tx) error {
		var app, version, count int
		if err := tx.QueryRowContext(ctx, "PRAGMA application_id").Scan(&app); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'").Scan(&count); err != nil {
			return err
		}
		if app == 0 && version == 0 && count == 0 {
			if _, err := tx.ExecContext(ctx, schema); err != nil {
				return fmt.Errorf("initialize schema: %w", err)
			}
			if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA application_id=%d; PRAGMA user_version=%d", applicationID, schemaVersion)); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO metadata VALUES (?)", newID("runtime")); err != nil {
				return err
			}
		} else if app != applicationID || version != schemaVersion {
			return ErrSchema
		}
		if err := tx.QueryRowContext(ctx, "SELECT runtime_id FROM metadata").Scan(&s.identity); err != nil {
			return err
		}
		for _, builtin := range session.Builtins() {
			if _, err := registerDefinition(ctx, tx, builtin); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := enableWAL(ctx, db); err != nil {
		return nil, fmt.Errorf("configure journal: %w", err)
	}
	return s, nil
}

// Switching journal modes can return BUSY immediately during concurrent opens,
// even with busy_timeout. Retry only this idempotent setup statement, never an
// execution transaction or a provider/effect dispatch.
func enableWAL(ctx context.Context, db *sql.DB) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		var mode string
		err := db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&mode)
		if err == nil {
			if mode != "wal" {
				return fmt.Errorf("unexpected journal mode %q", mode)
			}
			return nil
		}
		var sqliteErr *sqlite.Error
		if !errors.As(err, &sqliteErr) || sqliteErr.Code()&0xff != 5 {
			return err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *Store) Close() error                { return s.db.Close() }
func (s *Store) Identity() session.RuntimeID { return s.identity }
func newID(kind string) string               { return kind + "_" + rand.Text() }
func now() int64                             { return time.Now().UTC().UnixMicro() }
func timestamp(value int64) time.Time        { return time.UnixMicro(value).UTC() }
func optionalTime(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	t := timestamp(value.Int64)
	return &t
}

func (s *Store) write(ctx context.Context, fn func(*sql.Tx) error) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("rollback: %w", rollbackErr))
		}
	}()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

type querier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func found(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
func encode(value any) (string, error) { raw, err := json.Marshal(value); return string(raw), err }
func pageLimit(limit int) error {
	if limit < 1 || limit > 100 {
		return fmt.Errorf("%w: page limit must be 1–100", session.ErrInvalid)
	}
	return nil
}

func registerDefinition(ctx context.Context, tx *sql.Tx, document session.DefinitionDocument) (session.DefinitionRevision, error) {
	_, raw, ref, err := session.CanonicalDefinition(document)
	if err != nil {
		return session.DefinitionRevision{}, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO definition_revisions VALUES (?,?,?,?) ON CONFLICT DO NOTHING", ref.ID, ref.Revision, string(raw), now()); err != nil {
		return session.DefinitionRevision{}, err
	}
	return definition(ctx, tx, ref)
}

func definition(ctx context.Context, q querier, ref session.DefinitionRef) (session.DefinitionRevision, error) {
	var result session.DefinitionRevision
	var raw string
	var created int64
	err := q.QueryRowContext(ctx, "SELECT document,created_at FROM definition_revisions WHERE id=? AND revision=?", ref.ID, ref.Revision).Scan(&raw, &created)
	if err != nil {
		return result, found(err)
	}
	result.Ref, result.CreatedAt = ref, timestamp(created)
	err = json.Unmarshal([]byte(raw), &result.Document)
	return result, err
}

func (s *Store) RegisterDefinition(ctx context.Context, doc session.DefinitionDocument) (result session.DefinitionRevision, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error { var err error; result, err = registerDefinition(ctx, tx, doc); return err })
	return
}

func (s *Store) Definition(ctx context.Context, ref session.DefinitionRef) (session.DefinitionRevision, error) {
	return definition(ctx, s.db, ref)
}

type DefinitionCursor struct{ ID, Revision string }

func (s *Store) Definitions(ctx context.Context, after DefinitionCursor, limit int) ([]session.DefinitionRevision, error) {
	if err := pageLimit(limit); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id,revision,document,created_at FROM definition_revisions WHERE (id,revision)>(?,?) ORDER BY id,revision LIMIT ?", after.ID, after.Revision, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []session.DefinitionRevision{}
	size := 0
	for rows.Next() {
		var d session.DefinitionRevision
		var raw string
		var created int64
		if err := rows.Scan(&d.Ref.ID, &d.Ref.Revision, &raw, &created); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &d.Document); err != nil {
			return nil, err
		}
		d.CreatedAt = timestamp(created)
		size += len(raw) + 512
		if size > MaxPageBytes {
			break
		}
		result = append(result, d)
	}
	return result, rows.Err()
}
