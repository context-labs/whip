package rpc_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func TestDiscoveryRPCMetadataPaginationAndBounds(t *testing.T) {
	_, c := fixture(t)
	empty := call[protocol.ListTreesResult](t, c, "trees.list", protocol.ListTreesParams{Limit: 100})
	if empty.Items == nil || len(empty.Items) != 0 || empty.NextCursor != nil {
		t.Fatal(empty)
	}
	created := call[protocol.CreateTreeResult](t, c, "trees.create", protocol.CreateTreeParams{Engine: "starlark", Definition: c.Builtins()[0], WorkingDirectory: t.TempDir(), Overrides: protocol.ConfigPatch{Instructions: &protocol.Instructions{Text: "private root instructions"}, Model: &protocol.ModelSelection{Provider: "scripted", Name: "scripted"}}})
	call[protocol.Tree](t, c, "trees.update", protocol.UpdateTreeParams{TreeID: created.Tree.ID, ExpectedRevision: created.Tree.Revision, Metadata: protocol.TreeMetadata{Title: new("Read from catalog"), Pinned: true}})
	page := call[protocol.ListTreesResult](t, c, "trees.list", protocol.ListTreesParams{Pinned: new(true), Archived: new(false), Limit: 1})
	if len(page.Items) != 1 || page.Items[0].RootID != created.Root.ID || page.Items[0].Tree.Metadata.Title == nil || *page.Items[0].Tree.Metadata.Title != "Read from catalog" || page.NextCursor != nil {
		t.Fatal(page)
	}
	raw := call[json.RawMessage](t, c, "trees.list", protocol.ListTreesParams{Limit: 100})
	for _, private := range []string{"private root instructions", "configuration", "working_directory", "messages"} {
		if strings.Contains(string(raw), private) {
			t.Fatal("catalog loaded private body", string(raw))
		}
	}
	refs := map[string]bool{}
	for _, name := range []string{"One", "Two"} {
		d := call[protocol.Definition](t, c, "definitions.register", protocol.DefinitionDocument{ID: "custom", Name: name, Defaults: protocol.ConfigPatch{Instructions: &protocol.Instructions{Text: "private definition body"}}})
		refs[d.Ref.Revision] = true
	}
	var after *protocol.DefinitionRef
	seen := 0
	for {
		definitions := call[protocol.ListDefinitionsResult](t, c, "definitions.list", protocol.ListDefinitionsParams{After: after, Limit: 1})
		if len(definitions.Items) != 1 {
			t.Fatal(definitions)
		}
		item := definitions.Items[0]
		if item.Ref.ID == "custom" {
			if !refs[item.Ref.Revision] {
				t.Fatal(item)
			}
			seen++
		}
		if definitions.NextCursor == nil {
			break
		}
		after = definitions.NextCursor
	}
	if seen != 2 {
		t.Fatal(seen)
	}
	raw = call[json.RawMessage](t, c, "definitions.list", protocol.ListDefinitionsParams{Limit: 100})
	if strings.Contains(string(raw), "private definition body") || strings.Contains(string(raw), "defaults") {
		t.Fatal(string(raw))
	}
	for _, method := range []string{"trees.list", "definitions.list"} {
		for _, limit := range []int{0, 101} {
			var out json.RawMessage
			if err := c.Call(t.Context(), method, map[string]any{"limit": limit}, &out); err == nil {
				t.Fatal(method, limit)
			}
		}
	}
}
