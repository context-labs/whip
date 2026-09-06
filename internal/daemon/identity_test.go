package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

type permissionModeRunner struct {
	*fakeRunner
	external bool
	resolved []resolvedPermission
}

type resolvedPermission struct {
	id       string
	decision capability.Decision
}

func (r *permissionModeRunner) SetExternalPermissions(enabled bool) {
	r.external = enabled
}

func (r *permissionModeRunner) ExternalPermissionsEnabled() bool {
	return r.external
}

func (r *permissionModeRunner) ResolvePermission(permissionID string, decision capability.Decision) error {
	r.resolved = append(r.resolved, resolvedPermission{permissionID, decision})
	return nil
}

func TestTrustedClientPermissionIdentityMethodsAreRemoved(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(value, ServerOptions{Generation: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client := pipeClient(t, server, InitializeParams{
		ProtocolMajor: ProtocolMajor, ClientID: "client", ClientKind: "automation",
	})
	defer client.Close()
	for _, method := range []string{"identity.enroll", "identity.status"} {
		err := client.Call(t.Context(), method, struct{}{}, nil)
		failure, ok := errors.AsType[*RPCError](err)
		if !ok || failure.Code != -32601 {
			t.Fatalf("removed method %s = %v", method, err)
		}
	}
	if err := client.Call(t.Context(), "permission.mode", struct{}{}, nil); err == nil {
		t.Fatal("removed permission mode RPC was accepted")
	}
}

func TestTrustedClientPermissionDecisionsAreScopedAndIdempotent(t *testing.T) {
	for _, kind := range []string{"tui", "automation"} {
		t.Run(kind, func(t *testing.T) {
			store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
			rootID := createRoot(t, store)
			otherRootID := createRoot(t, store)
			value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
				return Components{Runner: &fakeRunner{}}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			server, err := NewServer(value, ServerOptions{Generation: 9})
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			root, err := value.Open(rootID)
			if err != nil {
				t.Fatal(err)
			}
			dispatcher := capability.NewDispatcher(store, store.Workspaces(), nil)
			if err := dispatcher.Register(capability.Registration{
				Operation: "write", Mutation: capability.MutationPath, Permission: true,
				Path:    func(json.RawMessage) (string, error) { return "approved.txt", nil },
				Handler: func(context.Context, capability.Call) (string, error) { return "ok", nil },
			}); err != nil {
				t.Fatal(err)
			}
			_, err = dispatcher.Dispatch(t.Context(), capability.Request{
				RootID: rootID, AgentID: root.authority.AgentID, CapabilityID: root.authority.Files.ID,
				CapabilityGeneration: root.authority.Files.Generation, OperationID: "pending-operation", Operation: "write",
				Arguments: json.RawMessage(`{}`), TraceID: "trace", WorkingDirectory: root.meta.CWD,
			})
			pending, ok := errors.AsType[*capability.PermissionPendingError](err)
			if !ok {
				t.Fatalf("permission admission = %v", err)
			}
			initialize := InitializeParams{ProtocolMajor: ProtocolMajor, ClientID: "approver", ClientKind: kind}
			client := pipeClient(t, server, initialize)
			defer client.Close()
			wrongRoot := PermissionDecision{
				CommandID: "wrong-root", RootID: otherRootID, PermissionID: pending.PermissionID, Allow: true,
			}
			_, err = client.DecidePermission(t.Context(), wrongRoot)
			failure, ok := errors.AsType[*RPCError](err)
			if !ok || failure.Code != -32003 {
				t.Fatalf("wrong-root decision = %v", err)
			}
			decision := PermissionDecision{
				CommandID: "approve", RootID: rootID, PermissionID: pending.PermissionID, Allow: true,
			}
			result, err := client.DecidePermission(t.Context(), decision)
			if err != nil || result.OperationID != "pending-operation" || result.LeaseID == "" {
				t.Fatalf("ordinary client decision = %+v, %v", result, err)
			}
			_ = client.Close()
			reconnected := pipeClient(t, server, initialize)
			defer reconnected.Close()
			if retry, err := reconnected.DecidePermission(t.Context(), decision); err != nil || retry != result {
				t.Fatalf("permission retry after reconnect = %+v, %v", retry, err)
			}
			changed := decision
			changed.Allow = false
			_, err = reconnected.DecidePermission(t.Context(), changed)
			failure, ok = errors.AsType[*RPCError](err)
			if !ok || failure.Code != -32009 {
				t.Fatalf("changed decision with reused command ID = %v", err)
			}
			stale := decision
			stale.CommandID = "stale"
			if _, err := reconnected.DecidePermission(t.Context(), stale); err == nil {
				t.Fatal("a new command approved an already resolved permission")
			}
			competing := pipeClient(t, server, InitializeParams{
				ProtocolMajor: ProtocolMajor, ClientID: "other-client", ClientKind: "tui",
			})
			defer competing.Close()
			if _, err := competing.DecidePermission(t.Context(), decision); err == nil {
				t.Fatal("another client's command namespace reused a resolved permission")
			}
			if _, err := os.Stat(filepath.Join(root.meta.CWD, "approved.txt")); !os.IsNotExist(err) {
				t.Fatalf("permission approval bypassed operation ownership: %v", err)
			}
		})
	}
}

func TestTrustedClientPermissionModesUseOrdinaryCommands(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	runner := &permissionModeRunner{fakeRunner: &fakeRunner{}}
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: runner}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(value, ServerOptions{Generation: 10})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	root, err := value.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"automation", "tui"} {
		client := pipeClient(t, server, InitializeParams{ProtocolMajor: ProtocolMajor, ClientID: kind, ClientKind: kind})
		defer client.Close()
		for _, external := range []bool{false, true} {
			commandID := "automatic"
			if external {
				commandID = "ask"
			}
			payload := mustJSON(t, map[string]bool{"external_permissions": external})
			result, err := client.Command(t.Context(), CommandParams{
				CommandID: commandID, Scope: string(session.CommandScopeRoot), RootID: rootID,
				Operation: "permission.mode", Payload: payload,
			})
			if err != nil || result.Status != "succeeded" {
				t.Fatalf("%s mode %s = %+v, %v", kind, commandID, result, err)
			}
			got, err := routeControlValue(root, t.Context(), func(context.Context) (bool, error) {
				return runner.external, nil
			})
			if err != nil || got != external {
				t.Fatalf("%s mode %s external=%v, want %v: %v", kind, commandID, got, external, err)
			}
		}
	}
}

