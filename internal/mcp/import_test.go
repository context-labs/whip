package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/config"
)

// importFixture lays out one server per interesting state across all four
// import sources and returns the project dir. Codex's ahrefs carries a
// literal token so the tests can prove no secret reaches a Candidate.
func importFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	codex := write("codex.toml", `
[mcp_servers.paper]
url = "http://127.0.0.1:29979/mcp"
[mcp_servers.node_repl]
command = "/Applications/ChatGPT.app/Contents/Resources/cua_node/bin/node_repl"
[mcp_servers.computer-use]
command = "./Codex Computer Use.app/Contents/MacOS/SkyComputerUseClient"
args = ["mcp"]
enabled = false
[mcp_servers.chrome]
command = "npx"
args = ["-y", "chrome-devtools-mcp@latest"]
[mcp_servers.ahrefs]
url = "https://api.ahrefs.com/mcp/mcp"
[mcp_servers.ahrefs.http_headers]
Authorization = "secret-token"
`)
	claude := write("claude.json", `{"mcpServers": {
		"exa": {"type": "http", "url": "https://mcp.exa.ai/mcp"},
		"legacy": {"type": "sse", "url": "https://old.example.com/sse"},
		"playwright": {"command": "npx", "args": ["-y", "@playwright/mcp@latest", "--browser", "chrome"]}
	}}`)
	opencode := write("opencode.json", `{"mcp": {
		"figma": {"type": "remote", "url": "https://mcp.figma.com/mcp", "oauth": {}},
		"paper": {"type": "local", "command": ["paper-from-opencode"]},
		"oc-only": {"type": "local", "command": ["uvx", "analytics-mcp"], "environment": {"GOOGLE_APPLICATION_CREDENTIALS": "$CREDS"}}
	}}`)
	write(".mcp.json", `{"mcpServers": {"proj": {"command": "proj-srv"}}}`)
	origOC, origC, origG := OpenCodePaths, CodexPath, ClaudeGlobalPath
	OpenCodePaths = func() []string { return []string{opencode} }
	CodexPath = func() string { return codex }
	ClaudeGlobalPath = func() string { return claude }
	t.Cleanup(func() { OpenCodePaths, CodexPath, ClaudeGlobalPath = origOC, origC, origG })
	return dir
}

func TestCandidatesStatesAndOrder(t *testing.T) {
	dir := importFixture(t)
	native := FromConfigMap(map[string]config.MCPServer{"ahrefs": {URL: "https://api.ahrefs.com/mcp/mcp"}})
	policy := everySource()
	policy.Codex.Exclude = map[string]bool{"node_repl": true}
	cands, errs := Candidates(dir, native, policy)
	if len(errs) != 0 {
		t.Fatalf("unexpected discovery errors: %v", errs)
	}
	want := map[string]struct {
		state  CandidateState
		source string
		hint   string
	}{
		"ahrefs":       {CandidateNative, "codex", "api.ahrefs.com"},
		"chrome":       {CandidateImportable, "codex", "chrome-devtools-mcp"},
		"computer-use": {CandidateDisabled, "codex", "SkyComputerUseClient"},
		"exa":          {CandidateImportable, "claude", "mcp.exa.ai"},
		"figma":        {CandidateUnsupported, "opencode", "mcp.figma.com"},
		"legacy":       {CandidateUnsupported, "claude", "old.example.com"},
		"node_repl":    {CandidateExcluded, "codex", "node_repl"},
		"oc-only":      {CandidateImportable, "opencode", "analytics-mcp"},
		"paper":        {CandidateImportable, "codex", "127.0.0.1"},
		"playwright":   {CandidateImportable, "claude", "@playwright/mcp"},
		"proj":         {CandidateImportable, "project", "proj-srv"},
	}
	if len(cands) != len(want) {
		t.Fatalf("got %d candidates, want %d: %+v", len(cands), len(want), cands)
	}
	for i, c := range cands {
		if i > 0 && cands[i-1].Name >= c.Name {
			t.Errorf("candidates must be sorted by name: %q after %q", c.Name, cands[i-1].Name)
		}
		w, ok := want[c.Name]
		if !ok {
			t.Errorf("unexpected candidate %+v", c)
			continue
		}
		if c.State != w.state || c.Source != w.source || c.BrandHint != w.hint {
			t.Errorf("%s = state %s source %s hint %q, want %s %s %q", c.Name, c.State, c.Source, c.BrandHint, w.state, w.source, w.hint)
		}
		if c.SourcePath == "" {
			t.Errorf("%s has no source path", c.Name)
		}
		if (c.Transport == "http") != c.config.Remote() {
			t.Errorf("%s transport %q disagrees with its config", c.Name, c.Transport)
		}
	}
	if cands[byName(cands, "figma")].Note != SignInNote {
		t.Error("an oauth entry keeps the sign-in note as its reason")
	}
	// The shared name resolved to codex, and its opencode copy is gone.
	if p := cands[byName(cands, "paper")]; p.SourcePath != CodexPath() {
		t.Errorf("paper should come from codex, got %q", p.SourcePath)
	}
	// Nothing exported on a candidate carries a secret.
	blob, _ := json.Marshal(cands)
	if strings.Contains(string(blob), "secret-token") || strings.Contains(string(blob), "$CREDS") {
		t.Fatalf("a candidate leaked a header or env value: %s", blob)
	}

	// Gates are ignored: a source that is off still offers its servers.
	off := policy
	off.Codex.Enabled = false
	cands, _ = Candidates(dir, native, off)
	if c := cands[byName(cands, "chrome")]; c.State != CandidateImportable {
		t.Errorf("a disabled gate must not hide candidates, got %s", c.State)
	}
	// An only-list excludes everything it does not name.
	only := policy
	only.Codex.Only = map[string]bool{"paper": true}
	cands, _ = Candidates(dir, native, only)
	if c := cands[byName(cands, "chrome")]; c.State != CandidateExcluded {
		t.Errorf("a name outside the only list is excluded, got %s", c.State)
	}
	if c := cands[byName(cands, "paper")]; c.State != CandidateImportable {
		t.Errorf("the only-listed name stays importable, got %s", c.State)
	}
}

