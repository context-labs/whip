package session

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/context-labs/whip/internal/capability"
)

func TestAttentionIncludesUnopenedRootsAndPagesByIdentity(t *testing.T) {
	store, rootID, rootAgent := newMailboxFixture(t)
	otherID, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.EnsureAuthority(t.Context(), otherID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(t.Context(), `UPDATE agents SET status='running' WHERE root_id=?`, rootID); err != nil {
		t.Fatal(err)
	}
	dispatcher := capability.NewDispatcher(store, store.Workspaces(), nil)
	if err := dispatcher.Register(capability.Registration{
		Operation: "write", Mutation: capability.MutationPath, Permission: true,
		Path:    func(json.RawMessage) (string, error) { return "pending.txt", nil },
		Handler: func(_ context.Context, _ capability.Call) (string, error) { return "unused", nil },
	}); err != nil {
		t.Fatal(err)
	}
	cwd, err := store.WorkspaceRoot(t.Context(), otherID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = dispatcher.Dispatch(t.Context(), capability.Request{
		RootID: otherID, AgentID: other.AgentID, CapabilityID: other.Files.ID,
		CapabilityGeneration: other.Files.Generation, OperationID: "attention-permission", Operation: "write", Arguments: json.RawMessage(`{}`),
		TraceID: "test", WorkingDirectory: filepath.Clean(cwd),
	})
	if _, ok := errors.AsType[*capability.PermissionPendingError](err); !ok {
		t.Fatalf("permission was not pending: %v", err)
	}
	page, err := store.AttentionRoots(t.Context(), "", nil, 1)
	if err != nil || len(page) != 1 {
		t.Fatalf("first attention page %+v %v", page, err)
	}
	next, err := store.AttentionRoots(t.Context(), page[0].RootID, nil, 1)
	if err != nil || len(next) != 1 || next[0].RootID == page[0].RootID {
		t.Fatalf("next attention page %+v %v", next, err)
	}
	items := append(page, next...)
	active := items[slices.IndexFunc(items, func(item AttentionRoot) bool { return item.RootID == rootID })]
	waiting := items[slices.IndexFunc(items, func(item AttentionRoot) bool { return item.RootID == otherID })]
	if active.ActiveAgents != 2 || waiting.PendingPermissions != 1 || rootAgent != rootID {
		t.Fatalf("attention counts %+v", items)
	}
	if page, err := store.AttentionRoots(t.Context(), next[0].RootID, nil, 1); err != nil || len(page) != 0 {
		t.Fatalf("last attention page %+v %v", page, err)
	}
}
