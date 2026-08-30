package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/capability"
)

func TestCapabilityLedgerBindsAgentOperationBudgetAndScope(t *testing.T) {
	root := t.TempDir()
	st, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rootID, err := st.Create(root, "m", "p")
	if err != nil {
		t.Fatal(err)
	}
	for _, agent := range []RuntimeAgent{
		{ID: "owner", RootID: rootID, Status: "idle"},
		{ID: "child", RootID: rootID, ParentID: "owner", Status: "idle"},
		{ID: "sibling", RootID: rootID, Status: "idle"},
	} {
		a := agent
		if _, err := st.CommitRuntime(context.Background(), RuntimeTransition{Agent: &a}); err != nil {
			t.Fatal(err)
		}
	}
	otherRoot, err := st.Create(t.TempDir(), "m", "p")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CommitRuntime(context.Background(), RuntimeTransition{Agent: &RuntimeAgent{ID: "other-owner", RootID: otherRoot, Status: "idle"}}); err != nil {
		t.Fatal(err)
	}
	if err := st.IssueCapability(context.Background(), capability.Grant{
		ID: "writer", RootID: rootID, AgentID: "owner", Operations: []string{"write"}, Scopes: []string{filepath.Join(root, "allowed")}, Generation: 2,
	}); err != nil {
		t.Fatal(err)
	}
	for _, grant := range []capability.Grant{
		{ID: "expired", RootID: rootID, AgentID: "owner", Operations: []string{"write"}, Scopes: []string{filepath.Join(root, "allowed")}, ExpiresAt: time.Now().Add(-time.Hour)},
		{ID: "revoked", RootID: rootID, AgentID: "owner", Operations: []string{"write"}, Scopes: []string{filepath.Join(root, "allowed")}},
	} {
		if err := st.IssueCapability(context.Background(), grant); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.RevokeCapability(context.Background(), "revoked"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetCapabilityBudget(context.Background(), rootID, "active_operations", 2); err != nil {
		t.Fatal(err)
	}
	dispatcher := capability.NewDispatcher(st, capability.NewWorkspaces(), nil)
	if err := dispatcher.Register(capability.Registration{
		Operation: "write", Mutation: capability.MutationPath,
		Path: func(arguments json.RawMessage) (string, error) {
			var args struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(arguments, &args); err != nil {
				return "", err
			}
			return args.Path, nil
		},
		Handler: func(_ context.Context, call capability.Call) (string, error) {
			if err := os.MkdirAll(filepath.Dir(call.CanonicalPath), 0o700); err != nil {
				return "", err
			}
			return "ok", os.WriteFile(call.CanonicalPath, []byte("changed"), 0o600)
		},
	}); err != nil {
		t.Fatal(err)
	}
	base := capability.Request{RootID: rootID, AgentID: "owner", CapabilityID: "writer", CapabilityGeneration: 2, TraceID: "trace", Operation: "write"}
	base.OperationID = "allowed"
	base.Arguments = json.RawMessage(`{"path":"allowed/file"}`)
	if _, err := dispatcher.Dispatch(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*capability.Request){
		"sibling":    func(request *capability.Request) { request.AgentID = "sibling" },
		"parent":     func(request *capability.Request) { request.AgentID = "child" },
		"unknown":    func(request *capability.Request) { request.AgentID = "unknown" },
		"cross-root": func(request *capability.Request) { request.RootID, request.AgentID = otherRoot, "other-owner" },
		"expired":    func(request *capability.Request) { request.CapabilityID, request.CapabilityGeneration = "expired", 0 },
		"revoked":    func(request *capability.Request) { request.CapabilityID, request.CapabilityGeneration = "revoked", 1 },
		"generation": func(request *capability.Request) { request.CapabilityGeneration = 1 },
		"scope":      func(request *capability.Request) { request.Arguments = json.RawMessage(`{"path":"sibling/file"}`) },
		"traversal": func(request *capability.Request) {
			request.Arguments = json.RawMessage(`{"path":"allowed/../sibling/file"}`)
		},
	} {
		request := base
		request.OperationID = "denied-" + name
		mutate(&request)
		if _, err := dispatcher.Dispatch(context.Background(), request); !errors.Is(err, capability.ErrDenied) {
			t.Errorf("%s error = %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "sibling", "file")); !os.IsNotExist(err) {
		t.Fatalf("scope denial changed workspace: %v", err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "allowed", "escape")); err == nil {
		request := base
		request.OperationID = "denied-symlink"
		request.Arguments = json.RawMessage(`{"path":"allowed/escape/file"}`)
		if _, err := dispatcher.Dispatch(context.Background(), request); err == nil {
			t.Fatal("symlink escape was accepted")
		}
		if _, err := os.Stat(filepath.Join(outside, "file")); !os.IsNotExist(err) {
			t.Fatalf("symlink denial changed outside workspace: %v", err)
		}
	}
	var status string
	if err := st.db.QueryRow(`SELECT status FROM operations WHERE id='denied-scope'`).Scan(&status); err != nil || status != string(capability.StatusDenied) {
		t.Fatalf("denied operation status=%q err=%v", status, err)
	}
}

func TestEnsureClassicAuthorityIsIdempotent(t *testing.T) {
	root := t.TempDir()
	st, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rootID, err := st.Create(root, "m", "p")
	if err != nil {
		t.Fatal(err)
	}

	first, err := st.EnsureClassicAuthority(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.EnsureClassicAuthority(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("authority changed across bootstrap: %#v != %#v", first, second)
	}
	if first.RootID != rootID || first.AgentID == "" || first.Files.ID == "" || first.Shell.ID == "" || first.Files.ID == first.Shell.ID {
		t.Fatalf("invalid classic authority: %#v", first)
	}
	var agents, grants, budgets int
	if err := st.db.QueryRow(`SELECT count(*) FROM agents WHERE root_id=?`, rootID).Scan(&agents); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRow(`SELECT count(*) FROM capabilities WHERE root_id=?`, rootID).Scan(&grants); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRow(`SELECT count(*) FROM budgets WHERE root_id=? AND kind='active_operations'`, rootID).Scan(&budgets); err != nil {
		t.Fatal(err)
	}
	if agents != 1 || grants != 2 || budgets != 1 {
		t.Fatalf("bootstrap rows agents=%d grants=%d budgets=%d", agents, grants, budgets)
	}
}

func TestCapabilityPermissionSurvivesDispatcherAndRevalidates(t *testing.T) {
	root := t.TempDir()
	st, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rootID, _ := st.Create(root, "m", "p")
	_, err = st.CommitRuntime(context.Background(), RuntimeTransition{Agent: &RuntimeAgent{ID: "owner", RootID: rootID, Status: "idle"}})
	if err != nil {
		t.Fatal(err)
	}
	grant := capability.Grant{ID: "writer", RootID: rootID, AgentID: "owner", Operations: []string{"write"}, Scopes: []string{root}, ExpiresAt: time.Now().Add(time.Hour)}
	if err := st.IssueCapability(context.Background(), grant); err != nil {
		t.Fatal(err)
	}
	if err := st.SetCapabilityBudget(context.Background(), rootID, "active_operations", 1); err != nil {
		t.Fatal(err)
	}
	dispatcher := capability.NewDispatcher(st, capability.NewWorkspaces(), nil)
	if err := dispatcher.Register(capability.Registration{
		Operation: "write", Mutation: capability.MutationPath, Permission: true,
		Path: func(arguments json.RawMessage) (string, error) {
			var args struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(arguments, &args); err != nil {
				return "", err
			}
			return args.Path, nil
		},
		Handler: func(_ context.Context, call capability.Call) (string, error) {
			return "ok", os.WriteFile(call.CanonicalPath, []byte("changed"), 0o600)
		},
	}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "file")
	request := capability.Request{RootID: rootID, AgentID: "owner", CapabilityID: "writer", OperationID: "pending", TraceID: "trace", Operation: "write", Arguments: json.RawMessage(`{"path":"file"}`)}
	_, err = dispatcher.Dispatch(context.Background(), request)
	var pending *capability.PermissionPendingError
	if !errors.As(err, &pending) {
		t.Fatalf("permission error = %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("pending operation changed workspace: %v", err)
	}
	if err := st.RevokeCapability(context.Background(), "writer"); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatcher.Decide(context.Background(), pending.PermissionID, capability.Decision{Allow: true, PrincipalID: "paired-human"}); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("revoked revalidation error = %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("stale approval changed workspace: %v", err)
	}
	var statuses string
	if err := st.db.QueryRow(`SELECT status || ':' || (SELECT status FROM operations WHERE id='pending') FROM permission_requests WHERE id=?`, pending.PermissionID).Scan(&statuses); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(statuses, "denied/paired-human:denied") {
		t.Fatalf("terminal provenance/status = %q", statuses)
	}
}

func TestWorkspaceMutationRequiresDistinctRootWriter(t *testing.T) {
	root := t.TempDir()
	st, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rootID, err := st.Create(root, "m", "p")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CommitRuntime(context.Background(), RuntimeTransition{Agent: &RuntimeAgent{ID: "owner", RootID: rootID, Status: "idle"}}); err != nil {
		t.Fatal(err)
	}
	grants := []capability.Grant{
		{ID: "shell", RootID: rootID, AgentID: "owner", Operations: []string{"bash"}},
		{ID: "writer", RootID: rootID, AgentID: "owner", Operations: []string{"workspace.write"}, Scopes: []string{root}, Generation: 2},
		{ID: "path-writer", RootID: rootID, AgentID: "owner", Operations: []string{"workspace.write"}, Scopes: []string{filepath.Join(root, "sub")}},
	}
	for _, grant := range grants {
		if err := st.IssueCapability(context.Background(), grant); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetCapabilityBudget(context.Background(), rootID, "active_operations", 1); err != nil {
		t.Fatal(err)
	}
	var runs int
	dispatcher := capability.NewDispatcher(st, capability.NewWorkspaces(), nil)
	if err := dispatcher.Register(capability.Registration{
		Operation: "bash", Mutation: capability.MutationWorkspace,
		Handler: func(context.Context, capability.Call) (string, error) {
			runs++
			return "ok", nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	base := capability.Request{
		RootID: rootID, AgentID: "owner", CapabilityID: "shell", Operation: "bash", TraceID: "trace",
		Arguments: json.RawMessage(`{"command":"true"}`),
	}
	for name, writer := range map[string]struct {
		id         string
		generation int64
	}{
		"missing": {},
		"same":    {id: "shell"},
		"stale":   {id: "writer", generation: 1},
		"scoped":  {id: "path-writer"},
	} {
		request := base
		request.OperationID = "denied-writer-" + name
		request.WriterCapabilityID = writer.id
		request.WriterCapabilityGeneration = writer.generation
		if _, err := dispatcher.Dispatch(context.Background(), request); !errors.Is(err, capability.ErrDenied) {
			t.Errorf("%s writer error = %v", name, err)
		}
		var status string
		if err := st.db.QueryRow(`SELECT status FROM operations WHERE id=?`, request.OperationID).Scan(&status); err != nil || status != string(capability.StatusDenied) {
			t.Errorf("%s writer status=%q err=%v", name, status, err)
		}
	}
	valid := base
	valid.OperationID = "valid-writer"
	valid.WriterCapabilityID = "writer"
	valid.WriterCapabilityGeneration = 2
	if _, err := dispatcher.Dispatch(context.Background(), valid); err != nil {
		t.Fatal(err)
	}
	if runs != 1 {
		t.Fatalf("workspace handler ran %d times, want 1", runs)
	}
}

func TestPendingPermissionResumesAfterStoreReopen(t *testing.T) {
	root := t.TempDir()
	database := filepath.Join(t.TempDir(), "sessions.db")
	st, err := Open(database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rootID, err := st.Create(root, "m", "p")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CommitRuntime(context.Background(), RuntimeTransition{Agent: &RuntimeAgent{ID: "owner", RootID: rootID, Status: "idle"}}); err != nil {
		t.Fatal(err)
	}
	if err := st.IssueCapability(context.Background(), capability.Grant{
		ID: "writer", RootID: rootID, AgentID: "owner", Operations: []string{"write"}, Scopes: []string{root},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetCapabilityBudget(context.Background(), rootID, "active_operations", 1); err != nil {
		t.Fatal(err)
	}
	registration := capability.Registration{
		Operation: "write", Mutation: capability.MutationPath, Permission: true,
		Path: func(arguments json.RawMessage) (string, error) {
			var args struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(arguments, &args); err != nil {
				return "", err
			}
			return args.Path, nil
		},
		Handler: func(_ context.Context, call capability.Call) (string, error) {
			return "ok", os.WriteFile(call.CanonicalPath, []byte("changed"), 0o600)
		},
	}
	dispatcher := capability.NewDispatcher(st, capability.NewWorkspaces(), nil)
	if err := dispatcher.Register(registration); err != nil {
		t.Fatal(err)
	}
	request := capability.Request{
		RootID: rootID, AgentID: "owner", CapabilityID: "writer", OperationID: "pending-reopen", TraceID: "trace", Operation: "write",
		Arguments: json.RawMessage(`{"path":"file"}`),
	}
	_, err = dispatcher.Dispatch(context.Background(), request)
	var pending *capability.PermissionPendingError
	if !errors.As(err, &pending) {
		t.Fatalf("permission error = %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(database)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher = capability.NewDispatcher(st, capability.NewWorkspaces(), nil)
	if err := dispatcher.Register(registration); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatcher.Decide(context.Background(), pending.PermissionID, capability.Decision{Allow: true, PrincipalID: "paired-human"}); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(filepath.Join(root, "file")); err != nil || string(body) != "changed" {
		t.Fatalf("resumed write = %q, %v", body, err)
	}
}
