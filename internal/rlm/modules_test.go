package rlm

import "testing"

func TestModuleRegistryIsCompleteAndClosed(t *testing.T) {
	want := []string{"context", "files", "shell", "browser", "computer", "models", "agents", "messages", "mcp", "state", "artifacts", "schedules", "permissions"}
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
	for _, removed := range []struct{ module, operation string }{{"answer", "submit"}, {"agents", "await"}, {"agents", "steer"}, {"messages", "receive"}} {
		if err := validateModuleOperation(removed.module, removed.operation); err == nil {
			t.Errorf("removed operation %s.%s was accepted", removed.module, removed.operation)
		}
	}
}
