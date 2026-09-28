package rpc_test

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

func TestWorkspaceRPCScopedOverlayReceiptsAndPrivateMetadata(t *testing.T) {
	_, c := fixture(t)
	directory := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		command := exec.CommandContext(t.Context(), "git", args...)
		command.Dir = directory
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("fixture git %v: %v: %s", args, err, out)
		}
		return strings.TrimSuffix(string(out), "\n")
	}
	write := func(path, value string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(directory, path), []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	read := func(path string) string {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(directory, path))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	errorKind := func(method string, params any, kind string) {
		t.Helper()
		var out json.RawMessage
		err := c.Call(t.Context(), method, params, &out)
		var remote *client.Error
		if !errors.As(err, &remote) || remote.Kind != kind {
			t.Fatalf("%s got %v, expected %s", method, err, kind)
		}
	}
	git("init", "-q")
	git("config", "user.name", "Test")
	git("config", "user.email", "test@localhost")
	if err := os.Mkdir(filepath.Join(directory, "scope [literal] "), 0o700); err != nil {
		t.Fatal(err)
	}
	tracked := "scope [literal] /tracked"
	write(tracked, "base")
	write("outside", "base")
	git("add", ".")
	git("commit", "-qm", "base")
	write(tracked, "captured")
	tree := call[protocol.CreateTreeResult](t, c, "trees.create", protocol.CreateTreeParams{Engine: "starlark", Definition: c.Builtins()[0], WorkingDirectory: filepath.Join(directory, "scope [literal] "), Overrides: protocol.ConfigPatch{Model: &protocol.ModelSelection{Provider: "scripted", Name: "scripted"}}})
	params := protocol.WorkspaceActionParams{ActionID: "capture", SnapshotID: "snapshot-a", SessionID: tree.Root.ID}
	first := call[protocol.WorkspaceResult](t, c, "workspace.capture", params)
	if first.Action.State != "succeeded" || first.Snapshot.Scope != "session_working_directory" || first.Snapshot.Semantics == "" || first.Snapshot.ReleasedAt != nil {
		t.Fatal(first)
	}
	var public json.RawMessage
	if err := c.Call(t.Context(), "workspace.snapshot", protocol.WorkspaceSnapshotParams{SessionID: tree.Root.ID, SnapshotID: params.SnapshotID}, &public); err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{directory, "scope [literal]", "object_id", "binding", "worktree"} {
		if strings.Contains(string(public), private) {
			t.Fatal("host identity leaked", string(public))
		}
	}
	second := params
	second.ActionID, second.SnapshotID = "capture-b", "snapshot-b"
	call[protocol.WorkspaceResult](t, c, "workspace.capture", second)
	page := call[protocol.WorkspaceSnapshotsResult](t, c, "workspace.snapshots", protocol.WorkspaceSnapshotsParams{SessionID: tree.Root.ID, Limit: 1})
	if len(page.Items) != 1 || page.Items[0].ID != params.SnapshotID {
		t.Fatal(page)
	}
	page = call[protocol.WorkspaceSnapshotsResult](t, c, "workspace.snapshots", protocol.WorkspaceSnapshotsParams{SessionID: tree.Root.ID, After: page.Items[0].ID, Limit: 1})
	if len(page.Items) != 1 || page.Items[0].ID != second.SnapshotID {
		t.Fatal(page)
	}
	for _, method := range []string{"workspace.restore", "workspace.release"} {
		errorKind(method, params, "CONFLICT")
	}
	errorKind("workspace.action", protocol.ReadWorkspaceActionParams{SessionID: "other", ActionID: params.ActionID}, "NOT_FOUND")
	errorKind("workspace.snapshot", protocol.WorkspaceSnapshotParams{SessionID: "other", SnapshotID: params.SnapshotID}, "NOT_FOUND")
	var invalid protocol.WorkspaceSnapshotsResult
	if err := c.Call(t.Context(), "workspace.snapshots", protocol.WorkspaceSnapshotsParams{SessionID: tree.Root.ID, Limit: 101}, &invalid); err == nil {
		t.Fatal("oversized page accepted")
	}
	errorKind("sessions.delete", protocol.SessionParams{SessionID: tree.Root.ID}, "BUSY")
	write(tracked, "later")
	write("outside", "keep outside")
	write("scope [literal] /untracked", "keep untracked")
	restore := params
	restore.ActionID = "restore"
	restored := call[protocol.WorkspaceResult](t, c, "workspace.restore", restore)
	if restored.Action.Kind != "restore" || restored.Action.State != "succeeded" || read(tracked) != "captured" || read("outside") != "keep outside" || read("scope [literal] /untracked") != "keep untracked" {
		t.Fatal(restored)
	}
	write(tracked, "after restore")
	observed := call[protocol.WorkspaceAction](t, c, "workspace.action", protocol.ReadWorkspaceActionParams{SessionID: tree.Root.ID, ActionID: restore.ActionID})
	if !reflect.DeepEqual(observed, restored.Action) {
		t.Fatal(observed, restored.Action)
	}
	retry := call[protocol.WorkspaceResult](t, c, "workspace.restore", restore)
	if !reflect.DeepEqual(retry, restored) || read(tracked) != "after restore" {
		t.Fatal("read or retry replayed restore", retry)
	}
	for _, capture := range []protocol.WorkspaceActionParams{params, second} {
		release := capture
		release.ActionID = "release-" + capture.SnapshotID
		value := call[protocol.WorkspaceResult](t, c, "workspace.release", release)
		if value.Action.State != "succeeded" || value.Snapshot.ReleasedAt == nil {
			t.Fatal(value)
		}
	}
	history := call[protocol.HistoryResult](t, c, "sessions.history", protocol.HistoryParams{SessionID: tree.Root.ID, Limit: 100})
	if len(history.Items) != 0 {
		t.Fatal("workspace action fabricated history")
	}
	call[protocol.DeleteResult](t, c, "sessions.delete", protocol.SessionParams{SessionID: tree.Root.ID})
	retry = call[protocol.WorkspaceResult](t, c, "workspace.restore", restore)
	if retry.Action.State != "succeeded" || retry.Snapshot.ReleasedAt == nil || read(tracked) != "after restore" {
		t.Fatal("deleted owner lost action receipt", retry)
	}
	observed = call[protocol.WorkspaceAction](t, c, "workspace.action", protocol.ReadWorkspaceActionParams{SessionID: tree.Root.ID, ActionID: restore.ActionID})
	if !reflect.DeepEqual(observed, restored.Action) {
		t.Fatal("deleted owner lost action observation", observed)
	}
}
