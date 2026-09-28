package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func childOperationCell(t *testing.T, s *Store, owner session.SessionID) session.Cell {
	t.Helper()
	turn := claim(t, s, owner).Turn
	message, err := s.AppendMessage(t.Context(), turn.ID, session.MessageDraft{
		ID: session.MessageID("message_" + string(owner)), Role: session.Assistant,
		Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "call", Name: "execute", Arguments: []byte(`{"code":"1"}`)}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	cell, dispatch, err := s.BeginCell(t.Context(), session.CellSpec{ID: session.CellID("cell_" + string(owner)), TurnID: turn.ID, CallMessageID: message.ID, CallID: "call"})
	if err != nil || !dispatch {
		t.Fatalf("child cell: %v %v", dispatch, err)
	}
	return cell
}

func childGrants(t *testing.T, s *Store, owner session.SessionID) []session.Grant {
	t.Helper()
	grants, err := s.Grants(t.Context(), owner, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	return grants
}

func TestDelegatedGrantsNarrowWithoutPermissionWidening(t *testing.T) {
	s := fresh(t)
	root, rootCell := operationCell(t, s)
	issuer := standingGrant(t, s, root.ID, "issuer")
	request := childRequest(root.ID)
	request.GrantIDs = []session.GrantID{}
	child := spawnChildTest(t, s, "child", request)
	cell := childOperationCell(t, s, child.Session.ID)
	denied := admitOperation(t, s, operationSpec(cell, "child-denied"))
	if denied.State != session.OperationDenied || denied.Result == nil || denied.FinishedAt == nil || denied.GrantID != nil || count(t, s, "permissions") != 0 {
		t.Fatalf("child without authority did not record denial: %+v", denied)
	}
	if _, err := s.ResolvePermission(t.Context(), denied.ID, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("approval widened child authority: %v", err)
	}
	delegated := session.Grant{ID: "child-grant", SessionID: child.Session.ID, Capability: issuer.Capability, Resource: issuer.Resource, IssuerID: &issuer.ID}
	if _, err := s.CreateGrant(t.Context(), delegated); err != nil {
		t.Fatal(err)
	}
	if retry := admitOperation(t, s, operationSpec(cell, "child-denied")); retry.State != session.OperationDenied {
		t.Fatal("late grant resurrected denied operation")
	}
	ready := admitOperation(t, s, operationSpec(cell, "child-ready"))
	if ready.State != session.OperationReady || ready.GrantID == nil || *ready.GrantID != delegated.ID {
		t.Fatalf("explicit later delegation did not authorize new operation: %+v", ready)
	}
	changed := operationSpec(cell, "wrong-scope")
	changed.Resource = "/workspace/subdirectory"
	if op := admitOperation(t, s, changed); op.State != session.OperationDenied {
		t.Fatal("resource prefix implicitly widened exact grant scope")
	}
	changed = operationSpec(rootCell, "root-prompt")
	changed.Capability = "filesystem.write"
	if op := admitOperation(t, s, changed); op.State != session.OperationWaiting {
		t.Fatal("root permission behavior changed")
	}
}

func TestCreateGrantRequiresExactLiveDirectParentIssuer(t *testing.T) {
	s := fresh(t)
	root, cell := operationCell(t, s)
	issuer := standingGrant(t, s, root.ID, "root-issuer")
	child := spawnChildTest(t, s, "child", childRequest(root.ID))
	childIssuer := childGrants(t, s, child.Session.ID)[0]
	grandchild := spawnChildTest(t, s, "grandchild", childRequest(child.Session.ID))
	sibling := spawnChildTest(t, s, "sibling", childRequest(root.ID))
	siblingIssuer := childGrants(t, s, sibling.Session.ID)[0]
	_, other := create(t, s, session.DefaultTreePolicy())
	foreign := standingGrant(t, s, other.ID, "foreign")
	spec := operationSpec(cell, "approval")
	spec.Resource = "/one-use"
	admitOperation(t, s, spec)
	if _, err := s.ResolvePermission(t.Context(), spec.ID, true); err != nil {
		t.Fatal(err)
	}
	approved, err := s.Operation(t.Context(), spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	base := session.Grant{ID: "attempt", SessionID: child.Session.ID, Capability: issuer.Capability, Resource: issuer.Resource, IssuerID: &issuer.ID}
	for _, test := range []struct {
		name   string
		change func(*session.Grant)
	}{
		{"missing-issuer", func(g *session.Grant) { g.IssuerID = nil }},
		{"root-with-issuer", func(g *session.Grant) { g.SessionID = root.ID }},
		{"foreign", func(g *session.Grant) { g.IssuerID = &foreign.ID }},
		{"sibling", func(g *session.Grant) { g.SessionID = grandchild.Session.ID; g.IssuerID = &siblingIssuer.ID }},
		{"grandparent", func(g *session.Grant) { g.SessionID = grandchild.Session.ID }},
		{"capability", func(g *session.Grant) { g.Capability = "filesystem.write" }},
		{"resource", func(g *session.Grant) { g.Resource = "/workspace/subdirectory" }},
		{"one-use", func(g *session.Grant) { g.IssuerID = approved.GrantID; g.Resource = spec.Resource }},
	} {
		grant := base
		test.change(&grant)
		if _, err := s.CreateGrant(t.Context(), grant); !errors.Is(err, ErrConflict) {
			t.Fatalf("%s accepted invalid delegation: %v", test.name, err)
		}
	}
	base.ID = "valid-later"
	created, err := s.CreateGrant(t.Context(), base)
	if err != nil {
		t.Fatal(err)
	}
	changed := base
	changed.IssuerID = &childIssuer.ID
	if _, err := s.CreateGrant(t.Context(), changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("retry changed issuer: %v", err)
	}
	mustFail(t, s, "UPDATE grants SET issuer_id=? WHERE id=?", childIssuer.ID, created.ID)
	if _, err := s.RevokeGrant(t.Context(), issuer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateGrant(t.Context(), base); err != nil {
		t.Fatalf("immutable retry depended on current issuer validity: %v", err)
	}
	base.ID = "after-revocation"
	if _, err := s.CreateGrant(t.Context(), base); !errors.Is(err, ErrConflict) {
		t.Fatalf("delegated revoked issuer: %v", err)
	}
	base.SessionID, base.IssuerID = grandchild.Session.ID, &childIssuer.ID
	if _, err := s.CreateGrant(t.Context(), base); !errors.Is(err, ErrConflict) {
		t.Fatalf("delegated invalid ancestor chain: %v", err)
	}
}

func TestAncestorRevocationAndAdmissionValidateWholeChain(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, session.DefaultTreePolicy())
	issuer := standingGrant(t, s, root.ID, "root")
	child := spawnChildTest(t, s, "child", childRequest(root.ID))
	grandchild := spawnChildTest(t, s, "grandchild", childRequest(child.Session.ID))
	cell := childOperationCell(t, s, grandchild.Session.ID)
	ready := admitOperation(t, s, operationSpec(cell, "ready"))
	dispatched := admitOperation(t, s, operationSpec(cell, "dispatched"))
	if ok, err := s.DispatchOperation(t.Context(), dispatched.ID); err != nil || !ok {
		t.Fatalf("valid chain failed dispatch: %v %v", ok, err)
	}
	// The revocation and ready descendant denial must roll back together.
	execTest(t, s, "CREATE TRIGGER fail_deny BEFORE UPDATE ON operations WHEN NEW.state='denied' BEGIN SELECT RAISE(ABORT,'denial fault'); END")
	if _, err := s.RevokeGrant(t.Context(), issuer.ID); err == nil {
		t.Fatal("denial fault did not abort revocation")
	}
	if grant := childGrants(t, s, root.ID)[0]; grant.RevokedAt != nil {
		t.Fatal("failed revocation changed issuer")
	}
	execTest(t, s, "DROP TRIGGER fail_deny")
	for range 2 {
		if _, err := s.RevokeGrant(t.Context(), issuer.ID); err != nil {
			t.Fatal(err)
		}
	}
	denied, err := s.Operation(t.Context(), ready.ID)
	if err != nil || denied.State != session.OperationDenied {
		t.Fatalf("ready descendant survived ancestor revocation: %+v %v", denied, err)
	}
	inflight, err := s.Operation(t.Context(), dispatched.ID)
	if err != nil || inflight.State != session.OperationDispatched {
		t.Fatalf("revocation relabeled dispatched effect: %+v %v", inflight, err)
	}
	if op := admitOperation(t, s, operationSpec(cell, "new-after-revoke")); op.State != session.OperationDenied {
		t.Fatal("admission ignored revoked ancestor")
	}
	grandchildGrant := childGrants(t, s, grandchild.Session.ID)[0]
	if grandchildGrant.RevokedAt != nil {
		t.Fatal("ancestor revocation overwrote descendant's own revocation fact")
	}
	// A malformed persisted association still has to pass the dispatch gate.
	execTest(t, s, `INSERT INTO operations (id,cell_id,request_id,capability,resource,arguments,state,grant_id,created_at)
 VALUES ('forged-chain',?,'forged-chain','filesystem.read','/workspace','{}','ready',?,?)`, cell.ID, grandchildGrant.ID, now())
	if ok, err := s.DispatchOperation(t.Context(), "forged-chain"); ok || !errors.Is(err, ErrConflict) {
		t.Fatalf("dispatch ignored revoked ancestor: %v %v", ok, err)
	}
	inherited := spawnChildTest(t, s, "after-revoke", childRequest(child.Session.ID))
	if grants := childGrants(t, s, inherited.Session.ID); len(grants) != 0 {
		t.Fatalf("nil inheritance retained invalid chain: %+v", grants)
	}
	request := childRequest(child.Session.ID)
	request.GrantIDs = []session.GrantID{childGrants(t, s, child.Session.ID)[0].ID}
	if _, err := s.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "client", RequestID: "invalid-chain"}, request); !errors.Is(err, ErrConflict) {
		t.Fatalf("explicit inheritance accepted invalid chain: %v", err)
	}
}

func TestAncestorRevocationRacesDispatch(t *testing.T) {
	for i := range 8 {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "runtime.db")
			s, other := openTest(t, path), openTest(t, path)
			_, root := create(t, s, session.DefaultTreePolicy())
			issuer := standingGrant(t, s, root.ID, "issuer")
			child := spawnChildTest(t, s, "child", childRequest(root.ID))
			cell := childOperationCell(t, s, child.Session.ID)
			op := admitOperation(t, s, operationSpec(cell, "op"))
			start := make(chan struct{})
			var dispatched bool
			var dispatchErr, revokeErr error
			var workers sync.WaitGroup
			workers.Go(func() { <-start; dispatched, dispatchErr = s.DispatchOperation(t.Context(), op.ID) })
			workers.Go(func() { <-start; _, revokeErr = other.RevokeGrant(t.Context(), issuer.ID) })
			close(start)
			workers.Wait()
			if dispatchErr != nil || revokeErr != nil {
				t.Fatalf("dispatch/revoke: %v %v", dispatchErr, revokeErr)
			}
			settled, err := s.Operation(t.Context(), op.ID)
			want := session.OperationDenied
			if dispatched {
				want = session.OperationDispatched
			}
			if err != nil || settled.State != want {
				t.Fatalf("revocation did not linearize at dispatch: %+v %v", settled, err)
			}
		})
	}
}

