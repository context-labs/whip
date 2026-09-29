package rpc_test

import (
	"encoding/json"
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func TestBindingDefinitionAndConfigurationSocketContract(t *testing.T) {
	_, client := fixture(t)
	doc := protocol.DefinitionDocument{ID: "bound", Name: "Bound", Defaults: protocol.ConfigPatch{Modules: []protocol.ID{"files"}, Tools: map[string]protocol.ToolDeclaration{"lookup": {TimeoutMillis: 900000, InputSchema: json.RawMessage(`{"type":"object"}`)}}, Hooks: map[string]protocol.HookDeclaration{"before_tool": {Operations: []protocol.ID{"files.read"}, TimeoutMillis: 0}}}}
	registered := call[protocol.Definition](t, client, "definitions.register", doc)
	result := call[protocol.CreateTreeResult](t, client, "trees.create", protocol.CreateTreeParams{CreationID: "bindings-root", Engine: "quickjs", Definition: registered.Ref, WorkingDirectory: t.TempDir(), Overrides: protocol.ConfigPatch{Model: &protocol.ModelSelection{Provider: "scripted", Name: "scripted"}}})
	config := result.Root.Configuration
	if config.ToolsDefinition == nil || *config.ToolsDefinition != registered.Ref || config.HooksDefinition == nil || *config.HooksDefinition != registered.Ref || len(config.Modules) != 1 || config.Tools["lookup"].TimeoutMillis != 900000 {
		t.Fatal("lost captured binding contract", config)
	}
	narrowed := call[protocol.Session](t, client, "sessions.configure", protocol.UpdateConfigurationParams{SessionID: result.Root.ID, ExpectedRevision: result.Root.ConfigRevision, Patch: protocol.ConfigPatch{Modules: []protocol.ID{}, Tools: map[string]protocol.ToolDeclaration{}}})
	if narrowed.Configuration.Modules == nil || len(narrowed.Configuration.Modules) != 0 || narrowed.Configuration.ToolsDefinition == nil {
		t.Fatal("empty subset erased ceiling/source", narrowed)
	}
	restored := call[protocol.Session](t, client, "sessions.configure", protocol.UpdateConfigurationParams{SessionID: result.Root.ID, ExpectedRevision: narrowed.ConfigRevision, Patch: protocol.ConfigPatch{Modules: config.Modules, Tools: config.Tools}})
	for _, patch := range []protocol.ConfigPatch{{Modules: []protocol.ID{"shell"}}, {Hooks: map[string]protocol.HookDeclaration{}}} {
		var rejected protocol.Session
		err := client.Call(t.Context(), "sessions.configure", protocol.UpdateConfigurationParams{SessionID: result.Root.ID, ExpectedRevision: restored.ConfigRevision, Patch: patch}, &rejected)
		if !rpcKind(err, "INVALID") {
			t.Fatal("binding expansion was accepted", err)
		}
	}
}
