package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
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

func TestExternalPermissionDecisionResumesOwningDurableChild(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	rootAgent := agent.New(llm.New("http://127.0.0.1:1", "key"), "model", 100, "system")
	owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: NewAgentRunner(rootAgent)}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	if result := clientCommand(t, root, "human", "external-on", "permission.mode", map[string]bool{"external_permissions": true}); result.Status != "succeeded" {
		t.Fatalf("external permission mode = %+v", result)
	}

	child := agent.New(llm.New("http://127.0.0.1:1", "key"), "model", 100, "child")
	if err := root.AdmitRLMSubagent(t.Context(), "writer", child, []string{"write"}, nil); err != nil {
		t.Fatal(err)
	}
	if !child.Services.ExternalPermissionsEnabled() {
		t.Fatal("durable child did not inherit external permission mode")
	}
	if err := root.StartSubagent(t.Context(), "writer"); err != nil {
		t.Fatal(err)
	}
	if result := clientCommand(t, root, "human", "external-off-running", "permission.mode", map[string]bool{"external_permissions": false}); result.Status != "failed" {
		t.Fatalf("permission mode changed during child turn: %+v", result)
	}

	var write tools.Tool
	for _, candidate := range child.Tools {
		if candidate.Def.Function.Name == "write" {
			write = candidate
			break
		}
	}
	if write.Run == nil {
		t.Fatal("delegated child has no write tool")
	}
	toolCtx, err := tools.WithTurnIdentity(t.Context(), "child-permission-test")
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root.WorkingDirectory(), "approved-child.txt")
	arguments, err := json.Marshal(map[string]string{"path": target, "content": "approved"})
	if err != nil {
		t.Fatal(err)
	}
	type toolResult struct {
		output string
		err    error
	}
	resultCh := make(chan toolResult, 1)
	go func() {
		output, runErr := write.Run(tools.WithOperationIdentity(toolCtx, "write"), arguments)
		resultCh <- toolResult{output: output, err: runErr}
	}()

	var permission session.PermissionSnapshot
	deadline := time.Now().Add(5 * time.Second)
	for permission.ID == "" {
		snapshot, snapshotErr := root.Snapshot(t.Context())
		if snapshotErr != nil {
			t.Fatal(snapshotErr)
		}
		if len(snapshot.Permissions) > 0 {
			permission = snapshot.Permissions[0]
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child permission did not become pending")
		}
		time.Sleep(time.Millisecond)
	}
	if permission.AgentID != root.AgentID()+":writer" {
		t.Fatalf("pending permission owner = %q", permission.AgentID)
	}
	ticket, err := root.DecidePermissionCommand(t.Context(), session.CommandAdmission{
		ClientID: "human", CommandID: "approve-child", RequestDigest: "approve-child",
	}, permission.ID, capability.Decision{Allow: true, PrincipalID: "paired-human"})
	if err != nil || ticket.OperationID != permission.OperationID {
		t.Fatalf("child permission decision = %+v, %v", ticket, err)
	}
	select {
	case result := <-resultCh:
		if result.err != nil || result.output == "" {
			t.Fatalf("approved child write = %q, %v", result.output, result.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("approved child operation did not resume")
	}
	if body, err := os.ReadFile(target); err != nil || string(body) != "approved" {
		t.Fatalf("approved child file = %q, %v", body, err)
	}
	if err := root.FinishSubagent(t.Context(), "writer", agent.TaskDone); err != nil {
		t.Fatal(err)
	}
	if result := clientCommand(t, root, "human", "external-off", "permission.mode", map[string]bool{"external_permissions": false}); result.Status != "succeeded" || child.Services.ExternalPermissionsEnabled() {
		t.Fatalf("settled child permission mode = %+v external=%v", result, child.Services.ExternalPermissionsEnabled())
	}
	root.ReleaseSubagent("writer")
}
