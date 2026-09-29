package tool

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func TestPermissionModeChangeDuringPathLockPreventsEffect(t *testing.T) {
	db, dispatcher, root, turn, _ := dispatchFixture(t)
	if _, err := db.SetPermissionMode(t.Context(), session.PermissionModeRequest{ID: "enable", SessionID: root.ID, ExpectedRevision: 1, Mode: session.PermissionAutomatic}); err != nil {
		t.Fatal(err)
	}
	prepared, err := dispatcher.files.Prepare(root.WorkingDirectory, "files.write", invokeWrite(root, "held").Arguments)
	if err != nil {
		t.Fatal(err)
	}
	release, err := prepared.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	done := make(chan error, 1)
	go func() { _, _, err := dispatcher.Call(t.Context(), invokeWrite(root, "held-mode")); done <- err }()
	admitted := awaitOperation(t, db, turn.ID, "held-mode", session.OperationReady)
	if admitted.PermissionRevision == nil {
		t.Fatal("missing policy authority")
	}
	if _, err := db.SetPermissionMode(t.Context(), session.PermissionModeRequest{ID: "disable", SessionID: root.ID, ExpectedRevision: 2, Mode: session.PermissionPrompt}); err != nil {
		t.Fatal(err)
	}
	release()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("retired policy produced an effect")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("policy retirement left blocked caller")
	}
	if _, err := os.Stat(filepath.Join(root.WorkingDirectory, "note.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("mode change lost dispatch boundary", err)
	}
}

func TestPermissionModeNeverBypassesPreparedFileContainment(t *testing.T) {
	db, dispatcher, root, turn, _ := dispatchFixture(t)
	if _, err := db.SetPermissionMode(t.Context(), session.PermissionModeRequest{ID: "enable", SessionID: root.ID, ExpectedRevision: 1, Mode: session.PermissionAutomatic}); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	call := invokeWrite(root, "outside")
	call.Arguments["path"] = outside
	if _, _, err := dispatcher.Call(t.Context(), call); err == nil {
		t.Fatal("Full Access bypassed intrinsic file scope")
	}
	prepared, err := dispatcher.files.Prepare(root.WorkingDirectory, "files.write", invokeWrite(root, "held").Arguments)
	if err != nil {
		t.Fatal(err)
	}
	release, err := prepared.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	done := make(chan error, 1)
	go func() { _, _, err := dispatcher.Call(t.Context(), invokeWrite(root, "swap")); done <- err }()
	admitted := awaitOperation(t, db, turn.ID, "swap", session.OperationReady)
	if err := os.Symlink(outside, filepath.Join(root.WorkingDirectory, "note.txt")); err != nil {
		t.Fatal(err)
	}
	release()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("policy bypassed file revalidation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("file revalidation remained blocked")
	}
	if _, err := os.Stat(outside); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("escaping symlink wrote outside scope", err)
	}
	operation, err := db.Operation(t.Context(), admitted.ID)
	if err != nil || operation.DispatchedAt != nil || operation.State != session.OperationCancelled {
		t.Fatal("invalid path dispatched", operation, err)
	}
}
