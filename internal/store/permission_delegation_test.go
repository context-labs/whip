package store

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func delegatedReadSpec(owner session.Session, cell session.Cell, id string) session.OperationSpec {
	spec := operationSpec(cell, id)
	spec.Capability, spec.Resource = "files.read", owner.WorkingDirectory
	return spec
}

func TestDefaultChildPolicyDelegationPersistsAcrossNestedSpawnAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	root, _ := operationCell(t, s)
	setModeTest(t, s, root.ID, "automatic", 1, session.PermissionAutomatic)
	child := spawnChildTest(t, s, "child", childRequest(root.ID))
	grandchild := spawnChildTest(t, s, "grandchild", childRequest(child.Session.ID))
	var ready []session.Operation
	for _, admitted := range []ChildAdmission{child, grandchild} {
		owner := *admitted.Session
		cell := childOperationCell(t, s, owner.ID)
		for _, capability := range []string{"files.read", "shell.run", "mail.send", "agents.spawn"} {
			spec := delegatedReadSpec(owner, cell, capability+"_"+string(owner.ID))
			spec.Capability = capability
			if capability == "mail.send" || capability == "agents.spawn" {
				spec.Resource = string(owner.TreeID)
			}
			operation := admitOperation(t, s, spec)
			if operation.State != session.OperationReady || operation.GrantID != nil || operation.PermissionRevision == nil || *operation.PermissionRevision != 2 {
				t.Fatalf("default child did not inherit captured automatic policy: %+v", operation)
			}
			ready = append(ready, operation)
		}
	}
	if count(t, s, "grants") != 0 || count(t, s, "permissions") != 0 {
		t.Fatal("policy delegation fabricated standing grants or approvals")
	}
	reopened := openTest(t, path)
	for _, operation := range ready {
		if allowed, err := reopened.DispatchOperation(t.Context(), operation.ID); err != nil || !allowed {
			t.Fatalf("reopened delegated operation lost authority: %v %v", allowed, err)
		}
	}
}

func TestChildPolicyDelegationDoesNotWidenExplicitGrantSelection(t *testing.T) {
	for _, selected := range []bool{false, true} {
		name := "empty"
		if selected {
			name = "subset"
		}
		t.Run(name, func(t *testing.T) {
			s := fresh(t)
			root, _ := operationCell(t, s)
			setModeTest(t, s, root.ID, "automatic", 1, session.PermissionAutomatic)
			issuer, err := s.CreateGrant(t.Context(), session.Grant{
				ID: "read", SessionID: root.ID, Capability: "files.read", Resource: root.WorkingDirectory,
			})
			if err != nil {
				t.Fatal(err)
			}
			request := childRequest(root.ID)
			request.GrantIDs = []session.GrantID{}
			if selected {
				request.GrantIDs = []session.GrantID{issuer.ID}
			}
			child := spawnChildTest(t, s, "child", request)
			grandchild := spawnChildTest(t, s, "grandchild", childRequest(child.Session.ID))
			var ready []session.Operation
			for _, admitted := range []ChildAdmission{child, grandchild} {
				owner := *admitted.Session
				cell := childOperationCell(t, s, owner.ID)
				read := admitOperation(t, s, delegatedReadSpec(owner, cell, "read_"+string(owner.ID)))
				want := session.OperationDenied
				if selected {
					want = session.OperationReady
					ready = append(ready, read)
				}
				if read.State != want || read.PermissionRevision != nil || (read.GrantID != nil) != selected {
					t.Fatal("explicit selection did not remain exact", read)
				}
				shell := delegatedReadSpec(owner, cell, "shell_"+string(owner.ID))
				shell.Capability = "shell.run"
				if denied := admitOperation(t, s, shell); denied.State != session.OperationDenied || denied.PermissionRevision != nil {
					t.Fatal("restricted child or descendant gained policy authority", denied)
				}
			}
			if _, err := s.RevokeGrant(t.Context(), issuer.ID); err != nil {
				t.Fatal(err)
			}
			for _, operation := range ready {
				if allowed, err := s.DispatchOperation(t.Context(), operation.ID); err != nil || allowed {
					t.Fatal("automatic policy replaced a revoked issuer", allowed, err)
				}
			}
		})
	}
}