func TestSpawnOperationRejectsMismatchedScopeAndCancelledTurn(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	_, foreign := create(t, s, session.DefaultTreePolicy())
	for _, test := range []struct {
		id, capability, resource string
		parent                   session.SessionID
	}{
		{"wrong-capability", "filesystem.read", string(owner.TreeID), owner.ID},
		{"wrong-tree", "agents.spawn", string(foreign.TreeID), owner.ID},
		{"wrong-parent", "agents.spawn", string(owner.TreeID), foreign.ID},
		{"cancelled", "agents.spawn", string(owner.TreeID), owner.ID},
	} {
		if _, err := s.CreateGrant(t.Context(), session.Grant{ID: session.GrantID(test.id), SessionID: owner.ID, Capability: test.capability, Resource: test.resource}); err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(childRequest(test.parent))
		if err != nil {
			t.Fatal(err)
		}
		op := admitOperation(t, s, session.OperationSpec{ID: session.OperationID(test.id), CellID: cell.ID, RequestID: test.id, Capability: test.capability, Resource: test.resource, Arguments: raw})
		if test.id == "cancelled" {
			if _, err := s.CancelTurn(t.Context(), cell.TurnID); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.SpawnChildOperation(t.Context(), op.ID); err == nil {
			t.Fatalf("%s applied invalid child spawn", test.id)
		}
		stored, err := s.Operation(t.Context(), op.ID)
		if err != nil || stored.State != session.OperationReady || stored.DispatchedAt != nil || count(t, s, "sessions") != 2 {
			t.Fatalf("invalid spawn changed durable state: %+v %v", stored, err)
		}
	}
}

func TestStoppingParentDoesNotRevokeChildAuthority(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, session.DefaultTreePolicy())
	standingGrant(t, s, root.ID, "issuer")
	child := spawnChildTest(t, s, "child", childRequest(root.ID))
	cell := childOperationCell(t, s, child.Session.ID)
	ready := admitOperation(t, s, operationSpec(cell, "before-stop"))
	if _, err := s.SetLifecycle(t.Context(), root.ID, session.Stopped); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.DispatchOperation(t.Context(), ready.ID); err != nil || !ok {
		t.Fatalf("parent stop revoked child's admitted authority: %v %v", ok, err)
	}
	fresh := admitOperation(t, s, operationSpec(cell, "after-stop"))
	if fresh.State != session.OperationReady {
		t.Fatalf("parent stop revoked child's standing authority: %+v", fresh)
	}
	if _, err := s.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "client", RequestID: "stopped-parent"}, childRequest(root.ID)); !errors.Is(err, ErrStopped) {
		t.Fatalf("stopped parent spawned new child: %v", err)
	}
}
