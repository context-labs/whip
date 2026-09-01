package daemon

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

func TestCapabilityAuthorityRoutesThroughRootActor(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	daemon, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	root, err := daemon.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := store.WorkspaceRoot(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := root.AdmitChildWithCapabilities(ctx, root.authority.AgentID, "child", "exec-child", []session.CapabilityDelegation{{
		ID: "child-read", Issuer: root.authority.Files, AgentID: "child", Operations: []string{"read"}, Scopes: []string{workspace},
	}}); err != nil {
		t.Fatal(err)
	}
	record, err := root.InspectCapability(ctx, root.authority.AgentID, "child-read")
	if err != nil || record.AgentID != "child" || record.Status != "active" {
		t.Fatalf("actor inspection = %+v, %v", record, err)
	}
	record, err = root.RevokeCapability(ctx, root.authority.AgentID, "child-read")
	if err != nil || record.Status != "revoked" || record.Generation != 2 {
		t.Fatalf("actor revocation = %+v, %v", record, err)
	}
	if err := root.AdmitChild(ctx, "child", "grandchild", "exec-grandchild"); err != nil {
		t.Fatal(err)
	}
	if _, err := root.DelegateCapability(ctx, "child", session.CapabilityDelegation{
		ID: "stale-child", Issuer: capability.Reference{ID: "child-read", Generation: 1}, AgentID: "grandchild", Operations: []string{"read"},
	}); err == nil {
		t.Fatal("actor accepted a stale capability reference")
	}
}
