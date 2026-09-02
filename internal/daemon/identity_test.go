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

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

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
	_, firstKey, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := first.EnrollIdentity(context.Background(), firstKey, false, "", nil); err == nil || !strings.Contains(err.Error(), "TTY") {
		t.Fatalf("non-TTY first enrollment = %v", err)
	}
	if result, err := first.EnrollIdentity(context.Background(), firstKey, true, "", nil); err != nil || result.ClientID != "human-1" {
		t.Fatalf("first enrollment = %+v, %v", result, err)
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
	if _, err := bot.DecidePermission(context.Background(), botKey, PermissionDecision{RootID: rootID, PermissionID: pending.PermissionID, Allow: true}); err == nil || !strings.Contains(err.Error(), "paired human") {
		t.Fatalf("automation decision = %v", err)
	}

	human := pipeClient(t, server, InitializeParams{ProtocolMajor: 1, ClientID: "approver", ClientKind: "tui"})
	defer human.Close()
	_, humanKey, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := human.EnrollIdentity(context.Background(), humanKey, true, "", nil); err != nil {
		t.Fatal(err)
	}
	wrongRoot := PermissionDecision{RootID: otherRootID, PermissionID: pending.PermissionID, Allow: true}
	if _, err := human.DecidePermission(context.Background(), humanKey, wrongRoot); !errors.Is(err, capability.ErrDenied) && (err == nil || !strings.Contains(err.Error(), capability.ErrDenied.Error())) {
		t.Fatalf("wrong-root decision = %v", err)
	}
	result, err := human.DecidePermission(context.Background(), humanKey, PermissionDecision{RootID: rootID, PermissionID: pending.PermissionID, Allow: true})
	if err != nil || result.OperationID != "pending-operation" || result.LeaseID == "" {
		t.Fatalf("signed decision = %+v, %v", result, err)
	}
	if _, err := human.DecidePermission(context.Background(), humanKey, PermissionDecision{RootID: rootID, PermissionID: pending.PermissionID, Allow: true}); err == nil {
		t.Fatal("stale permission decision was not authoritative")
	}
	if _, err := os.Stat(filepath.Join(root.meta.CWD, "approved.txt")); !os.IsNotExist(err) {
		t.Fatalf("test decision unexpectedly bypassed operation ownership: %v", err)
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
