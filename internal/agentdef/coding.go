package agentdef

// codingRules are the operating rules the coding agent adds to the runtime
// guide. The instruction scope block that accompanies project instruction
// discovery lives with the composer, not here.
const codingRules = `Operating rules:
- When the user tags a file with @, inspect the listed path with files.read.
- Bias toward acting on reasonable assumptions. After repeated failures on one blocker, escalate it plainly instead of looping.
- For child collaboration, use messages and the configured report mode; do not assume the parent receives the full child transcript.
- Git hygiene: inspect staged changes for secrets, stage intentional files only, and never force-push.`

// Coding is Whip's first-party coding agent. It selects every host module and
// every capability, discovers project instructions, skills, and standing
// instructions, and leaves model and compaction defaults to the host.
func Coding() Definition {
	return Definition{
		ID: "coding",
		Instructions: Instructions{
			Persona:              "You are an expert coding agent.",
			Rules:                codingRules,
			ProjectFiles:         []string{"CLAUDE.md", "AGENTS.md"},
			SkillDiscovery:       true,
			StandingInstructions: true,
		},
		Modules: []string{
			"context", "files", "shell", "browser", "computer", "models", "agents",
			"messages", "mcp", "state", "artifacts", "schedules", "permissions", "user",
		},
		Capabilities: []string{"read", "write", "shell", "browser", "computer", "mcp"},
		Surface:      Surface{AutoTitle: true, GoalLoop: true},
	}
}
