// Package mcp implements whip's Model Context Protocol support: a client that
// connects to configured MCP servers (stdio and streamable HTTP) and exposes
// their tools to the agent loop, plus a server (`whip mcp serve`) exposing
// whip's own tools.
//
// Configuration is backwards compatible with claude-style (.mcp.json project
// files) and codex-style (~/.codex/config.toml [mcp_servers]) formats; both
// are normalized into ServerConfig and merged with whip's own "mcp" block in
// ~/.whip/config.json, which always wins on name conflicts.
package mcp

import (
	"fmt"
	"hash/fnv"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/config"
)

// ServerConfig is whip's normalized MCP server definition. Claude-style
// (type: stdio/http/sse, command+args+env, url+headers) and codex-style
// (command/args/headers/startup_timeout_sec) entries both parse into this
// shape. A server is stdio when Command is set, remote when URL is set.
type ServerConfig struct {
	// stdio
	Command []string          `json:"command,omitempty"` // argv: program + arguments
	Env     map[string]string `json:"env,omitempty"`     // extra env (layered over whip's own environment)
	Cwd     string            `json:"cwd,omitempty"`     // working directory; "" = whip's cwd
	// remote
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	// common
	Enabled        *bool  `json:"enabled,omitempty"`        // nil = enabled
	Note           string `json:"note,omitempty"`           // surfaced in /mcp status (e.g. unsupported import)
	StartupTimeout int    `json:"startupTimeout,omitempty"` // seconds to connect + list tools (default 30)
	ToolTimeout    int    `json:"toolTimeout,omitempty"`    // seconds per tool call (default 60)

	// Source is the config file this server came from (".mcp.json",
	// "~/.codex/config.toml", "~/.whip/config.json"). Set by discovery for
	// display (a failed server should point at the file to fix); never
	// persisted.
	Source string `json:"-"`
}

// Remote reports whether the server connects over HTTP rather than stdio.
func (c ServerConfig) Remote() bool { return c.URL != "" }

// Disabled reports whether the server is explicitly turned off.
func (c ServerConfig) Disabled() bool { return c.Enabled != nil && !*c.Enabled }

// StartupTimeoutDuration bounds connect + tools/list for the server.
func (c ServerConfig) StartupTimeoutDuration() time.Duration {
	if c.StartupTimeout > 0 {
		return time.Duration(c.StartupTimeout) * time.Second
	}
	return 30 * time.Second
}

// ToolTimeoutDuration bounds one tool call (the model's ctx may cancel sooner).
func (c ServerConfig) ToolTimeoutDuration() time.Duration {
	if c.ToolTimeout > 0 {
		return time.Duration(c.ToolTimeout) * time.Second
	}
	return 60 * time.Second
}

// Valid reports a config error, if any; "" means usable.
func (c ServerConfig) Valid() string {
	switch {
	case c.Remote() && len(c.Command) > 0:
		return "both command and url set"
	case !c.Remote() && len(c.Command) == 0:
		return "neither command nor url set"
	case c.Remote() && c.URL != "" && !strings.HasPrefix(c.URL, "http://") && !strings.HasPrefix(c.URL, "https://"):
		return "url must start with http:// or https://"
	}
	return ""
}

var notNameChar = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// sanitize maps a name to the provider-safe charset, as in opencode's
// mcp/catalog.ts (`[^a-zA-Z0-9_-] → "_"`).
func sanitize(s string) string {
	if s == "" {
		return "_"
	}
	return notNameChar.ReplaceAllString(s, "_")
}

// serverKey derives the unique identifier embedded in tool names for a
// configured server. Names already in the safe charset pass through
// unchanged (keeping tool names short and greppable); names needing
// sanitization — including "__" runs, which would break the ParseToolName
// split — get a short hash of the ORIGINAL name appended, so "a.b" and "a b"
// (which both sanitize to "a_b" — a collision opencode ships) stay distinct.
func serverKey(name string) string {
	if name != "" && notNameChar.FindStringIndex(name) == nil && !strings.Contains(name, "__") {
		return name // already safe and "__"-free
	}
	sum := fnv.New32a()
	_, _ = sum.Write([]byte(name)) // hash.Hash never errors
	return fmt.Sprintf("%s_%08x", strings.ReplaceAll(sanitize(name), "_", "-"), sum.Sum32())
}

// ToolName derives the agent-facing tool name: "mcp__<serverKey>__<tool>" —
// the claude-code convention. Double underscores make the split unambiguous:
// server keys and sanitized tool names never contain "__".
func ToolName(server, tool string) string {
	return "mcp__" + serverKey(server) + "__" + sanitize(tool)
}

