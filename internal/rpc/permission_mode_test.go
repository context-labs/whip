package rpc_test

import (
	"crypto/rand"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func TestPermissionModeRPCExactReceiptsStoppedEditingAndHostDefaults(t *testing.T) {
	r, c := fixture(t)
	first := create(t, c)
	before := call[protocol.DefaultPermissionMode](t, c, "host.permission_default", protocol.EmptyParams{})
	if before.Mode != "prompt" {
		t.Fatal("default is not Ask", before)
	}
	automatic := call[protocol.DefaultPermissionMode](t, c, "host.set_permission_default", protocol.SetDefaultPermissionModeParams{ExpectedRevision: before.Revision, Mode: "automatic"})
	if automatic.Mode != "automatic" || automatic.Revision == before.Revision {
		t.Fatal("host edit did not publish", automatic)
	}
	next := create(t, c)
	for _, test := range []struct {
		owner protocol.ID
		mode  string
	}{{first.Root.ID, "prompt"}, {next.Root.ID, "automatic"}} {
		policy := call[protocol.PermissionPolicy](t, c, "permissions.policy", protocol.SessionParams{SessionID: test.owner})
		if policy.Mode != test.mode || policy.Revision != 1 {
			t.Fatal("host default mutated existing root or missed new root", policy)
		}
	}
	explicit := call[protocol.CreateTreeResult](t, c, "trees.create", protocol.CreateTreeParams{CreationID: protocol.ID(rand.Text()), PermissionMode: new("prompt"), Engine: "starlark", Definition: c.Builtins()[0], WorkingDirectory: t.TempDir(), Overrides: protocol.ConfigPatch{Model: &protocol.ModelSelection{Provider: "scripted", Name: "scripted"}}})
	if policy := call[protocol.PermissionPolicy](t, c, "permissions.policy", protocol.SessionParams{SessionID: explicit.Root.ID}); policy.Mode != "prompt" {
		t.Fatal("explicit mode was ignored", policy)
	}
	requireHistoryError(t, c, "host.set_permission_default", protocol.SetDefaultPermissionModeParams{ExpectedRevision: before.Revision, Mode: "prompt"}, "CONFLICT")
	for _, mode := range []string{"", "Prompt", "full_access", " automatic"} {
		var rejected protocol.DefaultPermissionMode
		if err := c.Call(t.Context(), "host.set_permission_default", protocol.SetDefaultPermissionModeParams{ExpectedRevision: automatic.Revision, Mode: mode}, &rejected); err == nil {
			t.Fatal("invalid mode passed the Go client contract", mode)
		}
	}
	if again := call[protocol.DefaultPermissionMode](t, c, "host.permission_default", protocol.EmptyParams{}); again != automatic {
		t.Fatal("failed host edits changed file", again)
	}
	// Seed an otherwise ordinary policy above the JS safe-integer range. The
	// production mutation still exercises its revision guard and socket encoding.
	db, err := sql.Open("sqlite", filepath.Join(filepath.Dir(r.SocketPath()), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(t.Context(), "DELETE FROM permission_policies WHERE tree_id=?", first.Tree.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(), "INSERT INTO permission_policies (tree_id,mode,revision,updated_at) VALUES (?,'prompt',9007199254740993,1)", first.Tree.ID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	params := protocol.SetPermissionModeParams{EditID: "Edit.Mixed-Case", SessionID: first.Root.ID, ExpectedRevision: 9007199254740993, Mode: "automatic"}
	edited := call[protocol.PermissionModeEdit](t, c, "permissions.set_mode", params)
	if edited.ID != params.EditID || edited.ExpectedRevision != 9007199254740993 || edited.Policy.Revision != 9007199254740994 || edited.PreviousMode != "prompt" {
		t.Fatal("receipt lost exact identity", edited)
	}
	same := params
	same.EditID, same.ExpectedRevision = "Same.Mixed-Case", edited.Policy.Revision
	sameResult := call[protocol.PermissionModeEdit](t, c, "permissions.set_mode", same)
	if sameResult.Policy != edited.Policy {
		t.Fatal("same mode changed revision", sameResult)
	}
	call[protocol.Session](t, c, "sessions.lifecycle", protocol.LifecycleParams{SessionID: first.Root.ID, Lifecycle: "stopped"})
	later := protocol.SetPermissionModeParams{EditID: "later", SessionID: first.Root.ID, ExpectedRevision: edited.Policy.Revision, Mode: "prompt"}
	stopped := call[protocol.PermissionModeEdit](t, c, "permissions.set_mode", later)
	if stopped.Policy.Mode != "prompt" || stopped.Policy.Revision != 9007199254740995 {
		t.Fatal("stopped root cannot change policy", stopped)
	}
	for _, expected := range []struct {
		request protocol.SetPermissionModeParams
		result  protocol.PermissionModeEdit
	}{{params, edited}, {same, sameResult}} {
		if retry := call[protocol.PermissionModeEdit](t, c, "permissions.set_mode", expected.request); !reflect.DeepEqual(retry, expected.result) {
			t.Fatal("retry reinterpreted original result", retry)
		}
	}
	if original := call[protocol.PermissionModeEdit](t, c, "permissions.mode_edit", protocol.PermissionModeEditParams{SessionID: first.Root.ID, EditID: params.EditID}); original != edited {
		t.Fatal("receipt read changed", original)
	}
	requireHistoryError(t, c, "permissions.mode_edit", protocol.PermissionModeEditParams{SessionID: next.Root.ID, EditID: params.EditID}, "NOT_FOUND")
	params.Mode = "prompt"
	requireHistoryError(t, c, "permissions.set_mode", params, "CONFLICT")
}
