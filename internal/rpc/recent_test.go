package rpc_test

import (
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/protocol"
)

func TestRecentTreesRPCIsBoundedMetadataWithCurrentModel(t *testing.T) {
	_, c := fixture(t)
	empty := call[protocol.RecentTreesResult](t, c, "trees.recent", protocol.RecentTreesParams{Limit: 1})
	if empty.Items == nil || len(empty.Items) != 0 || empty.HasMore || empty.CatalogRevision == 0 {
		t.Fatal(empty)
	}
	var last protocol.CreateTreeResult
	for range 2 {
		last = call[protocol.CreateTreeResult](t, c, "trees.create", protocol.CreateTreeParams{CreationID: protocol.ID(rand.Text()), Engine: "starlark", Definition: c.Builtins()[0], WorkingDirectory: t.TempDir(), Overrides: protocol.ConfigPatch{Instructions: &protocol.Instructions{Text: "private instructions body"}, Model: &protocol.ModelSelection{Provider: "scripted", Name: "scripted"}}})
	}
	page := call[protocol.RecentTreesResult](t, c, "trees.recent", protocol.RecentTreesParams{Limit: 1})
	if len(page.Items) != 1 || !page.HasMore || page.Items[0].RootID != last.Root.ID || page.Items[0].WorkingDirectory != last.Root.WorkingDirectory || page.Items[0].Model.Name != "scripted" {
		t.Fatal(page)
	}
	if _, err := time.Parse(time.RFC3339Nano, page.Items[0].LastActivityAt); err != nil {
		t.Fatal(err)
	}
	if page.NextCursor == nil {
		t.Fatal("missing recency cursor", page)
	}
	next := call[protocol.RecentTreesResult](t, c, "trees.recent", protocol.RecentTreesParams{Limit: 1, After: page.NextCursor, Archived: new(false)})
	if len(next.Items) != 1 || next.Items[0].RootID == last.Root.ID || next.HasMore || next.NextCursor != nil {
		t.Fatal("did not advance", next)
	}
	raw := call[json.RawMessage](t, c, "trees.recent", protocol.RecentTreesParams{Limit: 100})
	for _, private := range []string{"private instructions body", "configuration", "messages"} {
		if strings.Contains(string(raw), private) {
			t.Fatal("recent read included a private body", string(raw))
		}
	}
	for _, params := range []any{map[string]any{}, map[string]any{"limit": 0}, map[string]any{"limit": 101}, map[string]any{"limit": 1, "after": last.Tree.ID},
		protocol.RecentTreesParams{Limit: 1, After: &protocol.RecentTreeCursor{TreeID: last.Tree.ID, LastActivityAt: "yesterday"}},
		protocol.RecentTreesParams{Limit: 1, After: &protocol.RecentTreeCursor{TreeID: last.Tree.ID, LastActivityAt: "2026-09-29T00:00:00.000000001Z"}},
	} {
		if err := c.Call(t.Context(), "trees.recent", params, new(json.RawMessage)); err == nil {
			t.Fatal("accepted invalid recency request", params)
		}
	}
}
