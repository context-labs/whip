package rpc_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

func TestWorkspaceCompletionUsesExactSelectedSessionAndExplicitPaths(t *testing.T) {
	r, c := fixture(t)
	rootDir, childDir := t.TempDir(), t.TempDir()
	for path, body := range map[string]string{filepath.Join(rootDir, "root-file.txt"): "ROOT_CONTENT_SECRET", filepath.Join(childDir, "child-file.txt"): "CHILD_CONTENT_SECRET"} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tree := call[protocol.CreateTreeResult](t, c, "trees.create", protocol.CreateTreeParams{CreationID: "completion-root", Resources: []protocol.ResourceLimit{{Kind: "runnable_descendants", Limit: new(protocol.Counter(0))}}, Engine: "starlark", Definition: c.Builtins()[0], WorkingDirectory: rootDir, Overrides: protocol.ConfigPatch{Model: &protocol.ModelSelection{Provider: "scripted", Name: "scripted"}}})
	child := call[protocol.SpawnSessionResult](t, c, "sessions.spawn", protocol.SpawnSessionParams{Identity: protocol.RequestIdentity{ClientID: "completion", RequestID: "child"}, ParentID: tree.Root.ID, WorkingDirectory: &childDir, Parts: []protocol.Part{{Type: "text", Text: "queued"}}, GrantIDs: []protocol.ID{}})
	for _, scope := range []struct {
		id            protocol.ID
		cwd, expected string
	}{{tree.Root.ID, rootDir, "@root-file.txt"}, {child.Session.ID, childDir, "@child-file.txt"}} {
		result := call[protocol.WorkspaceCompletionResult](t, c, "workspace.complete", protocol.WorkspaceCompletionParams{SessionID: scope.id, Kind: "mention", Prefix: "file", Limit: 64})
		if result.WorkingDirectory != scope.cwd || len(result.Candidates) != 1 || result.Candidates[0].Text != scope.expected || result.Truncated {
			t.Fatal(result)
		}
	}
	explicit := call[protocol.WorkspaceCompletionResult](t, c, "workspace.complete", protocol.WorkspaceCompletionParams{SessionID: child.Session.ID, Kind: "path", Prefix: filepath.ToSlash(rootDir) + "/", Limit: 64})
	if len(explicit.Candidates) != 1 || explicit.Candidates[0].Text != filepath.ToSlash(filepath.Join(rootDir, "root-file.txt")) {
		t.Fatal(explicit)
	}
	for _, request := range []protocol.WorkspaceCompletionParams{{SessionID: "missing", Kind: "mention", Limit: 1}, {SessionID: tree.Root.ID, Kind: "mention", Limit: 65}, {SessionID: tree.Root.ID, Kind: "mention", Prefix: strings.Repeat("é", 2049), Limit: 1}} {
		err := c.Call(t.Context(), "workspace.complete", request, new(protocol.WorkspaceCompletionResult))
		var rpcErr *client.Error
		if err == nil {
			t.Fatal("accepted invalid scope/bounds")
		}
		if errors.As(err, &rpcErr) && rpcErr.Kind != "INVALID" && rpcErr.Kind != "NOT_FOUND" {
			t.Fatal(err)
		}
	}
	for _, id := range []protocol.ID{tree.Root.ID, child.Session.ID} {
		grants, err := r.Grants(t.Context(), session.SessionID(id), "", 64)
		if err != nil || len(grants) != 0 {
			t.Fatal("completion minted authority", grants, err)
		}
		messages, err := r.History(t.Context(), session.SessionID(id), 0, 64)
		if err != nil || len(messages) != 0 {
			t.Fatal("completion created conversation", messages, err)
		}
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := c.Call(ctx, "workspace.complete", protocol.WorkspaceCompletionParams{SessionID: tree.Root.ID, Kind: "mention", Limit: 1}, new(protocol.WorkspaceCompletionResult)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestWorkspaceCompletionReadsFreshDirectory(t *testing.T) {
	r, c := fixture(t)
	tree := create(t, c)
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "fresh.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := r.SetWorkingDirectory(t.Context(), session.WorkspaceSetRequest{ID: "completion-cd", SessionID: session.SessionID(tree.Root.ID), ExpectedRevision: session.Revision(tree.Root.ConfigRevision), Path: target})
	if err != nil {
		t.Fatal(err)
	}
	fresh := call[protocol.WorkspaceCompletionResult](t, c, "workspace.complete", protocol.WorkspaceCompletionParams{SessionID: tree.Root.ID, Kind: "mention", Prefix: "fresh", Limit: 64})
	if fresh.WorkingDirectory != changed.Session.WorkingDirectory || len(fresh.Candidates) != 1 || fresh.Candidates[0].Text != "@fresh.txt" {
		t.Fatal("completion used stale cwd", fresh)
	}
}
