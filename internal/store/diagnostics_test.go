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

func TestDiagnosticsRetainOnlyCurrentRootAutomaticAuthority(t *testing.T) {
	s := fresh(t)
	root, cell := operationCell(t, s)
	setModeTest(t, s, root.ID, "enable", 1, session.PermissionAutomatic)
	spec := operationSpec(cell, "optional-auto")
	spec.Capability, spec.Resource = "lsp.diagnostics", root.WorkingDirectory
	admitted, err := s.AdmitStandingOperation(t.Context(), spec)
	if err != nil || admitted.State != session.OperationReady || admitted.GrantID != nil || admitted.PermissionRevision == nil || *admitted.PermissionRevision != 2 {
		t.Fatal("optional root diagnostics missed automatic authority", admitted, err)
	}
	if allowed, err := s.DispatchOperation(t.Context(), admitted.ID); err != nil || !allowed {
		t.Fatal("automatic diagnostics not dispatched", allowed, err)
	}
	if retain, err := s.DiagnosticRetention(t.Context(), admitted.ID); err != nil || !retain {
		t.Fatal("automatic diagnostics could not retain server", retain, err)
	}
	setModeTest(t, s, root.ID, "same", 2, session.PermissionAutomatic)
	if retain, err := s.DiagnosticRetention(t.Context(), admitted.ID); err != nil || !retain {
		t.Fatal("same mode invalidated current authority", retain, err)
	}
	child := spawnChildTest(t, s, "child", childRequest(root.ID))
	childCell := childOperationCell(t, s, child.Session.ID)
	childSpec := operationSpec(childCell, "child-auto")
	childSpec.Capability, childSpec.Resource = spec.Capability, spec.Resource
	if skipped, err := s.AdmitStandingOperation(t.Context(), childSpec); err != nil || skipped.ID != "" {
		t.Fatal("child auto mode widened optional diagnostics", skipped, err)
	}
	setModeTest(t, s, root.ID, "disable", 2, session.PermissionPrompt)
	if retain, err := s.DiagnosticRetention(t.Context(), admitted.ID); !errors.Is(err, ErrConflict) || retain {
		t.Fatal("old revision retained server after downgrade", retain, err)
	}
	setModeTest(t, s, root.ID, "reenable", 3, session.PermissionAutomatic)
	if retain, err := s.DiagnosticRetention(t.Context(), admitted.ID); !errors.Is(err, ErrConflict) || retain {
		t.Fatal("ABA reused old diagnostic authority", retain, err)
	}
}
