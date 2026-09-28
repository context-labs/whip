package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func resourceState(t *testing.T, s *Store, owner session.SessionID, kind session.ResourceKind) session.ResourceUsage {
	t.Helper()
	values, err := s.Resources(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range values {
		if value.SessionID == owner && value.Kind == kind {
			return value
		}
	}
	t.Fatalf("missing resource %s for %s", kind, owner)
	return session.ResourceUsage{}
}

func resourceLimit(t *testing.T, s *Store, owner session.SessionID, kind session.ResourceKind, limit int64) session.ResourceUsage {
	t.Helper()
	current := resourceState(t, s, owner, kind)
	result, err := s.SetResource(t.Context(), owner, current.Revision, session.ResourceLimit{Kind: kind, Limit: &limit})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestResourceSiblingsCompeteForLastDescendantAndDeletionReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s, other := openTest(t, path), openTest(t, path)
	_, root := create(t, s, []session.ResourceLimit{{Kind: session.ResourceDescendants, Limit: new(int64(3))}})
	left := spawnChildTest(t, s, "left", childRequest(root.ID))
	right := spawnChildTest(t, s, "right", childRequest(root.ID))
	requests := []ChildRequest{childRequest(left.Session.ID), childRequest(right.Session.ID)}
	identities := []session.RequestIdentity{{ClientID: "resource", RequestID: "left"}, {ClientID: "resource", RequestID: "right"}}
	results := make([]ChildAdmission, 2)
	failures := make([]error, 2)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for i, db := range []*Store{s, other} {
		workers.Go(func() {
			<-start
			results[i], failures[i] = db.SpawnChild(t.Context(), identities[i], requests[i])
		})
	}
	close(start)
	workers.Wait()
	winner, loser := 0, 1
	if failures[0] != nil {
		winner, loser = 1, 0
	}
	if failures[winner] != nil || !errors.Is(failures[loser], ErrLimit) {
		t.Fatalf("siblings did not share one remaining slot: %v", failures)
	}
	accepted := results[winner]
	if accepted.Session == nil || resourceState(t, s, root.ID, session.ResourceDescendants).Used != 3 {
		t.Fatal("accepted child was not charged to root")
	}
	retry, err := other.SpawnChild(t.Context(), identities[winner], requests[winner])
	if err != nil || !reflect.DeepEqual(retry, accepted) {
		t.Fatalf("retry at exhausted capacity changed admission: %+v %v", retry, err)
	}
	if _, err := s.SetLifecycle(t.Context(), accepted.Session.ID, session.Stopped); err != nil {
		t.Fatal(err)
	}
	if _, err := other.SpawnChild(t.Context(), identities[loser], requests[loser]); !errors.Is(err, ErrLimit) {
		t.Fatalf("stopping a retained session released its slot: %v", err)
	}
	if err := s.DeleteSubtree(t.Context(), accepted.Session.ID); err != nil {
		t.Fatal(err)
	}
	deleted, err := other.SpawnChild(t.Context(), identities[winner], requests[winner])
	if err != nil || deleted.Session != nil || deleted.Admission.Input != nil || deleted.Admission.Receipt.DeletedAt == nil {
		t.Fatalf("deleted receipt recreated capacity: %+v %v", deleted, err)
	}
	if resourceState(t, other, root.ID, session.ResourceDescendants).Used != 2 {
		t.Fatal("deletion did not release exactly one descendant")
	}
	if _, err := other.SpawnChild(t.Context(), identities[loser], requests[loser]); err != nil {
		t.Fatalf("loser could not reuse released slot: %v", err)
	}
}

func TestResourceDepthZeroInheritanceAndRevisionCAS(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, nil)
	request := childRequest(root.ID)
	request.Resources = []session.ResourceLimit{{Kind: session.ResourceDepth, Limit: new(int64(0))}}
	child := spawnChildTest(t, s, "child", request).Session
	values, err := s.Resources(t.Context(), child.ID)
	if err != nil || len(values) != 2*len(session.ResourceKinds()) {
		t.Fatalf("ancestor scopes: %+v %v", values, err)
	}
	for i, value := range values {
		if i < len(session.ResourceKinds()) {
			if value.SessionID != child.ID || value.Used != 0 && value.Kind != session.ResourceQueuedInputs {
				t.Fatalf("child scopes were not first: %+v", values)
			}
			if value.Kind != session.ResourceDepth && (value.Revision != 0 || value.Limit != nil) {
				t.Fatalf("implicit child cap copied ancestor policy: %+v", value)
			}
		} else if value.SessionID != root.ID || value.Revision != 1 || value.Limit == nil {
			t.Fatalf("root defaults were not persisted: %+v", value)
		}
	}
	identity := session.RequestIdentity{ClientID: "resource", RequestID: "grandchild"}
	grandchildRequest := childRequest(child.ID)
	if _, err := s.SpawnChild(t.Context(), identity, grandchildRequest); !errors.Is(err, ErrLimit) {
		t.Fatalf("local depth zero permitted grandchild: %v", err)
	}
	current := resourceState(t, s, child.ID, session.ResourceDepth)
	cleared, err := s.SetResource(t.Context(), child.ID, current.Revision, session.ResourceLimit{Kind: session.ResourceDepth})
	if err != nil || cleared.Limit != nil || cleared.Revision != current.Revision+1 {
		t.Fatalf("clear local cap: %+v %v", cleared, err)
	}
	if _, err := s.SetResource(t.Context(), child.ID, current.Revision, session.ResourceLimit{Kind: session.ResourceDepth}); !errors.Is(err, ErrConflict) {
		t.Fatalf("same desired payload accepted stale revision: %v", err)
	}
	if _, err := s.SpawnChild(t.Context(), identity, grandchildRequest); err != nil {
		t.Fatalf("inherited ancestor capacity was unavailable: %v", err)
	}
	if _, err := s.SetResource(t.Context(), root.ID, 1, session.ResourceLimit{Kind: session.ResourceDepth}); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("root became unlimited: %v", err)
	}
	if _, err := s.SetResource(t.Context(), root.ID, 1, session.ResourceLimit{Kind: session.ResourceDepth, Limit: new(int64(1))}); !errors.Is(err, ErrLimit) {
		t.Fatalf("depth tightened below retained descendant: %v", err)
	}
}

