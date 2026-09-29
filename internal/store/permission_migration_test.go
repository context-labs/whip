package store

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestChildPermissionMigrationRestoresOnlyProvenDefaultSpawns(t *testing.T) {
	for _, version := range []int{55, 56} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "runtime.db")
			s := openTest(t, path)
			root, cell := operationCell(t, s)
			grant, err := s.CreateGrant(t.Context(), session.Grant{
				ID: "spawn", SessionID: root.ID, Capability: "agents.spawn", Resource: string(root.TreeID),
			})
			if err != nil {
				t.Fatal(err)
			}
			var inherited, restricted []session.Session
			for _, selection := range []string{"default", "empty", "subset", "different-workspace"} {
				request := childRequest(root.ID)
				switch selection {
				case "empty":
					request.GrantIDs = []session.GrantID{}
				case "subset":
					request.GrantIDs = []session.GrantID{grant.ID}
				case "different-workspace":
					request.WorkingDirectory = t.TempDir()
				}
				raw, err := json.Marshal(request)
				if err != nil {
					t.Fatal(err)
				}
				op := admitOperation(t, s, session.OperationSpec{
					ID: session.OperationID(selection), CellID: cell.ID, RequestID: selection,
					Capability: grant.Capability, Resource: grant.Resource, Arguments: raw,
				})
				child, err := s.SpawnChildOperation(t.Context(), op.ID)
				if err != nil || child.Session == nil {
					t.Fatal(child, err)
				}
				if selection == "default" {
					inherited = append(inherited, *child.Session)
				} else {
					restricted = append(restricted, *child.Session)
				}
			}
			unknown := spawnChildTest(t, s, "client-without-original-request", childRequest(root.ID))
			restricted = append(restricted, *unknown.Session)
			// Ask denied this operation. Restoring inheritance must not replay it.
			childCell := childOperationCell(t, s, inherited[0].ID)
			old := admitOperation(t, s, delegatedReadSpec(inherited[0], childCell, "old-read"))
			if old.State != session.OperationDenied {
				t.Fatal(old)
			}
			execTest(t, s, "DELETE FROM child_permission_policies")
			if version == 55 {
				execTest(t, s, "DROP TABLE child_permission_policies")
			}
			execTest(t, s, fmt.Sprintf("PRAGMA user_version=%d", version))
			reopened := openTest(t, path)
			if reopened.Identity() != s.Identity() || count(t, reopened, "child_permission_policies") != len(inherited) {
				t.Fatal("migration changed identity or guessed delegation")
			}
			setModeTest(t, reopened, root.ID, "enable", 1, session.PermissionAutomatic)
			if allowed, err := reopened.DispatchOperation(t.Context(), old.ID); err != nil || allowed {
				t.Fatal("migration replayed an old denied operation", allowed, err)
			}
			next := admitOperation(t, reopened, delegatedReadSpec(inherited[0], childCell, "new-read"))
			if next.State != session.OperationReady || next.PermissionRevision == nil || *next.PermissionRevision != 2 {
				t.Fatal("default child did not regain live inheritance", next)
			}
			for _, child := range restricted {
				cell := childOperationCell(t, reopened, child.ID)
				if read := admitOperation(t, reopened, delegatedReadSpec(child, cell, "read-"+string(child.ID))); read.State != session.OperationDenied || read.PermissionRevision != nil {
					t.Fatal("migration widened a restricted or unknown child", read)
				}
			}
			if count(t, reopened, "inputs") != count(t, s, "inputs") || count(t, reopened, "permissions") != 0 {
				t.Fatal("migration admitted work or a permission request")
			}
		})
	}
}

func TestChildPermissionMigrationPreservesDataWithoutGuessingClientGrantSelections(t *testing.T) {
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
	if err := reopened.db.QueryRowContext(t.Context(), "PRAGMA user_version").Scan(&version); err != nil || version != schemaVersion {
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
