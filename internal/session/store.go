package session

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync/atomic"

	_ "modernc.org/sqlite"

	"github.com/context-labs/whip/internal/capability"
	contentstore "github.com/context-labs/whip/internal/content"
)

type Store struct {
	db          *sql.DB
	content     *contentstore.Store
	workspaces  *capability.Workspaces
	daemonOwned atomic.Bool
	globalRules atomic.Pointer[[]string] // config permissions.allow, "operation:rule" entries
}

// AcquireDaemon is the in-process guard for one Daemon per Store. The runtime
// also holds the cross-process socket/file lock before constructing the Store.
func (s *Store) AcquireDaemon() bool { return s.daemonOwned.CompareAndSwap(false, true) }
func (s *Store) ReleaseDaemon()      { s.daemonOwned.Store(false) }

// Open opens (creating if needed) the sessions database at path and borrows workspaces.
func Open(path string, workspaces *capability.Workspaces) (*Store, error) {
	if workspaces == nil {
		return nil, errors.New("session store requires a workspace coordinator")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	failed := true
	defer func() {
		if failed {
			_ = db.Close()
		}
	}()
	if _, err := db.ExecContext(context.Background(), "PRAGMA busy_timeout=5000"); err != nil {
		return nil, err
	}
	if err := migrate(context.Background(), db, path); err != nil {
		return nil, err
	}
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",   // faster commits, no read/write blocking
		"PRAGMA synchronous=NORMAL", // safe in WAL; skips per-commit fsync
		"PRAGMA temp_store=MEMORY",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.ExecContext(context.Background(), pragma); err != nil {
			return nil, err
		}
	}
	content, err := contentstore.New(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	store := &Store{
		db: db, content: content,
		workspaces: workspaces,
	}
	failed = false
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }
