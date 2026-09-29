// Package mcpconfig contains declarations shared by host configuration and MCP discovery.
package mcpconfig

// MCPImport selects which imported MCP server definitions whipcode picks up.
// Three sources: claude (the user's ~/.claude.json), codex (the user's
// ~/.codex/config.toml) and project (the repository's .mcp.json in the
// session cwd). A nil claude or codex entry (or nil Enabled) leaves that
// source on: they are the user's own files. project is off unless enabled,
// because a repository author wrote it and enabling it runs those programs
// at session start. Example:
//
//	"mcpImport": {
//	  "codex": { "enabled": true, "exclude": ["node_repl"] },
//	  "project": { "enabled": true }
//	}
type Import struct {
	Claude  *ImportSource `json:"claude,omitempty"`
	Codex   *ImportSource `json:"codex,omitempty"`
	Project *ImportSource `json:"project,omitempty"`
	// Opencode is the user's ~/.config/opencode files; on unless disabled,
	// like the other user-owned sources.
	Opencode *ImportSource `json:"opencode,omitempty"`
	// Offered records that the import screen was shown on this host and
	// answered (imported or skipped); the app does not offer again by itself.
	Offered bool `json:"offered,omitempty"`
}

// MCPImportSource gates one import source. Enabled nil means on; Only, when
// non-empty, is an allowlist of server names; Exclude is a denylist and wins
// over Only when both are set (documented behavior, no validation error).
type ImportSource struct {
	Enabled *bool    `json:"enabled,omitempty"`
	Only    []string `json:"only,omitempty"`
	Exclude []string `json:"exclude,omitempty"`
}

// MCPServer is the config-file form of an MCP server entry. It mirrors
// mcp.ServerConfig without importing that package (config is a leaf).
type Server struct {
	Origin         string            `json:"origin,omitempty"`
	Source         string            `json:"source,omitempty"`
	Command        []string          `json:"command,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	Cwd            string            `json:"cwd,omitempty"`
	URL            string            `json:"url,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	Enabled        *bool             `json:"enabled,omitempty"`
	Note           string            `json:"note,omitempty"`
	StartupTimeout int               `json:"startupTimeout,omitempty"`
	ToolTimeout    int               `json:"toolTimeout,omitempty"`
}
