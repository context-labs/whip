package agentdef

// juniorDeveloperRules mention only modules the definition selects.
const juniorDeveloperRules = `Operating rules:
- Keep each change small, and explain what you changed and why in plain language.
- Run the project's tests or build after every change; if you cannot run them, say so.
- Never rewrite history, force-push, delete branches, or remove files you did not create.
- Do not add dependencies or change build, CI, or deployment configuration; ask first.
- When a task is ambiguous or risky, ask the user with user.ask instead of guessing.`

// JuniorDeveloper is a deliberately limited agent: it reads, edits, and runs
// tests inside the project, keeps notes, and asks questions. It cannot
// delegate, reach MCP servers, or drive a browser or the desktop. It exists to
// prove that a definition constrains the runtime, not only the prompt.
func JuniorDeveloper() Definition {
	return Definition{
		ID: "junior-developer",
		Instructions: Instructions{
			Persona:              "You are a junior developer working under review.",
			Rules:                juniorDeveloperRules,
			ProjectFiles:         []string{"CLAUDE.md", "AGENTS.md"},
			StandingInstructions: true,
		},
		Modules:      []string{"context", "files", "shell", "state", "artifacts", "permissions", "user"},
		Capabilities: []string{"read", "write", "shell"},
		Surface:      Surface{AutoTitle: true},
	}
}
