package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

// Identity-only creation is retained solely for old store fixtures.
func (s *Store) SpawnSession(ctx context.Context, request SpawnSession) (result session.Session, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		result, err = spawnSession(ctx, tx, request)
		return err
	})
	return
}

func childRequest(parent session.SessionID) ChildRequest {
	return ChildRequest{ParentID: parent, Parts: []session.Part{{Type: "text", Text: "child task"}}}
}

func spawnChildTest(t *testing.T, s *Store, key string, request ChildRequest) ChildAdmission {
	t.Helper()
	result, err := s.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "spawn-test", RequestID: key}, request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Session == nil || result.Admission.Input == nil {
		t.Fatal("missing child admission")
	}
	return result
}

func TestSpawnChildConcurrentRetryRestartAndDeletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s, other := openTest(t, path), openTest(t, path)
	policy := session.DefaultTreePolicy()
	policy.MaxSessions = 2
	_, parent := create(t, s, policy)
	grant := standingGrant(t, s, parent.ID, "parent-grant")
	request := childRequest(parent.ID)
	identity := session.RequestIdentity{ClientID: "client", RequestID: "spawn"}
	results := make(chan ChildAdmission, 12)
	var workers sync.WaitGroup
	for i := range 12 {
		workers.Go(func() {
			db := s
			if i%2 != 0 {
				db = other
			}
			result, err := db.SpawnChild(t.Context(), identity, request)
			if err != nil {
				t.Errorf("spawn: %v", err)
				return
			}
			results <- result
		})
	}
	workers.Wait()
	close(results)
	var first ChildAdmission
	for result := range results {
		if first.Session == nil {
			first = result
		}
		if !reflect.DeepEqual(first, result) {
			t.Fatal("retry produced different child or input")
		}
	}
	if first.Session == nil || count(t, s, "sessions") != 2 || count(t, s, "inputs") != 1 || count(t, s, "receipts") != 1 || count(t, s, "grants") != 2 {
		t.Fatal("concurrent spawn was not one atomic admission")
	}
	if _, err := s.RevokeGrant(t.Context(), grant.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openTest(t, path)
	retried, err := reopened.SpawnChild(t.Context(), identity, request)
	if err != nil || !reflect.DeepEqual(first, retried) {
		t.Fatalf("restart or issuer revocation changed receipt: %+v %v", retried, err)
	}
	changed := request
	changed.GrantIDs = []session.GrantID{}
	if _, err := reopened.SpawnChild(t.Context(), identity, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("nil and empty grant selections aliased: %v", err)
	}
	changed = request
	changed.Parts = []session.Part{{Type: "text", Text: "changed"}}
	if _, err := reopened.SpawnChild(t.Context(), identity, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed payload accepted: %v", err)
	}
	if _, err := reopened.Admit(t.Context(), identity, Submission{SessionID: parent.ID, Source: session.AgentInput, Parts: request.Parts}); !errors.Is(err, ErrConflict) {
		t.Fatalf("different operation reused spawn receipt: %v", err)
	}
	if err := reopened.DeleteSubtree(t.Context(), first.Session.ID); err != nil {
		t.Fatal(err)
	}
	deleted, err := reopened.SpawnChild(t.Context(), identity, request)
	if err != nil || deleted.Session != nil || deleted.Admission.Input != nil || deleted.Admission.Receipt.DeletedAt == nil || deleted.Admission.Receipt.Digest != first.Admission.Receipt.Digest {
		t.Fatalf("deleted receipt recreated child: %+v %v", deleted, err)
	}
	if count(t, reopened, "sessions") != 1 || count(t, reopened, "grants") != 1 {
		t.Fatal("child rows survived deletion")
	}
}

