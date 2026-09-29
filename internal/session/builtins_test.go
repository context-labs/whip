package session

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestBuiltinPersonasKeepHostModelsAndConstrainDeclaredSurface(t *testing.T) {
	base := Configuration{
		Model: ModelSelection{Provider: "host", Name: "model", Effort: "high"}, Compaction: CompactionPolicy{ThresholdPercent: 63}, GoalsEnabled: true,
		Tools: map[string]ToolDeclaration{"unexpected": {InputSchema: json.RawMessage(`{"type":"object"}`)}},
		Hooks: map[string]HookDeclaration{"turn_start": {Optional: true}}, Children: map[string]DefinitionRef{}, OutputSchema: json.RawMessage(`{"type":"string"}`),
	}
	builtins := Builtins()
	if len(builtins) != 3 || builtins[0].ID != "assistant" {
		t.Fatal("existing default builtin changed")
	}
	for _, document := range builtins[1:] {
		t.Run(document.ID, func(t *testing.T) {
			canonical, _, ref, err := CanonicalDefinition(document)
			if err != nil {
				t.Fatal(err)
			}
			config, err := Resolve(base, canonical, ConfigPatch{})
			if err != nil {
				t.Fatal(err)
			}
			if ref.ID != document.ID || !reflect.DeepEqual(config.Model, base.Model) || config.Compaction.ThresholdPercent != 63 {
				t.Fatal("host model defaults changed", config)
			}
			if !config.AutomaticTitle || !config.Instructions.StandingInstructions || !slices.Equal(config.Instructions.ProjectFiles, []string{"CLAUDE.md", "AGENTS.md"}) {
				t.Fatal("discovery defaults changed", config)
			}
			if len(config.Tools) != 0 || len(config.Hooks) != 0 || len(config.Children) != 0 || (len(config.OutputSchema) != 0 && string(config.OutputSchema) != "null") {
				t.Fatal("builtin inherited undeclared custom contract", config)
			}
			if document.ID == "coding" {
				if !config.GoalsEnabled || !config.Instructions.DiscoverSkills || !config.MCPServers.All || !slices.Contains(config.Modules, "agents") || !strings.Contains(config.Instructions.Text, "You are an expert coding agent.") || !strings.Contains(config.Instructions.Text, "never force-push") {
					t.Fatal("coding behavior changed", config)
				}
			} else {
				if config.GoalsEnabled || config.Instructions.DiscoverSkills || config.MCPServers.All || len(config.MCPServers.Servers) != 0 || !strings.Contains(config.Instructions.Text, "Do not add dependencies") {
					t.Fatal("junior behavior changed", config)
				}
				for _, name := range []string{"agents", "models", "mcp", "browser", "computer", "schedules", "goals", "skills"} {
					if slices.Contains(config.Modules, name) {
						t.Fatalf("junior exposes %s", name)
					}
				}
			}
			config.Modules[0] = "mutated"
			config.Instructions.ProjectFiles[0] = "mutated"
			next, err := Resolve(base, document, ConfigPatch{})
			if err != nil || slices.Contains(next.Modules, "mutated") || next.Instructions.ProjectFiles[0] != "CLAUDE.md" {
				t.Fatal("builtin mutable alias", err)
			}
		})
	}
}
