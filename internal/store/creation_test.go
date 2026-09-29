package store

import (
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func creationRequestTest(t *testing.T, id session.CreationID) session.TreeCreationRequest {
	t.Helper()
	_, _, ref, err := session.CanonicalDefinition(session.Builtins()[0])
	if err != nil {
		t.Fatal(err)
	}
	return session.TreeCreationRequest{ID: id, Engine: session.QuickJS, Definition: ref, WorkingDirectory: t.TempDir()}
}

func creationDefaultsTest() session.TreeCreationDefaults {
	return session.TreeCreationDefaults{
		Configuration:  session.Configuration{Model: session.ModelSelection{Provider: "test", Name: "original", Temperature: new(0.0)}},
		Resources:      []session.ResourceLimit{{Kind: session.ResourceDescendants, Limit: new(int64(7))}},
		PermissionMode: session.PermissionAutomatic,
	}
}

func catalogTest(t *testing.T, s *Store) session.Revision {
	t.Helper()
	revision, err := s.TreeCatalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return revision
}

func TestTreeCreationExactRetryCurrentProjectionAndRetainedDeletion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	request := creationRequestTest(t, "MiXeD:Creation")
	original, err := s.CreateRoot(t.Context(), request, creationDefaultsTest())
	if err != nil || original.Root == nil || original.Tree == nil || original.Deleted {
		t.Fatal(original, err)
	}
	if catalogTest(t, s) != 2 || count(t, s, "inputs") != 0 || count(t, s, "receipts") != 0 {
		t.Fatal("creation fabricated execution or omitted invalidation")
	}
	if original.Root.Config.Model.Name != "original" || original.Root.Config.Model.Temperature == nil || *original.Root.Config.Model.Temperature != 0 || original.Tree.Engine != session.QuickJS {
		t.Fatal("defaults not captured", original)
	}
	policy, err := s.PermissionPolicy(t.Context(), original.Root.ID)
	if err != nil || policy.Mode != session.PermissionAutomatic {
		t.Fatal(policy, err)
	}
	limits, err := s.Resources(t.Context(), original.Root.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, limit := range limits {
		if limit.Kind == session.ResourceDescendants && (limit.Limit == nil || *limit.Limit != 7) {
			t.Fatal("resource default not captured")
		}
	}
	other := openTest(t, path)
	retry, err := other.CreateRoot(t.Context(), request, session.TreeCreationDefaults{PermissionMode: "invalid-current-default"})
	if err != nil || !reflect.DeepEqual(retry, original) || catalogTest(t, s) != 2 {
		t.Fatal("retry revalidated defaults or duplicated root", retry, err)
	}
	changed := request
	changed.Engine = session.Starlark
	if _, err := s.CreateRoot(t.Context(), changed, creationDefaultsTest()); !errors.Is(err, ErrConflict) {
		t.Fatal("changed engine did not conflict", err)
	}
	changed = request
	changed.PermissionMode = new(session.PermissionAutomatic)
	if _, err := s.CreateRoot(t.Context(), changed, creationDefaultsTest()); !errors.Is(err, ErrConflict) {
		t.Fatal("omitted versus explicit mode identity collapsed", err)
	}
	updated, err := s.UpdateTree(t.Context(), original.Tree.ID, original.Tree.Revision, session.TreeMetadata{Title: new("Later human title"), Pinned: true})
	if err != nil {
		t.Fatal(err)
	}
	retry, exists, err := s.CreationRetry(t.Context(), request)
	if err != nil || !exists || retry.Creation != original.Creation || !reflect.DeepEqual(*retry.Tree, updated) {
		t.Fatal("receipt changed or returned stale projection", retry, err)
	}
	if _, err := s.SetLifecycle(t.Context(), original.Root.ID, session.Stopped); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSubtree(t.Context(), original.Root.ID); err != nil {
		t.Fatal(err)
	}
	deletedHead := catalogTest(t, s)
	deleted, err := other.TreeCreation(t.Context(), request.ID)
	if err != nil || !deleted.Deleted || deleted.Tree != nil || deleted.Root != nil || deleted.Creation != original.Creation {
		t.Fatal(deleted, err)
	}
	retry, err = other.CreateRoot(t.Context(), request, creationDefaultsTest())
	if err != nil || !reflect.DeepEqual(retry, deleted) || catalogTest(t, s) != deletedHead || count(t, s, "session_trees") != 0 {
		t.Fatal("deleted retry recreated root", err)
	}
	mustFail(t, s, "UPDATE tree_creations SET digest='changed' WHERE id=?", request.ID)
	mustFail(t, s, "DELETE FROM tree_creations WHERE id=?", request.ID)
}

func TestTreeCreationRollbackAndConcurrentDelivery(t *testing.T) {
	s := fresh(t)
	request := creationRequestTest(t, "root")
	for _, fault := range []string{"BEFORE INSERT ON tree_creations", "BEFORE UPDATE ON tree_catalog", "BEFORE INSERT ON permission_policies", "BEFORE INSERT ON budget_limits"} {
		execTest(t, s, "CREATE TRIGGER creation_failure "+fault+" BEGIN SELECT RAISE(ABORT,'injected'); END")
		if _, err := s.CreateRoot(t.Context(), request, creationDefaultsTest()); err == nil {
			t.Fatal("fault did not roll back", fault)
		}
		if count(t, s, "tree_creations") != 0 || count(t, s, "session_trees") != 0 || count(t, s, "sessions") != 0 || catalogTest(t, s) != 1 {
			t.Fatal("partial creation escaped", fault)
		}
		execTest(t, s, "DROP TRIGGER creation_failure")
	}
	const parallel = 8
	var wg sync.WaitGroup
	results := make(chan session.TreeCreationResult, parallel)
	failures := make(chan error, parallel)
	for range parallel {
		wg.Go(func() {
			r, err := s.CreateRoot(t.Context(), request, creationDefaultsTest())
			results <- r
			failures <- err
		})
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	var original session.TreeCreation
	for result := range results {
		if original.ID == "" {
			original = result.Creation
		}
		if result.Creation != original {
			t.Fatal("concurrent duplicate destination")
		}
	}
	if count(t, s, "session_trees") != 1 || catalogTest(t, s) != 2 {
		t.Fatal("duplicate concurrent invalidation")
	}
}
