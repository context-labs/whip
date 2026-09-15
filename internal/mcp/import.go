package mcp

import (
	"fmt"
	"maps"
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	"github.com/context-labs/whip/internal/config"
)

// CandidateState says what the import screen (or `whip mcp import`) can do
// with a discovered server.
type CandidateState string

const (
	// CandidateImportable can be copied into native configuration as is.
	CandidateImportable CandidateState = "importable"
	// CandidateNative already has an entry in whip's own config; the native
	// entry wins and there is nothing to import.
	CandidateNative CandidateState = "native"
	// CandidateDisabled is turned off in its source file. It can still be
	// imported; the person's tick is the choice to run it.
	CandidateDisabled CandidateState = "disabled"
	// CandidateExcluded is named by an only/exclude list in mcpImport. It can
	// still be imported after an explicit "include".
	CandidateExcluded CandidateState = "excluded"
	// CandidateUnsupported needs something whip does not have (a browser
	// sign-in, the legacy sse transport). Never imported.
	CandidateUnsupported CandidateState = "unsupported"
)

// Candidate is one server discovered outside native configuration, with
// enough to render a row and nothing that could leak a secret: no command
// line, env or headers cross this type's exported surface.
type Candidate struct {
	Name      string
	Source    string // codex | claude | project | opencode
	State     CandidateState
	Gated     bool   // the source's enabled gate is off: the screen ignores that, the CLI honours it
	Note      string // the source's own reason, when it gave one
	BrandHint string // URL host, or the command's package/binary name
	config    ServerConfig
}

// Candidates lists every discovered server once, resolved by the same source
// precedence as Merge (project over codex over claude over opencode), sorted
// by name. The per-source enabled gates only set Gated: the import screen is
// the explicit path, and a gate decides what runs untrusted at session start.
// Explicit only/exclude lists are honoured as CandidateExcluded. errs names
// sources that could not be read.
func Candidates(cwd string, native map[string]ServerConfig, policy ImportPolicy) ([]Candidate, map[string]error) {
	d := loadSources(cwd)
	byName := map[string]Candidate{}
	for _, s := range d.sources(policy) { // lowest precedence first: a later source overwrites
		for name, cfg := range s.cfgs {
			c := Candidate{Name: name, Source: s.name, Gated: !s.policy.Enabled, Note: cfg.Note, BrandHint: brandHint(cfg), config: cfg}
			_, owned := native[name]
			switch {
			case owned:
				c.State = CandidateNative
			case unsupported(cfg):
				c.State = CandidateUnsupported
			case s.policy.listed(name):
				c.State = CandidateExcluded
			case cfg.Disabled():
				c.State = CandidateDisabled
			default:
				c.State = CandidateImportable
			}
			byName[name] = c
		}
	}
	return slices.SortedFunc(maps.Values(byName), func(a, b Candidate) int { return strings.Compare(a.Name, b.Name) }), d.errs
}

// unsupported reports whether discovery turned the server off because whip
// cannot run it: an OAuth sign-in or the legacy sse transport, each marked by
// its parser with a known note. A plain enabled:false in the source is a
// choice, not a limitation.
func unsupported(cfg ServerConfig) bool {
	return cfg.Disabled() && (cfg.Note == SignInNote || cfg.Note == SSENote)
}

// launcherTokens are argv words that name a runner rather than the server.
var launcherTokens = map[string]bool{
	"npx": true, "bunx": true, "pnpx": true, "uvx": true, "uv": true, "run": true,
	"node": true, "python": true, "python3": true, "deno": true,
}

// brandHint derives a short, secret-free identifier for icon lookup: the URL
// host for remote servers, else the first argv word that is not a launcher or
// a flag, reduced to its package or binary name.
//
// ponytail: good enough for npx/uvx/node launchers and .app bundles; a server
// started through a wrapper script gets the script's name, which is fine.
func brandHint(cfg ServerConfig) string {
	if cfg.Remote() {
		u, err := url.Parse(cfg.URL)
		if err != nil {
			return ""
		}
		return u.Hostname()
	}
	for _, tok := range cfg.Command {
		if launcherTokens[tok] || strings.HasPrefix(tok, "-") {
			continue
		}
		if strings.HasPrefix(tok, "@") { // scoped npm package: keep the scope, drop the version
			if i := strings.LastIndex(tok, "@"); i > 0 {
				tok = tok[:i]
			}
			return tok
		}
		tok = filepath.Base(tok)
		if i := strings.Index(tok, "@"); i > 0 {
			tok = tok[:i]
		}
		return tok
	}
	return ""
}

// Apply copies the named candidates into cfg.MCPServers as native entries:
// import provenance dropped (so they load trusted, like `whip mcp import` has
// always written them) and Enabled cleared, because choosing a server is the
// decision to run it even when its source had it off. A name no source
// defines is an error before anything is written; names already native or
// unsupported come back in skipped with a reason. Returns the entries added.
func Apply(cfg *config.Config, cands []Candidate, names []string) (added map[string]config.MCPServer, skipped map[string]string, err error) {
	byName := make(map[string]Candidate, len(cands))
	for _, c := range cands {
		byName[c.Name] = c
	}
	for _, name := range names {
		if _, ok := byName[name]; !ok {
			return nil, nil, fmt.Errorf("%s is not a discovered MCP server", name)
		}
	}
	added, skipped = map[string]config.MCPServer{}, map[string]string{}
	for _, name := range names {
		if _, done := added[name]; done {
			continue // the same name twice is one import
		}
		c := byName[name]
		_, owned := cfg.MCPServers[name]
		switch {
		case owned || c.State == CandidateNative:
			skipped[name] = "already in Whip"
			continue
		case c.State == CandidateUnsupported:
			skipped[name] = c.Note
			continue
		}
		if cfg.MCPServers == nil {
			cfg.MCPServers = map[string]config.MCPServer{}
		}
		entry := config.MCPServer{
			Command: c.config.Command, Env: c.config.Env, Cwd: c.config.Cwd,
			URL: c.config.URL, Headers: c.config.Headers,
			Note: c.config.Note, StartupTimeout: c.config.StartupTimeout, ToolTimeout: c.config.ToolTimeout,
		}
		cfg.MCPServers[name] = entry
		added[name] = entry
	}
	return added, skipped, nil
}