func TestSpawnChildRollbackAndReferenceIsolation(t *testing.T) {
	s := fresh(t)
	_, parent := create(t, s, session.DefaultTreePolicy())
	_, unrelated := create(t, s, session.DefaultTreePolicy())
	standingGrant(t, s, parent.ID, "grant")
	reference := contentReference(parent.ID, "parent-ref", "body")
	if _, err := s.RegisterContent(t.Context(), reference); err != nil {
		t.Fatal(err)
	}
	request := childRequest(parent.ID)
	request.Parts = []session.Part{{Type: "content", ReferenceID: reference.ID}, {Type: "content", ReferenceID: reference.ID}}
	identity := session.RequestIdentity{ClientID: "client", RequestID: "atomic"}
	tables := []string{"sessions", "session_configurations", "inputs", "receipts", "grants", "content_references", "content_bodies"}
	before := make(map[string]int)
	for _, table := range tables {
		before[table] = count(t, s, table)
	}
	for _, table := range []string{"session_configurations", "grants", "content_references", "inputs", "receipts"} {
		// Table names are fixed test constants. Abort at each stage, including the last receipt write.
		execTest(t, s, "CREATE TRIGGER fail_spawn BEFORE INSERT ON "+table+" BEGIN SELECT RAISE(ABORT,'injected spawn failure'); END")
		if _, err := s.SpawnChild(t.Context(), identity, request); err == nil {
			t.Fatalf("%s injection did not fail", table)
		}
		for _, check := range tables {
			if count(t, s, check) != before[check] {
				t.Fatalf("%s fault left rows in %s", table, check)
			}
		}
		execTest(t, s, "DROP TRIGGER fail_spawn")
	}
	accepted, err := s.SpawnChild(t.Context(), identity, request)
	if err != nil {
		t.Fatal(err)
	}
	child := accepted.Session.ID
	sharedID := accepted.Admission.Input.Parts[0].ReferenceID
	if sharedID == reference.ID || sharedID != accepted.Admission.Input.Parts[1].ReferenceID || request.Parts[0].ReferenceID != reference.ID {
		t.Fatal("references were not copied once without mutating request")
	}
	shared, err := s.ContentReference(t.Context(), child, sharedID)
	if err != nil || shared.Digest != reference.Digest || shared.Size != reference.Size || shared.MediaType != reference.MediaType {
		t.Fatalf("shared immutable body metadata: %+v %v", shared, err)
	}
	for _, owner := range []session.SessionID{parent.ID, unrelated.ID} {
		if _, err := s.ContentReference(t.Context(), owner, sharedID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("child reference escaped owner: %v", err)
		}
	}
	if _, err := s.ContentReference(t.Context(), child, reference.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("parent reference authorized child: %v", err)
	}
	foreign := childRequest(unrelated.ID)
	foreign.Parts = request.Parts
	if _, err := s.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "client", RequestID: "foreign"}, foreign); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign parent reference accepted: %v", err)
	}
	if count(t, s, "sessions") != 3 || count(t, s, "content_references") != 2 || count(t, s, "content_bodies") != 1 {
		t.Fatal("sharing duplicated body metadata or failed admission leaked child")
	}
	if err := s.DeleteSubtree(t.Context(), child); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ContentReference(t.Context(), parent.ID, reference.ID); err != nil {
		t.Fatalf("child deletion removed parent's reference: %v", err)
	}
}

func TestSpawnChildGrantSelectionAndLimits(t *testing.T) {
	s := fresh(t)
	parent, cell := operationCell(t, s)
	read := standingGrant(t, s, parent.ID, "read")
	write, err := s.CreateGrant(t.Context(), session.Grant{ID: "write", SessionID: parent.ID, Capability: "filesystem.write", Resource: "/workspace"})
	if err != nil {
		t.Fatal(err)
	}
	revoked := standingGrant(t, s, parent.ID, "revoked")
	if _, err := s.RevokeGrant(t.Context(), revoked.ID); err != nil {
		t.Fatal(err)
	}
	spec := operationSpec(cell, "one-use")
	spec.Resource = "/different"
	admitOperation(t, s, spec)
	if _, err := s.ResolvePermission(t.Context(), spec.ID, true); err != nil {
		t.Fatal(err)
	}
	oneUse, err := s.Operation(t.Context(), spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		ids  []session.GrantID
		want int
	}{{"inherit", nil, 2}, {"none", []session.GrantID{}, 0}, {"selected", []session.GrantID{write.ID}, 1}} {
		request := childRequest(parent.ID)
		request.GrantIDs = test.ids
		child := spawnChildTest(t, s, test.name, request)
		grants, err := s.Grants(t.Context(), child.Session.ID, "", 100)
		if err != nil || len(grants) != test.want {
			t.Fatalf("%s delegation: %+v %v", test.name, grants, err)
		}
		for _, grant := range grants {
			if grant.IssuerID == nil || grant.OperationID != nil || (*grant.IssuerID != read.ID && *grant.IssuerID != write.ID) {
				t.Fatalf("invalid inherited issuer: %+v", grant)
			}
		}
	}
	_, other := create(t, s, session.DefaultTreePolicy())
	foreign := standingGrant(t, s, other.ID, "foreign")
	for i, ids := range [][]session.GrantID{{revoked.ID}, {*oneUse.GrantID}, {foreign.ID}, {"missing"}, {read.ID, read.ID}, make([]session.GrantID, session.MaxGrantsPerSession+1)} {
		request := childRequest(parent.ID)
		request.GrantIDs = ids
		before := count(t, s, "sessions")
		if _, err := s.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "client", RequestID: fmt.Sprintf("invalid-%d", i)}, request); err == nil {
			t.Fatalf("invalid delegation accepted: %v", ids)
		}
		if count(t, s, "sessions") != before {
			t.Fatal("invalid delegation left a child")
		}
	}
}

