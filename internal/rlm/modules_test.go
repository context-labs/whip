package rlm

import "testing"

func TestModuleRegistryIsCompleteAndClosed(t *testing.T) {
	want := []string{"context", "files", "shell", "models", "agents", "messages", "state", "artifacts", "schedules", "permissions", "answer"}
	modules := Modules()
	for _, name := range want {
		if len(modules[name]) == 0 {
			t.Errorf("module %q is missing", name)
		}
	}
	modules["files"][0] = "corrupt"
	if err := validateModuleOperation("files", "list"); err != nil {
		t.Fatal("Modules returned an aliased registry")
	}
	if err := validateModuleOperation("os", "getenv"); err == nil {
		t.Fatal("ambient module was accepted")
	}
}
