package daemon

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
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
}

func (r *permissionModeRunner) SetExternalPermissions(enabled bool) {
	r.external = enabled
}

func (r *permissionModeRunner) ExternalPermissionsEnabled() bool {
	return r.external
}

func (*permissionModeRunner) ResolvePermission(string, capability.Decision) error {
	return nil
}

func TestHumanEnrollmentIsExplicitAuthenticatedAndAutomationSafe(t *testing.T) {
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
	defer func() { _ = server.Close() }()

	automation := pipeClient(t, server, InitializeParams{ProtocolMajor: 1, ClientID: "bot", ClientKind: "automation"})
	defer automation.Close()
	_, botKey, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := automation.EnrollIdentity(context.Background(), botKey, true, "", nil); err == nil || !strings.Contains(err.Error(), "automation") {
		t.Fatalf("automation enrollment = %v", err)
	}
	if count, err := store.HumanIdentityCount(context.Background()); err != nil || count != 0 {
		t.Fatalf("automation consumed enrollment: count=%d err=%v", count, err)
	}

	first := pipeClient(t, server, InitializeParams{ProtocolMajor: 1, ClientID: "human-1", ClientKind: "tui"})
	defer first.Close()
	if status, err := first.IdentityStatus(context.Background()); err != nil || status.Paired || !status.EnrollmentOpen {
		t.Fatalf("initial identity status = %+v, %v", status, err)
	}
	_, firstKey, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := first.EnrollIdentity(context.Background(), firstKey, false, "", nil); err == nil || !strings.Contains(err.Error(), "TTY") {
		t.Fatalf("non-TTY first enrollment = %v", err)
	}
	if result, err := first.EnrollIdentity(context.Background(), firstKey, true, "", nil); err != nil || result.ClientID != "human-1" {
		t.Fatalf("first enrollment = %+v, %v", result, err)
	}
	if status, err := first.IdentityStatus(context.Background()); err != nil || !status.Paired || status.EnrollmentOpen {
		t.Fatalf("paired identity status = %+v, %v", status, err)
	}

	second := pipeClient(t, server, InitializeParams{ProtocolMajor: 1, ClientID: "human-2", ClientKind: "acp"})
	defer second.Close()
	_, secondKey, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := second.EnrollIdentity(context.Background(), secondKey, false, "", nil); err == nil || !strings.Contains(err.Error(), "authenticated") {
		t.Fatalf("unsigned later enrollment = %v", err)
	}
	_, wrongKey, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := second.EnrollIdentity(context.Background(), secondKey, false, "human-1", wrongKey); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("wrong signer enrollment = %v", err)
	}
	if result, err := second.EnrollIdentity(context.Background(), secondKey, false, "human-1", firstKey); err != nil || result.ClientID != "human-2" {
		t.Fatalf("authenticated enrollment = %+v, %v", result, err)
	}
	if count, err := store.HumanIdentityCount(context.Background()); err != nil || count != 2 {
		t.Fatalf("human count = %d, %v", count, err)
	}
}

