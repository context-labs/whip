package daemon

import (
	"context"
	"encoding/json"
	"errors"
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

func TestDirectPermissionControlsStayRootScoped(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	otherRootID := createRoot(t, store)
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	root, err := value.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	other, err := value.Open(otherRootID)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := capability.NewDispatcher(store, store.Workspaces(), nil)
	if err := dispatcher.Register(capability.Registration{
		Operation: "write", Mutation: capability.MutationPath, Permission: true,
		Path:    func(json.RawMessage) (string, error) { return "permission.txt", nil },
		Handler: func(context.Context, capability.Call) (string, error) { return "ok", nil },
	}); err != nil {
		t.Fatal(err)
	}
	_, err = dispatcher.Dispatch(t.Context(), capability.Request{
		RootID: rootID, AgentID: root.AgentID(), CapabilityID: root.authority.Files.ID,
		CapabilityGeneration: root.authority.Files.Generation, OperationID: "direct-permission", Operation: "write",
		Arguments: json.RawMessage(`{}`), TraceID: "trace", WorkingDirectory: root.meta.CWD,
	})
	var pending *capability.PermissionPendingError
	if !errors.As(err, &pending) {
		t.Fatalf("permission admission = %v", err)
	}
	if inspected, err := root.InspectPermission(t.Context(), pending.PermissionID); err != nil || inspected.Request.OperationID != "direct-permission" {
		t.Fatalf("permission inspection = %+v, %v", inspected, err)
	}
	if _, err := other.InspectPermission(t.Context(), pending.PermissionID); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("cross-root inspection = %v", err)
	}
	if _, err := other.DecidePermission(t.Context(), pending.PermissionID, capability.Decision{Allow: true}); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("cross-root decision = %v", err)
	}
	if _, err := root.DecidePermission(t.Context(), "missing", capability.Decision{Allow: true}); err == nil {
		t.Fatal("missing permission was decided")
	}
	if _, err := root.DecidePermission(t.Context(), pending.PermissionID, capability.Decision{PrincipalID: "tester", Reason: "not approved"}); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("denied permission = %v", err)
	}
}
