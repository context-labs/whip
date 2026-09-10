package session

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func versionThirteenDatabase(t *testing.T) (string, *sql.DB) {
	t.Helper()
	path, db := versionTwelveDatabase(t)
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := upgradeV12(t.Context(), conn); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `
		UPDATE sessions SET permission_mode='automatic' WHERE id='saved-root';
		INSERT INTO agents(id,root_id,parent_id,name,status,created_at,updated_at)
		VALUES('saved-root','saved-root',NULL,'root','idle','created','updated');
		INSERT INTO agents(id,root_id,parent_id,name,status,created_at,updated_at)
		VALUES('saved-child','saved-root','saved-root','child','idle','created','updated');`); err != nil {
		t.Fatal(err)
	}
	return path, db
}

func TestVersionThirteenUpgradePreservesAuthorityAndSessionState(t *testing.T) {
	path, db := versionThirteenDatabase(t)
	const operations = `["read","write","edit","workspace.write"]`
	const rootScopes = `{"paths":["/original/project"],"expires_at":"2999-01-01T00:00:00Z","retained":{"unknown":true}}`
	const childScopes = `{"paths":["/original/project"],"expires_at":"2999-01-01T00:00:00Z"}`
	if _, err := db.ExecContext(t.Context(), `
		INSERT INTO capabilities VALUES('files:saved-root','saved-root','saved-root','',?1,?2,7,'active','first-created','last-updated');
		INSERT INTO capabilities VALUES('child-files','saved-root','saved-child','saved-root',?1,?3,9,'active','first-created','last-updated');
		INSERT INTO budgets(root_id,agent_id,kind,limit_value,used_value,reserved_value,updated_at)
		VALUES('saved-root','saved-child','operations',20,5,2,'budget-updated');`, operations, rootScopes, childScopes); err != nil {
		t.Fatal(err)
	}
	var beforeEvents, beforeLastEvent, beforeCatalog int64
	if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*),MAX(seq) FROM events`).Scan(&beforeEvents, &beforeLastEvent); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), `SELECT catalog_revision FROM runtime_schema`).Scan(&beforeCatalog); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	var firstCollection int64
	for attempt := range 2 {
		store, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		var scopes, ops, status, created, updated string
		var generation int64
		if err := store.db.QueryRowContext(t.Context(), `SELECT scopes,operations,generation,status,created_at,updated_at
			FROM capabilities WHERE id='files:saved-root'`).Scan(&scopes, &ops, &generation, &status, &created, &updated); err != nil {
			t.Fatal(err)
		}
		if ops != operations || generation != 7 || status != "active" || created != "first-created" || updated != "last-updated" {
			t.Fatalf("root authority changed: %s %d %s %s %s", ops, generation, status, created, updated)
		}
		var fields, original map[string]json.RawMessage
		if err := json.Unmarshal([]byte(scopes), &fields); err != nil {
			t.Fatal(err)
		}
		if string(fields["file_scope"]) != `"session"` {
			t.Fatalf("root does not follow saved session mode: %s", scopes)
		}
		delete(fields, "file_scope")
		if err := json.Unmarshal([]byte(rootScopes), &original); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(fields, original) {
			t.Fatalf("root scope metadata changed: %s", scopes)
		}
		if err := store.db.QueryRowContext(t.Context(), `SELECT scopes,generation FROM capabilities WHERE id='child-files'`).Scan(&scopes, &generation); err != nil {
			t.Fatal(err)
		}
		if scopes != childScopes || generation != 9 {
			t.Fatalf("ambiguous child authority broadened: %s generation %d", scopes, generation)
		}
		var mode, cwd, runtime, identity string
		var collection, catalog, count, last, limit, used, reserved int64
		if err := store.db.QueryRowContext(t.Context(), `SELECT permission_mode,cwd,collection_revision FROM sessions WHERE id='saved-root'`).Scan(&mode, &cwd, &collection); err != nil {
			t.Fatal(err)
		}
		if mode != PermissionModeAutomatic || cwd != "/project/界" {
			t.Fatalf("session state changed: mode=%s cwd=%s", mode, cwd)
		}
		if attempt == 0 {
			firstCollection = collection
		} else if collection != firstCollection {
			t.Fatalf("reopen repeated migration: revision %d became %d", firstCollection, collection)
		}
		if err := store.db.QueryRowContext(t.Context(), `SELECT identity,runtime_id,catalog_revision FROM runtime_schema`).Scan(&identity, &runtime, &catalog); err != nil {
			t.Fatal(err)
		}
		if identity != schemaIdentity || runtime != "persistent-runtime" || catalog != beforeCatalog {
			t.Fatalf("runtime state changed: %s %s %d", identity, runtime, catalog)
		}
		if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*),MAX(seq) FROM events`).Scan(&count, &last); err != nil {
			t.Fatal(err)
		}
		if count != beforeEvents || last != beforeLastEvent {
			t.Fatalf("event history changed: count=%d last=%d", count, last)
		}
		if err := store.db.QueryRowContext(t.Context(), `SELECT limit_value,used_value,reserved_value FROM budgets
			WHERE root_id='saved-root' AND agent_id='saved-child' AND kind='operations'`).Scan(&limit, &used, &reserved); err != nil {
			t.Fatal(err)
		}
		if limit != 20 || used != 5 || reserved != 2 {
			t.Fatalf("budget changed: %d/%d/%d", limit, used, reserved)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestVersionThirteenUpgradeOnlyTagsKnownBootstrapGrants(t *testing.T) {
	const operations = `["read","write","edit","workspace.write"]`
	const scopes = `{"paths":["/original/project"]}`
	for _, test := range []struct {
		name, id, agent, issuer, ops, scopes, status string
		wantSession                                  bool
	}{
		{name: "active", status: "active", wantSession: true},
		{name: "revoked", status: "revoked", wantSession: true},
		{name: "expired", scopes: `{"paths":["/original/project"],"expires_at":"2000-01-01T00:00:00Z"}`, wantSession: true},
		{name: "reordered operations", ops: `["write","workspace.write","edit","read"]`, wantSession: true},
		{name: "explicit root grant", id: "explicit-files"},
		{name: "child with root-shaped identity", agent: "saved-child"},
		{name: "issued root grant", issuer: "saved-root"},
		{name: "read-only operations", ops: `["read"]`},
		{name: "duplicate operations", ops: `["read","read","edit","workspace.write"]`},
		{name: "malformed operations", ops: `invalid`},
		{name: "malformed scopes", scopes: `invalid`},
		{name: "missing paths", scopes: `{}`},
		{name: "empty paths", scopes: `{"paths":[]}`},
		{name: "empty path", scopes: `{"paths":[""]}`},
		{name: "relative path", scopes: `{"paths":["project"]}`},
		{name: "unclean path", scopes: `{"paths":["/original/../project"]}`},
		{name: "nul path", scopes: `{"paths":["/project\u0000"]}`},
		{name: "multiple paths", scopes: `{"paths":["/original/project","/another/project"]}`},
		{name: "existing semantics", scopes: `{"paths":["/original/project"],"file_scope":"inherit"}`},
		{name: "existing issuer", scopes: `{"paths":["/original/project"],"file_issuer_id":"prior"}`},
		{name: "malformed expiry", scopes: `{"paths":["/original/project"],"expires_at":23}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			path, db := versionThirteenDatabase(t)
			if test.id == "" {
				test.id = "files:saved-root"
			}
			if test.agent == "" {
				test.agent = "saved-root"
			}
			if test.ops == "" {
				test.ops = operations
			}
			if test.scopes == "" {
				test.scopes = scopes
			}
			if test.status == "" {
				test.status = "active"
			}
			if _, err := db.ExecContext(t.Context(), `INSERT INTO capabilities VALUES(?, 'saved-root', ?, ?, ?, ?, 4, ?, 'created', 'updated')`,
				test.id, test.agent, test.issuer, test.ops, test.scopes, test.status); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			store, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			var actual, status string
			var generation int64
			if err := store.db.QueryRowContext(t.Context(), `SELECT scopes,status,generation FROM capabilities WHERE id=?`, test.id).Scan(&actual, &status, &generation); err != nil {
				t.Fatal(err)
			}
			if status != test.status || generation != 4 {
				t.Fatalf("grant lifecycle changed: status=%s generation=%d", status, generation)
			}
			if !test.wantSession {
				if actual != test.scopes {
					t.Fatalf("ambiguous grant changed from %s to %s", test.scopes, actual)
				}
				return
			}
			var got, want map[string]any
			if err := json.Unmarshal([]byte(actual), &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(test.scopes), &want); err != nil {
				t.Fatal(err)
			}
			want["file_scope"] = "session"
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("scopes=%s want=%v", actual, want)
			}
		})
	}
}

func TestVersionThirteenUpgradeRollsBackAndRetries(t *testing.T) {
	path, db := versionThirteenDatabase(t)
	const scopes = `{"paths":["/original/project"]}`
	if _, err := db.ExecContext(t.Context(), `
		INSERT INTO capabilities VALUES('files:saved-root','saved-root','saved-root','',
		'["read","write","edit","workspace.write"]',?,2,'active','created','updated');
		CREATE TRIGGER reject_file_upgrade BEFORE UPDATE ON runtime_schema
		BEGIN SELECT RAISE(ABORT,'upgrade failed'); END;`, scopes); err != nil {
		t.Fatal(err)
	}
	if store, err := Open(path); err == nil {
		_ = store.Close()
		t.Fatal("upgrade unexpectedly succeeded")
	}
	var version int
	var identity, actual string
	if err := db.QueryRowContext(t.Context(), `PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), `SELECT identity FROM runtime_schema`).Scan(&identity); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), `SELECT scopes FROM capabilities WHERE id='files:saved-root'`).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if version != 13 || identity != "whip-recursive-runtime-v13" || actual != scopes {
		t.Fatalf("partial migration: version=%d identity=%s scopes=%s", version, identity, actual)
	}
	if _, err := db.ExecContext(t.Context(), `DROP TRIGGER reject_file_upgrade`); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.db.QueryRowContext(t.Context(), `SELECT json_extract(scopes,'$.file_scope') FROM capabilities WHERE id='files:saved-root'`).Scan(&actual); err != nil || actual != "session" {
		t.Fatalf("retried migration scope=%s error=%v", actual, err)
	}
}

func TestEarlierUpgradesTolerateAnotherOpenerFinishing(t *testing.T) {
	for _, finishedVersion := range []int{13, 14} {
		t.Run(fmt.Sprintf("v%d", finishedVersion), func(t *testing.T) {
			path, db := versionThirteenDatabase(t)
			conn, err := db.Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if finishedVersion == 14 {
				if err := upgradeV13(t.Context(), conn); err != nil {
					t.Fatal(err)
				}
			}
			// Each step rechecks under its write lock. An opener that previously
			// observed an older schema can safely resume after another finishes.
			if err := upgradeV10(t.Context(), conn); err != nil {
				t.Fatal(err)
			}
			if err := upgradeV11(t.Context(), conn, path); err != nil {
				t.Fatal(err)
			}
			if err := upgradeV12(t.Context(), conn); err != nil {
				t.Fatal(err)
			}
			if err := upgradeV13(t.Context(), conn); err != nil {
				t.Fatal(err)
			}
		})
	}
}
