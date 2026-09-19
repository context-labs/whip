package mcp

import (
	"encoding/json"
	"slices"
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
	codex := writeFile(t, dir, "codex.toml", `
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
	claude := writeFile(t, dir, "claude.json", `{"mcpServers": {
		"exa": {"type": "http", "url": "https://mcp.exa.ai/mcp"},
		"legacy": {"type": "sse", "url": "https://old.example.com/sse"},
		"playwright": {"command": "npx", "args": ["-y", "@playwright/mcp@latest", "--browser", "chrome"]}
	}}`)
	opencode := writeFile(t, dir, "opencode.json", `{"mcp": {
		"figma": {"type": "remote", "url": "https://mcp.figma.com/mcp", "oauth": {}},
		"paper": {"type": "local", "command": ["paper-from-opencode"]},
		"oc-only": {"type": "local", "command": ["uvx", "analytics-mcp"], "environment": {"GOOGLE_APPLICATION_CREDENTIALS": "$CREDS"}}
	}}`)
	writeFile(t, dir, ".mcp.json", `{"mcpServers": {"proj": {"command": "proj-srv"}}}`)
	stubSources(t, codex, claude, opencode)
	return dir
}

func at(cands []Candidate, name string) Candidate {
	i := slices.IndexFunc(cands, func(c Candidate) bool { return c.Name == name })
	if i < 0 {
		return Candidate{}
	}
	return cands[i]
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
		if c.State != w.state || c.Source != w.source || c.BrandHint != w.hint || c.Gated {
			t.Errorf("%s = state %s source %s hint %q gated %t, want %s %s %q ungated", c.Name, c.State, c.Source, c.BrandHint, c.Gated, w.state, w.source, w.hint)
		}
	}
	if at(cands, "figma").Note != SignInNote || at(cands, "legacy").Note != SSENote {
		t.Error("unsupported entries keep their parser's note as the reason")
	}
	// Nothing exported on a candidate carries a secret.
	blob, _ := json.Marshal(cands)
	if strings.Contains(string(blob), "secret-token") || strings.Contains(string(blob), "$CREDS") {
		t.Fatalf("a candidate leaked a header or env value: %s", blob)
	}

	// A gate that is off still offers its servers, marked gated for the CLI.
	off := policy
	off.Codex.Enabled = false
	cands, _ = Candidates(dir, native, off)
	if c := at(cands, "chrome"); c.State != CandidateImportable || !c.Gated {
		t.Errorf("a disabled gate must not hide candidates, got %+v", c)
	}
	// An only-list excludes everything it does not name.
	only := policy
	only.Codex.Only = map[string]bool{"paper": true}
	cands, _ = Candidates(dir, native, only)
	if at(cands, "chrome").State != CandidateExcluded || at(cands, "paper").State != CandidateImportable {
		t.Errorf("only-list handling wrong: chrome=%s paper=%s", at(cands, "chrome").State, at(cands, "paper").State)
	}
}

func TestApplyWritesNativeEntries(t *testing.T) {
	dir := importFixture(t)
	native := FromConfigMap(map[string]config.MCPServer{"ahrefs": {URL: "https://api.ahrefs.com/mcp/mcp"}})
	policy := everySource()
	policy.Codex.Exclude = map[string]bool{"node_repl": true}
	cands, _ := Candidates(dir, native, policy)

	cfg := &config.Config{MCPServers: map[string]config.MCPServer{"ahrefs": {URL: "https://api.ahrefs.com/mcp/mcp"}}}
	if _, _, err := Apply(cfg, cands, []string{"chrome", "ghost"}); err == nil || !strings.Contains(err.Error(), "ghost") || len(cfg.MCPServers) != 1 {
		t.Fatalf("an unknown name must fail before anything is written, got err=%v config=%v", err, cfg.MCPServers)
	}
	added, skipped, err := Apply(cfg, cands, []string{"chrome", "computer-use", "node_repl", "ahrefs", "figma"})
	if err != nil {
		t.Fatal(err)
	}
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
	if skipped["ahrefs"] != "already in Whip" || skipped["figma"] != SignInNote {
		t.Errorf("skip reasons wrong: %v", skipped)
	}
	if len(cfg.MCPServers) != 4 {
		t.Errorf("config should hold the native entry plus three imports, got %v", cfg.MCPServers)
	}
	// A second apply with the same names changes nothing.
	added, skipped, _ = Apply(cfg, cands, []string{"chrome", "computer-use", "node_repl"})
	if len(added) != 0 || len(skipped) != 3 || len(cfg.MCPServers) != 4 {
		t.Errorf("apply must be idempotent, got added=%v skipped=%v", added, skipped)
	}
	// Apply into an empty config allocates the block; a repeated name is one import.
	empty := &config.Config{}
	if added, skipped, _ := Apply(empty, cands, []string{"exa", "exa"}); len(added) != 1 || len(skipped) != 0 || empty.MCPServers["exa"].URL != "https://mcp.exa.ai/mcp" {
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
	if at(cands, "proj").Name != "" {
		t.Fatal("a relative .mcp.json must not be read when no cwd is given")
	}
	if f := LoadMergedFiltered("", nil, everySource()); f.Merged["proj"].Command != nil {
		t.Fatal("LoadMergedFiltered must not read the process directory either")
	}
}

func TestBrandKey(t *testing.T) {
	for _, test := range []struct {
		cfg  ServerConfig
		want string
	}{
		{ServerConfig{URL: "https://mcp.figma.com/mcp"}, "figma.com"},
		{ServerConfig{URL: "https://mcp.linear.app/mcp"}, "linear.app"},
		{ServerConfig{URL: "https://api.ahrefs.com/mcp/mcp"}, "ahrefs.com"},
		{ServerConfig{URL: "https://MCP.Example.CO.UK./x"}, "example.co.uk"},
		{ServerConfig{URL: "https://team.github.io/mcp"}, "team.github.io"}, // a private suffix: the team is the brand
		{ServerConfig{URL: "http://127.0.0.1:4789/mcp"}, ""},
		{ServerConfig{URL: "http://[::1]:8080/mcp"}, ""},
		{ServerConfig{URL: "http://localhost:3000/mcp"}, ""},
		{ServerConfig{URL: "https://kuzco-4090.tail7524e6.ts.net/mcp"}, ""},
		{ServerConfig{URL: "http://nas.local/mcp"}, ""},
		{ServerConfig{URL: "http://mcp.corp.internal/"}, ""},
		{ServerConfig{URL: "https://com/"}, ""},
		{ServerConfig{URL: "::not a url"}, ""},
		{ServerConfig{Command: []string{"npx", "-y", "@playwright/mcp@latest"}}, ""},
	} {
		if got := brandKey(test.cfg); got != test.want {
			t.Errorf("brandKey(%v) = %q, want %q", test.cfg, got, test.want)
		}
	}
	cands, _ := Candidates(importFixture(t), nil, everySource())
	if at(cands, "exa").BrandKey != "mcp.exa.ai"[4:] || at(cands, "paper").BrandKey != "" || at(cands, "chrome").BrandKey != "" {
		t.Errorf("candidates carry the key: exa=%q paper=%q chrome=%q", at(cands, "exa").BrandKey, at(cands, "paper").BrandKey, at(cands, "chrome").BrandKey)
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
