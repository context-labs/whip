package session

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/capability"
	contentstore "github.com/context-labs/whip/internal/content"
)

func mcpTestAdmission(t *testing.T, rootID, agentID, operationID string, reference capability.Reference, selector capability.MCPSelector) capability.Admission {
	t.Helper()
	arguments, err := json.Marshal(capability.MCPCall{
		MCPSelector: selector, Generation: "connection-1", Source: "whip", Trusted: true, Arguments: json.RawMessage(`{"value":1}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	return capability.Admission{
		Request: capability.Request{
			RootID: rootID, AgentID: agentID, CapabilityID: reference.ID, CapabilityGeneration: reference.Generation,
			OperationID: operationID, Operation: "mcp.call", Arguments: arguments,
			Reservations: []capability.Reservation{{Kind: "active_operations", Amount: 1}},
		},
		Mutation: capability.MutationNone, RequestDigest: operationID,
	}
}

func delegateTestMCP(t *testing.T, store *Store, rootID, parentID, childID string, issuer capability.Reference, selectors ...capability.MCPSelector) capability.Reference {
	t.Helper()
	admitTestChild(t, store, rootID, parentID, childID)
	record, err := store.DelegateCapability(t.Context(), rootID, parentID, CapabilityDelegation{
		ID: "mcp:" + childID, Issuer: issuer, AgentID: childID, Operations: []string{"mcp.call"}, MCP: selectors,
	})
	if err != nil {
		t.Fatal(err)
	}
	return capability.Reference{ID: record.ID, Generation: record.Generation}
}

func TestMCPExactDelegationSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	rootID, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := store.EnsureAuthority(t.Context(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	tool := capability.MCPSelector{Server: "test-server", Tool: "raw.tool", Definition: "schema-1"}
	other := capability.MCPSelector{Server: "test_server", Tool: "raw_tool", Definition: "schema-1"}
	parent := delegateTestMCP(t, store, rootID, rootID, "parent", authority.MCP, tool, other)
	child := delegateTestMCP(t, store, rootID, "parent", "child", parent, tool)
	empty := delegateTestMCP(t, store, rootID, rootID, "empty", authority.MCP)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, reference := range []capability.Reference{authority.MCP, parent, child} {
		agentID := strings.TrimPrefix(reference.ID, "mcp:")
		if err := store.AuthorizeMCP(t.Context(), rootID, agentID, reference, tool); err != nil {
			t.Fatalf("reopened %s: %v", agentID, err)
		}
	}
	selectors, all, err := store.MCPSelectors(t.Context(), rootID, "child", child)
	if err != nil || all || !slices.Equal(selectors, []capability.MCPSelector{tool}) {
		t.Fatalf("child selectors=%+v all=%t err=%v", selectors, all, err)
	}
	restored, names, err := store.LoadAgentAuthority(t.Context(), rootID, "child")
	if err != nil || restored.MCP != child || !slices.Equal(names, []string{"mcp"}) {
		t.Fatalf("restored=%+v names=%v err=%v", restored, names, err)
	}
	for name, selector := range map[string]capability.MCPSelector{
		"not inherited": other,
		"new schema":    {Server: tool.Server, Tool: tool.Tool, Definition: "schema-2"},
		"raw name":      {Server: tool.Server, Tool: "raw_tool", Definition: tool.Definition},
		"server name":   {Server: "test_server", Tool: tool.Tool, Definition: tool.Definition},
	} {
		if err := store.AuthorizeMCP(t.Context(), rootID, "child", child, selector); !errors.Is(err, capability.ErrDenied) {
			t.Errorf("%s error=%v", name, err)
		}
	}
	if err := store.AuthorizeMCP(t.Context(), rootID, "empty", empty, tool); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("empty grant error=%v", err)
	}
	if _, all, err := store.MCPSelectors(t.Context(), rootID, rootID, authority.MCP); err != nil || !all {
		t.Fatalf("root all=%t err=%v", all, err)
	}
	if _, _, err := store.MCPSelectors(t.Context(), rootID, "parent", child); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("wrong owner error=%v", err)
	}
}

func TestMCPDelegationRejectsEscalationAtomically(t *testing.T) {
	tool := capability.MCPSelector{Server: "server", Tool: "tool", Definition: "schema"}
	for name, change := range map[string]func(*CapabilityDelegation){
		"wildcard":     func(d *CapabilityDelegation) { d.MCPAll = true },
		"definition":   func(d *CapabilityDelegation) { d.MCP[0].Definition = "changed" },
		"missing tool": func(d *CapabilityDelegation) { d.MCP[0].Tool = "" },
		"duplicate":    func(d *CapabilityDelegation) { d.MCP = append(d.MCP, d.MCP[0]) },
		"wrong issuer": func(d *CapabilityDelegation) { d.Issuer.ID = "mcp:missing" },
		"generation":   func(d *CapabilityDelegation) { d.Issuer.Generation++ },
		"missing op":   func(d *CapabilityDelegation) { d.Operations = []string{"read"} },
		"mixed op":     func(d *CapabilityDelegation) { d.Operations = append(d.Operations, "read") },
		"filesystem":   func(d *CapabilityDelegation) { d.Scopes = []string{"."} },
	} {
		t.Run(name, func(t *testing.T) {
			store, rootID, rootAgentID := newSwarmFixture(t)
			parent := delegateTestMCP(t, store, rootID, rootAgentID, "parent", capability.Reference{ID: "mcp:" + rootID, Generation: 1}, tool)
			delegation := CapabilityDelegation{
				ID: "mcp:child", Issuer: parent, AgentID: "child", Operations: []string{"mcp.call"}, MCP: []capability.MCPSelector{tool},
			}
			change(&delegation)
			_, err := store.AdmitAgent(t.Context(), AgentAdmission{RootID: rootID, ParentAgentID: "parent", ChildAgentID: "child", Capabilities: []CapabilityDelegation{delegation}})
			if !errors.Is(err, capability.ErrDenied) {
				t.Fatalf("delegation error=%v", err)
			}
			var count int
			if err := store.db.QueryRowContext(t.Context(), `SELECT count(*) FROM agents WHERE id='child'`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("failed admission child count=%d err=%v", count, err)
			}
		})
	}
	store, rootID, rootAgentID := newSwarmFixture(t)
	admitTestChild(t, store, rootID, rootAgentID, "child")
	if err := store.IssueCapability(t.Context(), capability.Grant{
		ID: "unlinked", RootID: rootID, AgentID: "child", Operations: []string{"mcp.call"}, MCP: []capability.MCPSelector{tool}, Generation: 1,
	}); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("unlinked child issue error=%v", err)
	}
}

func TestMCPAncestorRevocationSettlesPendingDescendants(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	if err := store.SetBudgetLimit(t.Context(), rootID, "", BudgetDepth, 3); err != nil {
		t.Fatal(err)
	}
	tool := capability.MCPSelector{Server: "server", Tool: "tool", Definition: "schema"}
	parent := delegateTestMCP(t, store, rootID, rootAgentID, "parent", capability.Reference{ID: "mcp:" + rootID, Generation: 1}, tool)
	child := delegateTestMCP(t, store, rootID, "parent", "child", parent, tool)
	admission := mcpTestAdmission(t, rootID, "child", "waiting", child, tool)
	admission.RequirePermission = true
	ticket, err := store.Begin(t.Context(), admission)
	if err != nil || ticket.PermissionID == "" {
		t.Fatalf("pending ticket=%+v err=%v", ticket, err)
	}
	if _, err := store.RevokeCapabilityFor(t.Context(), rootID, rootAgentID, parent.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Pending(t.Context(), ticket.PermissionID); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("revoked pending error=%v", err)
	}
	if _, err := store.Decide(t.Context(), admission, ticket.PermissionID, capability.Decision{Allow: true}); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("late approval error=%v", err)
	}
	if state := budgetState(t, store, rootID, "child", BudgetActiveOperations); state.Reserved != 0 {
		t.Fatalf("pending capacity leaked: %+v", state)
	}
	if err := store.AuthorizeMCP(t.Context(), rootID, "child", child, tool); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("revoked ancestor error=%v", err)
	}
	admitTestChild(t, store, rootID, "child", "grandchild")
	if _, err := store.DelegateCapability(t.Context(), rootID, "child", CapabilityDelegation{
		ID: "mcp:grandchild", Issuer: child, AgentID: "grandchild", Operations: []string{"mcp.call"}, MCP: []capability.MCPSelector{tool},
	}); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("revoked descendant redelegation error=%v", err)
	}
	admission.Request.OperationID = "after-revocation"
	if _, err := store.Begin(t.Context(), admission); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("revoked descendant begin error=%v", err)
	}
}

func TestMCPRevocationRollbackPreservesPendingDescendant(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	tool := capability.MCPSelector{Server: "server", Tool: "tool", Definition: "schema"}
	parent := delegateTestMCP(t, store, rootID, rootAgentID, "parent", capability.Reference{ID: "mcp:" + rootID, Generation: 1}, tool)
	child := delegateTestMCP(t, store, rootID, "parent", "child", parent, tool)
	admission := mcpTestAdmission(t, rootID, "child", "waiting", child, tool)
	admission.RequirePermission = true
	ticket, err := store.Begin(t.Context(), admission)
	if err != nil {
		t.Fatal(err)
	}
	mcpTestSQL(t, store, `CREATE TRIGGER fail_revocation BEFORE INSERT ON events WHEN NEW.kind='capability.revoked' BEGIN SELECT RAISE(ABORT,'event unavailable'); END`)
	if _, err := store.RevokeCapabilityFor(t.Context(), rootID, rootAgentID, parent.ID); err == nil {
		t.Fatal("revocation event failure was hidden")
	}
	if _, err := store.Pending(t.Context(), ticket.PermissionID); err != nil {
		t.Fatalf("rollback lost pending approval: %v", err)
	}
	if err := store.AuthorizeMCP(t.Context(), rootID, "child", child, tool); err != nil {
		t.Fatalf("rollback revoked ancestor: %v", err)
	}
	if state := budgetState(t, store, rootID, "child", BudgetActiveOperations); state.Reserved != 1 {
		t.Fatalf("rollback changed reservation: %+v", state)
	}
	mcpTestSQL(t, store, `DROP TRIGGER fail_revocation`)
	if _, err := store.RevokeCapabilityFor(t.Context(), rootID, rootAgentID, parent.ID); err != nil {
		t.Fatal(err)
	}
	if state := budgetState(t, store, rootID, "child", BudgetActiveOperations); state.Reserved != 0 {
		t.Fatalf("retry did not settle reservation: %+v", state)
	}
}

func TestMCPAuthorityRejectsOtherRootAndSkippedTerminalParent(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	tool := capability.MCPSelector{Server: "server", Tool: "tool", Definition: "schema"}
	admitTestChild(t, store, rootID, rootAgentID, "parent")
	admitTestChild(t, store, rootID, "parent", "child")
	root := capability.Reference{ID: "mcp:" + rootID, Generation: 1}
	record, err := store.DelegateCapability(t.Context(), rootID, rootAgentID, CapabilityDelegation{
		ID: "mcp:child", AgentID: "child", Issuer: root, Operations: []string{"mcp.call"}, MCP: []capability.MCPSelector{tool},
	})
	if err != nil {
		t.Fatal(err)
	}
	child := capability.Reference{ID: record.ID, Generation: record.Generation}
	otherRoot, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.EnsureAuthority(t.Context(), otherRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AuthorizeMCP(t.Context(), otherRoot, otherRoot, child, tool); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("other root borrowed child grant: %v", err)
	}
	if err := store.AuthorizeMCP(t.Context(), rootID, "child", other.MCP, tool); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("child borrowed other root grant: %v", err)
	}
	mcpTestSQL(t, store, `UPDATE agents SET status='stopped' WHERE id='parent'`)
	if err := store.AuthorizeMCP(t.Context(), rootID, "child", child, tool); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("skipped terminal parent error=%v", err)
	}
}

func TestMCPAuthorityRejectsBrokenAncestorChains(t *testing.T) {
	for name, mutate := range map[string]func(*testing.T, *Store, string){
		"generation": func(t *testing.T, s *Store, rootID string) {
			mcpTestSQL(t, s, `UPDATE capabilities SET generation=generation+1 WHERE id=?`, "mcp:"+rootID)
		},
		"terminal parent": func(t *testing.T, s *Store, _ string) {
			mcpTestSQL(t, s, `UPDATE agents SET status='stopped' WHERE id='parent'`)
		},
		"expired": func(t *testing.T, s *Store, rootID string) {
			raw, _ := json.Marshal(storedCapabilityScopes{MCPAll: true, ExpiresAt: time.Now().Add(-time.Hour).Format(time.RFC3339Nano)})
			mcpTestSQL(t, s, `UPDATE capabilities SET scopes=? WHERE id=?`, raw, "mcp:"+rootID)
		},
		"missing reference": func(t *testing.T, s *Store, _ string) {
			mcpTestSQL(t, s, `UPDATE capabilities SET scopes=json_remove(scopes,'$.mcp_issuer_id') WHERE id='mcp:parent'`)
		},
		"child wildcard": func(t *testing.T, s *Store, _ string) {
			mcpTestSQL(t, s, `UPDATE capabilities SET scopes=json_set(scopes,'$.mcp_all',json('true')) WHERE id='mcp:parent'`)
		},
		"cycle": func(t *testing.T, s *Store, _ string) {
			mcpTestSQL(t, s, `UPDATE capabilities SET issuer_agent_id='child',scopes=json_set(scopes,'$.mcp_issuer_id','mcp:child') WHERE id='mcp:parent'`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			store, rootID, rootAgentID := newSwarmFixture(t)
			tool := capability.MCPSelector{Server: "server", Tool: "tool", Definition: "schema"}
			parent := delegateTestMCP(t, store, rootID, rootAgentID, "parent", capability.Reference{ID: "mcp:" + rootID, Generation: 1}, tool)
			child := delegateTestMCP(t, store, rootID, "parent", "child", parent, tool)
			mutate(t, store, rootID)
			if err := store.AuthorizeMCP(t.Context(), rootID, "child", child, tool); !errors.Is(err, capability.ErrDenied) {
				t.Fatalf("invalid ancestor error=%v", err)
			}
		})
	}
}

func TestMCPAdmissionValidatesEnvelope(t *testing.T) {
	for name, change := range map[string]func(*capability.Admission){
		"bare arguments": func(a *capability.Admission) { a.Request.Arguments = json.RawMessage(`{"value":1}`) },
		"new schema": func(a *capability.Admission) {
			a.Request.Arguments = json.RawMessage(strings.ReplaceAll(string(a.Request.Arguments), `"schema"`, `"new-schema"`))
		},
		"generation absent": func(a *capability.Admission) {
			a.Request.Arguments = json.RawMessage(strings.ReplaceAll(string(a.Request.Arguments), `"connection-1"`, `""`))
		},
		"array arguments": func(a *capability.Admission) {
			a.Request.Arguments = json.RawMessage(strings.ReplaceAll(string(a.Request.Arguments), `{"value":1}`, `[]`))
		},
		"workspace mutation": func(a *capability.Admission) { a.Mutation = capability.MutationWorkspace },
		"wrong owner":        func(a *capability.Admission) { a.Request.AgentID = a.Request.RootID },
	} {
		t.Run(name, func(t *testing.T) {
			store, rootID, rootAgentID := newSwarmFixture(t)
			tool := capability.MCPSelector{Server: "server", Tool: "tool", Definition: "schema"}
			child := delegateTestMCP(t, store, rootID, rootAgentID, "child", capability.Reference{ID: "mcp:" + rootID, Generation: 1}, tool)
			admission := mcpTestAdmission(t, rootID, "child", "invalid", child, tool)
			change(&admission)
			if _, err := store.Begin(t.Context(), admission); !errors.Is(err, capability.ErrDenied) {
				t.Fatalf("invalid envelope error=%v", err)
			}
			if state := budgetState(t, store, rootID, "child", BudgetActiveOperations); state.Reserved != 0 {
				t.Fatalf("denied envelope reserved capacity: %+v", state)
			}
		})
	}
}

func TestCapabilityFinishStorageFailureSettlesAndReleasesCapacity(t *testing.T) {
	for _, failure := range []string{"filesystem", "content_objects", "content_references", "content_grants"} {
		t.Run(failure, func(t *testing.T) {
			store, rootID, rootAgentID := newSwarmFixture(t)
			tool := capability.MCPSelector{Server: "server", Tool: "tool", Definition: "schema"}
			admission := mcpTestAdmission(t, rootID, rootAgentID, "result-failure", capability.Reference{ID: "mcp:" + rootID, Generation: 1}, tool)
			ticket, err := store.Begin(t.Context(), admission)
			if err != nil {
				t.Fatal(err)
			}
			if failure == "filesystem" {
				contentHome := t.TempDir()
				store.content, err = contentstore.New(contentHome)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.RemoveAll(filepath.Join(contentHome, "artifacts")); err != nil {
					t.Fatal(err)
				}
			} else {
				mcpTestSQL(t, store, `CREATE TRIGGER fail_result BEFORE INSERT ON `+failure+` BEGIN SELECT RAISE(ABORT,'result storage unavailable'); END`)
			}
			err = store.Finish(t.Context(), capability.Completion{Admission: admission, LeaseID: ticket.LeaseID, Status: capability.StatusSucceeded, Output: strings.Repeat("large", InlineValueLimit)})
			if err == nil {
				t.Fatal("storage error was hidden")
			}
			var operationStatus, leaseStatus string
			var inline []byte
			var reference sql.NullString
			if err := store.db.QueryRowContext(t.Context(), `SELECT o.status,l.status,o.result_inline,o.result_ref FROM operations o JOIN leases l ON o.id=l.operation_id WHERE o.id=?`, admission.Request.OperationID).Scan(&operationStatus, &leaseStatus, &inline, &reference); err != nil {
				t.Fatal(err)
			}
			if operationStatus != "failed" || leaseStatus != "failed" || reference.Valid || len(inline) > 128 || !strings.Contains(string(inline), "could not be stored") {
				t.Fatalf("operation=%s lease=%s ref=%v inline=%s", operationStatus, leaseStatus, reference, inline)
			}
			if state := budgetState(t, store, rootID, rootAgentID, BudgetActiveOperations); state.Reserved != 0 || state.Used != 0 {
				t.Fatalf("completion leaked capacity: %+v", state)
			}
			for _, table := range []string{"content_objects", "content_references", "content_grants"} {
				var count int
				if err := store.db.QueryRowContext(t.Context(), `SELECT count(*) FROM `+table).Scan(&count); err != nil || count != 0 {
					t.Fatalf("partial result metadata %s count=%d err=%v", table, count, err)
				}
			}
			if err := store.Finish(t.Context(), capability.Completion{Admission: admission, LeaseID: ticket.LeaseID, Status: capability.StatusSucceeded}); !errors.Is(err, capability.ErrDenied) {
				t.Fatalf("duplicate completion error=%v", err)
			}
		})
	}
}

func mcpTestSQL(t *testing.T, store *Store, statement string, args ...any) {
	t.Helper()
	if _, err := store.db.ExecContext(t.Context(), statement, args...); err != nil {
		t.Fatal(err)
	}
}

func TestCapabilityFinishDeniedSettlesAdmittedOperation(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	tool := capability.MCPSelector{Server: "server", Tool: "tool", Definition: "schema"}
	admission := mcpTestAdmission(t, rootID, rootAgentID, "denied-after-admission", capability.Reference{ID: "mcp:" + rootID, Generation: 1}, tool)
	ticket, err := store.Begin(t.Context(), admission)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Finish(t.Context(), capability.Completion{
		Admission: admission, LeaseID: ticket.LeaseID, Status: capability.StatusDenied, Error: "authority revoked before transmission",
	}); err != nil {
		t.Fatal(err)
	}
	var operationStatus, leaseStatus string
	if err := store.db.QueryRowContext(t.Context(), `SELECT o.status,l.status FROM operations o JOIN leases l ON o.id=l.operation_id WHERE o.id=?`, admission.Request.OperationID).Scan(&operationStatus, &leaseStatus); err != nil {
		t.Fatal(err)
	}
	if operationStatus != "denied" || leaseStatus != "denied" {
		t.Fatalf("post-admission denial statuses=%s/%s", operationStatus, leaseStatus)
	}
	if state := budgetState(t, store, rootID, rootAgentID, BudgetActiveOperations); state.Reserved != 0 || state.Used != 0 {
		t.Fatalf("post-admission denial leaked capacity: %+v", state)
	}
}
