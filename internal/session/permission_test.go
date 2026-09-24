package session

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/capability"
)

func TestAgentAdmissionDelegatesNarrowInspectableCapabilityAtomically(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	root, err := store.WorkspaceRoot(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	allowed := filepath.Join(root, "allowed")
	allowed, err = filepath.EvalSymlinks(filepath.Dir(allowed))
	if err != nil {
		t.Fatal(err)
	}
	allowed = filepath.Join(allowed, "allowed")

	if _, err := store.AdmitAgent(context.Background(), AgentAdmission{
		RootID: rootID, ParentAgentID: rootAgentID, ChildAgentID: "child", Capabilities: []CapabilityDelegation{{
			ID: "child-read", Issuer: capability.Reference{ID: "files:" + rootID, Generation: 1},
			AgentID: "child", Operations: []string{"read"}, Scopes: []string{allowed},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	record, err := store.InspectCapability(context.Background(), rootID, rootAgentID, "child-read")
	if err != nil {
		t.Fatal(err)
	}
	if record.RootID != rootID || record.AgentID != "child" || record.IssuerAgentID != rootAgentID || record.Generation != 1 || record.Status != "active" {
		t.Fatalf("delegated record = %+v", record)
	}
	if len(record.Operations) != 1 || record.Operations[0] != "read" || len(record.Scopes) != 1 || record.Scopes[0] != allowed {
		t.Fatalf("delegated authority = %+v", record)
	}

	if _, err := store.AdmitAgent(context.Background(), AgentAdmission{
		RootID: rootID, ParentAgentID: rootAgentID, ChildAgentID: "rolled-back", Capabilities: []CapabilityDelegation{{
			ID: "escalated", Issuer: capability.Reference{ID: "files:" + rootID, Generation: 1},
			AgentID: "rolled-back", Operations: []string{"network"},
		}},
	}); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("atomic escalation error = %v", err)
	}
	var children, grants int
	if err := store.db.QueryRowContext(context.Background(), `SELECT count(*) FROM agents WHERE id='rolled-back'`).Scan(&children); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(context.Background(), `SELECT count(*) FROM capabilities WHERE id='escalated'`).Scan(&grants); err != nil {
		t.Fatal(err)
	}
	if children != 0 || grants != 0 {
		t.Fatalf("failed admission persisted children=%d grants=%d", children, grants)
	}
}

func TestDelegationDeniesAuthorityEscalationAndUnrelatedCallers(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	ctx := context.Background()
	root, err := store.WorkspaceRoot(ctx, rootID)
	if err != nil {
		t.Fatal(err)
	}
	allowed := filepath.Join(root, "allowed")
	for _, child := range []string{"target", "unrelated", "terminal-parent"} {
		admitTestChild(t, store, rootID, rootAgentID, child)
	}
	admitTestChild(t, store, rootID, "terminal-parent", "terminal-child")
	expires := time.Now().Add(time.Hour)
	for _, grant := range []capability.Grant{
		{ID: "scoped", RootID: rootID, AgentID: rootAgentID, Operations: []string{"read", "write"}, Scopes: []string{allowed}, Generation: 4, ExpiresAt: expires},
		{ID: "read-only", RootID: rootID, AgentID: rootAgentID, Operations: []string{"read"}, Scopes: []string{allowed}, Generation: 1},
		{ID: "expired", RootID: rootID, AgentID: rootAgentID, Operations: []string{"read"}, Scopes: []string{allowed}, Generation: 1, ExpiresAt: time.Now().Add(-time.Hour)},
		{ID: "revoked", RootID: rootID, AgentID: rootAgentID, Operations: []string{"read"}, Scopes: []string{allowed}, Generation: 1},
		{ID: "terminal-issuer", RootID: rootID, AgentID: "terminal-parent", Operations: []string{"read"}, Scopes: []string{allowed}, Generation: 1},
	} {
		if err := store.IssueCapability(ctx, grant); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.RevokeCapability(ctx, "revoked"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.TerminalizeSubtree(ctx, rootID, rootAgentID, "terminal-parent", "stopped"); err != nil {
		t.Fatal(err)
	}

	base := CapabilityDelegation{
		ID: "denied", Issuer: capability.Reference{ID: "scoped", Generation: 4}, AgentID: "target",
		Operations: []string{"read"}, Scopes: []string{filepath.Join(allowed, "narrow")}, Generation: 1,
		ExpiresAt: time.Now().Add(30 * time.Minute),
	}
	tests := map[string]struct {
		rootID string
		caller string
		value  CapabilityDelegation
	}{
		"missing issuer":      {rootID, rootAgentID, func() CapabilityDelegation { v := base; v.Issuer.ID = ""; return v }()},
		"duplicate operation": {rootID, rootAgentID, func() CapabilityDelegation { v := base; v.Operations = []string{"read", "read"}; return v }()},
		"operation":           {rootID, rootAgentID, func() CapabilityDelegation { v := base; v.Operations = []string{"network"}; return v }()},
		"path":                {rootID, rootAgentID, func() CapabilityDelegation { v := base; v.Scopes = []string{filepath.Join(root, "other")}; return v }()},
		"expiry":              {rootID, rootAgentID, func() CapabilityDelegation { v := base; v.ExpiresAt = expires.Add(time.Minute); return v }()},
		"missing expiry":      {rootID, rootAgentID, func() CapabilityDelegation { v := base; v.ExpiresAt = time.Time{}; return v }()},
		"generation":          {rootID, rootAgentID, func() CapabilityDelegation { v := base; v.Generation = 2; return v }()},
		"issuer generation":   {rootID, rootAgentID, func() CapabilityDelegation { v := base; v.Issuer.Generation = 3; return v }()},
		"shell path": {rootID, rootAgentID, func() CapabilityDelegation {
			v := base
			v.Issuer = capability.Reference{ID: "shell:" + rootID, Generation: 1}
			v.Operations = []string{"bash"}
			return v
		}()},
		"writer without writer grant": {rootID, rootAgentID, func() CapabilityDelegation {
			v := base
			v.Issuer = capability.Reference{ID: "read-only", Generation: 1}
			v.Operations = []string{"workspace.write"}
			return v
		}()},
		"writer without scope": {rootID, rootAgentID, func() CapabilityDelegation {
			v := base
			v.Issuer = capability.Reference{ID: "files:" + rootID, Generation: 1}
			v.Operations = []string{"workspace.write"}
			v.Scopes = nil
			v.ExpiresAt = time.Time{}
			return v
		}()},
		"expired issuer": {rootID, rootAgentID, func() CapabilityDelegation {
			v := base
			v.Issuer = capability.Reference{ID: "expired", Generation: 1}
			return v
		}()},
		"revoked issuer": {rootID, rootAgentID, func() CapabilityDelegation {
			v := base
			v.Issuer = capability.Reference{ID: "revoked", Generation: 1}
			return v
		}()},
		"terminal issuer": {rootID, "terminal-parent", func() CapabilityDelegation {
			v := base
			v.Issuer = capability.Reference{ID: "terminal-issuer", Generation: 1}
			v.AgentID = "terminal-child"
			return v
		}()},
		"terminal subject": {rootID, rootAgentID, func() CapabilityDelegation { v := base; v.AgentID = "terminal-child"; return v }()},
		"unrelated":        {rootID, "unrelated", base},
	}
	otherRoot, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	otherAuthority, err := store.EnsureAuthority(ctx, otherRoot)
	if err != nil {
		t.Fatal(err)
	}
	tests["cross root"] = struct {
		rootID string
		caller string
		value  CapabilityDelegation
	}{rootID, otherAuthority.AgentID, base}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			value := test.value
			value.ID = "denied-" + strings.ReplaceAll(name, " ", "-")
			if _, err := store.DelegateCapability(ctx, test.rootID, test.caller, value); !errors.Is(err, capability.ErrDenied) {
				t.Fatalf("delegation error = %v", err)
			}
		})
	}
}

func TestDelegationAllowsEqualBoundedAuthorityButNotTransfer(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	ctx := context.Background()
	root, err := store.WorkspaceRoot(ctx, rootID)
	if err != nil {
		t.Fatal(err)
	}
	allowed := filepath.Join(root, "allowed")
	for _, child := range []string{"target", "sibling"} {
		admitTestChild(t, store, rootID, rootAgentID, child)
	}
	expires := time.Now().Add(time.Hour)
	if err := store.IssueCapability(ctx, capability.Grant{
		ID: "issuer-read", RootID: rootID, AgentID: rootAgentID, Operations: []string{"read"},
		Scopes: []string{allowed}, Generation: 1, ExpiresAt: expires,
	}); err != nil {
		t.Fatal(err)
	}
	record, err := store.DelegateCapability(ctx, rootID, rootAgentID, CapabilityDelegation{
		ID: "target-read", Issuer: capability.Reference{ID: "issuer-read", Generation: 1}, AgentID: "target",
		Operations: []string{"read"}, Scopes: []string{allowed}, Generation: 1, ExpiresAt: expires,
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.AgentID != "target" || record.IssuerAgentID != rootAgentID || !record.ExpiresAt.Equal(expires) {
		t.Fatalf("equal delegated record = %+v", record)
	}

	dispatcher := capability.NewDispatcher(store, store.Workspaces(), nil)
	runs := 0
	if err := dispatcher.Register(capability.Registration{
		Operation: "read",
		Path: func(arguments json.RawMessage) (string, error) {
			var request struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(arguments, &request); err != nil {
				return "", err
			}
			return request.Path, nil
		},
		Handler: func(context.Context, capability.Call) (string, error) {
			runs++
			return "ok", nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	arguments, err := json.Marshal(map[string]string{"path": filepath.Join(allowed, "file.txt")})
	if err != nil {
		t.Fatal(err)
	}
	request := capability.Request{
		RootID: rootID, AgentID: "target", CapabilityID: "target-read", CapabilityGeneration: 1,
		OperationID: "target-operation", Operation: "read", Arguments: arguments, TraceID: "target-trace",
	}
	if _, err := dispatcher.Dispatch(ctx, request); err != nil {
		t.Fatal(err)
	}
	request.AgentID = "sibling"
	request.OperationID = "sibling-operation"
	request.TraceID = "sibling-trace"
	if _, err := dispatcher.Dispatch(ctx, request); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("sibling transfer error = %v", err)
	}
	if runs != 1 {
		t.Fatalf("equal delegated capability ran %d handlers", runs)
	}
}

func TestCapabilityRevocationCancelsPendingPermissionAndRetainsHistory(t *testing.T) {
	for _, revokeWriter := range []bool{false, true} {
		name := "operation"
		if revokeWriter {
			name = "writer"
		}
		t.Run(name, func(t *testing.T) {
			store, rootID, rootAgentID := newSwarmFixture(t)
			ctx := context.Background()
			root, err := store.WorkspaceRoot(ctx, rootID)
			if err != nil {
				t.Fatal(err)
			}
			admitTestChild(t, store, rootID, rootAgentID, "child")
			admitTestChild(t, store, rootID, rootAgentID, "sibling")
			for _, grant := range []capability.Grant{
				{ID: "operation", RootID: rootID, AgentID: "child", IssuerAgentID: rootAgentID, Operations: []string{"write"}, Scopes: []string{root}, Generation: 1},
				{ID: "writer", RootID: rootID, AgentID: "child", IssuerAgentID: rootAgentID, Operations: []string{"workspace.write"}, Scopes: []string{root}, Generation: 1},
				{ID: "unrelated", RootID: rootID, AgentID: "child", IssuerAgentID: rootAgentID, Operations: []string{"read"}, Scopes: []string{root}, Generation: 1},
			} {
				if err := store.IssueCapability(ctx, grant); err != nil {
					t.Fatal(err)
				}
			}
			dispatcher := capability.NewDispatcher(store, store.Workspaces(), nil)
			runs := 0
			if err := dispatcher.Register(capability.Registration{
				Operation: "write", Mutation: capability.MutationWorkspace, Permission: true,
				Handler: func(context.Context, capability.Call) (string, error) { runs++; return "unexpected", nil },
			}); err != nil {
				t.Fatal(err)
			}
			request := capability.Request{
				RootID: rootID, AgentID: "child", CapabilityID: "operation", CapabilityGeneration: 1,
				WriterCapabilityID: "writer", WriterCapabilityGeneration: 1,
				OperationID: "pending", Operation: "write", TraceID: "trace", Arguments: json.RawMessage(`{"path":"file"}`),
			}
			_, err = dispatcher.Dispatch(ctx, request)
			var pending *capability.PermissionPendingError
			if !errors.As(err, &pending) {
				t.Fatalf("pending error = %v", err)
			}
			if got := budgetState(t, store, rootID, "child", BudgetActiveOperations); got.Reserved != 1 {
				t.Fatalf("pending budget = %+v", got)
			}
			revokedID := "operation"
			if revokeWriter {
				revokedID = "writer"
			}
			if _, err := store.RevokeCapabilityFor(ctx, rootID, "sibling", revokedID); !errors.Is(err, capability.ErrDenied) {
				t.Fatalf("unrelated revocation error = %v", err)
			}
			otherRoot, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
			if err != nil {
				t.Fatal(err)
			}
			otherAuthority, err := store.EnsureAuthority(ctx, otherRoot)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.RevokeCapabilityFor(ctx, rootID, otherAuthority.AgentID, revokedID); !errors.Is(err, capability.ErrDenied) {
				t.Fatalf("cross-root revocation error = %v", err)
			}
			record, err := store.RevokeCapabilityFor(ctx, rootID, rootAgentID, revokedID)
			if err != nil {
				t.Fatal(err)
			}
			if record.Status != "revoked" || record.Generation != 2 {
				t.Fatalf("revoked record = %+v", record)
			}
			if _, err := store.RevokeCapabilityFor(ctx, rootID, rootAgentID, revokedID); !errors.Is(err, capability.ErrDenied) {
				t.Fatalf("duplicate revocation error = %v", err)
			}
			if got := budgetState(t, store, rootID, "child", BudgetActiveOperations); got.Reserved != 0 || got.Used != 0 {
				t.Fatalf("revoked budget = %+v", got)
			}
			if _, err := dispatcher.Decide(ctx, pending.PermissionID, capability.Decision{Allow: true, PrincipalID: "late-human"}); !errors.Is(err, capability.ErrDenied) {
				t.Fatalf("stale approval error = %v", err)
			}
			request.OperationID = "later"
			if _, err := dispatcher.Dispatch(ctx, request); !errors.Is(err, capability.ErrDenied) {
				t.Fatalf("old generation admission error = %v", err)
			}
			if runs != 0 {
				t.Fatalf("revoked handler ran %d times", runs)
			}
			var permissionStatus, operationStatus, unrelatedStatus string
			if err := store.db.QueryRowContext(context.Background(), `SELECT status FROM permission_requests WHERE id=?`, pending.PermissionID).Scan(&permissionStatus); err != nil {
				t.Fatal(err)
			}
			if err := store.db.QueryRowContext(context.Background(), `SELECT status FROM operations WHERE id='pending'`).Scan(&operationStatus); err != nil {
				t.Fatal(err)
			}
			if err := store.db.QueryRowContext(context.Background(), `SELECT status FROM capabilities WHERE id='unrelated'`).Scan(&unrelatedStatus); err != nil {
				t.Fatal(err)
			}
			if permissionStatus != "denied/"+rootAgentID || operationStatus != "denied" || unrelatedStatus != "active" {
				t.Fatalf("statuses permission=%q operation=%q unrelated=%q", permissionStatus, operationStatus, unrelatedStatus)
			}
			inspected, err := store.InspectCapability(ctx, rootID, rootAgentID, revokedID)
			if err != nil || inspected.Status != "revoked" || inspected.Generation != 2 {
				t.Fatalf("historical inspection = %+v, %v", inspected, err)
			}
			var rows int
			if err := store.db.QueryRowContext(context.Background(), `SELECT count(*) FROM capabilities WHERE id=?`, revokedID).Scan(&rows); err != nil || rows != 1 {
				t.Fatalf("historical rows=%d err=%v", rows, err)
			}
			var payload []byte
			if err := store.db.QueryRowContext(context.Background(), `SELECT payload_inline FROM events WHERE root_id=? AND kind='capability.revoked' ORDER BY seq DESC LIMIT 1`, rootID).Scan(&payload); err != nil {
				t.Fatal(err)
			}
			var event actorEvent
			if err := json.Unmarshal(payload, &event); err != nil || event.AgentID != "child" || event.SenderAgentID != rootAgentID || event.CapabilityID != revokedID || event.Generation != 2 {
				t.Fatalf("revocation event = %+v, %v", event, err)
			}
		})
	}
}

func TestSubtreeStopReleasesPendingPermissionReservationWithoutConsumption(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	ctx := context.Background()
	admitTestChild(t, store, rootID, rootAgentID, "child")
	if err := store.SetBudgetLimit(ctx, rootID, "", BudgetTokens, 10); err != nil {
		t.Fatal(err)
	}
	if err := store.IssueCapability(ctx, capability.Grant{ID: "child-read", RootID: rootID, AgentID: "child", Operations: []string{"read"}, Generation: 1}); err != nil {
		t.Fatal(err)
	}
	admission := capability.Admission{Request: capability.Request{
		RootID: rootID, AgentID: "child", CapabilityID: "child-read", CapabilityGeneration: 1,
		OperationID: "waiting", Operation: "read", TraceID: "trace",
		Reservations: []capability.Reservation{{Kind: string(BudgetTokens), Amount: 4}},
	}, RequirePermission: true}
	ticket, err := store.Begin(ctx, admission)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.TerminalizeSubtree(ctx, rootID, rootAgentID, "child", "stopped"); err != nil {
		t.Fatal(err)
	}
	if got := budgetState(t, store, rootID, rootAgentID, BudgetTokens); got.Used != 0 || got.Reserved != 0 {
		t.Fatalf("stopped pending budget = %+v", got)
	}
	var permissionStatus, operationStatus string
	if err := store.db.QueryRowContext(context.Background(), `SELECT status FROM permission_requests WHERE id=?`, ticket.PermissionID).Scan(&permissionStatus); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(context.Background(), `SELECT status FROM operations WHERE id='waiting'`).Scan(&operationStatus); err != nil {
		t.Fatal(err)
	}
	if permissionStatus != "interrupted" || operationStatus != "interrupted" {
		t.Fatalf("stopped pending statuses=%q/%q", permissionStatus, operationStatus)
	}
}

func TestCapabilityInspectionRevocationAndCorruptRecords(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	ctx := context.Background()
	admitTestChild(t, store, rootID, rootAgentID, "child")
	admitTestChild(t, store, rootID, rootAgentID, "sibling")
	if err := store.IssueCapability(ctx, capability.Grant{ID: "inspect", RootID: rootID, AgentID: "child", Operations: []string{"read"}, Generation: 1}); err != nil {
		t.Fatal(err)
	}
	record, err := store.InspectCapability(ctx, rootID, "child", "inspect")
	if err != nil || record.ID != "inspect" || record.AgentID != "child" || record.Generation != 1 {
		t.Fatalf("self inspection=%+v err=%v", record, err)
	}
	for name, caller := range map[string]string{"sibling": "sibling", "missing caller": "missing"} {
		if _, err := store.InspectCapability(ctx, rootID, caller, "inspect"); !errors.Is(err, capability.ErrDenied) {
			t.Fatalf("%s inspection error=%v", name, err)
		}
	}
	if _, err := store.InspectCapability(ctx, rootID, rootAgentID, "missing"); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("missing capability inspection error=%v", err)
	}
	if _, err := store.RevokeCapabilityFor(ctx, rootID, rootAgentID, "missing"); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("missing capability revocation error=%v", err)
	}

	corruptions := []struct {
		name  string
		query string
		value string
	}{
		{"operations", `UPDATE capabilities SET operations=? WHERE id=?`, `{`},
		{"scopes", `UPDATE capabilities SET scopes=? WHERE id=?`, `{`},
		{"expiry", `UPDATE capabilities SET scopes=? WHERE id=?`, `{"expires_at":"bad"}`},
		{"created", `UPDATE capabilities SET created_at=? WHERE id=?`, `bad`},
		{"updated", `UPDATE capabilities SET updated_at=? WHERE id=?`, `bad`},
	}
	for _, corruption := range corruptions {
		id := "corrupt-" + corruption.name
		if err := store.IssueCapability(ctx, capability.Grant{ID: id, RootID: rootID, AgentID: "child", Operations: []string{"read"}, Generation: 1}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.db.ExecContext(ctx, corruption.query, corruption.value, id); err != nil {
			t.Fatal(err)
		}
		if _, err := store.InspectCapability(ctx, rootID, rootAgentID, id); err == nil {
			t.Fatalf("corrupt %s capability was inspected", corruption.name)
		}
	}

	admitTestChild(t, store, rootID, rootAgentID, "terminal-parent")
	admitTestChild(t, store, rootID, "terminal-parent", "terminal-child")
	if err := store.IssueCapability(ctx, capability.Grant{ID: "terminal-capability", RootID: rootID, AgentID: "terminal-child", Operations: []string{"read"}, Generation: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.TerminalizeSubtree(ctx, rootID, rootAgentID, "terminal-parent", "stopped"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RevokeCapabilityFor(ctx, rootID, "terminal-parent", "terminal-capability"); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("terminal caller revocation error=%v", err)
	}
}

func TestPermissionAPIsReturnClosedStoreErrors(t *testing.T) {
	store, rootID, agentID := newSwarmFixture(t)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"inspect": func() error { _, err := store.InspectCapability(ctx, rootID, agentID, "capability"); return err },
		"delegate": func() error {
			_, err := store.DelegateCapability(ctx, rootID, agentID, CapabilityDelegation{ID: "delegated", Issuer: capability.Reference{ID: "issuer"}, AgentID: "child", Operations: []string{"read"}})
			return err
		},
		"revoke": func() error { _, err := store.RevokeCapabilityFor(ctx, rootID, agentID, "capability"); return err },
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); err == nil {
				t.Fatal("closed store call succeeded")
			}
		})
	}
}

func TestDelegationMissingRecordsDuplicateAndWorkspace(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	ctx := context.Background()
	admitTestChild(t, store, rootID, rootAgentID, "child")
	if err := store.IssueCapability(ctx, capability.Grant{ID: "issuer", RootID: rootID, AgentID: rootAgentID, Operations: []string{"read"}, Generation: 1}); err != nil {
		t.Fatal(err)
	}
	base := CapabilityDelegation{
		ID: "delegated", Issuer: capability.Reference{ID: "issuer", Generation: 1}, AgentID: "child", Operations: []string{"read"},
	}
	missingSubject := base
	missingSubject.ID = "missing-subject"
	missingSubject.AgentID = "missing"
	if _, err := store.DelegateCapability(ctx, rootID, rootAgentID, missingSubject); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("missing subject error=%v", err)
	}
	missingIssuer := base
	missingIssuer.ID = "missing-issuer"
	missingIssuer.Issuer.ID = "missing"
	if _, err := store.DelegateCapability(ctx, rootID, rootAgentID, missingIssuer); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("missing issuer error=%v", err)
	}
	if _, err := store.DelegateCapability(ctx, rootID, rootAgentID, base); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DelegateCapability(ctx, rootID, rootAgentID, base); err == nil {
		t.Fatal("duplicate capability ID was accepted")
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE sessions SET cwd=? WHERE id=?`, filepath.Join(t.TempDir(), "missing"), rootID); err != nil {
		t.Fatal(err)
	}
	base.ID = "missing-workspace"
	if _, err := store.DelegateCapability(ctx, rootID, rootAgentID, base); err == nil {
		t.Fatal("delegation with a missing workspace succeeded")
	}
}
