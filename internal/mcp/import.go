package mcp

import (
	"net/url"
	"path/filepath"
	"sort"
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
	Name       string
	Source     string // codex | claude | project | opencode
	SourcePath string
	Transport  string // stdio | http
	State      CandidateState
	Note       string // the source's own reason, when it gave one
	BrandHint  string // URL host, or the command's package/binary name
	config     ServerConfig
}

// Candidates lists every discovered server once, resolved by the same source
// precedence as Merge (project over codex over claude over opencode), sorted
// by name. The per-source enabled gates are ignored on purpose: the import
// screen is the explicit path, and a gate only decides what runs untrusted
// at session start. Explicit only/exclude lists are honoured as
// CandidateExcluded. errs names sources that could not be read.
func Candidates(cwd string, native map[string]ServerConfig, policy ImportPolicy) ([]Candidate, map[string]error) {
	d := loadSources(cwd)
	type origin struct {
		source string
		policy ImportSourcePolicy
		cfgs   map[string]ServerConfig
	}
	// Lowest precedence first so a later source overwrites an earlier one.
	origins := []origin{
		{"opencode", policy.Opencode, d.opencode},
		{"claude", policy.Claude, d.claudeGlobal},
		{"codex", policy.Codex, d.codex},
		{"project", policy.Project, d.project},
	}
	byName := map[string]Candidate{}
	for _, o := range origins {
		for name, cfg := range o.cfgs {
			c := Candidate{
				Name:       name,
				Source:     o.source,
				SourcePath: cfg.Source,
				Transport:  "stdio",
				Note:       cfg.Note,
				BrandHint:  brandHint(cfg),
				config:     cfg,
			}
			if cfg.Remote() {
				c.Transport = "http"
			}
			switch {
			case nativeOwns(native, name):
				c.State = CandidateNative
			case unsupported(cfg):
				c.State = CandidateUnsupported
			case o.policy.listed(name):
				c.State = CandidateExcluded
			case cfg.Disabled():
				c.State = CandidateDisabled
			default:
				c.State = CandidateImportable
			}
			byName[name] = c
		}
	}
	out := make([]Candidate, 0, len(byName))
	for _, c := range byName {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, d.errs
}

// nativeOwns reports whether whip's own config already defines name. Any
// entry in the native map counts; FromConfigMap marks the trusted ones.
func nativeOwns(native map[string]ServerConfig, name string) bool {
	_, ok := native[name]
	return ok
}

// unsupported reports whether discovery turned the server off because whip
// cannot run it: an OAuth sign-in (SignInNote) or the legacy sse transport.
// A plain enabled:false in the source is a choice, not a limitation.
func unsupported(cfg ServerConfig) bool {
	return cfg.Disabled() && (cfg.Note == SignInNote || strings.Contains(cfg.Note, "unsupported"))
}

// listed reports whether an only/exclude list names the server, independent
// of the source's enabled gate.
func (p ImportSourcePolicy) listed(name string) bool {
	if p.Exclude[name] {
		return true
	}
	return len(p.Only) > 0 && !p.Only[name]
}

// For returns the policy of a named source; unknown names get the zero
// policy, which admits nothing.
func (p ImportPolicy) For(source string) ImportSourcePolicy {
	switch source {
	case "claude":
		return p.Claude
	case "codex":
		return p.Codex
	case "project":
		return p.Project
	case "opencode":
		return p.Opencode
	}
	return ImportSourcePolicy{}
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

// SkipUnknown is Apply's reason for a name that no source defines; callers
// that treat it as a validation error can tell it from a legitimate skip.
const SkipUnknown = "not a discovered server"

// Apply copies the named candidates into cfg.MCPServers as native entries:
// import provenance dropped (so they load trusted, like `whip mcp import` has
// always written them) and Enabled cleared, because choosing a server is the
// decision to run it even when its source had it off. Names that are not
// candidates, already native, or unsupported are reported in skipped with a
// reason and never written. Returns the entries it added.
func Apply(cfg *config.Config, cands []Candidate, names []string) (added map[string]config.MCPServer, skipped map[string]string) {
	added, skipped = map[string]config.MCPServer{}, map[string]string{}
	byName := make(map[string]Candidate, len(cands))
	for _, c := range cands {
		byName[c.Name] = c
	}
	for _, name := range names {
		c, ok := byName[name]
		_, owned := cfg.MCPServers[name]
		switch {
		case !ok:
			skipped[name] = SkipUnknown
			continue
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
	return added, skipped
}
