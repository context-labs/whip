package rpc_test

import (
	"crypto/rand"
	"encoding/json"
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func TestNativeTreeSummariesRPC(t *testing.T) {
	_, c := fixture(t)
	created := call[protocol.CreateTreeResult](t, c, "trees.create", protocol.CreateTreeParams{CreationID: protocol.ID(rand.Text()), Engine: "starlark", Definition: c.Builtins()[0], WorkingDirectory: t.TempDir()})
	page := call[protocol.TreeSummariesResult](t, c, "trees.summaries", protocol.TreeSummariesParams{RootIDs: []protocol.ID{created.Root.ID, "missing"}})
	if len(page.Items) != 1 || page.Items[0].RootID != created.Root.ID || page.Items[0].Tree.ID != created.Tree.ID || page.Items[0].WorkingDirectory != created.Root.WorkingDirectory || page.Items[0].Activity.ActiveTurnCount != 0 || len(page.MissingRootIDs) != 1 || page.MissingRootIDs[0] != "missing" {
		t.Fatal(page)
	}
	for _, ids := range [][]protocol.ID{nil, {}, {created.Root.ID, created.Root.ID}, make([]protocol.ID, 65)} {
		var out json.RawMessage
		if err := c.Call(t.Context(), "trees.summaries", protocol.TreeSummariesParams{RootIDs: ids}, &out); err == nil {
			t.Fatal("invalid roots accepted", ids)
		}
	}
}
