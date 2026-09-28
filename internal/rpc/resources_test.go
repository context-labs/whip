package rpc_test

import (
	"errors"
	"testing"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

func resource(t *testing.T, c *client.Client, requested, owner protocol.ID, kind string) protocol.ResourceUsage {
	t.Helper()
	page := call[protocol.ResourcesResult](t, c, "resources.list", protocol.SessionParams{SessionID: requested})
	for _, item := range page.Items {
		if item.SessionID == owner && item.Kind == kind {
			return item
		}
	}
	t.Fatalf("missing %s resource for %s in %+v", kind, owner, page)
	return protocol.ResourceUsage{}
}

func TestResourcesRPCEnforcesAncestorCapacityAndRevision(t *testing.T) {
	_, c := fixture(t)
	tree := create(t, c)
	root := tree.Root.ID
	capacity := resource(t, c, root, root, "descendants")
	capacity = call[protocol.ResourceUsage](t, c, "resources.set", protocol.SetResourceParams{SessionID: root, ExpectedRevision: capacity.Revision, Resource: protocol.ResourceLimit{Kind: "descendants", Limit: new(protocol.Counter(1))}})
	spawn := protocol.SpawnSessionParams{Identity: protocol.RequestIdentity{ClientID: "resources", RequestID: "child"}, ParentID: root, Parts: []protocol.Part{{Type: "text", Text: "queued"}}, Resources: []protocol.ResourceLimit{{Kind: "descendants", Limit: new(protocol.Counter(0))}, {Kind: "queued_inputs", Limit: new(protocol.Counter(2))}}}
	child := call[protocol.SpawnSessionResult](t, c, "sessions.spawn", spawn)
	page := call[protocol.ResourcesResult](t, c, "resources.list", protocol.SessionParams{SessionID: child.Session.ID})
	if len(page.Items) != 2*len(session.ResourceKinds()) {
		t.Fatalf("missing ancestor or child resource scopes: %+v", page)
	}
	for i, item := range page.Items {
		owner := child.Session.ID
		if i >= len(session.ResourceKinds()) {
			owner = root
		}
		if item.SessionID != owner {
			t.Fatalf("scopes not nearest first: %+v", page)
		}
	}
	if value := resource(t, c, child.Session.ID, root, "descendants"); value.Used != 1 || value.Limit == nil || *value.Limit != 1 {
		t.Fatalf("ancestor capacity not projected: %+v", value)
	}
	spawn.Identity.RequestID = "second_child"
	var extra protocol.SpawnSessionResult
	if err := c.Call(t.Context(), "sessions.spawn", spawn, &extra); !rpcKind(err, "LIMIT") {
		t.Fatalf("ancestor cap did not reject second child: %v", err)
	}
	queue := resource(t, c, child.Session.ID, child.Session.ID, "queued_inputs")
	cleared := call[protocol.ResourceUsage](t, c, "resources.set", protocol.SetResourceParams{SessionID: child.Session.ID, ExpectedRevision: queue.Revision, Resource: protocol.ResourceLimit{Kind: "queued_inputs"}})
	if cleared.Limit != nil || cleared.Used != 1 {
		t.Fatalf("clear lost inherited semantics or usage: %+v", cleared)
	}
	var updated protocol.ResourceUsage
	if err := c.Call(t.Context(), "resources.set", protocol.SetResourceParams{SessionID: child.Session.ID, ExpectedRevision: queue.Revision, Resource: protocol.ResourceLimit{Kind: "queued_inputs", Limit: new(protocol.Counter(5))}}, &updated); !rpcKind(err, "CONFLICT") {
		t.Fatalf("stale cap update did not conflict: %v", err)
	}
	call[protocol.Input](t, c, "inputs.cancel", protocol.InputParams{InputID: child.Admission.Input.ID})
	if value := resource(t, c, child.Session.ID, root, "queued_inputs"); value.Used != 0 {
		t.Fatalf("cancelled input retained capacity: %+v", value)
	}
	call[protocol.DeleteResult](t, c, "sessions.delete", protocol.SessionParams{SessionID: child.Session.ID})
	if value := resource(t, c, root, root, "descendants"); value.Used != 0 || value.Revision != capacity.Revision {
		t.Fatalf("deletion did not release capacity: %+v", value)
	}
	call[protocol.SpawnSessionResult](t, c, "sessions.spawn", spawn)
	operations := resource(t, c, root, root, "active_operations")
	exact := call[protocol.ResourceUsage](t, c, "resources.set", protocol.SetResourceParams{SessionID: root, ExpectedRevision: operations.Revision, Resource: protocol.ResourceLimit{Kind: "active_operations", Limit: new(protocol.Counter(9007199254740993))}})
	if exact.Limit == nil || *exact.Limit != 9007199254740993 {
		t.Fatalf("resource counter lost precision: %+v", exact)
	}
}

func rpcKind(err error, kind string) bool {
	var rpcError *client.Error
	return errors.As(err, &rpcError) && rpcError.Kind == kind
}

func TestResourceRequestsRejectDuplicateLimitsWithoutAdmission(t *testing.T) {
	_, c := fixture(t)
	limits := []protocol.ResourceLimit{{Kind: "descendants", Limit: new(protocol.Counter(1))}, {Kind: "descendants", Limit: new(protocol.Counter(2))}}
	var result protocol.CreateTreeResult
	params := protocol.CreateTreeParams{Engine: "starlark", Definition: c.Builtins()[0], Resources: limits, WorkingDirectory: t.TempDir(), Overrides: protocol.ConfigPatch{Model: &protocol.ModelSelection{Provider: "scripted", Name: "scripted"}}}
	if err := c.Call(t.Context(), "trees.create", params, &result); !rpcKind(err, "INVALID") {
		t.Fatalf("duplicate root limits accepted: %v", err)
	}
	tree := create(t, c)
	spawn := protocol.SpawnSessionParams{Identity: protocol.RequestIdentity{ClientID: "resources", RequestID: "duplicate"}, ParentID: tree.Root.ID, Parts: []protocol.Part{{Type: "text", Text: "child"}}, Resources: limits}
	var child protocol.SpawnSessionResult
	if err := c.Call(t.Context(), "sessions.spawn", spawn, &child); !rpcKind(err, "INVALID") {
		t.Fatalf("duplicate child limits accepted: %v", err)
	}
	if usage := resource(t, c, tree.Root.ID, tree.Root.ID, "descendants"); usage.Used != 0 {
		t.Fatalf("invalid request admitted child: %+v", usage)
	}
}