func byName(cands []Candidate, name string) int {
	for i, c := range cands {
		if c.Name == name {
			return i
		}
	}
	return -1
}

func TestApplyWritesNativeEntries(t *testing.T) {
	dir := importFixture(t)
	native := FromConfigMap(map[string]config.MCPServer{"ahrefs": {URL: "https://api.ahrefs.com/mcp/mcp"}})
	policy := everySource()
	policy.Codex.Exclude = map[string]bool{"node_repl": true}
	cands, _ := Candidates(dir, native, policy)

	cfg := &config.Config{MCPServers: map[string]config.MCPServer{"ahrefs": {URL: "https://api.ahrefs.com/mcp/mcp"}}}
	added, skipped := Apply(cfg, cands, []string{"chrome", "computer-use", "node_repl", "ahrefs", "figma", "ghost"})
	for _, name := range []string{"chrome", "computer-use", "node_repl"} {
		entry, ok := added[name]
		if !ok {
			t.Errorf("%s should be added, got added=%v skipped=%v", name, added, skipped)
			continue
		}
		if entry.Origin != "" || entry.Source != "" {
			t.Errorf("%s kept import provenance: %+v", name, entry)
		}
		if entry.Enabled != nil {
			t.Errorf("%s must import enabled (ticking it is the choice), got %v", name, *entry.Enabled)
		}
		if !FromConfigMap(cfg.MCPServers)[name].Trusted {
			t.Errorf("%s must be trusted once native", name)
		}
	}
	if added["chrome"].Command[1] != "-y" || added["computer-use"].Command[0] != "./Codex Computer Use.app/Contents/MacOS/SkyComputerUseClient" {
		t.Errorf("commands must be copied whole: %+v", added)
	}
	if skipped["ahrefs"] != "already in Whip" || skipped["figma"] != SignInNote || skipped["ghost"] != "not a discovered server" {
		t.Errorf("skip reasons wrong: %v", skipped)
	}
	if len(cfg.MCPServers) != 4 {
		t.Errorf("config should hold the native entry plus three imports, got %v", cfg.MCPServers)
	}
	// A second apply with the same names changes nothing.
	added, skipped = Apply(cfg, cands, []string{"chrome", "computer-use", "node_repl"})
	if len(added) != 0 || len(skipped) != 3 || len(cfg.MCPServers) != 4 {
		t.Errorf("apply must be idempotent, got added=%v skipped=%v", added, skipped)
	}
	// Apply into an empty config allocates the block; a repeated name is one import.
	empty := &config.Config{}
	if added, skipped := Apply(empty, cands, []string{"exa", "exa"}); len(added) != 1 || len(skipped) != 0 || empty.MCPServers["exa"].URL != "https://mcp.exa.ai/mcp" {
		t.Errorf("apply into an empty config failed: added=%v skipped=%v %+v", added, skipped, empty.MCPServers)
	}
}

// TestCandidatesWithoutCWDIgnoreTheProcessDirectory pins the host-level
// case: Settings and a New session tab without a folder send no cwd, and a
// .mcp.json in the daemon's own working directory must not be offered.
func TestCandidatesWithoutCWDIgnoreTheProcessDirectory(t *testing.T) {
	dir := importFixture(t)
	t.Chdir(dir) // dir holds the fixture's .mcp.json with "proj"
	cands, errs := Candidates("", nil, everySource())
	if len(errs) != 0 {
		t.Fatalf("unexpected discovery errors: %v", errs)
	}
	if byName(cands, "proj") >= 0 {
		t.Fatal("a relative .mcp.json must not be read when no cwd is given")
	}
	if f := LoadMergedFiltered("", nil, everySource()); f.Merged["proj"].Command != nil {
		t.Fatal("LoadMergedFiltered must not read the process directory either")
	}
}

func TestBrandHint(t *testing.T) {
	for _, test := range []struct {
		cfg  ServerConfig
		want string
	}{
		{ServerConfig{URL: "https://mcp.linear.app/mcp"}, "mcp.linear.app"},
		{ServerConfig{URL: "http://127.0.0.1:4789/mcp"}, "127.0.0.1"},
		{ServerConfig{Command: []string{"npx", "-y", "mcp-server-gsc"}}, "mcp-server-gsc"},
		{ServerConfig{Command: []string{"npx", "@playwright/mcp@latest", "--browser", "chrome"}}, "@playwright/mcp"},
		{ServerConfig{Command: []string{"uvx", "analytics-mcp"}}, "analytics-mcp"},
		{ServerConfig{Command: []string{"node", "/opt/srv/dist/index.js"}}, "index.js"},
		{ServerConfig{Command: []string{"/Applications/ChatGPT.app/Contents/Resources/cua_node/bin/node_repl"}}, "node_repl"},
		{ServerConfig{Command: []string{"npx"}}, ""},
	} {
		if got := brandHint(test.cfg); got != test.want {
			t.Errorf("brandHint(%v) = %q, want %q", test.cfg, got, test.want)
		}
	}
}
