package mcp

import (
	"encoding/json"
	"fmt"
	"os"
)

// claudeFile is the shape of a claude-style .mcp.json project file:
//
//	{"mcpServers": {"name": {"type": "stdio", "command": ..., "args": [...],
//	                         "env": {...}, "url": ..., "headers": {...}}}}
//
// "type" is optional: entries with a command default to stdio, entries with a
// url default to http. "sse" (legacy server-sent events transport) is
// imported as disabled with a note — the ecosystem moved to streamable HTTP
// and whip doesn't ship the legacy transport.
type claudeFile struct {
	MCPServers map[string]claudeServer `json:"mcpServers"`
}

type claudeServer struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	Cwd     string            `json:"cwd"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Enabled *bool             `json:"enabled"`
	Timeout int               `json:"timeout"` // seconds (claude-code uses ms for MCP_TIMEOUT; the file form is seconds)
}

// ParseClaude normalizes a claude-style .mcp.json document into server
// configs. "$VAR"/"${VAR}" references in env and header values are kept
// VERBATIM (references, not resolved values) — they resolve at connect time
// via config.ResolveSecret, so an import can never bake a missing-at-import
// var into an empty literal or leak a resolved secret into ~/.whip/config.json.
func ParseClaude(data []byte) (map[string]ServerConfig, error) {
	var f claudeFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse .mcp.json: %w", err)
	}
	out := make(map[string]ServerConfig, len(f.MCPServers))
	for name, s := range f.MCPServers {
		c := ServerConfig{
			Env:     s.Env,
			Cwd:     s.Cwd,
			URL:     s.URL,
			Headers: s.Headers,
			Enabled: s.Enabled,
		}
		if s.Command != "" {
			c.Command = append([]string{s.Command}, s.Args...)
		}
		switch s.Type {
		case "sse":
			disabled := false
			c.Enabled = &disabled
			c.Note = "claude sse transport is legacy and unsupported — switch the server to streamable http (type: \"http\")"
		case "http", "streamable-http", "":
			// "" infers from command/url above; both are our native shapes.
		case "stdio":
			// default; nothing to adjust
		default:
			c.Note = fmt.Sprintf("unknown claude transport type %q — assumed from command/url fields", s.Type)
		}
		if s.Timeout > 0 {
			c.StartupTimeout = s.Timeout
			c.ToolTimeout = s.Timeout
		}
		out[name] = c
	}
	return out, nil
}

// LoadClaude reads and parses a claude-style .mcp.json file. A missing file
// is not an error (returns nil map + os.IsNotExist-satisfying error) so
// callers can treat discovery as best-effort.
func LoadClaude(path string) (map[string]ServerConfig, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: reading the caller-named config file is the function's contract
	if err != nil {
		return nil, err
	}
	return ParseClaude(data)
}
