package store

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func browserOperation(t *testing.T, owner session.Session, cell session.Cell, id, kind string, scope session.BrowserScope, previous string) session.OperationSpec {
	t.Helper()
	raw, err := json.Marshal(session.BrowserIntent{SessionID: owner.ID, TreeID: owner.TreeID, ConfigRevision: owner.ConfigRevision, Kind: kind, Scope: scope, PreviousResource: previous, Arguments: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	spec := operationSpec(cell, id)
	spec.Capability = "browser.control"
	spec.Resource = scope.Resource()
	spec.Arguments = raw
	return spec
}

func browserScope() session.BrowserScope {
	return session.BrowserScope{ProviderID: "provider", ProviderEpoch: "epoch", TabID: "tab", TabGeneration: "generation", ProfileID: "profile", ControlLineage: "lineage", AttachmentID: "attachment", AttachmentGeneration: "attachment_generation"}
}

func approveDispatchBrowser(t *testing.T, s *Store, spec session.OperationSpec) {
	t.Helper()
	op := admitOperation(t, s, spec)
	if op.State == session.OperationWaiting {
		if _, err := s.ResolvePermission(t.Context(), op.ID, true); err != nil {
			t.Fatal(err)
		}
	}
	if ok, err := s.DispatchOperation(t.Context(), op.ID); err != nil || !ok {
		t.Fatal(ok, err)
	}
}

func TestBrowserPublicationRequiresExactDispatchedIntentAndKeepsOutcomeUnsettled(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	spec := browserOperation(t, owner, cell, "attach", "attach", browserScope(), "")
	op := admitOperation(t, s, spec)
	if err := s.PublishBrowserControl(t.Context(), op.ID); !errors.Is(err, ErrConflict) {
		t.Fatal("before consent", err)
	}
	if count(t, s, "grants") != 0 {
		t.Fatal("offer became authority")
	}
	if _, err := s.ResolvePermission(t.Context(), op.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := s.PublishBrowserControl(t.Context(), op.ID); !errors.Is(err, ErrConflict) {
		t.Fatal("before dispatch", err)
	}
	if ok, err := s.DispatchOperation(t.Context(), op.ID); err != nil || !ok {
		t.Fatal(ok, err)
	}
	for range 2 {
		if err := s.PublishBrowserControl(t.Context(), op.ID); err != nil {
			t.Fatal(err)
		}
	}
	op, err := s.Operation(t.Context(), op.ID)
	if err != nil || op.State != session.OperationDispatched || op.Result != nil {
		t.Fatal("publication fabricated success", op, err)
	}
	grant, err := matchingGrant(t.Context(), s.db, owner.ID, "browser.control", spec.Resource)
	if err != nil || grant.OperationID != nil {
		t.Fatal(grant, err)
	}
	run := browserOperation(t, owner, cell, "run", "run", browserScope(), "")
	if op := admitOperation(t, s, run); op.State != session.OperationReady {
		t.Fatal(op)
	}
	run.Resource = "browser:foreign"
	run.ID = "forged"
	run.RequestID = "forged"
	if _, err := s.AdmitOperation(t.Context(), run); err == nil {
		t.Fatal("resource did not bind canonical scope")
	}
}

func TestBrowserPreviewPublicationRetiresNarrowerAuthorityAndDetachDoesNotSettle(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	old := browserScope()
	old.Preview = &session.BrowserPreviewScope{HostID: "host", HostIdentity: "runtime", ConnectionGeneration: "connection", EnvironmentID: "env", Loopback: "127.0.0.1", Ports: []int{3000}}
	attach := browserOperation(t, owner, cell, "attach", "attach", old, "")
	approveDispatchBrowser(t, s, attach)
	if err := s.PublishBrowserControl(t.Context(), attach.ID); err != nil {
		t.Fatal(err)
	}
	run := browserOperation(t, owner, cell, "old-run", "run", old, "")
	if got := admitOperation(t, s, run); got.State != session.OperationReady {
		t.Fatal(got)
	}
	next := old
	copyPreview := *old.Preview
	copyPreview.Ports = []int{3000, 4000}
	next.Preview = &copyPreview
	expanded := browserOperation(t, owner, cell, "expand", "allow_preview_port", next, old.Resource())
	approveDispatchBrowser(t, s, expanded)
	if err := s.PublishBrowserControl(t.Context(), expanded.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := matchingGrant(t.Context(), s.db, owner.ID, "browser.control", old.Resource()); !errors.Is(err, ErrNotFound) {
		t.Fatal("old grant survived", err)
	}
	if op, _ := s.Operation(t.Context(), run.ID); op.State != session.OperationDenied {
		t.Fatal("ready old operation survived", op)
	}
	detach := browserOperation(t, owner, cell, "detach", "detach", next, "")
	approveDispatchBrowser(t, s, detach)
	if err := s.PublishBrowserControl(t.Context(), detach.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := matchingGrant(t.Context(), s.db, owner.ID, "browser.control", next.Resource()); !errors.Is(err, ErrNotFound) {
		t.Fatal("detached grant survived", err)
	}
	if op, _ := s.Operation(t.Context(), detach.ID); op.State != session.OperationDispatched {
		t.Fatal(op)
	}
}

func TestBrowserCatalogIntrinsicGuard(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	req := session.BrowserCatalogRequest{SessionID: owner.ID, TreeID: owner.TreeID, ConfigRevision: owner.ConfigRevision, Action: "list_tabs"}
	spec := operationSpec(cell, "catalog")
	spec.Capability = "browser.catalog"
	spec.Resource = string(owner.TreeID)
	for _, mutate := range []func(*session.BrowserCatalogRequest){func(r *session.BrowserCatalogRequest) { r.Action = "open" }, func(r *session.BrowserCatalogRequest) { r.SessionID = "foreign" }, func(r *session.BrowserCatalogRequest) { r.ConfigRevision++ }} {
		changed := req
		mutate(&changed)
		spec.Arguments, _ = json.Marshal(changed)
		if _, err := s.AdmitOperation(t.Context(), spec); err == nil {
			t.Fatal("forged intrinsic accepted")
		}
	}
	spec.Arguments, _ = json.Marshal(req)
	op := admitOperation(t, s, spec)
	if op.State != session.OperationReady || op.GrantID != nil || op.PermissionRevision != nil || count(t, s, "grants") != 0 || count(t, s, "permissions") != 0 {
		t.Fatal(op)
	}
	if ok, err := s.DispatchOperation(t.Context(), op.ID); err != nil || !ok {
		t.Fatal(ok, err)
	}
}

func TestBrowserPreviewRetirementRollsBackWithPublicationAndRevokesDescendants(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	old := browserScope()
	old.Preview = &session.BrowserPreviewScope{HostID: "host", HostIdentity: "runtime", ConnectionGeneration: "connection", EnvironmentID: "env", Loopback: "127.0.0.1", Ports: []int{3000}}
	attach := browserOperation(t, owner, cell, "attach", "attach", old, "")
	approveDispatchBrowser(t, s, attach)
	if err := s.PublishBrowserControl(t.Context(), attach.ID); err != nil {
		t.Fatal(err)
	}
	child := spawnChildTest(t, s, "child", childRequest(owner.ID))
	childCell := childOperationCell(t, s, child.Session.ID)
	childOp := browserOperation(t, *child.Session, childCell, "child-run", "run", old, "")
	if admitted := admitOperation(t, s, childOp); admitted.State != session.OperationReady {
		t.Fatal(admitted)
	}
	next := old
	copyPreview := *old.Preview
	copyPreview.Ports = []int{3000, 4000}
	next.Preview = &copyPreview
	expanded := browserOperation(t, owner, cell, "expand", "allow_preview_port", next, old.Resource())
	approveDispatchBrowser(t, s, expanded)
	execTest(t, s, `CREATE TRIGGER fail_browser_publish BEFORE INSERT ON grants WHEN NEW.operation_id IS NULL BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if err := s.PublishBrowserControl(t.Context(), expanded.ID); err == nil {
		t.Fatal("publication ignored failure")
	}
	if op, err := s.Operation(t.Context(), childOp.ID); err != nil || op.State != session.OperationReady {
		t.Fatal("failed publication committed partial retirement", op, err)
	}
	if _, err := matchingGrant(t.Context(), s.db, owner.ID, "browser.control", old.Resource()); err != nil {
		t.Fatal("rollback lost original scope", err)
	}
	execTest(t, s, `DROP TRIGGER fail_browser_publish`)
	if err := s.PublishBrowserControl(t.Context(), expanded.ID); err != nil {
		t.Fatal(err)
	}
	if op, err := s.Operation(t.Context(), childOp.ID); err != nil || op.State != session.OperationDenied {
		t.Fatal("descendant retained old scope", op, err)
	}
	if _, err := matchingGrant(t.Context(), s.db, child.Session.ID, "browser.control", next.Resource()); !errors.Is(err, ErrNotFound) {
		t.Fatal("descendant silently gained expansion", err)
	}
}
