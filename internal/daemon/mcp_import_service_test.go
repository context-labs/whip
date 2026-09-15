package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/protocol"
)

// mcpImportFixture isolates WHIP_HOME with a healthy config, points every
// discovery source at fixture files, and returns the service plus the project
// directory holding a .mcp.json.
func mcpImportFixture(t *testing.T) (*ProviderService, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(`{
  "defaultModel": "m1",
  "providers": { "a": { "baseUrl": "https://a", "api": "openai-completions" } },
  "models": { "m1": { "providers": ["a"] } },
  "mcp": { "ahrefs": { "url": "https://api.ahrefs.com/mcp/mcp" } },
  "mcpImport": { "codex": { "exclude": ["node_repl"] } }
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	write := func(path, body string) string {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	codex := write(filepath.Join(project, "codex.toml"), "[mcp_servers.paper]\nurl = \"http://127.0.0.1:29979/mcp\"\n"+
		"[mcp_servers.node_repl]\ncommand = \"/app/bin/node_repl\"\n"+
		"[mcp_servers.chrome]\ncommand = \"npx\"\nargs = [\"-y\", \"chrome-devtools-mcp@latest\"]\n"+
		"[mcp_servers.ahrefs]\nurl = \"https://api.ahrefs.com/mcp/mcp\"\n[mcp_servers.ahrefs.http_headers]\nAuthorization = \"secret-token\"\n")
	claude := write(filepath.Join(project, "claude.json"), `{"mcpServers": {"exa": {"type": "http", "url": "https://mcp.exa.ai/mcp"}}}`)
	opencode := write(filepath.Join(project, "opencode.json"), `{"mcp": {"figma": {"type": "remote", "url": "https://mcp.figma.com/mcp", "oauth": {}}}}`)
	write(filepath.Join(project, ".mcp.json"), `{"mcpServers": {"proj": {"command": "proj-srv"}}}`)
	origC, origG, origOC := mcp.CodexPath, mcp.ClaudeGlobalPath, mcp.OpenCodePaths
	mcp.CodexPath = func() string { return codex }
	mcp.ClaudeGlobalPath = func() string { return claude }
	mcp.OpenCodePaths = func() []string { return []string{opencode, filepath.Join(project, "opencode.jsonc")} }
	t.Cleanup(func() { mcp.CodexPath, mcp.ClaudeGlobalPath, mcp.OpenCodePaths = origC, origG, origOC })
	service := NewProviderService(t.Context(), "mcp-import")
	t.Cleanup(service.Close)
	return service, project
}

func TestMCPImportCandidatesListsEveryStateWithoutSecrets(t *testing.T) {
	service, project := mcpImportFixture(t)
	result, err := service.MCPImportCandidates(protocol.MCPImportCandidatesParams{CWD: project})
	if err != nil {
		t.Fatal(err)
	}
	if result.Offered {
		t.Error("a fresh host has not been offered the import yet")
	}
	if !strings.HasSuffix(result.ConfigPath, "config.json") {
		t.Errorf("config path = %q", result.ConfigPath)
	}
	states := map[string]string{}
	for _, c := range result.Candidates {
		states[c.Name] = c.State + "/" + c.Source
	}
	want := map[string]string{
		"ahrefs": "native/codex", "chrome": "importable/codex", "node_repl": "excluded/codex",
		"paper": "importable/codex", "exa": "importable/claude", "figma": "unsupported/opencode", "proj": "importable/project",
	}
	for name, state := range want {
		if states[name] != state {
			t.Errorf("%s = %q, want %q", name, states[name], state)
		}
	}
	if len(states) != len(want) {
		t.Errorf("candidates = %v", states)
	}
	// Without a cwd the repository file is not read.
	result, err = service.MCPImportCandidates(protocol.MCPImportCandidatesParams{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range result.Candidates {
		if c.Name == "proj" {
			t.Error("the project source needs an explicit cwd")
		}
	}
	if _, err := service.MCPImportCandidates(protocol.MCPImportCandidatesParams{CWD: "relative/dir"}); err == nil {
		t.Error("a relative cwd must be rejected")
	}
	// A broken source is reported, not fatal.
	if err := os.WriteFile(filepath.Join(project, "opencode.jsonc"), []byte(`{broken`), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err = service.MCPImportCandidates(protocol.MCPImportCandidatesParams{CWD: project})
	if err != nil || len(result.Errors) != 1 {
		t.Fatalf("broken file: result.Errors = %v, err = %v", result.Errors, err)
	}
}

func TestMCPImportApplyWritesNativeEntriesAndRecordsTheOffer(t *testing.T) {
	service, project := mcpImportFixture(t)
	result, err := service.MCPImportApply(protocol.MCPImportApplyParams{CWD: project, Names: []string{"paper", "chrome", "figma", "ahrefs"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(result.Imported, ",") != "chrome,paper" || !result.Offered {
		t.Errorf("result = %+v", result)
	}
	if result.Skipped["figma"] != mcp.SignInNote || result.Skipped["ahrefs"] != "already in Whip" {
		t.Errorf("skipped = %v", result.Skipped)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MCPServers["paper"].URL != "http://127.0.0.1:29979/mcp" || cfg.MCPServers["chrome"].Command[0] != "npx" {
		t.Errorf("imported entries missing: %+v", cfg.MCPServers)
	}
	if entry := cfg.MCPServers["chrome"]; entry.Origin != "" || entry.Source != "" || entry.Enabled != nil {
		t.Errorf("imported entry must be native and enabled: %+v", entry)
	}
	if cfg.MCPImport == nil || !cfg.MCPImport.Offered || len(cfg.MCPImport.Codex.Exclude) != 1 {
		t.Errorf("offered flag or existing import rules wrong: %+v", cfg.MCPImport)
	}
	if len(cfg.Providers) != 1 || cfg.DefaultModel != "m1" {
		t.Error("apply must leave the rest of the config alone")
	}
	// The next listing shows them as native and the offer as answered.
	listed, err := service.MCPImportCandidates(protocol.MCPImportCandidatesParams{CWD: project})
	if err != nil {
		t.Fatal(err)
	}
	if !listed.Offered {
		t.Error("offered should now be true")
	}
	for _, c := range listed.Candidates {
		if (c.Name == "paper" || c.Name == "chrome") && c.State != "native" {
			t.Errorf("%s should be native after import, got %s", c.Name, c.State)
		}
	}
}

func TestMCPImportApplyValidatesBeforeWriting(t *testing.T) {
	service, project := mcpImportFixture(t)
	if _, err := service.MCPImportApply(protocol.MCPImportApplyParams{CWD: project, Names: []string{"paper", "ghost"}}); err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("unknown name should fail the call, got %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.MCPServers["paper"]; ok || (cfg.MCPImport != nil && cfg.MCPImport.Offered) {
		t.Error("a failed apply must write nothing")
	}
	if _, err := service.MCPImportApply(protocol.MCPImportApplyParams{Names: []string{""}}); err == nil {
		t.Error("an empty name must be rejected")
	}
	// Skip for now: no names, offer recorded, config otherwise untouched.
	result, err := service.MCPImportApply(protocol.MCPImportApplyParams{CWD: project})
	if err != nil || len(result.Imported) != 0 || !result.Offered {
		t.Fatalf("skip = %+v, %v", result, err)
	}
	cfg, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.MCPImport.Offered || len(cfg.MCPServers) != 1 {
		t.Errorf("skip should only set offered: %+v %+v", cfg.MCPImport, cfg.MCPServers)
	}
}
