package agentdef

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/rlm"
)

func TestCodingSelectsEveryModuleAndCapability(t *testing.T) {
	coding := Coding()
	if err := coding.Validate(); err != nil {
		t.Fatal(err)
	}
	modules := slices.Clone(coding.Modules)
	slices.Sort(modules)
	var registry []string
	for name := range rlm.Modules() {
		registry = append(registry, name)
	}
	slices.Sort(registry)
	if !slices.Equal(modules, registry) {
		t.Fatalf("coding modules %v do not match the registry %v", modules, registry)
	}
	capabilities := slices.Clone(coding.Capabilities)
	slices.Sort(capabilities)
	known := slices.Clone(Capabilities)
	slices.Sort(known)
	if !slices.Equal(capabilities, known) {
		t.Fatalf("coding capabilities %v do not match %v", capabilities, known)
	}
	if !coding.Surface.AutoTitle || !coding.Surface.GoalLoop || !coding.Instructions.SkillDiscovery || !coding.Instructions.StandingInstructions {
		t.Fatalf("coding surface or discovery disabled: %+v", coding)
	}
}

func TestValidateRejectsUnknownAndRepeatedSelections(t *testing.T) {
	cases := map[string]func(*Definition){
		"unknown module":      func(d *Definition) { d.Modules = append(d.Modules, "telepathy") },
		"repeated module":     func(d *Definition) { d.Modules = append(d.Modules, "files") },
		"unknown capability":  func(d *Definition) { d.Capabilities = append(d.Capabilities, "network") },
		"repeated capability": func(d *Definition) { d.Capabilities = append(d.Capabilities, "read") },
		"no modules":          func(d *Definition) { d.Modules = nil },
		"no id":               func(d *Definition) { d.ID = "" },
		"threshold":           func(d *Definition) { d.Compaction.Threshold = 1 },
		"child widening": func(d *Definition) {
			d.Modules = []string{"files"}
			d.Children = map[string]Child{"w": {Modules: []string{"shell"}}}
		},
		"child report": func(d *Definition) { d.Children = map[string]Child{"w": {Report: "shout"}} },
		"child budget": func(d *Definition) { d.Children = map[string]Child{"w": {Budgets: map[string]int64{"tokens": -1}}} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			definition := Coding()
			mutate(&definition)
			if err := definition.Validate(); err == nil {
				t.Fatalf("expected %s to fail validation", name)
			}
		})
	}
}

