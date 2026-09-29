package store

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func testMCPToolScope(letter string) MCPToolScope {
	return MCPToolScope{Capability: "mcp.call.trusted", Resource: "mcp_call_" + strings.Repeat(letter, 64)}
}

func TestMCPChildScopeUsesLivePolicyAndRetainsExactCallsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	db := openTest(t, path)
	root, _ := operationCell(t, db)
	scope := testMCPToolScope("a")
	request := childRequest(root.ID)
	request.MCPTools = []MCPToolScope{scope}
	child := spawnChildTest(t, db, "child", request)
	nested := childRequest(child.Session.ID)
	nested.MCPTools = request.MCPTools
	grandchild := spawnChildTest(t, db, "grandchild", nested)
	var ready []session.Operation
	for _, admitted := range []ChildAdmission{child, grandchild} {
		owner := *admitted.Session
		cell := childOperationCell(t, db, owner.ID)
		spec := operationSpec(cell, "ask_"+string(owner.ID))
		spec.Capability, spec.Resource = scope.Capability, scope.Resource
		if denied := admitOperation(t, db, spec); denied.State != session.OperationDenied {
			t.Fatal("Ask child gained authority", denied)
		}
		access, err := db.MCPDelegatedAuthority(t.Context(), owner.ID)
		if err != nil || len(access) != 0 {
			t.Fatal("Ask catalog leaked authority", access, err)
		}
	}
	setModeTest(t, db, root.ID, "automatic", 1, session.PermissionAutomatic)
	for _, admitted := range []ChildAdmission{child, grandchild} {
		owner := *admitted.Session
		cell := session.Cell{ID: session.CellID("cell_" + string(owner.ID))}
		access, err := db.MCPDelegatedAuthority(t.Context(), owner.ID)
		if err != nil || !slices.Contains(access, scope) {
			t.Fatal("Full Access omitted captured tool", access, err)
		}
		spec := operationSpec(cell, "ready_"+string(owner.ID))
		spec.Capability, spec.Resource = scope.Capability, scope.Resource
		admitted := admitOperation(t, db, spec)
		if admitted.State != session.OperationReady || admitted.PermissionRevision == nil {
			t.Fatal("visible tool not callable", admitted)
		}
		ready = append(ready, admitted)
		spec.ID, spec.RequestID, spec.Resource = spec.ID+"_outside", spec.RequestID+"_outside", testMCPToolScope("b").Resource
		denied := admitOperation(t, db, spec)
		if denied.State != session.OperationDenied {
			t.Fatal("Full Access widened captured scope", denied)
		}
		denied.PermissionRevision = new(session.Revision(2))
		if err := authorizeOperation(t.Context(), db.db, denied); !errors.Is(err, ErrConflict) {
			t.Fatal("forged policy bypassed scope at dispatch", err)
		}
	}
	setModeTest(t, db, root.ID, "ask", 2, session.PermissionPrompt)
	setModeTest(t, db, root.ID, "automatic_again", 3, session.PermissionAutomatic)
	for _, op := range ready {
		if allowed, err := db.DispatchOperation(t.Context(), op.ID); err != nil || allowed {
			t.Fatal("retired operation replayed", allowed, err)
		}
		spec := op.OperationSpec
		spec.ID, spec.RequestID = spec.ID+"_new", spec.RequestID+"_new"
		next := admitOperation(t, db, spec)
		if next.State != session.OperationReady || next.PermissionRevision == nil || *next.PermissionRevision != 4 {
			t.Fatal("new operation lost live inheritance", next)
		}
		reopened := openTest(t, path)
		if allowed, err := reopened.DispatchOperation(t.Context(), next.ID); err != nil || !allowed {
			t.Fatal("captured scope lost after reopen", allowed, err)
		}
	}
	if count(t, db, "grants") != 0 || count(t, db, "permissions") != 0 {
		t.Fatal("catalog fabricated grants or approvals")
	}
}

func TestMCPChildScopeCannotWidenOrReplaceExplicitRestrictions(t *testing.T) {
	db := fresh(t)
	root, _ := operationCell(t, db)
	setModeTest(t, db, root.ID, "automatic", 1, session.PermissionAutomatic)
	scope := testMCPToolScope("a")
	for _, test := range []struct {
		name   string
		grants []session.GrantID
		tools  []MCPToolScope
	}{
		{name: "uncaptured"},
		{name: "explicit_empty", grants: []session.GrantID{}, tools: []MCPToolScope{scope}},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := childRequest(root.ID)
			request.GrantIDs, request.MCPTools = test.grants, test.tools
			child := spawnChildTest(t, db, test.name, request)
			cell := childOperationCell(t, db, child.Session.ID)
			spec := operationSpec(cell, test.name)
			spec.Capability, spec.Resource = scope.Capability, scope.Resource
			if op := admitOperation(t, db, spec); op.State != session.OperationDenied {
				t.Fatal("restricted child gained MCP authority", op)
			}
			access, err := db.MCPDelegatedAuthority(t.Context(), child.Session.ID)
			if err != nil || len(access) != 0 {
				t.Fatal("restricted catalog widened", access, err)
			}
		})
	}
	request := childRequest(root.ID)
	request.MCPTools = []MCPToolScope{scope}
	child := spawnChildTest(t, db, "scoped", request)
	request.ParentID, request.MCPTools = child.Session.ID, []MCPToolScope{testMCPToolScope("b")}
	if _, err := db.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "wider"}, request); !errors.Is(err, ErrConflict) {
		t.Fatal("descendant scope widened", err)
	}
	for _, invalid := range [][]MCPToolScope{
		{{Capability: "mcp.call", Resource: scope.Resource}},
		{{Capability: scope.Capability, Resource: "mcp_call_not-a-selector"}},
		{scope, scope},
		make([]MCPToolScope, MaxChildMCPTools+1),
	} {
		request.ParentID, request.MCPTools = root.ID, invalid
		if _, err := db.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "invalid"}, request); err == nil {
			t.Fatal("invalid MCP capture accepted")
		}
	}
}