func TestResourceNarrowingIncludesEveryAncestorDistance(t *testing.T) {
	s := fresh(t)
	limits := []session.ResourceLimit{
		{Kind: session.ResourceDepth, Limit: new(int64(3))},
		{Kind: session.ResourceDescendants, Limit: new(int64(4))},
		{Kind: session.ResourceQueuedInputs, Limit: new(int64(5))},
		{Kind: session.ResourceActiveOperations, Limit: new(int64(4))},
		{Kind: session.ResourceSubscriptions, Limit: new(int64(6))},
	}
	_, root := create(t, s, limits)
	child := spawnChildTest(t, s, "child", childRequest(root.ID)).Session
	grandchild := spawnChildTest(t, s, "grandchild", childRequest(child.ID)).Session
	for _, limit := range limits {
		allowed := *limit.Limit
		if limit.Kind == session.ResourceDepth || limit.Kind == session.ResourceDescendants {
			allowed -= 2
		}
		if _, err := s.SetResource(t.Context(), grandchild.ID, 0, session.ResourceLimit{Kind: limit.Kind, Limit: new(allowed + 1)}); !errors.Is(err, ErrLimit) {
			t.Fatalf("%s widened through missing parent cap: %v", limit.Kind, err)
		}
		resourceLimit(t, s, grandchild.ID, limit.Kind, allowed)
	}
	if _, err := s.SetResource(t.Context(), root.ID, 1, session.ResourceLimit{Kind: session.ResourceDescendants, Limit: new(int64(1))}); !errors.Is(err, ErrLimit) {
		t.Fatalf("descendants tightened below current occupancy: %v", err)
	}
}