func TestSpawnChildOperationAtomicDispatchAndRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s, other := openTest(t, path), openTest(t, path)
	owner, cell := operationCell(t, s)
	grant, err := s.CreateGrant(t.Context(), session.Grant{ID: "spawn", SessionID: owner.ID, Capability: "agents.spawn", Resource: string(owner.TreeID)})
	if err != nil {
		t.Fatal(err)
	}
	request := childRequest(owner.ID)
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	spec := session.OperationSpec{ID: "spawn-op", CellID: cell.ID, RequestID: "spawn-op", Capability: grant.Capability, Resource: grant.Resource, Arguments: raw}
	admitOperation(t, s, spec)
	execTest(t, s, "CREATE TRIGGER fail_spawn_settle BEFORE UPDATE ON operations WHEN NEW.state='succeeded' BEGIN SELECT RAISE(ABORT,'settlement fault'); END")
	if _, err := s.SpawnChildOperation(t.Context(), spec.ID); err == nil {
		t.Fatal("settlement fault did not fail")
	}
	afterFailure, err := s.Operation(t.Context(), spec.ID)
	if err != nil || afterFailure.State != session.OperationReady || afterFailure.DispatchedAt != nil || count(t, s, "sessions") != 1 || count(t, s, "inputs") != 1 || count(t, s, "receipts") != 1 || count(t, s, "grants") != 1 {
		t.Fatalf("native apply failed non-atomically: %+v %v", afterFailure, err)
	}
	execTest(t, s, "DROP TRIGGER fail_spawn_settle")
	results := make(chan ChildAdmission, 8)
	var workers sync.WaitGroup
	for i := range 8 {
		workers.Go(func() {
			db := s
			if i%2 != 0 {
				db = other
			}
			result, err := db.SpawnChildOperation(t.Context(), spec.ID)
			if err != nil {
				t.Errorf("native spawn: %v", err)
				return
			}
			results <- result
		})
	}
	workers.Wait()
	close(results)
	var first ChildAdmission
	for result := range results {
		if first.Session == nil {
			first = result
		}
		if !reflect.DeepEqual(first, result) {
			t.Fatal("native apply duplicated child")
		}
	}
	settled, err := s.Operation(t.Context(), spec.ID)
	if err != nil || settled.State != session.OperationSucceeded || settled.DispatchedAt == nil || settled.FinishedAt == nil || count(t, s, "sessions") != 2 {
		t.Fatalf("native apply did not settle: %+v %v", settled, err)
	}
	var value struct {
		SessionID session.SessionID `json:"session_id"`
		InputID   session.InputID   `json:"input_id"`
	}
	if err := json.Unmarshal(settled.Result.Value, &value); err != nil || value.SessionID != first.Session.ID || value.InputID != first.Admission.Input.ID {
		t.Fatalf("native result: %+v %v", value, err)
	}
	if _, err := s.RevokeGrant(t.Context(), grant.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSubtree(t.Context(), first.Session.ID); err != nil {
		t.Fatal(err)
	}
	retried, err := other.SpawnChildOperation(t.Context(), spec.ID)
	if err != nil || retried.Session != nil || retried.Admission.Receipt.DeletedAt == nil || count(t, s, "sessions") != 1 {
		t.Fatalf("native retry resurrected child: %+v %v", retried, err)
	}
}

func TestSpawnChildRevalidatesRemappedInputSize(t *testing.T) {
	s := fresh(t)
	_, parent := create(t, s, session.DefaultTreePolicy())
	if _, err := s.RegisterContent(t.Context(), contentReference(parent.ID, "a", "body")); err != nil {
		t.Fatal(err)
	}
	request := childRequest(parent.ID)
	request.Parts = []session.Part{{Type: "text", Text: "x"}, {Type: "content", ReferenceID: "a"}}
	raw, err := json.Marshal(request.Parts)
	if err != nil {
		t.Fatal(err)
	}
	request.Parts[0].Text = strings.Repeat("x", session.MaxDocumentBytes-len(raw)+1)
	if err := session.ValidateInputParts(request.Parts); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "client", RequestID: "size"}, request); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("rewritten input exceeded durable bound: %v", err)
	}
	if count(t, s, "sessions") != 1 || count(t, s, "content_references") != 1 || count(t, s, "inputs") != 0 || count(t, s, "receipts") != 0 {
		t.Fatal("oversized remapped input left partial admission")
	}
}
