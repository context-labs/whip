package tui

import (
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func TestNativePermissionsExactOwnerRevokeAndPolicyCAS(t *testing.T) {
	m, _ := nativeUIFixture(t)
	grant := nativeMenuRPC[protocol.Grant](t, m.connection, "grants.create", protocol.CreateGrantParams{ID: "terminal-grant", SessionID: m.owner.ID, Capability: "files.read", Resource: m.owner.WorkingDirectory})
	command := m.command("/permissions")
	m.Update(command())
	if m.permissionPolicy == nil || !strings.Contains(m.notice, string(grant.ID)) || !strings.Contains(m.notice, "standing") {
		t.Fatal(m.status, m.notice)
	}
	stale := m.command("/permissions mode automatic")
	original := *m.permissionPolicy
	other := nativeMenuRPC[protocol.PermissionModeEdit](t, m.connection, "permissions.set_mode", protocol.SetPermissionModeParams{SessionID: m.owner.ID, EditID: "other-client-mode", ExpectedRevision: original.Revision, Mode: "automatic"})
	m.Update(stale())
	if !strings.Contains(strings.ToLower(m.status), "conflict") {
		t.Fatal("stale edit silently rebased", m.status)
	}
	m.Update(m.command("/permissions")())
	command = m.command("/permissions mode prompt")
	response := command()
	m.Update(response)
	if m.permissionPolicy == nil || m.permissionPolicy.Revision != other.Policy.Revision+1 || m.permissionPolicy.Mode != "prompt" {
		t.Fatal(m.status, m.permissionPolicy)
	}
	// A captured explicit retry returns the original receipt even after a later mode edit.
	nativeMenuRPC[protocol.PermissionModeEdit](t, m.connection, "permissions.set_mode", protocol.SetPermissionModeParams{SessionID: m.owner.ID, EditID: "later-mode", ExpectedRevision: m.permissionPolicy.Revision, Mode: "automatic"})
	retry := command().(nativeControlResult)
	if retry.err != nil || retry.policy.Mode != "prompt" {
		t.Fatal(retry)
	}
	fresh := nativeMenuRPC[protocol.PermissionPolicy](t, m.connection, "permissions.policy", protocol.SessionParams{SessionID: m.owner.ID})
	if fresh.Mode != "automatic" {
		t.Fatal("receipt replay changed later mode", fresh)
	}
	revoke := m.command("/permissions forget " + string(grant.ID))
	m.Update(revoke())
	if strings.Contains(m.status, "mismatch") || m.retryControl != nil {
		t.Fatal(m.status)
	}
	grants := nativeMenuRPC[protocol.GrantsResult](t, m.connection, "grants.list", protocol.GrantsParams{SessionID: m.owner.ID, Limit: 100})
	if len(grants.Items) != 1 || grants.Items[0].RevokedAt == nil {
		t.Fatal(grants)
	}
	otherRoot := nativeMenuRPC[protocol.CreateTreeResult](t, m.connection, "trees.create", protocol.CreateTreeParams{CreationID: "foreign-permission-root", Definition: m.connection.Builtins()[0], WorkingDirectory: t.TempDir(), Overrides: protocol.ConfigPatch{Model: &protocol.ModelSelection{Provider: "scripted", Name: "scripted"}}})
	foreign := nativeMenuRPC[protocol.Grant](t, m.connection, "grants.create", protocol.CreateGrantParams{ID: "foreign-grant", SessionID: otherRoot.Root.ID, Capability: "files.read", Resource: otherRoot.Root.WorkingDirectory})
	m.Update(m.command("/permissions forget " + string(foreign.ID))())
	if !strings.Contains(strings.ToLower(m.status), "conflict") {
		t.Fatal("terminal forgot another owner's grant", m.status)
	}
	grants = nativeMenuRPC[protocol.GrantsResult](t, m.connection, "grants.list", protocol.GrantsParams{SessionID: otherRoot.Root.ID, Limit: 100})
	if len(grants.Items) != 1 || grants.Items[0].RevokedAt != nil {
		t.Fatal(grants)
	}
	m.owner.ParentID = new(protocol.ID("root"))
	if m.command("/permissions mode automatic") != nil || !strings.Contains(m.status, "delegated") {
		t.Fatal(m.status)
	}
}