func TestResourceQueueClaimCancelAndAtomicChildAdmission(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, []session.ResourceLimit{{Kind: session.ResourceQueuedInputs, Limit: new(int64(1))}})
	first := spawnChildTest(t, s, "first", childRequest(root.ID))
	request := childRequest(root.ID)
	request.Resources = []session.ResourceLimit{{Kind: session.ResourceDescendants, Limit: new(int64(0))}}
	identity := session.RequestIdentity{ClientID: "resource", RequestID: "second"}
	before := make(map[string]int)
	for _, table := range []string{"sessions", "inputs", "receipts", "session_configurations", "resource_limits"} {
		before[table] = count(t, s, table)
	}
	if _, err := s.SpawnChild(t.Context(), identity, request); !errors.Is(err, ErrLimit) {
		t.Fatalf("initial child input bypassed ancestor queue: %v", err)
	}
	for table, want := range before {
		if got := count(t, s, table); got != want {
			t.Fatalf("queue rejection left partial %s: got %d want %d", table, got, want)
		}
	}
	claim(t, s, first.Session.ID)
	if resourceState(t, s, root.ID, session.ResourceQueuedInputs).Used != 0 {
		t.Fatal("claim kept queued capacity until turn completion")
	}
	second, err := s.SpawnChild(t.Context(), identity, request)
	if err != nil || second.Admission.Input == nil {
		t.Fatalf("failed admission could not retry after claim: %+v %v", second, err)
	}
	if _, err := s.SetLifecycle(t.Context(), second.Session.ID, session.Stopped); err != nil {
		t.Fatal(err)
	}
	if resourceState(t, s, root.ID, session.ResourceQueuedInputs).Used != 1 {
		t.Fatal("stop silently released a retained queued input")
	}
	if _, err := s.CancelInput(t.Context(), second.Admission.Input.ID); err != nil {
		t.Fatal(err)
	}
	queued := submit(t, s, root.ID, "root-input")
	if retry := submit(t, s, root.ID, "root-input"); retry.Input.ID != queued.Input.ID {
		t.Fatal("retry consumed a second queue slot")
	}
	if resourceState(t, s, root.ID, session.ResourceQueuedInputs).Used != 1 {
		t.Fatal("cancel and retry changed current occupancy")
	}
	if _, err := s.CancelInput(t.Context(), queued.Input.ID); err != nil {
		t.Fatal(err)
	}
	if resourceState(t, s, root.ID, session.ResourceQueuedInputs).Used != 0 {
		t.Fatal("queued cancellation did not release capacity")
	}
}

func TestResourceAncestorTighteningAndAdmissionSerialize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s, other := openTest(t, path), openTest(t, path)
	_, root := create(t, s, []session.ResourceLimit{{Kind: session.ResourceDescendants, Limit: new(int64(1))}})
	failures := make([]error, 2)
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Go(func() {
		<-start
		_, failures[0] = s.SetResource(t.Context(), root.ID, 1, session.ResourceLimit{Kind: session.ResourceDescendants, Limit: new(int64(0))})
	})
	workers.Go(func() {
		<-start
		_, failures[1] = other.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "resource", RequestID: "race"}, childRequest(root.ID))
	})
	close(start)
	workers.Wait()
	winner, loser := 0, 1
	if failures[0] != nil {
		winner, loser = 1, 0
	}
	if failures[winner] != nil || !errors.Is(failures[loser], ErrLimit) {
		t.Fatalf("edit and admission did not serialize: %v", failures)
	}
	current := resourceState(t, s, root.ID, session.ResourceDescendants)
	if current.Limit == nil || current.Used > *current.Limit {
		t.Fatalf("ancestor limit was exceeded: %+v", current)
	}
}

