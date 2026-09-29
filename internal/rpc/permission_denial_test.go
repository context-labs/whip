package rpc_test

import (
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func TestPermissionDenialRPCPreservesModeReceiptsAndStoppedEditing(t *testing.T) {
	_, c := fixture(t)
	first := create(t, c)
	other := create(t, c)
	request := protocol.SetPermissionDenialParams{EditID: "Denial.Mixed-Case", SessionID: first.Root.ID, ExpectedRevision: 1, DenyInteractive: true}
	edit := call[protocol.PermissionDenialEdit](t, c, "permissions.set_denial", request)
	if edit.ID != request.EditID || !edit.Policy.DenyInteractive || edit.Policy.Mode != "prompt" || edit.Policy.Revision != 2 {
		t.Fatal(edit)
	}
	mode := call[protocol.PermissionModeEdit](t, c, "permissions.set_mode", protocol.SetPermissionModeParams{EditID: "mode", SessionID: first.Root.ID, ExpectedRevision: 2, Mode: "automatic"})
	if !mode.Policy.DenyInteractive {
		t.Fatal("mode erased denial", mode)
	}
	call[protocol.Session](t, c, "sessions.lifecycle", protocol.LifecycleParams{SessionID: first.Root.ID, Lifecycle: "stopped"})
	cleared := call[protocol.PermissionDenialEdit](t, c, "permissions.set_denial", protocol.SetPermissionDenialParams{EditID: "clear", SessionID: first.Root.ID, ExpectedRevision: 3, DenyInteractive: false})
	if cleared.Policy.DenyInteractive || cleared.Policy.Mode != "automatic" {
		t.Fatal(cleared)
	}
	if retry := call[protocol.PermissionDenialEdit](t, c, "permissions.set_denial", request); retry != edit {
		t.Fatal("exact retry changed receipt", retry)
	}
	if read := call[protocol.PermissionDenialEdit](t, c, "permissions.denial_edit", protocol.PermissionModeEditParams{SessionID: first.Root.ID, EditID: request.EditID}); read != edit {
		t.Fatal(read)
	}
	requireHistoryError(t, c, "permissions.denial_edit", protocol.PermissionModeEditParams{SessionID: other.Root.ID, EditID: request.EditID}, "NOT_FOUND")
	request.DenyInteractive = false
	requireHistoryError(t, c, "permissions.set_denial", request, "CONFLICT")
}
