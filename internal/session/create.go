package session

type CreateSession struct {
	ExecutionEngine string `json:"execution_engine,omitempty"`
	// Definition selects the agent definition; empty means the coding agent.
	// DefinitionRevision is pinned by the daemon for registered definitions and
	// is not accepted from clients.
	Definition         string      `json:"definition,omitempty"`
	DefinitionRevision string      `json:"definition_revision,omitempty"`
	Kind               SessionKind `json:"kind"`
	CWD                string      `json:"cwd"`
	Model              string      `json:"model"`
	Provider           string      `json:"provider"`
	// Effort is "off" or a catalog level. Blank asks the daemon to resolve the
	// definition's default, else the configured default, against the model.
	Effort         string `json:"effort,omitempty"`
	PermissionMode string `json:"permission_mode,omitempty"`
}