func TestResourceDeletionReleasesCapacityButNotModelSpend(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, []session.ResourceLimit{{Kind: session.ResourceDescendants, Limit: new(int64(1))}})
	budgetLimit(t, s, root.ID, session.BudgetModelCalls, 1)
	first := spawnChildTest(t, s, "first", childRequest(root.ID)).Session
	turn := claim(t, s, first.ID).Turn
	attempt := reserveTest(t, s, budgetRequest(turn.ID, "spent"))
	dispatchTest(t, s, attempt.ID)
	outcome := session.ModelAttemptResult{
		State: session.AttemptSucceeded, Usage: session.ModelUsage{Input: new(int64(1)), Output: new(int64(2))},
		ReportedCostNanoUSD: new(int64(3)), ElapsedMillis: new(int64(4)),
	}
	if _, err := s.SettleModelAttempt(t.Context(), attempt.ID, outcome, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSubtree(t.Context(), first.ID); err != nil {
		t.Fatal(err)
	}
	if resourceState(t, s, root.ID, session.ResourceDescendants).Used != 0 || budgetState(t, s, root.ID, session.BudgetModelCalls).Used != 1 {
		t.Fatal("deletion conflated reusable capacity with permanent model spend")
	}
	second := spawnChildTest(t, s, "second", childRequest(root.ID)).Session
	secondTurn := claim(t, s, second.ID).Turn
	if _, err := s.ReserveModelAttempt(t.Context(), budgetRequest(secondTurn.ID, "blocked")); !errors.Is(err, ErrLimit) {
		t.Fatalf("replacement child reused spent model budget: %v", err)
	}
}

func TestResourceOperationSettlementReleasesCapacity(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	resourceLimit(t, s, owner.ID, session.ResourceActiveOperations, 1)
	first := admitOperation(t, s, operationSpec(cell, "first"))
	if _, err := s.AdmitOperation(t.Context(), operationSpec(cell, "second")); !errors.Is(err, ErrLimit) {
		t.Fatalf("pending permission bypassed operation capacity: %v", err)
	}
	if count(t, s, "permissions") != 1 || resourceState(t, s, owner.ID, session.ResourceActiveOperations).Used != 1 {
		t.Fatal("rejected operation left a prompt or capacity")
	}
	if _, err := s.SettleOperation(t.Context(), first.ID, session.OperationResult{State: session.OperationCancelled}); err != nil {
		t.Fatal(err)
	}
	second := admitOperation(t, s, operationSpec(cell, "second"))
	if retry := admitOperation(t, s, operationSpec(cell, "first")); retry.State != session.OperationCancelled {
		t.Fatal("terminal retry reactivated cancelled operation")
	}
	if _, err := s.ResolvePermission(t.Context(), second.ID, false); err != nil {
		t.Fatal(err)
	}
	if resourceState(t, s, owner.ID, session.ResourceActiveOperations).Used != 0 {
		t.Fatal("denied operation retained capacity")
	}
	standingGrant(t, s, owner.ID, "standing")
	third := admitOperation(t, s, operationSpec(cell, "third"))
	if dispatched, err := s.DispatchOperation(t.Context(), third.ID); err != nil || !dispatched {
		t.Fatalf("dispatch: %v %v", dispatched, err)
	}
	if resourceState(t, s, owner.ID, session.ResourceActiveOperations).Used != 1 {
		t.Fatal("dispatch released capacity before effect settlement")
	}
	if _, err := s.SettleOperation(t.Context(), third.ID, session.OperationResult{State: session.OperationSucceeded}); err != nil {
		t.Fatal(err)
	}
	if resourceState(t, s, owner.ID, session.ResourceActiveOperations).Used != 0 {
		t.Fatal("settled operation retained capacity")
	}
}

