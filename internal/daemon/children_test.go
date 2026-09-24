package daemon

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/agentdef"
	"github.com/context-labs/whip/internal/session"
)

// agents.spawn(definition=...) selects a named child of the parent's
// definition: its instructions, modules, capabilities, tools, budgets, and
// report mode apply; explicit arguments still narrow; restart resolves the
// same child.
func TestNamedChildDefinitionsApplyAndRestore(t *testing.T) {
	_, client := promptRuntimeProvider(t)
	path := filepath.Join(t.TempDir(), "sessions.db")
	store := openStore(t, path)
	definition := agentdef.Coding()
	definition.ID = "tooling"
	definition.Tools = []agentdef.Tool{
		{Name: "lookup", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "other", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	persona := "You research one question and report back."
	definition.Children = map[string]agentdef.Child{
		"researcher": {
			Instructions: &agentdef.Instructions{Persona: persona, ProjectFiles: nil},
			Modules:      []string{"context", "files", "agents"}, Capabilities: []string{"read"}, Tools: []string{"lookup"},
			Budgets: map[string]int64{"tokens": 5000, "cost": 250}, Report: "message",
		},
	}
	document, err := agentdef.Encode(definition)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registerDefinition(t.Context(), store, document, "children-test"); err != nil {
		t.Fatal(err)
	}
	rootID := createDefinitionRoot(t, store, "tooling")
	owner, root, runtime := openPromptRuntime(t, store, rootID, client)
	parent := runtime.rootNode
	if _, err := parent.host.Call(t.Context(), "agents", "spawn", map[string]any{"prompt": "go", "name": "nobody", "definition": "archivist"}); err == nil || !strings.Contains(err.Error(), `has no child named "archivist"`) {
		t.Fatalf("unknown child error = %v", err)
	}
	if _, err := parent.host.Call(t.Context(), "agents", "spawn", map[string]any{"prompt": "go", "name": "wide", "definition": "researcher", "capabilities": []any{"write"}}); err == nil || err.Error() != `capability "write" is not available to the parent` {
		t.Fatalf("named child widened by arguments: %v", err)
	}
	child := spawnMCPChild(t, parent, map[string]any{"name": "scout", "definition": "researcher", "budgets": map[string]any{"tokens": 100}})
	waitAgentIdle(t, child)
	check := func(node *AgentSession, stage string) {
		t.Helper()
		if node.definition.ID != "tooling/researcher" || node.definition.Instructions.Persona != persona ||
			!slices.Equal(node.definition.Modules, []string{"context", "files", "agents"}) || !slices.Equal(node.definition.Capabilities, []string{"read"}) ||
			!slices.Equal(node.definition.ToolNames(), []string{"lookup"}) || node.report != "message" {
			t.Fatalf("%s child definition = %+v report=%q", stage, node.definition, node.report)
		}
	}
	check(child, "spawned")
	budgets, err := root.InspectBudgets(t.Context(), parent.id, child.id)
	if err != nil {
		t.Fatal(err)
	}
	limits := map[session.BudgetKind]int64{}
	for _, budget := range budgets {
		if budget.Limit != nil {
			limits[budget.Kind] = *budget.Limit
		}
	}
	// The explicit spawn budget overrides the child's default; untouched kinds keep it.
	if limits["tokens"] != 100 || limits["cost"] != 250 {
		t.Fatalf("child budgets = %v", limits)
	}
	if name, err := store.AgentDefinitionName(t.Context(), rootID, child.id); err != nil || name != "researcher" {
		t.Fatalf("stored child definition name = %q %v", name, err)
	}
	// A grandchild inherits the named child's narrowed definition.
	if _, err := child.host.Call(t.Context(), "agents", "spawn", map[string]any{"prompt": "go", "name": "regain", "tools": []any{"other"}}); err == nil || err.Error() != `tool "other" is not available to the parent` {
		t.Fatalf("grandchild regained a tool: %v", err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	_, _, restored := openPromptRuntime(t, openStore(t, path), rootID, client)
	restored.mu.RLock()
	node := restored.agents[child.id]
	restored.mu.RUnlock()
	if node == nil {
		t.Fatal("child not restored")
	}
	check(node, "restored")
}