// ParseToolName splits an agent-facing MCP tool name back into the server's
// key and the (sanitized) tool name. The manager keys servers by serverKey,
// so the returned server key identifies the server uniquely. ok is false
// when name is not an MCP tool name.
func ParseToolName(name string) (srvKey, tool string, ok bool) {
	rest, found := strings.CutPrefix(name, "mcp__")
	if !found {
		return "", "", false
	}
	srvKey, tool, found = strings.Cut(rest, "__")
	if !found || tool == "" {
		return "", "", false
	}
	return srvKey, tool, true
}

// Merge combines server configs by name, lowest precedence first: the global
// claude config (~/.claude.json), then the project .mcp.json, then codex, and
// whip's own config always wins whole-entry. No field-level merging —
// predictable, and matches how claude/codex treat their own scopes (more
// specific scope beats more general, user-scoped whip beats every import).
func Merge(whip, codex, claude, claudeGlobal map[string]ServerConfig) map[string]ServerConfig {
	out := make(map[string]ServerConfig, len(whip)+len(codex)+len(claude)+len(claudeGlobal))
	maps.Copy(out, claudeGlobal)
	maps.Copy(out, claude)
	maps.Copy(out, codex)
	maps.Copy(out, whip)
	return out
}

// ImportPolicy selects which imported (non-whip) server definitions whip
// picks up. The zero value imports both sources with no name filtering —
// the pre-gating behavior. ImportPolicyFrom converts the config-file block.
type ImportPolicy struct {
	Claude ImportSourcePolicy
	Codex  ImportSourcePolicy
}

// ImportSourcePolicy gates one import source. Enabled=false drops the source
// entirely; Only (non-empty) is a name allowlist; Exclude is a denylist and
// wins over Only when both are set.
type ImportSourcePolicy struct {
	Enabled bool
	Only    map[string]bool
	Exclude map[string]bool
}

// ImportPolicyFrom converts the config-file mcpImport block into the merge
// policy. imp may be nil (import everything).
func ImportPolicyFrom(imp *config.MCPImport) ImportPolicy {
	convert := func(s *config.MCPImportSource) ImportSourcePolicy {
		p := ImportSourcePolicy{Enabled: true}
		if s == nil {
			return p
		}
		if s.Enabled != nil {
			p.Enabled = *s.Enabled
		}
		if len(s.Only) > 0 {
			p.Only = make(map[string]bool, len(s.Only))
			for _, n := range s.Only {
				p.Only[n] = true
			}
		}
		if len(s.Exclude) > 0 {
			p.Exclude = make(map[string]bool, len(s.Exclude))
			for _, n := range s.Exclude {
				p.Exclude[n] = true
			}
		}
		return p
	}
	if imp == nil {
		return ImportPolicy{Claude: convert(nil), Codex: convert(nil)}
	}
	return ImportPolicy{Claude: convert(imp.Claude), Codex: convert(imp.Codex)}
}

// Admits reports whether a server name passes the source's gate.
func (p ImportSourcePolicy) Admits(name string) bool {
	if !p.Enabled {
		return false
	}
	if p.Exclude[name] {
		return false // denylist wins over allowlist
	}
	if len(p.Only) > 0 && !p.Only[name] {
		return false
	}
	return true
}

// Filtered is the discovery result when an ImportPolicy is applied: Merged is
// what the manager connects to; Blocked holds the servers the policy filtered
// out, forced disabled with a note so they stay visible (/mcp, whip mcp
// list) instead of vanishing silently. Blocked never shadows a whip entry of
// the same name. Sources attributes every discovered name (merged or
// blocked) to the file that contributes/would contribute it ("whip",
// ".mcp.json", or "codex") — codex wins over claude, whip over both. Each
// merged/blocked ServerConfig also carries its Source file path so a failed
// server can point at the file to fix.
type Filtered struct {
	Merged  map[string]ServerConfig
	Blocked map[string]ServerConfig
	Sources map[string]string
	Errs    map[string]error
}

// setSource stamps every entry of src with the file it was discovered from.
func setSource(src map[string]ServerConfig, path string) {
	for name, c := range src {
		c.Source = path
		src[name] = c
	}
}

