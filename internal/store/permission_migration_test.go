package store

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestChildPermissionMigrationPreservesDataWithoutBackfill(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	root, _ := operationCell(t, s)
	setModeTest(t, s, root.ID, "automatic", 1, session.PermissionAutomatic)
	child := spawnChildTest(t, s, "existing-child", childRequest(root.ID))
	// Restore the exact schema55 layout, retaining ordinary session/input data.
	execTest(t, s, "DROP TABLE child_permission_policies; PRAGMA user_version=55")
	identity := s.Identity()
	reopened := openTest(t, path)
	if reopened.Identity() != identity {
		t.Fatal("upgrade changed runtime identity")
	}
	var version int
	if err := reopened.db.QueryRowContext(t.Context(), "PRAGMA user_version").Scan(&version); err != nil || version != 56 {
		t.Fatal("upgrade did not advance version", version, err)
	}
	if count(t, reopened, "child_permission_policies") != 0 || count(t, reopened, "sessions") != 2 {
		t.Fatal("upgrade backfilled authority or changed existing sessions")
	}
	retry := spawnChildTest(t, reopened, "existing-child", childRequest(root.ID))
	if !reflect.DeepEqual(child, retry) {
		t.Fatal("upgrade changed admission receipt", child, retry)
	}
	cell := childOperationCell(t, reopened, child.Session.ID)
	denied := admitOperation(t, reopened, delegatedReadSpec(*child.Session, cell, "old-child-read"))
	if denied.State != session.OperationDenied || denied.PermissionRevision != nil {
		t.Fatal("historical child gained authority", denied)
	}
	newChild := spawnChildTest(t, reopened, "new-child", childRequest(root.ID))
	newCell := childOperationCell(t, reopened, newChild.Session.ID)
	ready := admitOperation(t, reopened, delegatedReadSpec(*newChild.Session, newCell, "new-child-read"))
	if ready.State != session.OperationReady || ready.PermissionRevision == nil {
		t.Fatal("new child did not capture policy after migration", ready)
	}
	if allowed, err := openTest(t, path).DispatchOperation(t.Context(), ready.ID); err != nil || !allowed {
		t.Fatal("repeated open changed migrated delegation", allowed, err)
	}
	var integrity string
	if err := reopened.db.QueryRowContext(t.Context(), "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatal(integrity, err)
	}
}

func TestChildPermissionMigrationFailureIsAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	execTest(t, s, "DROP TABLE child_permission_policies; PRAGMA user_version=55")
	// Fail after CREATE TABLE, proving partial DDL and the version cannot survive.
	execTest(t, s, `CREATE TRIGGER child_permission_policy_immutable BEFORE UPDATE ON metadata
 BEGIN SELECT RAISE(ABORT, 'test collision'); END`)
	if migrated, err := Open(t.Context(), path); err == nil {
		_ = migrated.Close()
		t.Fatal("upgrade accepted a conflicting schema object")
	}
	var version, count int
	if err := s.db.QueryRowContext(t.Context(), "PRAGMA user_version").Scan(&version); err != nil || version != 55 {
		t.Fatal("failed upgrade changed version", version, err)
	}
	if err := s.db.QueryRowContext(t.Context(), "SELECT count(*) FROM sqlite_schema WHERE name='child_permission_policies'").Scan(&count); err != nil || count != 0 {
		t.Fatal("failed upgrade retained partial DDL", count, err)
	}
	execTest(t, s, "DROP TRIGGER child_permission_policy_immutable")
	if openTest(t, path).Identity() != s.Identity() {
		t.Fatal("retry changed runtime identity")
	}
}
