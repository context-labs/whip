package rlm

import (
	"slices"
	"strings"
	"testing"
)

func TestGuideDescribesEveryRegisteredModule(t *testing.T) {
	described := ModuleNames()
	slices.Sort(described)
	registered := make([]string, 0, len(moduleRegistry))
	for name := range moduleRegistry {
		registered = append(registered, name)
	}
	slices.Sort(registered)
	if !slices.Equal(described, registered) {
		t.Fatalf("guide describes %v but the registry has %v", described, registered)
	}
}

func TestRuntimeGuideSelectsFragments(t *testing.T) {
	if _, err := RuntimeGuide(EngineStarlark, []string{"telepathy"}, "", nil); err == nil {
		t.Fatal("unknown module accepted")
	}
	files, err := RuntimeGuide(EngineStarlark, []string{"files", "browser"}, "/workspace", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"- files.list(", "- browser.run(...)\n", "Host module operations accept keyword arguments only", "Working directory: /workspace"} {
		if !strings.Contains(files, want) {
			t.Fatalf("guide missing %q:\n%s", want, files)
		}
	}
	for _, unwanted := range []string{"computer.run", "Messaging and delegation", "- Discover MCP tools", "- state.", "- user.ask"} {
		if strings.Contains(files, unwanted) {
			t.Fatalf("guide leaked %q:\n%s", unwanted, files)
		}
	}
	quickjs, err := RuntimeGuide(EngineQuickJS, []string{"messages"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(quickjs, "Messaging and delegation") || !strings.Contains(quickjs, `await messages.send({recipient: "..."`) || strings.Contains(quickjs, "Starlark") {
		t.Fatalf("quickjs guide = %s", quickjs)
	}
	prompt, err := SystemPrompt(EngineStarlark, "You are a test agent.", []string{"context"}, "", nil)
	if err != nil || !strings.HasPrefix(prompt, "You are a test agent. Your only tool is rlm_exec") {
		t.Fatalf("system prompt = %q, %v", prompt, err)
	}
}
