package agentdef

import (
	"slices"
	"testing"
)

func TestRegistryListsEveryDefinition(t *testing.T) {
	if ids := IDs(); !slices.Equal(ids, []string{"coding", "junior-developer"}) {
		t.Fatalf("ids = %v", ids)
	}
	for _, id := range IDs() {
		definition, ok := Lookup(id)
		if !ok || definition.ID != id {
			t.Fatalf("lookup %q = %+v %v", id, definition, ok)
		}
		if err := definition.Validate(); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	if _, ok := Lookup("architect"); ok {
		t.Fatal("unknown definition resolved")
	}
}

func TestJuniorDeveloperIsAStrictSubsetOfCoding(t *testing.T) {
	coding, junior := Coding(), JuniorDeveloper()
	for _, module := range junior.Modules {
		if !slices.Contains(coding.Modules, module) {
			t.Fatalf("junior module %q is not a coding module", module)
		}
	}
	for _, capability := range junior.Capabilities {
		if !slices.Contains(coding.Capabilities, capability) {
			t.Fatalf("junior capability %q is not a coding capability", capability)
		}
	}
	if len(junior.Modules) >= len(coding.Modules) || len(junior.Capabilities) >= len(coding.Capabilities) {
		t.Fatalf("junior is not narrower: %d/%d modules, %d/%d capabilities", len(junior.Modules), len(coding.Modules), len(junior.Capabilities), len(coding.Capabilities))
	}
	for _, excluded := range []string{"browser", "computer", "models", "agents", "messages", "mcp", "schedules"} {
		if slices.Contains(junior.Modules, excluded) {
			t.Fatalf("junior selects %q", excluded)
		}
	}
	if junior.Instructions.SkillDiscovery || junior.Surface.GoalLoop || !junior.Surface.AutoTitle || !junior.Instructions.StandingInstructions {
		t.Fatalf("junior toggles = %+v %+v", junior.Instructions, junior.Surface)
	}
	if junior.Instructions.Persona == coding.Instructions.Persona || junior.Instructions.Rules == coding.Instructions.Rules {
		t.Fatal("junior shares the coding persona or rules")
	}
}

func TestOperationsMapCapabilitiesInCanonicalOrder(t *testing.T) {
	files, shell, mcp := Operations(Coding().Capabilities)
	if !slices.Equal(files, fileOperations) || !slices.Equal(shell, shellOperations) || !mcp {
		t.Fatalf("coding operations = %v %v %v", files, shell, mcp)
	}
	files, shell, mcp = Operations(JuniorDeveloper().Capabilities)
	if !slices.Equal(files, fileOperations) || !slices.Equal(shell, []string{"bash", "shell_start", "workspace_process"}) || mcp {
		t.Fatalf("junior operations = %v %v %v", files, shell, mcp)
	}
	files, shell, mcp = Operations([]string{"browser", "shell", "read", "read"})
	if !slices.Equal(files, []string{"read"}) || !slices.Equal(shell, []string{"bash", "shell_start", "browser_exec", "workspace_process"}) || mcp {
		t.Fatalf("mixed operations = %v %v %v", files, shell, mcp)
	}
	files, shell, mcp = Operations(nil)
	if files != nil || shell != nil || mcp {
		t.Fatalf("empty operations = %v %v %v", files, shell, mcp)
	}
	if files, _, _ := Operations([]string{"write"}); !slices.Equal(files, fileOperations) {
		t.Fatalf("write implies every file operation: %v", files)
	}
}
