package session

import "github.com/context-labs/whip/internal/hostmodule"

// Builtins uses the same registration and revision path as user documents.
// Returned documents do not share mutable collections across calls.
func Builtins() []DefinitionDocument {
	return []DefinitionDocument{{
		ID:   "assistant",
		Name: "Assistant",
		Defaults: ConfigPatch{Modules: hostmodule.Names(), AutomaticTitle: new(true), GoalsEnabled: new(true), Instructions: &Instructions{
			Text:           "Help the user complete their task. Use only the operations made available to you.",
			ProjectFiles:   []string{"AGENTS.md"},
			DiscoverSkills: true,
		}},
	}, codingDefinition(), juniorDeveloperDefinition()}
}

const codingRules = `Operating rules:
- When the user tags a file with @, inspect the listed path with files.read.
- Bias toward acting on reasonable assumptions. After repeated failures on one blocker, escalate it plainly instead of looping.
- For child collaboration, use messages and the configured report mode; do not assume the parent receives the full child transcript.
- Git hygiene: inspect staged changes for secrets, stage intentional files only, and never force-push.`

const juniorDeveloperRules = `Operating rules:
- Keep each change small, and explain what you changed and why in plain language.
- Run the project's tests or build after every change; if you cannot run them, say so.
- Never rewrite history, force-push, delete branches, or remove files you did not create.
- Do not add dependencies or change build, CI, or deployment configuration; ask first.
- When a task is ambiguous or risky, ask the user with user.ask instead of guessing.`

func codingDefinition() DefinitionDocument {
	return DefinitionDocument{ID: "coding", Name: "Coding", Defaults: ConfigPatch{
		Instructions: &Instructions{Text: "You are an expert coding agent.\n\n" + codingRules, ProjectFiles: []string{"CLAUDE.md", "AGENTS.md"}, DiscoverSkills: true, StandingInstructions: true},
		Modules:      hostmodule.Names(), MCPServers: &MCPSelection{All: true, Servers: []string{}}, AutomaticTitle: new(true), GoalsEnabled: new(true),
		Tools: map[string]ToolDeclaration{}, Hooks: map[string]HookDeclaration{}, Children: map[string]DefinitionRef{}, Output: &OutputPolicy{},
	}}
}

func juniorDeveloperDefinition() DefinitionDocument {
	return DefinitionDocument{ID: "junior-developer", Name: "Junior Developer", Defaults: ConfigPatch{
		Instructions: &Instructions{Text: "You are a junior developer working under review.\n\n" + juniorDeveloperRules, ProjectFiles: []string{"CLAUDE.md", "AGENTS.md"}, StandingInstructions: true},
		Modules:      []string{"context", "files", "shell", "state", "artifacts", "permissions", "user"}, MCPServers: &MCPSelection{Servers: []string{}}, AutomaticTitle: new(true), GoalsEnabled: new(false),
		Tools: map[string]ToolDeclaration{}, Hooks: map[string]HookDeclaration{}, Children: map[string]DefinitionRef{}, Output: &OutputPolicy{},
	}}
}
