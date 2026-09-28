package store

import (
	"errors"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestOptionalDiagnosticsNeverRequestPermission(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	spec := operationSpec(cell, "diagnostic")
	spec.Capability = "lsp.diagnostics"
	spec.Resource = owner.WorkingDirectory
	op, err := s.AdmitStandingOperation(t.Context(), spec)
	if err != nil || op.ID != "" || count(t, s, "operations") != 0 || count(t, s, "permissions") != 0 {
		t.Fatal("optional diagnostics admitted without standing authority", op, err)
	}
	explicit := spec
	explicit.ID = "explicit"
	explicit.RequestID = "explicit"
	op, err = s.AdmitOperation(t.Context(), explicit)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolvePermission(t.Context(), op.ID, true); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.DispatchOperation(t.Context(), op.ID); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if retain, err := s.DiagnosticRetention(t.Context(), op.ID); err != nil || retain {
		t.Fatal("one-use approval retained server", retain, err)
	}
	op, err = s.AdmitStandingOperation(t.Context(), spec)
	if err != nil || op.ID != "" || count(t, s, "operations") != 1 || count(t, s, "permissions") != 1 {
		t.Fatal("one-use approval widened", op, err)
	}
	grant, err := s.CreateGrant(t.Context(), session.Grant{ID: "standing", SessionID: owner.ID, Capability: spec.Capability, Resource: spec.Resource})
	if err != nil {
		t.Fatal(err)
	}
	op, err = s.AdmitStandingOperation(t.Context(), spec)
	if err != nil || op.State != session.OperationReady || op.GrantID == nil || *op.GrantID != grant.ID {
		t.Fatal(op, err)
	}
	if _, err := s.RevokeGrant(t.Context(), grant.ID); err != nil {
		t.Fatal(err)
	}
	retry, err := s.AdmitStandingOperation(t.Context(), spec)
	if err != nil || retry.ID != op.ID {
		t.Fatal("exact retry did not preserve intent", retry, err)
	}
	if ok, err := s.DispatchOperation(t.Context(), op.ID); ok || err != nil {
		t.Fatal("revoked diagnostics dispatched", ok, err)
	}
	if _, err := s.DiagnosticRetention(t.Context(), op.ID); !errors.Is(err, ErrConflict) {
		t.Fatal("revoked diagnostic retained", err)
	}
}