func TestChildInheritsAndNarrowsOnly(t *testing.T) {
	parent := Coding()
	parent.Model = ModelDefaults{Model: "parent-model", Provider: "parent-provider", Effort: "high"}
	inherited, err := parent.Child("", ChildOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(inherited.Modules, parent.Modules) || !slices.Equal(inherited.Capabilities, parent.Capabilities) || inherited.Model != parent.Model || !reflect.DeepEqual(inherited.Instructions, parent.Instructions) {
		t.Fatalf("unnamed child did not inherit: %+v", inherited)
	}
	inherited.Modules[0] = "mutated"
	if parent.Modules[0] == "mutated" {
		t.Fatal("child shares the parent's module slice")
	}
	narrowed, err := parent.Child("", ChildOverrides{Capabilities: []string{"read", "read", "mcp"}, Modules: []string{"files", "context"}, Model: ModelDefaults{Effort: "low"}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(narrowed.Capabilities, []string{"read", "mcp"}) || !slices.Equal(narrowed.Modules, []string{"files", "context"}) {
		t.Fatalf("narrowing = %+v", narrowed)
	}
	if narrowed.Model != (ModelDefaults{Model: "parent-model", Provider: "parent-provider", Effort: "low"}) {
		t.Fatalf("model merge = %+v", narrowed.Model)
	}
	if _, err := parent.Child("", ChildOverrides{Capabilities: []string{"network"}}); err == nil || err.Error() != `capability "network" is not available to the parent` {
		t.Fatalf("widening capability error = %v", err)
	}
	if _, err := narrowed.Child("", ChildOverrides{Modules: []string{"shell"}}); err == nil || err.Error() != `module "shell" is not available to the parent` {
		t.Fatalf("widening module error = %v", err)
	}
}

func TestNamedChildAppliesItsDefinitionThenOverrides(t *testing.T) {
	parent := Coding()
	persona := Instructions{Persona: "You review changes."}
	parent.Children = map[string]Child{
		"reviewer": {Instructions: &persona, Modules: []string{"context", "files", "messages"}, Capabilities: []string{"read"}, Model: ModelDefaults{Effort: "medium"}, Report: "message"},
	}
	if err := parent.Validate(); err != nil {
		t.Fatal(err)
	}
	child, err := parent.Child("reviewer", ChildOverrides{Modules: []string{"files"}})
	if err != nil {
		t.Fatal(err)
	}
	if child.ID != "coding/reviewer" || child.Instructions.Persona != persona.Persona || child.Instructions.Rules != "" {
		t.Fatalf("named child instructions = %+v", child.Instructions)
	}
	if !slices.Equal(child.Modules, []string{"files"}) || !slices.Equal(child.Capabilities, []string{"read"}) || child.Model.Effort != "medium" || child.Children != nil {
		t.Fatalf("named child = %+v", child)
	}
	if _, err := parent.Child("missing", ChildOverrides{}); err == nil || !strings.Contains(err.Error(), `no child named "missing"`) {
		t.Fatalf("missing child error = %v", err)
	}
	if _, err := parent.Child("reviewer", ChildOverrides{Capabilities: []string{"write"}}); err == nil {
		t.Fatal("override widened a named child")
	}
}

// Hooks are validated like tools: bounded timeouts, an operation filter that
// names real operations once, and a filter only where it applies. Children
// inherit hooks unchanged.
func TestHooksValidateAndInherit(t *testing.T) {
	definition := Coding()
	definition.Tools = []Tool{{Name: "lookup", InputSchema: []byte(`{"type":"object"}`)}}
	definition.Hooks = &Hooks{
		BeforeTool:  &Hook{Operations: []string{"shell.run", "files.write", "tools.lookup", "agents.spawn"}, TimeoutMillis: 1000},
		BeforeSpawn: &Hook{Optional: true},
		TurnStart:   &Hook{TimeoutMillis: MaxHookTimeout.Milliseconds()},
	}
	if err := definition.Validate(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(definition.HookNames(), []string{HookBeforeTool, HookBeforeSpawn, HookTurnStart}) {
		t.Fatalf("hook names = %v", definition.HookNames())
	}
	if definition.Hook(HookBeforeTool).Timeout() != time.Second || definition.Hook(HookBeforeSpawn).Timeout() != DefaultHookTimeout || definition.Hook("nope") != nil {
		t.Fatalf("hook timeouts = %v %v", definition.Hook(HookBeforeTool).Timeout(), definition.Hook(HookBeforeSpawn).Timeout())
	}
	child, err := definition.Child("", ChildOverrides{Capabilities: []string{"read"}})
	if err != nil || !reflect.DeepEqual(child.Hooks, definition.Hooks) {
		t.Fatalf("child hooks = %+v %v", child.Hooks, err)
	}
	cases := map[string]func(*Definition){
		"unknown operation":  func(d *Definition) { d.Hooks.BeforeTool.Operations = []string{"shell.dance"} },
		"unknown module":     func(d *Definition) { d.Hooks.BeforeTool.Operations = []string{"telepathy.read"} },
		"unknown tool":       func(d *Definition) { d.Hooks.BeforeTool.Operations = []string{"tools.missing"} },
		"malformed":          func(d *Definition) { d.Hooks.BeforeTool.Operations = []string{"shell"} },
		"repeated operation": func(d *Definition) { d.Hooks.BeforeTool.Operations = []string{"shell.run", "shell.run"} },
		"timeout ceiling":    func(d *Definition) { d.Hooks.TurnStart.TimeoutMillis = MaxHookTimeout.Milliseconds() + 1 },
		"negative timeout":   func(d *Definition) { d.Hooks.BeforeSpawn.TimeoutMillis = -1 },
		"spawn filter":       func(d *Definition) { d.Hooks.BeforeSpawn.Operations = []string{"agents.spawn"} },
		"turn filter":        func(d *Definition) { d.Hooks.TurnStart.Operations = []string{"shell.run"} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			value := definition
			hooks := *definition.Hooks
			beforeTool, beforeSpawn, turnStart := *hooks.BeforeTool, *hooks.BeforeSpawn, *hooks.TurnStart
			hooks.BeforeTool, hooks.BeforeSpawn, hooks.TurnStart = &beforeTool, &beforeSpawn, &turnStart
			value.Hooks = &hooks
			mutate(&value)
			if err := value.Validate(); err == nil {
				t.Fatalf("expected %s to fail validation", name)
			}
		})
	}
}
