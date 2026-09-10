package daemon

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"

	"github.com/context-labs/whip/internal/agentdef"
	"github.com/context-labs/whip/internal/session"
)

// toolingDefinition registers a coding-shaped definition with custom tools
// (two by default) and returns its id.
func toolingDefinition(t *testing.T, store *session.Store, tools ...agentdef.Tool) string {
	t.Helper()
	definition := agentdef.Coding()
	definition.ID = "tooling"
	definition.Tools = tools
	if len(tools) == 0 {
		definition.Tools = []agentdef.Tool{
			{Name: "lookup", Description: "Fetch a ticket by id", InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`)},
			{Name: "other", Description: "Another tool", InputSchema: json.RawMessage(`{"type":"object"}`)},
		}
	}
	document, err := agentdef.Encode(definition)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registerDefinition(t.Context(), store, document, "tools-test"); err != nil {
		t.Fatal(err)
	}
	return definition.ID
}

// A root receives a tools grant for its definition's tools; children narrow it
// by name through the spawn argument, never widen it, and keep the narrowing
// across a daemon restart.
func TestToolsGrantFollowsDefinitionAndChildNarrowing(t *testing.T) {
	_, client := promptRuntimeProvider(t)
	path := filepath.Join(t.TempDir(), "sessions.db")
	store := openStore(t, path)
	rootID := createDefinitionRoot(t, store, toolingDefinition(t, store))
	owner, root, runtime := openPromptRuntime(t, store, rootID, client)
	parent := runtime.rootNode
	if !slices.Equal(parent.definition.ToolNames(), []string{"lookup", "other"}) {
		t.Fatalf("root tools = %v", parent.definition.ToolNames())
	}
	if err := store.AuthorizeCapability(t.Context(), rootID, rootID, root.authority.Tools, "tools.lookup", ""); err != nil {
		t.Fatalf("root tools grant missing: %v", err)
	}
	narrow := spawnMCPChild(t, parent, map[string]any{"name": "narrow", "tools": []any{"lookup"}})
	none := spawnMCPChild(t, parent, map[string]any{"name": "none", "tools": []any{}})
	inherit := spawnMCPChild(t, parent, map[string]any{"name": "inherit"})
	if !slices.Equal(narrow.definition.ToolNames(), []string{"lookup"}) || len(none.definition.ToolNames()) != 0 || !slices.Equal(inherit.definition.ToolNames(), []string{"lookup", "other"}) {
		t.Fatalf("child tools = %v / %v / %v", narrow.definition.ToolNames(), none.definition.ToolNames(), inherit.definition.ToolNames())
	}
	if tools, err := store.LoadAgentTools(t.Context(), rootID, narrow.id); err != nil || !slices.Equal(tools, []string{"lookup"}) {
		t.Fatalf("narrow child grant = %v %v", tools, err)
	}
	if _, err := parent.host.Call(t.Context(), "agents", "spawn", map[string]any{"prompt": "no", "name": "wide", "tools": []any{"missing"}}); err == nil || err.Error() != `tool "missing" is not available to the parent` {
		t.Fatalf("widening spawn error = %v", err)
	}
	if _, err := narrow.host.Call(t.Context(), "agents", "spawn", map[string]any{"prompt": "no", "name": "regain", "tools": []any{"other"}}); err == nil || err.Error() != `tool "other" is not available to the parent` {
		t.Fatalf("child regained a narrowed tool: %v", err)
	}
	for _, node := range []*AgentSession{narrow, none, inherit} {
		waitAgentIdle(t, node)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	_, _, restored := openPromptRuntime(t, openStore(t, path), rootID, client)
	restored.mu.RLock()
	defer restored.mu.RUnlock()
	for _, want := range []struct {
		id    string
		tools []string
	}{{narrow.id, []string{"lookup"}}, {none.id, nil}, {inherit.id, []string{"lookup", "other"}}} {
		node := restored.agents[want.id]
		if node == nil || !slices.Equal(node.definition.ToolNames(), want.tools) {
			t.Fatalf("restored child %s tools = %v, want %v", want.id, node, want.tools)
		}
	}
}
