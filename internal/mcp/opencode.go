package mcp

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"

	"github.com/context-labs/whip/internal/config"
)

// SignInNote marks an imported server whose source says it authenticates
// with OAuth. Whip has no browser sign-in for MCP servers, so discovery turns
// the entry off with this note instead of letting it fail with a 401 on
// every connect; the import screen reads the same note as "unsupported".
const SignInNote = "needs a sign-in Whip can't do yet"

// opencodeFile is the shape of OpenCode's config (opencode.json / .jsonc):
//
//	{"mcp": {"name": {"type": "local", "command": ["npx", "-y", "pkg"],
//	                  "environment": {...}, "enabled": true},
//	         "name2": {"type": "remote", "url": ..., "headers": {...},
//	                   "oauth": {...}}}}
//
// command is argv (not command + args), environment is what claude calls
// env, and oauth (an object, or false to opt out) means the server expects a
// browser sign-in. timeout is ignored.
type opencodeFile struct {
	MCP map[string]opencodeServer `json:"mcp"`
}

type opencodeServer struct {
	Type        string            `json:"type"`
	Command     []string          `json:"command"`
	Environment map[string]string `json:"environment"`
	URL         string            `json:"url"`
	Headers     map[string]string `json:"headers"`
	Enabled     *bool             `json:"enabled"`
	OAuth       any               `json:"oauth"` // absent/null → nil, false → opted out, an object → OAuth
}

// ParseOpenCode normalizes an OpenCode config document into server configs.
// Like ParseClaude and ParseCodex it keeps "$VAR" references verbatim.
func ParseOpenCode(data []byte) (map[string]ServerConfig, error) {
	var f opencodeFile
	if err := config.ParseJSONC(data, &f); err != nil {
		return nil, fmt.Errorf("parse opencode config: %w", err)
	}
	out := make(map[string]ServerConfig, len(f.MCP))
	for name, s := range f.MCP {
		c := ServerConfig{
			Command: s.Command,
			Env:     opencodeReferences(s.Environment),
			URL:     s.URL,
			Headers: opencodeReferences(s.Headers),
			Enabled: s.Enabled,
		}
		switch s.Type {
		case "local", "remote", "":
			// both are our native shapes; "" infers from command/url
		default:
			c.Note = fmt.Sprintf("unknown opencode transport type %q — assumed from command/url fields", s.Type)
		}
		if s.OAuth != nil && s.OAuth != false {
			off := false
			c.Enabled = &off
			c.Note = SignInNote
		}
		out[name] = c
	}
	return out, nil
}

var opencodeEnvRef = regexp.MustCompile(`\{env:([A-Za-z_][A-Za-z0-9_]*)\}`)

// opencodeReferences rewrites OpenCode's "{env:NAME}" placeholders into the
// "${NAME}" references whip resolves at connect time, so an imported secret
// stays a reference. Its "{file:path}" form has no whip equivalent and is
// left as written.
func opencodeReferences(values map[string]string) map[string]string {
	if len(values) == 0 {
		return values
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = opencodeEnvRef.ReplaceAllString(value, "${$1}")
	}
	return out
}

// LoadOpenCode reads and parses one OpenCode config file. A missing file is
// not an error (nil map + os.IsNotExist-satisfying error), like LoadClaude.
func LoadOpenCode(path string) (map[string]ServerConfig, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: reading the caller-named config file is the function's contract
	if err != nil {
		return nil, err
	}
	return ParseOpenCode(data)
}

// OpenCodePaths lists OpenCode's global config files in the order OpenCode
// itself merges them (config.json, opencode.json, opencode.jsonc under
// $XDG_CONFIG_HOME/opencode or ~/.config/opencode); a later file's entry wins
// per name. A variable so tests can point it at fixtures.
var OpenCodePaths = defaultOpenCodePaths

func defaultOpenCodePaths() []string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		dir = filepath.Join(home, ".config")
	}
	dir = filepath.Join(dir, "opencode")
	return []string{
		filepath.Join(dir, "config.json"),
		filepath.Join(dir, "opencode.json"),
		filepath.Join(dir, "opencode.jsonc"),
	}
}

// loadOpenCodeAll merges every OpenCode config file, later files winning, and
// stamps each entry with the file it came from. Read failures other than a
// missing file are reported per path.
func loadOpenCodeAll(errs map[string]error) map[string]ServerConfig {
	out := map[string]ServerConfig{}
	for _, path := range OpenCodePaths() {
		entries, err := LoadOpenCode(path)
		if err != nil {
			if !os.IsNotExist(err) {
				errs[path] = err
			}
			continue
		}
		setSource(entries, path, "opencode")
		maps.Copy(out, entries)
	}
	return out
}
