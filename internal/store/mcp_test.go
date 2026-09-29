package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestMCPCatalogIntrinsicGuardRejectsForgedIntent(t *testing.T) {
	db := fresh(t)
	owner, cell := operationCell(t, db)
	request := session.MCPCatalogRequest{SessionID: owner.ID, TreeID: owner.TreeID, ConfigRevision: owner.ConfigRevision, Selection: owner.Config.MCPServers, Action: "list_servers", Limit: 20}
	for index, mutate := range []func(*session.MCPCatalogRequest){
		func(r *session.MCPCatalogRequest) { r.Action = "call" },
		func(r *session.MCPCatalogRequest) { r.Action = "refresh" },
		func(r *session.MCPCatalogRequest) { r.SessionID = "foreign" },
		func(r *session.MCPCatalogRequest) { r.TreeID = "foreign" },
		func(r *session.MCPCatalogRequest) { r.ConfigRevision++ },
		func(r *session.MCPCatalogRequest) { r.Selection = &session.MCPSelection{} },
		func(r *session.MCPCatalogRequest) { r.Limit = 101 },
	} {
		altered := request
		mutate(&altered)
		raw, _ := json.Marshal(altered)
		spec := operationSpec(cell, "forged")
		spec.Capability = "mcp.catalog"
		spec.Resource = string(owner.TreeID)
		spec.Arguments = raw
		if _, err := db.AdmitOperation(t.Context(), spec); err == nil {
			t.Fatal("forged read admitted", index)
		}
	}
	raw, _ := json.Marshal(request)
	spec := operationSpec(cell, "catalog")
	spec.Capability = "mcp.catalog"
	spec.Resource = string(owner.TreeID)
	spec.Arguments = raw
	operation := admitOperation(t, db, spec)
	if operation.State != session.OperationReady || operation.GrantID != nil || count(t, db, "grants") != 0 || count(t, db, "permissions") != 0 {
		t.Fatal("metadata invented authority", operation)
	}
	if dispatched, err := db.DispatchOperation(t.Context(), operation.ID); err != nil || !dispatched {
		t.Fatal(dispatched, err)
	}
	if _, err := db.SettleOperation(t.Context(), operation.ID, session.OperationResult{State: session.OperationSucceeded, Value: json.RawMessage(`[]`)}); err != nil {
		t.Fatal(err)
	}
}

func TestMCPUntrustedCapabilitiesCannotUseAutomaticAdmissionOrDispatch(t *testing.T) {
	db := fresh(t)
	owner, cell := operationCell(t, db)
	setModeTest(t, db, owner.ID, "automatic", 1, session.PermissionAutomatic)
	for index, capability := range []string{"mcp.call", "mcp.connect"} {
		spec := operationSpec(cell, fmt.Sprintf("untrusted_%d", index))
		spec.Capability = capability
		operation := admitOperation(t, db, spec)
		if operation.State != session.OperationWaiting || operation.PermissionRevision != nil || operation.GrantID != nil {
			t.Fatal("untrusted operation used full access", operation)
		}
		forged := operation
		forged.PermissionRevision = new(session.Revision(2))
		if err := authorizeOperation(t.Context(), db.db, forged); !errors.Is(err, ErrConflict) {
			t.Fatal("untrusted dispatch used forged policy revision", err)
		}
		if _, err := db.ResolvePermission(t.Context(), operation.ID, true); err != nil {
			t.Fatal(err)
		}
		if allowed, err := db.DispatchOperation(t.Context(), operation.ID); err != nil || !allowed {
			t.Fatal("explicit approval failed", allowed, err)
		}
	}
	for index, capability := range []string{"mcp.call.trusted", "mcp.connect.trusted"} {
		spec := operationSpec(cell, fmt.Sprintf("trusted_%d", index))
		spec.Capability = capability
		operation := admitOperation(t, db, spec)
		if operation.State != session.OperationReady || operation.PermissionRevision == nil {
			t.Fatal("trusted preparation lost automatic policy", operation)
		}
	}
}