func TestChildPolicyDelegationExpiresAcrossPolicyChanges(t *testing.T) {
	for _, change := range []string{"mode", "interactive-denial"} {
		t.Run(change, func(t *testing.T) {
			s := fresh(t)
			root, _ := operationCell(t, s)
			setModeTest(t, s, root.ID, "automatic", 1, session.PermissionAutomatic)
			request := childRequest(root.ID)
			child := spawnChildTest(t, s, "child", request)
			grandchild := spawnChildTest(t, s, "grandchild", childRequest(child.Session.ID))
			var ready []session.Operation
			for _, admitted := range []ChildAdmission{child, grandchild} {
				owner := *admitted.Session
				cell := childOperationCell(t, s, owner.ID)
				operation := admitOperation(t, s, delegatedReadSpec(owner, cell, "ready_"+string(owner.ID)))
				if operation.State != session.OperationReady || operation.PermissionRevision == nil || *operation.PermissionRevision != 2 {
					t.Fatal("default inheritance was not captured", operation)
				}
				ready = append(ready, operation)
			}
			dispatchedSpec := ready[0].OperationSpec
			dispatchedSpec.ID, dispatchedSpec.RequestID = "already-dispatched", "already-dispatched"
			dispatched := admitOperation(t, s, dispatchedSpec)
			if allowed, err := s.DispatchOperation(t.Context(), dispatched.ID); err != nil || !allowed {
				t.Fatal(allowed, err)
			}
			if change == "mode" {
				setModeTest(t, s, root.ID, "disable", 2, session.PermissionPrompt)
				setModeTest(t, s, root.ID, "reenable", 3, session.PermissionAutomatic)
			} else {
				denyTest(t, s, root.ID, "deny", 2, true)
				denyTest(t, s, root.ID, "permit", 3, false)
			}
			for _, operation := range ready {
				if allowed, err := s.DispatchOperation(t.Context(), operation.ID); err != nil || allowed {
					t.Fatal("retired policy authority dispatched", allowed, err)
				}
				saved, err := s.Operation(t.Context(), operation.ID)
				if err != nil || saved.State != session.OperationDenied || saved.PermissionRevision == nil || *saved.PermissionRevision != 2 {
					t.Fatal("policy retirement lost immutable operation evidence", saved, err)
				}
				next := operation.OperationSpec
				next.ID, next.RequestID = operation.ID+"_new", operation.RequestID+"_new"
				if denied := admitOperation(t, s, next); denied.State != session.OperationDenied || denied.PermissionRevision != nil {
					t.Fatal("later automatic policy revived captured child delegation", denied)
				}
			}
			if _, err := s.SettleOperation(t.Context(), dispatched.ID, session.OperationResult{State: session.OperationSucceeded}); err != nil {
				t.Fatal("policy change retroactively changed dispatched work", err)
			}
			if retry := spawnChildTest(t, s, "child", request); retry.Session.ID != child.Session.ID {
				t.Fatal("old spawn receipt was replaced")
			}
			blocked := spawnChildTest(t, s, "stale-parent-child", childRequest(child.Session.ID))
			blockedCell := childOperationCell(t, s, blocked.Session.ID)
			if denied := admitOperation(t, s, delegatedReadSpec(*blocked.Session, blockedCell, "stale-parent-read")); denied.State != session.OperationDenied {
				t.Fatal("stale ancestor delegated the new policy revision", denied)
			}
			freshChild := spawnChildTest(t, s, "fresh-child", childRequest(root.ID))
			freshCell := childOperationCell(t, s, freshChild.Session.ID)
			freshRead := admitOperation(t, s, delegatedReadSpec(*freshChild.Session, freshCell, "fresh-read"))
			if freshRead.State != session.OperationReady || freshRead.PermissionRevision == nil || *freshRead.PermissionRevision != 4 {
				t.Fatal("new explicit inheritance missed the current policy", freshRead)
			}
		})
	}
}

