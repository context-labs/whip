package store

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestPermissionInspectionCannotGrantOrEscapeItsOwner(t *testing.T) {
	db := fresh(t)
	owner, cell := operationCell(t, db)
	request := session.PermissionInspection{SessionID: owner.ID, ConfigRevision: owner.ConfigRevision, Action: "request"}
	for _, mutate := range []func(*session.PermissionInspection){
		func(r *session.PermissionInspection) { r.SessionID = "foreign" },
		func(r *session.PermissionInspection) { r.ConfigRevision++ },
		func(r *session.PermissionInspection) { r.Action = "approve" },
		func(r *session.PermissionInspection) { r.Action = "status" },
		func(r *session.PermissionInspection) { r.OperationID = "unexpected" },
	} {
		altered := request
		mutate(&altered)
		raw, _ := json.Marshal(altered)
		spec := operationSpec(cell, "forged")
		spec.Capability, spec.Resource, spec.Arguments = "permissions.inspect", string(owner.ID), raw
		if _, err := db.AdmitOperation(t.Context(), spec); err == nil {
			t.Fatal("forged permission inspection admitted", altered)
		}
	}
	raw, _ := json.Marshal(request)
	spec := operationSpec(cell, "inspection")
	spec.Capability, spec.Resource, spec.Arguments = "permissions.inspect", string(owner.ID), raw
	operation := admitOperation(t, db, spec)
	if operation.State != session.OperationReady || operation.GrantID != nil || operation.PermissionRevision != nil || count(t, db, "grants") != 0 || count(t, db, "permissions") != 0 {
		t.Fatal("inspection manufactured authority", operation)
	}
	if ok, err := db.DispatchOperation(t.Context(), operation.ID); err != nil || !ok {
		t.Fatal(ok, err)
	}
	forged := operation
	forged.GrantID = new(session.GrantID("invented"))
	if err := authorizeOperation(t.Context(), db.db, forged); !errors.Is(err, ErrConflict) {
		t.Fatal("inspection accepted forged authority", err)
	}
	pending := admitOperation(t, db, operationSpec(cell, "pending"))
	if permission, err := db.Permission(t.Context(), owner.ID, pending.ID); err != nil || permission.State != session.PermissionPending {
		t.Fatal(permission, err)
	}
	stranger, _ := operationCell(t, db)
	if _, err := db.Permission(t.Context(), stranger.ID, pending.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign permission escaped", err)
	}
	if _, err := db.ResolvePermission(t.Context(), pending.ID, false); err != nil {
		t.Fatal(err)
	}
	if permission, err := db.Permission(t.Context(), owner.ID, pending.ID); err != nil || permission.State != session.PermissionDenied || permission.ResolvedAt == nil {
		t.Fatal("inspection lost durable decision", permission, err)
	}
}
