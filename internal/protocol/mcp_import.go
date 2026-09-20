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
	Name      string `json:"name"`
	Source    string `json:"source"` // codex | claude | project | opencode
	State     string `json:"state"`  // importable | native | disabled | excluded | unsupported
	Note      string `json:"note,omitempty"`
	BrandHint string `json:"brand_hint,omitempty"`
	// BrandKey is the registrable domain behind a remote server, the key for
	// the app's bundled marks and for mcp.brand.icons; empty for local hosts
	// and stdio servers.
	BrandKey string `json:"brand_key,omitempty"`
}

// MCPBrandIconsParams names registrable domains (MCPImportCandidate.BrandKey)
// the app has no bundled mark for.
type MCPBrandIconsParams struct {
	Keys []string `json:"keys"`
}

// MCPBrandIconsResult maps a key to a small data: URI. Keys with no mark, and
// every key when the host has brandIcons off, are absent.
type MCPBrandIconsResult struct {
	Icons map[string]string `json:"icons"`
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
