package store

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestComputerExplicitConsentAndChildDelegationRemainExact(t *testing.T) {
	s := fresh(t)
	root, cell := operationCell(t, s)
	setModeTest(t, s, root.ID, "auto", 1, session.PermissionAutomatic)
	resource := "computer:control_fixture:exact-app-set"
	for _, capability := range []string{"computer.run", "computer.applescript"} {
		spec := operationSpec(cell, capability)
		spec.Capability = capability
		spec.Resource = resource
		operation := admitOperation(t, s, spec)
		if operation.State != session.OperationWaiting || operation.PermissionRevision != nil {
			t.Fatal("automatic policy widened explicit computer consent", operation)
		}
		if ok, err := s.DispatchOperation(t.Context(), operation.ID); err != nil || ok {
			t.Fatal("unapproved computer dispatch", ok, err)
		}
	}
	issuer, err := s.CreateGrant(t.Context(), session.Grant{ID: "computer-issuer", SessionID: root.ID, Capability: "computer.run.trusted", Resource: resource})
	if err != nil {
		t.Fatal(err)
	}
	request := childRequest(root.ID)
	request.GrantIDs = []session.GrantID{}
	child := spawnChildTest(t, s, "computer-child", request)
	childCell := childOperationCell(t, s, child.Session.ID)
	spec := session.OperationSpec{ID: "no-delegation", CellID: childCell.ID, RequestID: "none", Capability: issuer.Capability, Resource: resource, Arguments: json.RawMessage(`{"code":"state(\"app\")"}`)}
	if got := admitOperation(t, s, spec); got.State != session.OperationDenied {
		t.Fatal("child inherited availability", got)
	}
	if _, err := s.CreateGrant(t.Context(), session.Grant{ID: "delegated-computer", SessionID: child.Session.ID, Capability: issuer.Capability, Resource: resource, IssuerID: &issuer.ID}); err != nil {
		t.Fatal(err)
	}
	spec.ID, spec.RequestID = "delegated", "delegated"
	op := admitOperation(t, s, spec)
	if op.State != session.OperationReady || op.PermissionRevision != nil {
		t.Fatal(op)
	}
	if err := s.CheckDispatchedOperation(t.Context(), op.ID); !errors.Is(err, ErrConflict) {
		t.Fatal("effect recheck replaced dispatch", err)
	}
	if ok, err := s.DispatchOperation(t.Context(), op.ID); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := s.CheckDispatchedOperation(t.Context(), op.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RevokeGrant(t.Context(), issuer.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckDispatchedOperation(t.Context(), op.ID); !errors.Is(err, ErrConflict) {
		t.Fatal("revoked ancestor kept batch authority", err)
	}
	spec.ID, spec.RequestID, spec.Resource = "other-generation", "other-generation", "computer:new-generation:exact-app-set"
	if got := admitOperation(t, s, spec); got.State != session.OperationDenied {
		t.Fatal("child reused different generation", got)
	}
}