func TestResourceSubscriptionsShareAncestorAndReleaseOnUnsubscribeOrDeletion(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, []session.ResourceLimit{{Kind: session.ResourceSubscriptions, Limit: new(int64(1))}})
	child := spawnChildTest(t, s, "child", childRequest(root.ID)).Session
	request := session.StateSubscribe{Key: "topic", Delivery: session.MailQueued}
	first := subscribeStateTest(t, s, child.ID, "first", "topic", 0)
	if _, err := s.SubscribeState(t.Context(), root.ID, "second", request); !errors.Is(err, ErrLimit) {
		t.Fatalf("parent bypassed child's occupied subscription slot: %v", err)
	}
	if _, err := s.UnsubscribeState(t.Context(), child.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	second := subscribeStateTest(t, s, root.ID, "second", "topic", 0)
	if retry := subscribeStateTest(t, s, child.ID, "first", "topic", 0); retry.CancelledAt == nil {
		t.Fatal("subscription retry reactivated cancelled subscription")
	}
	if resourceState(t, s, root.ID, session.ResourceSubscriptions).Used != 1 {
		t.Fatal("cancelled subscription consumed another slot")
	}
	if _, err := s.UnsubscribeState(t.Context(), root.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	subscribeStateTest(t, s, child.ID, "third", "topic", 0)
	if err := s.DeleteSubtree(t.Context(), child.ID); err != nil {
		t.Fatal(err)
	}
	if resourceState(t, s, root.ID, session.ResourceSubscriptions).Used != 0 {
		t.Fatal("deleted owner's subscription retained capacity")
	}
	subscribeStateTest(t, s, root.ID, "fourth", "topic", 0)
}

func TestResourceAbsoluteDepthBoundThroughInheritedScopes(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, []session.ResourceLimit{
		{Kind: session.ResourceDepth, Limit: new(int64(session.MaxSessionDepth))},
		{Kind: session.ResourceDescendants, Limit: new(int64(200))},
	})
	parent := root.ID
	issuer := standingGrant(t, s, root.ID, "root-grant").ID
	// Build a valid deep fixture without repeating every admission projection;
	// the final allowed and first denied edges use the public transaction.
	if err := s.write(t.Context(), func(tx *sql.Tx) error {
		for range session.MaxSessionDepth - 1 {
			child, err := insertSession(t.Context(), tx, root.TreeID, &parent, root.Definition, root.Config, root.WorkingDirectory)
			if err != nil {
				return err
			}
			grant := session.Grant{
				ID: session.GrantID("grant_" + child.ID), SessionID: child.ID,
				Capability: "filesystem.read", Resource: "/workspace", IssuerID: &issuer,
			}
			if err := insertGrant(t.Context(), tx, grant); err != nil {
				return err
			}
			issuer = grant.ID
			parent = child.ID
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	parent = spawnChildTest(t, s, "max-depth", childRequest(parent)).Session.ID
	if _, err := s.CreateGrant(t.Context(), session.Grant{
		ID: "last-grant", SessionID: parent, Capability: "filesystem.read", Resource: "/workspace", IssuerID: &issuer,
	}); err != nil {
		t.Fatalf("supported depth rejected its complete grant chain: %v", err)
	}
	if resourceState(t, s, root.ID, session.ResourceDepth).Used != session.MaxSessionDepth {
		t.Fatal("supported maximum depth was not reachable")
	}
	if _, err := s.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "resource", RequestID: "too-deep"}, childRequest(parent)); !errors.Is(err, ErrLimit) {
		t.Fatalf("missing local caps bypassed absolute depth bound: %v", err)
	}
	if got := resourceState(t, s, root.ID, session.ResourceDescendants).Used; got != session.MaxSessionDepth {
		t.Fatalf("rejected child remained in deepest subtree: %d", got)
	}
}

func TestResourceDeniedChildOperationNeedsNoActiveCapacity(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, []session.ResourceLimit{{Kind: session.ResourceActiveOperations, Limit: new(int64(0))}})
	child := spawnChildTest(t, s, "child", childRequest(root.ID)).Session
	turn := claim(t, s, child.ID).Turn
	message, err := s.AppendMessage(t.Context(), turn.ID, session.MessageDraft{
		ID: "call", Role: session.Assistant,
		Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "call", Name: "execute", Arguments: []byte(`{"code":"1"}`)}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	cell, dispatch, err := s.BeginCell(t.Context(), session.CellSpec{ID: "cell", TurnID: turn.ID, CallMessageID: message.ID, CallID: "call"})
	if err != nil || !dispatch {
		t.Fatalf("begin child cell: %v %v", dispatch, err)
	}
	operation := admitOperation(t, s, operationSpec(cell, "denied"))
	if operation.State != session.OperationDenied || resourceState(t, s, root.ID, session.ResourceActiveOperations).Used != 0 || count(t, s, "permissions") != 0 {
		t.Fatalf("denial consumed capacity or opened a permission: %+v", operation)
	}
}
