package session

import (
	"bytes"
	"context"
	"database/sql"
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
	if err := st.db.QueryRowContext(context.Background(), `SELECT status FROM operations WHERE id='denied-scope'`).Scan(&status); err != nil || status != string(capability.StatusDenied) {
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
	if err := st.db.QueryRowContext(context.Background(), `SELECT count(*) FROM agents WHERE root_id=?`, rootID).Scan(&agents); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRowContext(context.Background(), `SELECT count(*) FROM capabilities WHERE root_id=?`, rootID).Scan(&grants); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRowContext(context.Background(), `SELECT count(*) FROM budgets WHERE root_id=? AND kind='active_operations'`, rootID).Scan(&budgets); err != nil {
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
	if err := st.db.QueryRowContext(context.Background(), `SELECT status || ':' || (SELECT status FROM operations WHERE id='pending') FROM permission_requests WHERE id=?`, pending.PermissionID).Scan(&statuses); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(statuses, "denied/owner:denied") {
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
		if err := st.db.QueryRowContext(context.Background(), `SELECT status FROM operations WHERE id=?`, request.OperationID).Scan(&status); err != nil || status != string(capability.StatusDenied) {
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

func TestLargeCapabilityPayloadRoundTripUsesReferences(t *testing.T) {
	st, rootID, agentID := actorFailureFixture(t)
	arguments := json.RawMessage(`{"blob":"` + strings.Repeat("x", MaxContentRead+InlineValueLimit) + `"}`)
	admission := capability.Admission{
		Request: capability.Request{
			RootID: rootID, AgentID: agentID, CapabilityID: "classic-files:" + rootID,
			CapabilityGeneration: 1, OperationID: "large-operation", Operation: "read", Arguments: arguments,
		},
		RequirePermission: true,
		RequestDigest:     "large-digest",
	}
	ticket, err := st.Begin(context.Background(), admission)
	if err != nil {
		t.Fatal(err)
	}
	var inline []byte
	var reference sql.NullString
	if err := st.db.QueryRowContext(context.Background(), `SELECT payload_inline,payload_ref FROM operations WHERE id=?`, admission.Request.OperationID).Scan(&inline, &reference); err != nil {
		t.Fatal(err)
	}
	if len(inline) != 0 || !reference.Valid {
		t.Fatalf("large admission stored inline=%d reference=%v", len(inline), reference)
	}
	pending, err := st.Pending(context.Background(), ticket.PermissionID)
	if err != nil || !sameCapabilityAdmission(pending, admission) {
		t.Fatalf("pending admission changed: equal=%v err=%v", sameCapabilityAdmission(pending, admission), err)
	}
	approved, err := st.Decide(context.Background(), pending, ticket.PermissionID, capability.Decision{Allow: true, PrincipalID: "human"})
	if err != nil {
		t.Fatal(err)
	}
	output := strings.Repeat("result", InlineValueLimit)
	if err := st.Finish(context.Background(), capability.Completion{Admission: admission, LeaseID: approved.LeaseID, Status: capability.StatusSucceeded, Output: output}); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRowContext(context.Background(), `SELECT result_inline,result_ref FROM operations WHERE id=?`, admission.Request.OperationID).Scan(&inline, &reference); err != nil {
		t.Fatal(err)
	}
	result, err := st.readRuntimeValue(context.Background(), inline, reference)
	if err != nil || !bytes.Contains(result, []byte(output)) {
		t.Fatalf("large result did not round-trip: bytes=%d err=%v", len(result), err)
	}
}

func TestCapabilityDecisionsTerminalizeDeniedAndStaleAdmissions(t *testing.T) {
	st, rootID, agentID := actorFailureFixture(t)
	base := capability.Admission{
		Request: capability.Request{
			RootID: rootID, AgentID: agentID, CapabilityID: "classic-files:" + rootID,
			CapabilityGeneration: 1, Operation: "read",
			Reservations: []capability.Reservation{{Kind: "active_operations", Amount: 1}},
		},
		RequirePermission: true,
		RequestDigest:     "digest",
	}
	begin := func(t *testing.T, operationID string) (capability.Admission, capability.Ticket) {
		t.Helper()
		admission := base
		admission.Request.OperationID = operationID
		ticket, err := st.Begin(context.Background(), admission)
		if err != nil {
			t.Fatal(err)
		}
		return admission, ticket
	}
	assertDenied := func(t *testing.T, operationID, permissionID string) {
		t.Helper()
		var statuses string
		if err := st.db.QueryRowContext(context.Background(), `SELECT p.status || ':' || o.status FROM permission_requests p JOIN operations o ON o.id=p.operation_id WHERE p.id=?`, permissionID).Scan(&statuses); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(statuses, "denied/") || !strings.HasSuffix(statuses, ":denied") {
			t.Fatalf("%s statuses=%q", operationID, statuses)
		}
	}

	t.Run("changed admission", func(t *testing.T) {
		admission, ticket := begin(t, "changed-admission")
		changed := admission
		changed.RequestDigest = "different"
		if _, err := st.Decide(context.Background(), changed, ticket.PermissionID, capability.Decision{Allow: true, PrincipalID: "human"}); !errors.Is(err, capability.ErrStaleAdmission) {
			t.Fatalf("decision error=%v", err)
		}
		assertDenied(t, admission.Request.OperationID, ticket.PermissionID)
		if _, err := st.Decide(context.Background(), admission, ticket.PermissionID, capability.Decision{Allow: true}); !errors.Is(err, capability.ErrDenied) {
			t.Fatalf("repeated decision error=%v", err)
		}
	})

	t.Run("explicit denial", func(t *testing.T) {
		admission, ticket := begin(t, "explicit-denial")
		if _, err := st.Decide(context.Background(), admission, ticket.PermissionID, capability.Decision{PrincipalID: "human", Reason: "no"}); !errors.Is(err, capability.ErrDenied) {
			t.Fatalf("decision error=%v", err)
		}
		assertDenied(t, admission.Request.OperationID, ticket.PermissionID)
	})

	t.Run("grant changed before approval", func(t *testing.T) {
		admission, ticket := begin(t, "stale-grant")
		if _, err := st.db.ExecContext(context.Background(), `UPDATE capabilities SET generation=2 WHERE id=?`, admission.Request.CapabilityID); err != nil {
			t.Fatal(err)
		}
		if _, err := st.Decide(context.Background(), admission, ticket.PermissionID, capability.Decision{Allow: true, PrincipalID: "human"}); !errors.Is(err, capability.ErrDenied) {
			t.Fatalf("decision error=%v", err)
		}
		assertDenied(t, admission.Request.OperationID, ticket.PermissionID)
	})

	var reserved int64
	if err := st.db.QueryRowContext(context.Background(), `SELECT reserved_value FROM budgets WHERE root_id=? AND kind='active_operations'`, rootID).Scan(&reserved); err != nil || reserved != 0 {
		t.Fatalf("reserved budget=%d err=%v", reserved, err)
	}
}

func TestCapabilityValidationCorruptionAndRollbackPaths(t *testing.T) {
	ctx := context.Background()
	type dbState struct {
		operations, permissions, leases int
		operation, permission, lease    string
		reserved, used, references      int64
	}
	state := func(t *testing.T, st *Store, rootID, operationID string) dbState {
		t.Helper()
		var got dbState
		err := st.db.QueryRowContext(ctx, `SELECT
			(SELECT COUNT(*) FROM operations WHERE root_id=?),
			(SELECT COUNT(*) FROM permission_requests WHERE root_id=?),
			(SELECT COUNT(*) FROM leases WHERE root_id=?),
			COALESCE((SELECT status FROM operations WHERE id=?),''),
			COALESCE((SELECT status FROM permission_requests WHERE operation_id=?),''),
			COALESCE((SELECT status FROM leases WHERE operation_id=?),''),
			COALESCE((SELECT SUM(reserved_value) FROM budgets WHERE root_id=?),0),
			COALESCE((SELECT SUM(used_value) FROM budgets WHERE root_id=?),0),
			(SELECT COUNT(*) FROM content_references)`,
			rootID, rootID, rootID, operationID, operationID, operationID, rootID, rootID).
			Scan(&got.operations, &got.permissions, &got.leases, &got.operation, &got.permission, &got.lease, &got.reserved, &got.used, &got.references)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	admission := func(rootID, agentID, operationID string) capability.Admission {
		return capability.Admission{Request: capability.Request{
			RootID: rootID, AgentID: agentID, CapabilityID: "classic-files:" + rootID,
			CapabilityGeneration: 1, OperationID: operationID, Operation: "read",
		}}
	}
	pending := func(t *testing.T, st *Store, value *capability.Admission) capability.Ticket {
		t.Helper()
		value.RequirePermission = true
		ticket, err := st.Begin(ctx, *value)
		if err != nil {
			t.Fatal(err)
		}
		return ticket
	}
	running := func(t *testing.T, st *Store, value capability.Admission) capability.Ticket {
		t.Helper()
		ticket, err := st.Begin(ctx, value)
		if err != nil {
			t.Fatal(err)
		}
		return ticket
	}

	t.Run("accessors and bootstrap", func(t *testing.T) {
		st, rootID, agentID := actorFailureFixture(t)
		if st.Processes() == nil {
			t.Fatal("process manager is nil")
		}
		if _, err := st.WorkspaceRoot(ctx, "missing"); err == nil {
			t.Fatal("missing workspace root was accepted")
		}
		exec(t, st, `UPDATE agents SET status='stopped' WHERE id=?`, agentID)
		if _, err := st.EnsureClassicAuthority(ctx, rootID); !errors.Is(err, ErrRootTerminal) {
			t.Fatalf("terminal authority error=%v", err)
		}
	})

	t.Run("closed store", func(t *testing.T) {
		st, rootID, agentID := actorFailureFixture(t)
		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
		value := admission(rootID, agentID, "closed")
		for name, call := range map[string]func() error{
			"workspace": func() error { _, err := st.WorkspaceRoot(ctx, rootID); return err },
			"revoke":    func() error { return st.RevokeCapability(ctx, "classic-files:"+rootID) },
			"begin":     func() error { _, err := st.Begin(ctx, value); return err },
			"pending":   func() error { _, err := st.Pending(ctx, "missing"); return err },
			"decide":    func() error { _, err := st.Decide(ctx, value, "missing", capability.Decision{}); return err },
			"finish": func() error {
				return st.Finish(ctx, capability.Completion{Admission: value, LeaseID: "missing", Status: capability.StatusSucceeded})
			},
		} {
			t.Run(name, func(t *testing.T) {
				if err := call(); err == nil {
					t.Fatal("closed store call succeeded")
				}
			})
		}
	})

	t.Run("missing workspace", func(t *testing.T) {
		st, rootID := capabilityRoot(t, filepath.Join(t.TempDir(), "missing"))
		if _, err := st.EnsureClassicAuthority(ctx, rootID); err == nil {
			t.Fatal("missing workspace was accepted")
		}
	})

	for _, test := range []struct {
		name    string
		trigger string
	}{
		{name: "agent insert", trigger: `CREATE TRIGGER fail_bootstrap BEFORE INSERT ON agents BEGIN SELECT RAISE(ABORT,'agent'); END`},
		{name: "capability insert", trigger: `CREATE TRIGGER fail_bootstrap BEFORE INSERT ON capabilities BEGIN SELECT RAISE(ABORT,'capability'); END`},
		{name: "budget insert", trigger: `CREATE TRIGGER fail_bootstrap BEFORE INSERT ON budgets BEGIN SELECT RAISE(ABORT,'budget'); END`},
	} {
		t.Run("bootstrap "+test.name+" failure", func(t *testing.T) {
			st, rootID := capabilityRoot(t, t.TempDir())
			exec(t, st, test.trigger)
			if _, err := st.EnsureClassicAuthority(ctx, rootID); err == nil {
				t.Fatal("bootstrap write failure was ignored")
			}
		})
	}

	t.Run("issue validation", func(t *testing.T) {
		st, rootID, agentID := actorFailureFixture(t)
		root, err := st.WorkspaceRoot(ctx, rootID)
		if err != nil {
			t.Fatal(err)
		}
		for name, grant := range map[string]capability.Grant{
			"identity":       {},
			"outside scope":  {ID: "outside", RootID: rootID, AgentID: agentID, Operations: []string{"read"}, Scopes: []string{filepath.Dir(root)}},
			"scoped shell":   {ID: "shell", RootID: rootID, AgentID: agentID, Operations: []string{"bash"}, Scopes: []string{root}},
			"unknown agent":  {ID: "unknown-agent", RootID: rootID, AgentID: "missing", Operations: []string{"read"}},
			"unknown issuer": {ID: "unknown-issuer", RootID: rootID, AgentID: agentID, IssuerAgentID: "missing", Operations: []string{"read"}},
		} {
			if err := st.IssueCapability(ctx, grant); err == nil {
				t.Errorf("%s grant succeeded", name)
			}
		}
		grant := capability.Grant{ID: "duplicate", RootID: rootID, AgentID: agentID, Operations: []string{"read"}}
		if err := st.IssueCapability(ctx, grant); err != nil {
			t.Fatal(err)
		}
		if err := st.IssueCapability(ctx, grant); err == nil {
			t.Fatal("duplicate grant succeeded")
		}
	})

	t.Run("begin marshal failure", func(t *testing.T) {
		st, rootID, agentID := actorFailureFixture(t)
		value := admission(rootID, agentID, "bad-json")
		value.Request.Arguments = json.RawMessage(`{`)
		if _, err := st.Begin(ctx, value); err == nil {
			t.Fatal("invalid admission JSON was accepted")
		}
	})

	t.Run("begin terminal and denied", func(t *testing.T) {
		st, rootID, agentID := actorFailureFixture(t)
		exec(t, st, `UPDATE agents SET status='stopped' WHERE id=?`, agentID)
		if _, err := st.Begin(ctx, admission(rootID, agentID, "terminal-agent")); !errors.Is(err, capability.ErrDenied) {
			t.Fatalf("terminal agent error=%v", err)
		}

		st, rootID, agentID = actorFailureFixture(t)
		value := admission(rootID, agentID, "unsupported-operation")
		value.Request.Operation = "missing"
		if _, err := st.Begin(ctx, value); !errors.Is(err, capability.ErrDenied) {
			t.Fatalf("unsupported operation error=%v", err)
		}

		value = admission(rootID, agentID, "budget-denied")
		value.Request.Reservations = []capability.Reservation{{Kind: "active_operations", Amount: 1 << 30}}
		if _, err := st.Begin(ctx, value); !errors.Is(err, capability.ErrDenied) {
			t.Fatalf("budget denial error=%v", err)
		}
	})

	t.Run("begin persistence failures", func(t *testing.T) {
		for _, test := range []struct {
			name    string
			trigger string
			prepare func(*capability.Admission)
		}{
			{name: "content", trigger: `CREATE TRIGGER fail_begin BEFORE INSERT ON content_objects BEGIN SELECT RAISE(ABORT,'content'); END`, prepare: func(value *capability.Admission) {
				value.Request.Arguments = json.RawMessage(`{"value":"` + strings.Repeat("x", InlineValueLimit) + `"}`)
			}},
			{name: "budget", trigger: `CREATE TRIGGER fail_begin BEFORE UPDATE ON budgets BEGIN SELECT RAISE(ABORT,'budget'); END`, prepare: func(value *capability.Admission) {
				value.Request.Reservations = []capability.Reservation{{Kind: "active_operations", Amount: 1}}
			}},
			{name: "permission", trigger: `CREATE TRIGGER fail_begin BEFORE INSERT ON permission_requests BEGIN SELECT RAISE(ABORT,'permission'); END`, prepare: func(value *capability.Admission) {
				value.RequirePermission = true
			}},
			{name: "lease", trigger: `CREATE TRIGGER fail_begin BEFORE INSERT ON leases BEGIN SELECT RAISE(ABORT,'lease'); END`},
		} {
			t.Run(test.name, func(t *testing.T) {
				st, rootID, agentID := actorFailureFixture(t)
				value := admission(rootID, agentID, "begin-"+test.name)
				if test.prepare != nil {
					test.prepare(&value)
				}
				value.Request.Reservations = []capability.Reservation{{Kind: "active_operations", Amount: 1}}
				exec(t, st, test.trigger)
				before := state(t, st, rootID, value.Request.OperationID)
				if _, err := st.Begin(ctx, value); err == nil {
					t.Fatal("begin write failure was ignored")
				}
				if after := state(t, st, rootID, value.Request.OperationID); after != before {
					t.Fatalf("begin failure did not roll back: before=%+v after=%+v", before, after)
				}
			})
		}
	})

	t.Run("duplicate operations", func(t *testing.T) {
		st, rootID, agentID := actorFailureFixture(t)
		value := admission(rootID, agentID, "duplicate-running")
		if _, err := st.Begin(ctx, value); err != nil {
			t.Fatal(err)
		}
		if _, err := st.Begin(ctx, value); err == nil {
			t.Fatal("duplicate running operation succeeded")
		}

		denied := admission(rootID, agentID, "duplicate-denied")
		denied.Request.Operation = "missing"
		if _, err := st.Begin(ctx, denied); err == nil {
			t.Fatal("first denied operation returned nil")
		}
		if _, err := st.Begin(ctx, denied); err == nil {
			t.Fatal("duplicate denied operation returned nil")
		}
	})

	t.Run("denied admission content failure", func(t *testing.T) {
		st, rootID, agentID := actorFailureFixture(t)
		value := admission(rootID, agentID, "denied-content")
		value.Request.Operation = "missing"
		value.Request.Arguments = json.RawMessage(`{"value":"` + strings.Repeat("x", InlineValueLimit) + `"}`)
		exec(t, st, `CREATE TRIGGER fail_denied_content BEFORE INSERT ON content_objects BEGIN SELECT RAISE(ABORT,'content'); END`)
		if _, err := st.Begin(ctx, value); err == nil {
			t.Fatal("denied content persistence failure was ignored")
		}
	})

	t.Run("pending terminal and corruption", func(t *testing.T) {
		st, rootID, agentID := actorFailureFixture(t)
		value := admission(rootID, agentID, "pending-terminal")
		ticket := pending(t, st, &value)
		exec(t, st, `UPDATE operations SET status='running' WHERE id=?`, value.Request.OperationID)
		if _, err := st.Pending(ctx, ticket.PermissionID); !errors.Is(err, capability.ErrDenied) {
			t.Fatalf("terminal pending error=%v", err)
		}

		st, rootID, agentID = actorFailureFixture(t)
		value = admission(rootID, agentID, "pending-corrupt")
		ticket = pending(t, st, &value)
		exec(t, st, `UPDATE operations SET payload_inline='{' WHERE id=?`, value.Request.OperationID)
		if _, err := st.Pending(ctx, ticket.PermissionID); err == nil {
			t.Fatal("corrupt pending admission was accepted")
		}
	})

	t.Run("decision lookup and corruption", func(t *testing.T) {
		st, rootID, agentID := actorFailureFixture(t)
		value := admission(rootID, agentID, "missing-permission")
		if _, err := st.Decide(ctx, value, "missing", capability.Decision{Allow: true}); err == nil {
			t.Fatal("missing permission was accepted")
		}

		value.Request.OperationID = "corrupt-decision"
		ticket := pending(t, st, &value)
		exec(t, st, `UPDATE operations SET payload_inline='{' WHERE id=?`, value.Request.OperationID)
		if _, err := st.Decide(ctx, value, ticket.PermissionID, capability.Decision{Allow: true}); err == nil {
			t.Fatal("corrupt decision admission was accepted")
		}
	})

	t.Run("decision revalidation", func(t *testing.T) {
		st, rootID, agentID := actorFailureFixture(t)
		value := admission(rootID, agentID, "terminal-decision")
		ticket := pending(t, st, &value)
		exec(t, st, `UPDATE agents SET status='stopped' WHERE id=?`, agentID)
		if _, err := st.Decide(ctx, value, ticket.PermissionID, capability.Decision{Allow: true}); !errors.Is(err, capability.ErrDenied) {
			t.Fatalf("terminal decision error=%v", err)
		}

		st, rootID, agentID = actorFailureFixture(t)
		value = admission(rootID, agentID, "budget-decision")
		value.Request.Reservations = []capability.Reservation{{Kind: "active_operations", Amount: 1}}
		ticket = pending(t, st, &value)
		exec(t, st, `UPDATE budgets SET reserved_value=0 WHERE root_id=? AND kind='active_operations'`, rootID)
		if _, err := st.Decide(ctx, value, ticket.PermissionID, capability.Decision{Allow: true}); !errors.Is(err, capability.ErrDenied) {
			t.Fatalf("budget decision error=%v", err)
		}
	})

	for _, test := range []struct {
		name     string
		decision capability.Decision
		change   func(*capability.Admission)
		trigger  string
	}{
		{name: "stale permission update", decision: capability.Decision{Allow: true}, change: func(value *capability.Admission) { value.RequestDigest = "changed" }, trigger: `CREATE TRIGGER fail_decide BEFORE UPDATE ON permission_requests BEGIN SELECT RAISE(ABORT,'permission'); END`},
		{name: "denied operation update", decision: capability.Decision{}, trigger: `CREATE TRIGGER fail_decide BEFORE UPDATE ON operations BEGIN SELECT RAISE(ABORT,'operation'); END`},
		{name: "lease insert", decision: capability.Decision{Allow: true}, trigger: `CREATE TRIGGER fail_decide BEFORE INSERT ON leases BEGIN SELECT RAISE(ABORT,'lease'); END`},
		{name: "approval operation update", decision: capability.Decision{Allow: true}, trigger: `CREATE TRIGGER fail_decide BEFORE UPDATE ON operations BEGIN SELECT RAISE(ABORT,'operation'); END`},
		{name: "approval permission update", decision: capability.Decision{Allow: true}, trigger: `CREATE TRIGGER fail_decide BEFORE UPDATE ON permission_requests BEGIN SELECT RAISE(ABORT,'permission'); END`},
	} {
		t.Run("decision "+test.name+" failure", func(t *testing.T) {
			st, rootID, agentID := actorFailureFixture(t)
			stored := admission(rootID, agentID, "decision-"+test.name)
			stored.Request.Reservations = []capability.Reservation{{Kind: "active_operations", Amount: 1}}
			ticket := pending(t, st, &stored)
			supplied := stored
			if test.change != nil {
				test.change(&supplied)
			}
			exec(t, st, test.trigger)
			before := state(t, st, rootID, stored.Request.OperationID)
			if _, err := st.Decide(ctx, supplied, ticket.PermissionID, test.decision); err == nil {
				t.Fatal("decision write failure was ignored")
			}
			if after := state(t, st, rootID, stored.Request.OperationID); after != before {
				t.Fatalf("decision failure did not roll back: before=%+v after=%+v", before, after)
			}
		})
	}

	t.Run("malformed capability records", func(t *testing.T) {
		for _, test := range []struct{ column, value string }{
			{column: "operations", value: "{"},
			{column: "scopes", value: "{"},
		} {
			t.Run(test.column, func(t *testing.T) {
				st, rootID, agentID := actorFailureFixture(t)
				exec(t, st, `UPDATE capabilities SET `+test.column+`=? WHERE id=?`, test.value, "classic-files:"+rootID)
				if _, err := st.Begin(ctx, admission(rootID, agentID, "malformed-"+test.column)); err == nil {
					t.Fatal("malformed capability record was accepted")
				}
			})
		}
	})

	t.Run("finish validation and corruption", func(t *testing.T) {
		st, rootID, agentID := actorFailureFixture(t)
		value := admission(rootID, agentID, "finish-validation")
		if err := st.Finish(ctx, capability.Completion{Admission: value, Status: capability.StatusDenied}); err == nil {
			t.Fatal("invalid completion status was accepted")
		}
		if err := st.Finish(ctx, capability.Completion{Admission: value, LeaseID: "missing", Status: capability.StatusSucceeded}); err == nil {
			t.Fatal("missing completion was accepted")
		}

		ticket := running(t, st, value)
		exec(t, st, `UPDATE operations SET status='succeeded' WHERE id=?`, value.Request.OperationID)
		if err := st.Finish(ctx, capability.Completion{Admission: value, LeaseID: ticket.LeaseID, Status: capability.StatusSucceeded}); !errors.Is(err, capability.ErrDenied) {
			t.Fatalf("terminal completion error=%v", err)
		}

		st, rootID, agentID = actorFailureFixture(t)
		value = admission(rootID, agentID, "finish-corrupt")
		ticket = running(t, st, value)
		exec(t, st, `UPDATE operations SET payload_inline='{' WHERE id=?`, value.Request.OperationID)
		if err := st.Finish(ctx, capability.Completion{Admission: value, LeaseID: ticket.LeaseID, Status: capability.StatusSucceeded}); err == nil {
			t.Fatal("corrupt completion admission was accepted")
		}

		st, rootID, agentID = actorFailureFixture(t)
		value = admission(rootID, agentID, "finish-stale")
		ticket = running(t, st, value)
		changed := value
		changed.RequestDigest = "changed"
		if err := st.Finish(ctx, capability.Completion{Admission: changed, LeaseID: ticket.LeaseID, Status: capability.StatusSucceeded}); !errors.Is(err, capability.ErrStaleAdmission) {
			t.Fatalf("stale completion error=%v", err)
		}
	})

	for _, test := range []struct {
		name         string
		trigger      string
		reservations []capability.Reservation
	}{
		{name: "content insert", trigger: `CREATE TRIGGER fail_finish BEFORE INSERT ON content_objects BEGIN SELECT RAISE(ABORT,'content'); END`},
		{name: "operation update", trigger: `CREATE TRIGGER fail_finish BEFORE UPDATE ON operations BEGIN SELECT RAISE(ABORT,'operation'); END`},
		{name: "lease update", trigger: `CREATE TRIGGER fail_finish BEFORE UPDATE ON leases BEGIN SELECT RAISE(ABORT,'lease'); END`},
		{name: "budget update", trigger: `CREATE TRIGGER fail_finish BEFORE UPDATE ON budgets BEGIN SELECT RAISE(ABORT,'budget'); END`, reservations: []capability.Reservation{{Kind: "active_operations", Amount: 1}}},
	} {
		t.Run("finish "+test.name+" failure", func(t *testing.T) {
			st, rootID, agentID := actorFailureFixture(t)
			value := admission(rootID, agentID, "finish-"+test.name)
			value.Request.Reservations = test.reservations
			ticket := running(t, st, value)
			exec(t, st, test.trigger)
			before := state(t, st, rootID, value.Request.OperationID)
			completion := capability.Completion{Admission: value, LeaseID: ticket.LeaseID, Status: capability.StatusSucceeded}
			if test.name == "content insert" {
				completion.Output = strings.Repeat("output", InlineValueLimit)
			}
			if err := st.Finish(ctx, completion); err == nil {
				t.Fatal("completion write failure was ignored")
			}
			if after := state(t, st, rootID, value.Request.OperationID); after != before {
				t.Fatalf("completion failure did not roll back: before=%+v after=%+v", before, after)
			}
		})
	}

	t.Run("runtime value corruption", func(t *testing.T) {
		st, _, _ := actorFailureFixture(t)
		missing := sql.NullString{String: "missing", Valid: true}
		if _, err := st.readRuntimeValue(ctx, nil, missing); err == nil {
			t.Fatal("missing runtime reference was accepted")
		}
		tx, err := st.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.readRuntimeValueTx(ctx, tx, nil, missing); err == nil {
			t.Fatal("missing transactional runtime reference was accepted")
		}
		_ = tx.Rollback()

		body, err := st.content.Put([]byte("short"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.readContentBody(body.Digest, body.Size+1); err == nil {
			t.Fatal("truncated content body was accepted")
		}
		if _, err := st.readContentBody(strings.Repeat("0", 64), 1); err == nil {
			t.Fatal("missing content body was accepted")
		}
	})
}

func capabilityRoot(t *testing.T, workspace string) (*Store, string) {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rootID, err := st.Create(workspace, "m", "p")
	if err != nil {
		t.Fatal(err)
	}
	return st, rootID
}