func TestPermissionDecisionsRequireConnectionBoundHumanSignature(t *testing.T) {
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
	defer func() { _ = server.Close() }()
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
	_, err = dispatcher.Dispatch(context.Background(), capability.Request{
		RootID: rootID, AgentID: root.authority.AgentID, CapabilityID: root.authority.Files.ID,
		CapabilityGeneration: root.authority.Files.Generation, OperationID: "pending-operation", Operation: "write",
		Arguments: json.RawMessage(`{}`), TraceID: "trace", WorkingDirectory: root.meta.CWD,
	})
	var pending *capability.PermissionPendingError
	if !errors.As(err, &pending) {
		t.Fatalf("permission admission = %v", err)
	}

	bot := pipeClient(t, server, InitializeParams{ProtocolMajor: 1, ClientID: "bot-decision", ClientKind: "automation"})
	defer bot.Close()
	_, botKey, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := bot.DecidePermission(context.Background(), botKey, PermissionDecision{CommandID: "bot", RootID: rootID, PermissionID: pending.PermissionID, Allow: true}); err == nil || !strings.Contains(err.Error(), "paired human") {
		t.Fatalf("automation decision = %v", err)
	}

	human := pipeClient(t, server, InitializeParams{ProtocolMajor: 1, ClientID: "approver", ClientKind: "tui"})
	defer human.Close()
	_, humanKey, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := human.EnrollIdentity(context.Background(), humanKey, true, "", nil); err != nil {
		t.Fatal(err)
	}
	wrongRoot := PermissionDecision{CommandID: "wrong-root", RootID: otherRootID, PermissionID: pending.PermissionID, Allow: true}
	if _, err := human.DecidePermission(context.Background(), humanKey, wrongRoot); !errors.Is(err, capability.ErrDenied) && (err == nil || !strings.Contains(err.Error(), capability.ErrDenied.Error())) {
		t.Fatalf("wrong-root decision = %v", err)
	}
	decision := PermissionDecision{CommandID: "approve", RootID: rootID, PermissionID: pending.PermissionID, Allow: true}
	result, err := human.DecidePermission(context.Background(), humanKey, decision)
	if err != nil || result.OperationID != "pending-operation" || result.LeaseID == "" {
		t.Fatalf("signed decision = %+v, %v", result, err)
	}
	if retry, err := human.DecidePermission(context.Background(), humanKey, decision); err != nil || retry.OperationID != result.OperationID || retry.LeaseID != result.LeaseID {
		t.Fatalf("idempotent permission retry = %+v, %v", retry, err)
	}
	if _, err := human.DecidePermission(context.Background(), humanKey, PermissionDecision{CommandID: "stale", RootID: rootID, PermissionID: pending.PermissionID, Allow: true}); err == nil {
		t.Fatal("stale permission decision was not authoritative")
	}
	if _, err := os.Stat(filepath.Join(root.meta.CWD, "approved.txt")); !os.IsNotExist(err) {
		t.Fatalf("test decision unexpectedly bypassed operation ownership: %v", err)
	}
}

func TestAutomaticPermissionModeRequiresConnectionBoundHumanSignature(t *testing.T) {
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
	defer func() { _ = server.Close() }()
	payload, _ := json.Marshal(map[string]bool{"external_permissions": false})
	command := CommandParams{
		CommandID: "automatic", Scope: string(session.CommandScopeRoot), RootID: rootID,
		Operation: "permission.mode", Payload: payload,
	}

	automation := pipeClient(t, server, InitializeParams{ProtocolMajor: 1, ClientID: "automatic-bot", ClientKind: "automation"})
	defer automation.Close()
	if _, err := automation.Command(context.Background(), command); err == nil || !strings.Contains(err.Error(), "signed paired-human") {
		t.Fatalf("unsigned automatic mode = %v", err)
	}

	human := pipeClient(t, server, InitializeParams{ProtocolMajor: 1, ClientID: "automatic-human", ClientKind: "acp"})
	defer human.Close()
	_, humanKey, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := human.EnrollIdentity(context.Background(), humanKey, true, "", nil); err != nil {
		t.Fatal(err)
	}
	_, wrongKey, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := human.SetPermissionMode(context.Background(), wrongKey, command); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("wrong automatic signer = %v", err)
	}
	command.CommandID = "automatic-signed"
	result, err := human.SetPermissionMode(context.Background(), humanKey, command)
	if err != nil || result.Status != "succeeded" || runner.external {
		t.Fatalf("signed automatic mode = %+v, external=%v, err=%v", result, runner.external, err)
	}

	payload, _ = json.Marshal(map[string]bool{"external_permissions": true})
	result, err = automation.Command(context.Background(), CommandParams{
		CommandID: "ask", Scope: string(session.CommandScopeRoot), RootID: rootID,
		Operation: "permission.mode", Payload: payload,
	})
	if err != nil || result.Status != "succeeded" || !runner.external {
		t.Fatalf("safe ask mode = %+v, external=%v, err=%v", result, runner.external, err)
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