// LoadMergedFiltered discovers server configs like LoadMerged, then applies
// the import policy: filtered-out claude/codex entries land in Blocked as
// disabled+noted copies. whipCfg entries always pass through.
func LoadMergedFiltered(cwd string, whipCfg map[string]ServerConfig, policy ImportPolicy) Filtered {
	errs := map[string]error{}
	claudeGlobalPath := ClaudeGlobalPath()
	claudeGlobal, err := LoadClaude(claudeGlobalPath)
	if err != nil && !os.IsNotExist(err) {
		errs[claudeGlobalPath] = err
	}
	claudePath := filepath.Join(cwd, ".mcp.json")
	claude, err := LoadClaude(claudePath)
	if err != nil && !os.IsNotExist(err) {
		errs[claudePath] = err
	}
	codexPath := CodexPath()
	codex, err := LoadCodex(codexPath)
	if err != nil && !os.IsNotExist(err) {
		errs[codexPath] = err
	}
	setSource(claudeGlobal, claudeGlobalPath)
	setSource(claude, claudePath)
	setSource(codex, codexPath)
	setSource(whipCfg, whipConfigPath())
	blocked := map[string]ServerConfig{}
	split := func(src map[string]ServerConfig, p ImportSourcePolicy) map[string]ServerConfig {
		kept := make(map[string]ServerConfig, len(src))
		for name, c := range src {
			if !p.Admits(name) {
				if _, owned := whipCfg[name]; !owned { // whip always wins; no ghost row
					off := false
					c.Enabled = &off
					if c.Note != "" { // keep an existing import note (e.g. legacy sse)
						c.Note = "blocked by mcpImport config — " + c.Note
					} else {
						c.Note = "blocked by mcpImport config"
					}
					blocked[name] = c
				}
				continue
			}
			kept[name] = c
		}
		return kept
	}
	claudeGlobalKept := split(claudeGlobal, policy.Claude)
	claudeKept := split(claude, policy.Claude)
	codexKept := split(codex, policy.Codex)
	sources := make(map[string]string, len(whipCfg)+len(codex)+len(claude)+len(claudeGlobal))
	for name := range whipCfg {
		sources[name] = "whip"
	}
	for name := range claudeGlobal { // lowest precedence: project, codex, whip all overwrite
		sources[name] = "~/.claude.json"
	}
	for name := range claude {
		sources[name] = ".mcp.json"
	}
	for name := range codex { // codex wins over claude in Merge
		sources[name] = "codex"
	}
	return Filtered{
		Merged:  Merge(whipCfg, codexKept, claudeKept, claudeGlobalKept),
		Blocked: blocked,
		Sources: sources,
		Errs:    errs,
	}
}

// LoadMerged discovers MCP server configs from all supported sources and
// merges them: the project .mcp.json in cwd (claude-style), the codex config,
// then whip's own config on top. cwd is the project directory; whipCfg may
// be nil. Discovery failures (unreadable/unparseable files) are reported in
// errs, keyed by source path, and never abort the merge. No import policy is
// applied — both sources are imported wholesale.
func LoadMerged(cwd string, whipCfg map[string]ServerConfig) (map[string]ServerConfig, map[string]error) {
	f := LoadMergedFiltered(cwd, whipCfg, ImportPolicyFrom(nil))
	return f.Merged, f.Errs
}

// CodexPath is the codex CLI's config file location (~/.codex/config.toml).
// A variable so tests can point it at fixtures.
var CodexPath = defaultCodexPath

// ClaudeGlobalPath is the claude CLI's global config location
// (~/.claude.json), which carries user-scoped mcpServers alongside the
// project .mcp.json. A variable so tests can point it at fixtures.
var ClaudeGlobalPath = defaultClaudeGlobalPath

// whipConfigPath is whip's own config file location (~/.whip/config.json) —
// the source of any server from the config's "mcp" block. Best-effort: ""
// when the home dir isn't resolvable.
func whipConfigPath() string {
	dir, err := config.Dir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "config.json")
}

// FromConfigMap converts whip's config-file MCP block (identical field
// shape, defined in internal/config to keep that package a leaf) into
// normalized server configs.
func FromConfigMap(in map[string]config.MCPServer) map[string]ServerConfig {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]ServerConfig, len(in))
	for name, c := range in {
		out[name] = ServerConfig{
			Command:        c.Command,
			Env:            c.Env, // secret references stay references;
			Cwd:            c.Cwd,
			URL:            c.URL,
			Headers:        c.Headers, // resolution happens in defaultTransport
			Enabled:        c.Enabled,
			Note:           c.Note,
			StartupTimeout: c.StartupTimeout,
			ToolTimeout:    c.ToolTimeout,
		}
	}
	return out
}

func defaultCodexPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex", "config.toml")
}

func defaultClaudeGlobalPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude.json")
}

// "$VAR"/"${VAR}" secret references in env and header values are NOT expanded
// at parse/load time — they stay references all the way into persisted config
// and resolve at the point of use: headers in defaultTransport's
// headerTransport build, stdio env via config.ResolveEnvMap at spawn. This is
// the property that makes codex/claude imports work when the referenced var
// isn't set in whip's import-time environment (the customer.io "failed to
// auth after importing from codex" report) and keeps resolved secrets out of
// ~/.whip/config.json.
