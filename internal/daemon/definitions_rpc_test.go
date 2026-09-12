package daemon

import (
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func TestDefinitionRegistryAcrossTransports(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	fixture := newV2Fixture(t, &fakeRunner{})
	author := fixture.dial("unix", "definition-author")
	reader := fixture.dial("websocket", "definition-reader")
	document := juniorDeveloperDocument(t)
	var first protocol.DefinitionRegisterResult
	if err := author.Call(t.Context(), "definitions.register", map[string]any{"definition": document}, &first); err != nil || !first.Created {
		t.Fatalf("register: %+v %v", first, err)
	}
	var duplicate protocol.DefinitionRegisterResult
	if err := reader.Call(t.Context(), "definitions.register", map[string]any{"definition": document}, &duplicate); err != nil || duplicate.Created || duplicate.Revision != first.Revision {
		t.Fatalf("idempotent registration: %+v %v", duplicate, err)
	}
	var authored map[string]any
	if err := json.Unmarshal(document, &authored); err != nil {
		t.Fatal(err)
	}
	authored["instructions"].(map[string]any)["persona"] = "Updated review instructions."
	var second protocol.DefinitionRegisterResult
	if err := author.Call(t.Context(), "definitions.register", map[string]any{"definition": authored}, &second); err != nil || !second.Created || second.Revision == first.Revision {
		t.Fatalf("new revision: %+v %v", second, err)
	}
	var pinned, latest protocol.DefinitionRecord
	if err := reader.Call(t.Context(), "definitions.get", protocol.DefinitionParams{ID: first.ID, Revision: first.Revision}, &pinned); err != nil || pinned.Revision != first.Revision || pinned.RegisteredBy != "definition-author" || pinned.CreatedAt == "" {
		t.Fatalf("pinned revision: %+v %v", pinned, err)
	}
	if err := reader.Call(t.Context(), "definitions.get", protocol.DefinitionParams{ID: first.ID}, &latest); err != nil || latest.Revision != second.Revision || latest.Definition.Instructions.Persona != "Updated review instructions." || pinned.Definition.Instructions.Persona == latest.Definition.Instructions.Persona {
		t.Fatalf("latest revision: %+v %v", latest, err)
	}
	var list protocol.DefinitionList
	if err := reader.Call(t.Context(), "definitions.list", protocol.Empty{}, &list); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(list.Items, func(item protocol.DefinitionSummary) bool {
		return item.ID == first.ID && item.Revision == second.Revision && !item.BuiltIn
	}) {
		t.Fatalf("registry does not advertise latest revision: %+v", list)
	}
	var builtin protocol.DefinitionRecord
	if err := reader.Call(t.Context(), "definitions.get", protocol.DefinitionParams{ID: "coding"}, &builtin); err != nil || !builtin.BuiltIn || builtin.Revision != "" {
		t.Fatalf("built-in definition: %+v %v", builtin, err)
	}
	reserved := maps.Clone(authored)
	reserved["id"] = "coding"
	for _, test := range []struct {
		name, method string
		params       any
	}{
		{"malformed registration parameters", "definitions.register", []string{"invalid"}},
		{"malformed definition", "definitions.register", map[string]any{"definition": "not a document"}},
		{"reserved definition", "definitions.register", map[string]any{"definition": reserved}},
		{"malformed lookup parameters", "definitions.get", []string{"invalid"}},
		{"missing ID", "definitions.get", protocol.DefinitionParams{}},
		{"unknown definition", "definitions.get", protocol.DefinitionParams{ID: "missing-definition"}},
		{"unknown revision", "definitions.get", protocol.DefinitionParams{ID: first.ID, Revision: "missing-revision"}},
		{"revision of built-in", "definitions.get", protocol.DefinitionParams{ID: "coding", Revision: first.Revision}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var result json.RawMessage
			err := reader.Call(t.Context(), test.method, test.params, &result)
			var rpc *RPCError
			if !errors.As(err, &rpc) || rpc.Code != -32602 {
				t.Fatalf("invalid request must be rejected as invalid params: %v", err)
			}
		})
	}
}
