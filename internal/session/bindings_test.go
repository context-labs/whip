package session

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
)

func bindingTool() ToolDeclaration {
	return ToolDeclaration{InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"const":9007199254740993}},"required":["id"],"additionalProperties":false}`)}
}

func TestBindingResolutionPreservesMixedOriginsAndExplicitEmpty(t *testing.T) {
	rootDoc := DefinitionDocument{ID: "root", Name: "Root", Defaults: ConfigPatch{Modules: []string{"files", "agents"}, Tools: map[string]ToolDeclaration{"lookup": bindingTool()}, Hooks: map[string]HookDeclaration{"before_tool": {Operations: []string{"files.read"}}}}}
	base := Configuration{Model: ModelSelection{Provider: "test", Name: "model"}}
	root, err := Resolve(base, rootDoc, ConfigPatch{})
	if err != nil {
		t.Fatal(err)
	}
	if root.ToolsDefinition == nil || root.HooksDefinition == nil || *root.ToolsDefinition != *root.HooksDefinition {
		t.Fatal("root lost sources", root)
	}
	for _, tools := range []bool{true, false} {
		childDoc := DefinitionDocument{ID: "child", Name: "Child"}
		if tools {
			childDoc.Defaults.Tools = root.Tools
		} else {
			childDoc.Defaults.Hooks = root.Hooks
		}
		child, err := Resolve(root, childDoc, ConfigPatch{Modules: []string{}})
		if err != nil {
			t.Fatal(err)
		}
		if child.Modules == nil || len(child.Modules) != 0 {
			t.Fatal("explicit none inherited modules")
		}
		if *child.ToolsDefinition == *child.HooksDefinition {
			t.Fatal("mixed sources collapsed")
		}
		if tools && *child.HooksDefinition != *root.HooksDefinition || !tools && *child.ToolsDefinition != *root.ToolsDefinition {
			t.Fatal("inherited family changed owner")
		}
		clone := child.Clone()
		clone.ToolsDefinition.ID = "changed"
		if child.ToolsDefinition.ID == "changed" {
			t.Fatal("provenance aliases")
		}
	}
	unowned, err := Resolve(base, DefinitionDocument{}, ConfigPatch{Tools: root.Tools, Hooks: root.Hooks})
	if err != nil || unowned.ToolsDefinition != nil || unowned.HooksDefinition != nil {
		t.Fatal("override invented owner", err)
	}
}

func TestBindingCeilingContractsAndRequiredHooks(t *testing.T) {
	initial := Configuration{Modules: []string{"files", "agents"}, Tools: map[string]ToolDeclaration{"lookup": bindingTool()}, Hooks: map[string]HookDeclaration{"before_tool": {}}}
	candidate := initial.Clone()
	candidate.Modules = []string{}
	candidate.Tools = map[string]ToolDeclaration{}
	if err := UpdateBindings(initial, candidate); err != nil {
		t.Fatal(err)
	}
	if err := UpdateBindings(initial, initial); err != nil {
		t.Fatal("reenable initial ceiling", err)
	}
	for _, mutate := range []func(*Configuration){
		func(c *Configuration) { c.Modules = append(c.Modules, "shell") },
		func(c *Configuration) { c.Tools["new"] = bindingTool() },
		func(c *Configuration) {
			changed := c.Tools["lookup"]
			changed.TimeoutMillis = 1000
			c.Tools["lookup"] = changed
		},
		func(c *Configuration) { c.Hooks = map[string]HookDeclaration{} },
		func(c *Configuration) { c.ToolsDefinition = &DefinitionRef{ID: "different"} },
	} {
		changed := initial.Clone()
		mutate(&changed)
		if !errors.Is(UpdateBindings(initial, changed), ErrInvalid) {
			t.Fatal("binding expansion accepted", changed)
		}
	}
	if !SameContract(json.RawMessage(`{"b":2,"a":1}`), json.RawMessage(`{ "a":1,"b":2 }`)) {
		t.Fatal("equivalent schema changed contract")
	}
	if SameContract(json.RawMessage(`9007199254740993`), json.RawMessage(`9007199254740992`)) {
		t.Fatal("contract number rounded")
	}
}

func TestBindingDeclarationsAndExactInputValidation(t *testing.T) {
	for _, patch := range []ConfigPatch{
		{Modules: []string{"unknown"}},
		{Modules: []string{"files", "files"}},
		{Tools: map[string]ToolDeclaration{"BadName": bindingTool()}},
		{Hooks: map[string]HookDeclaration{"before_tool": {Operations: []string{"shell.dance"}}}},
		{Hooks: map[string]HookDeclaration{"before_tool": {Operations: []string{"files.read", "files.read"}}}},
	} {
		if !errors.Is(patch.Validate(), ErrInvalid) {
			t.Fatal("invalid binding accepted", patch)
		}
	}
	tool := bindingTool()
	if err := tool.ValidateInput(map[string]any{"id": json.Number("9007199254740993")}); err != nil {
		t.Fatal(err)
	}
	for _, arguments := range []map[string]any{nil, {"id": json.Number("9007199254740992")}, {"id": json.Number("9007199254740993"), "extra": true}} {
		if !errors.Is(tool.ValidateInput(arguments), ErrInvalid) {
			t.Fatal("input contract bypassed", arguments)
		}
	}
	resolved, err := Resolve(Configuration{Model: ModelSelection{Provider: "test", Name: "model"}}, DefinitionDocument{}, ConfigPatch{})
	if err != nil || !slices.Contains(resolved.Modules, "files") {
		t.Fatal("omission did not resolve explicit defaults", err)
	}
}