func TestChildPolicyDelegationKeepsExactWorkspaceAndExplicitConsent(t *testing.T) {
	s := fresh(t)
	root, _ := operationCell(t, s)
	setModeTest(t, s, root.ID, "automatic", 1, session.PermissionAutomatic)
	for _, cwd := range []string{filepath.Join(root.WorkingDirectory, "nested"), t.TempDir()} {
		request := childRequest(root.ID)
		request.WorkingDirectory = cwd
		child := spawnChildTest(t, s, "child_"+filepath.Base(cwd), request)
		grandchild := spawnChildTest(t, s, "grandchild_"+filepath.Base(cwd), childRequest(child.Session.ID))
		for _, admitted := range []ChildAdmission{child, grandchild} {
			owner := *admitted.Session
			cell := childOperationCell(t, s, owner.ID)
			if denied := admitOperation(t, s, delegatedReadSpec(owner, cell, "scope_"+string(owner.ID))); denied.State != session.OperationDenied || denied.PermissionRevision != nil {
				t.Fatal("different workspace inherited automatic policy", denied)
			}
		}
	}
	child := spawnChildTest(t, s, "same-workspace", childRequest(root.ID))
	cell := childOperationCell(t, s, child.Session.ID)
	for _, capability := range []string{"mcp.call", "mcp.connect", "computer.run", "computer.applescript"} {
		spec := delegatedReadSpec(*child.Session, cell, capability)
		spec.Capability = capability
		operation := admitOperation(t, s, spec)
		if operation.State != session.OperationDenied || operation.PermissionRevision != nil || operation.GrantID != nil {
			t.Fatal("inherited policy bypassed explicit host consent", operation)
		}
		forged := operation
		forged.PermissionRevision = new(session.Revision(2))
		if err := authorizeOperation(t.Context(), s.db, forged); !errors.Is(err, ErrConflict) {
			t.Fatal("forged delegated policy bypassed explicit consent", err)
		}
	}
	question := admitOperation(t, s, questionSpec(t, cell, "child-question", false))
	if question.State != session.OperationDenied || question.PermissionRevision != nil {
		t.Fatal("inherited policy bypassed root-only questions", question)
	}
	if count(t, s, "permissions") != 0 {
		t.Fatal("child denial created a widening permission prompt")
	}
}

func TestChildPolicyDelegationDoesNotInheritAskOrOneUseApproval(t *testing.T) {
	s := fresh(t)
	root, rootCell := operationCell(t, s)
	approved := admitOperation(t, s, delegatedReadSpec(root, rootCell, "one-use"))
	if _, err := s.ResolvePermission(t.Context(), approved.ID, true); err != nil {
		t.Fatal(err)
	}
	child := spawnChildTest(t, s, "ask-child", childRequest(root.ID))
	cell := childOperationCell(t, s, child.Session.ID)
	if denied := admitOperation(t, s, delegatedReadSpec(*child.Session, cell, "ask-read")); denied.State != session.OperationDenied || denied.GrantID != nil {
		t.Fatal("child inherited a one-use approval", denied)
	}
	setModeTest(t, s, root.ID, "automatic", 1, session.PermissionAutomatic)
	if denied := admitOperation(t, s, delegatedReadSpec(*child.Session, cell, "later-read")); denied.State != session.OperationDenied || denied.PermissionRevision != nil {
		t.Fatal("existing Ask child gained automatic authority retroactively", denied)
	}
	grandchild := spawnChildTest(t, s, "grandchild", childRequest(child.Session.ID))
	grandchildCell := childOperationCell(t, s, grandchild.Session.ID)
	if denied := admitOperation(t, s, delegatedReadSpec(*grandchild.Session, grandchildCell, "grandchild-read")); denied.State != session.OperationDenied {
		t.Fatal("undelegated ancestor passed root automatic authority", denied)
	}
}
