package protocol

// MCPImportCandidatesParams scopes discovery to a working directory so the
// repository's .mcp.json can be offered; empty means the user's files only.
type MCPImportCandidatesParams struct {
	CWD string `json:"cwd,omitempty"`
}

// MCPImportCandidate is one server found in another agent's configuration.
// It carries what a row needs and nothing that could leak a secret: no
// command line, environment or headers.
type MCPImportCandidate struct {
	Name       string `json:"name"`
	Source     string `json:"source"` // codex | claude | project | opencode
	SourcePath string `json:"source_path"`
	Transport  string `json:"transport"` // stdio | http
	State      string `json:"state"`     // importable | native | disabled | excluded | unsupported
	Note       string `json:"note,omitempty"`
	BrandHint  string `json:"brand_hint,omitempty"`
}

type MCPImportCandidatesResult struct {
	Candidates []MCPImportCandidate `json:"candidates"`
	// Offered is true once the host's import offer was shown and answered.
	Offered    bool              `json:"offered"`
	ConfigPath string            `json:"config_path"`
	Errors     map[string]string `json:"errors,omitempty"` // unreadable sources, by path
}

// MCPImportApplyParams names the candidates to copy into native configuration.
// An empty Names only records that the offer was seen.
type MCPImportApplyParams struct {
	CWD   string   `json:"cwd,omitempty"`
	Names []string `json:"names"`
}

type MCPImportApplyResult struct {
	Imported []string          `json:"imported"`
	Skipped  map[string]string `json:"skipped,omitempty"`
}
