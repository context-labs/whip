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
	if _, err := RuntimeGuide(EngineStarlark, []string{"telepathy"}, nil, nil, "", nil); err == nil {
		t.Fatal("unknown module accepted")
	}
	files, err := RuntimeGuide(EngineStarlark, []string{"files", "browser"}, nil, nil, "/workspace", nil)
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
	quickjs, err := RuntimeGuide(EngineQuickJS, []string{"messages"}, nil, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(quickjs, "Messaging and delegation") || !strings.Contains(quickjs, `await messages.send({recipient: "..."`) || strings.Contains(quickjs, "Starlark") {
		t.Fatalf("quickjs guide = %s", quickjs)
	}
	prompt, err := SystemPrompt(EngineStarlark, "You are a test agent.", []string{"context"}, nil, nil, "", nil)
	if err != nil || !strings.HasPrefix(prompt, "You are a test agent. Your only tool is rlm_exec") {
		t.Fatalf("system prompt = %q, %v", prompt, err)
	}
}

// Custom tools render one bounded catalog line each, with required keywords
// first, plus one rule line; definitions without tools leave the guide alone.
func TestRuntimeGuideCatalogsCustomTools(t *testing.T) {
	tools := []CustomTool{
		{Name: "lookup_ticket", Description: "Fetch a ticket by id.\n  Returns the ticket record.", InputSchema: []byte(`{"type":"object","properties":{"verbose":{"type":"boolean"},"id":{"type":"string"}},"required":["id"]}`),
			OutputSchema: []byte(`{"type":"object","properties":{"title":{"type":"string"},"id":{"type":"string"}}}`)},
		{Name: "noop", Description: strings.Repeat("long ", 100), InputSchema: []byte(`{"type":"object"}`)},
	}
	starlark, err := RuntimeGuide(EngineStarlark, []string{"context"}, tools, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Custom tools (keyword arguments as listed):\n- tools.lookup_ticket(id=..., verbose=...) -> {id, title}: Fetch a ticket by id. Returns the ticket record.\n- tools.noop(): long long",
		"...\n\nRules:",
		"\n- tools.<name> calls run outside the runtime",
	} {
		if !strings.Contains(starlark, want) {
			t.Fatalf("starlark guide missing %q:\n%s", want, starlark)
		}
	}
	if line := starlark[strings.Index(starlark, "- tools.noop"):]; len(line[:strings.Index(line, "\n")]) > maxToolDescriptionBytes+32 {
		t.Fatalf("tool description not bounded: %d bytes", len(line[:strings.Index(line, "\n")]))
	}
	quickjs, err := RuntimeGuide(EngineQuickJS, []string{"context"}, tools, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(quickjs, "- tools.lookup_ticket({id, verbose}) -> {id, title}: Fetch a ticket") || !strings.Contains(quickjs, "Pass one object matching the listed schema") {
		t.Fatalf("quickjs guide lacks the tools catalog:\n%s", quickjs)
	}
	plain, err := RuntimeGuide(EngineStarlark, []string{"context"}, nil, nil, "", nil)
	if err != nil || strings.Contains(plain, "tools.") {
		t.Fatalf("guide without tools mentions tools: %v\n%s", err, plain)
	}
}

// An output contract adds one bounded rule line stating the schema; a
// definition without one leaves the guide alone.
func TestRuntimeGuideStatesTheOutputContract(t *testing.T) {
	schema := []byte("{\n  \"type\": \"object\", \"properties\": {\"summary\": {\"type\": \"string\"}}, \"required\": [\"summary\"] }")
	guide, err := RuntimeGuide(EngineStarlark, []string{"context"}, nil, schema, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := `- Output contract: your final assistant message for this turn must be exactly one JSON value matching this schema, with no surrounding prose or code fence: {"properties":{"summary":{"type":"string"}},"required":["summary"],"type":"object"}. A message that does not match is returned to you once for correction; a second mismatch fails the turn.`
	if !strings.Contains(guide, want) {
		t.Fatalf("guide lacks the output contract:\n%s", guide)
	}
	for _, absent := range [][]byte{nil, []byte("null")} {
		plain, err := RuntimeGuide(EngineStarlark, []string{"context"}, nil, absent, "", nil)
		if err != nil || strings.Contains(plain, "Output contract") {
			t.Fatalf("guide without a contract mentions one: %v", err)
		}
	}
	if shape := returnShape([]byte(`{"type":"array"}`)); shape != "array" {
		t.Fatalf("array return shape = %q", shape)
	}
	if shape := returnShape([]byte(`{"type":["string","null"]}`)); shape != "string|null" {
		t.Fatalf("union return shape = %q", shape)
	}
}