func TestRootSnapshotIsACompleteAuthoritativeClientView(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
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
	if _, err := store.SetBlackboard(t.Context(), rootID, root.authority.AgentID, "evidence", session.RuntimePayload{Data: []byte("bounded")}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddSchedule(rootID, "@every 1h", "inspect", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	dispatcher := capability.NewDispatcher(store, store.Workspaces(), nil)
	const secretArgument = "snapshot-must-not-contain-this"
	if err := dispatcher.Register(capability.Registration{
		Operation: "write", Mutation: capability.MutationPath, Permission: true,
		Path:    func(json.RawMessage) (string, error) { return "approved.txt", nil },
		Handler: func(context.Context, capability.Call) (string, error) { return "ok", nil },
	}); err != nil {
		t.Fatal(err)
	}
	_, err = dispatcher.Dispatch(t.Context(), capability.Request{
		RootID: rootID, AgentID: root.authority.AgentID, CapabilityID: root.authority.Files.ID,
		CapabilityGeneration: root.authority.Files.Generation, OperationID: "snapshot-operation", Operation: "write",
		Arguments: json.RawMessage(`{"secret":"` + secretArgument + `"}`), TraceID: "trace", WorkingDirectory: root.meta.CWD,
	})
	var pending *capability.PermissionPendingError
	if !errors.As(err, &pending) {
		t.Fatalf("permission admission = %v", err)
	}

	snapshot, err := root.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Blackboard) != 1 || len(snapshot.Budgets) == 0 || len(snapshot.Capabilities) == 0 || len(snapshot.Schedules) != 1 || len(snapshot.Permissions) != 1 {
		t.Fatalf("incomplete snapshot: blackboard=%d budgets=%d capabilities=%d schedules=%d permissions=%d",
			len(snapshot.Blackboard), len(snapshot.Budgets), len(snapshot.Capabilities), len(snapshot.Schedules), len(snapshot.Permissions))
	}
	permission := snapshot.Permissions[0]
	if permission.ID != pending.PermissionID || permission.OperationID != "snapshot-operation" || permission.Operation != "write" || permission.CanonicalPath == "" || permission.RequestDigest == "" {
		t.Fatalf("permission snapshot = %+v", permission)
	}
	if len(snapshot.Agents) != 1 || snapshot.Agents[0].LifecyclePhase != "blocked" || snapshot.Agents[0].BlockingReason != "permission" {
		t.Fatalf("agent presentation state = %+v", snapshot.Agents)
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secretArgument) || strings.Contains(string(raw), "Arguments") {
		t.Fatalf("snapshot leaked raw permission arguments: %s", raw)
	}
}

func pipeClient(t *testing.T, server *Server, initialize InitializeParams) *Client {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	go server.serveConn(serverConn)
	client, err := NewClient(context.Background(), clientConn, initialize)
	if err != nil {
		t.Fatal(err)
	}
	return client
}
