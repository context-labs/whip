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

// mcpImportFixture isolates WHIP_HOME with a healthy config that already
// owns "ahrefs", points discovery at a Codex file with three servers and an
// empty OpenCode slot, and returns the service plus a project directory. The
// state rules themselves are pinned in internal/mcp; these tests cover what
// the service adds on top.
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
	codex := filepath.Join(project, "codex.toml")
	if err := os.WriteFile(codex, []byte("[mcp_servers.paper]\nurl = \"http://127.0.0.1:29979/mcp\"\n"+
		"[mcp_servers.node_repl]\ncommand = \"/app/bin/node_repl\"\n[mcp_servers.ahrefs]\nurl = \"https://api.ahrefs.com/mcp/mcp\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".mcp.json"), []byte(`{"mcpServers": {"proj": {"command": "proj-srv"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	origC, origG, origOC := mcp.CodexPath, mcp.ClaudeGlobalPath, mcp.OpenCodePaths
	mcp.CodexPath = func() string { return codex }
	mcp.ClaudeGlobalPath = func() string { return filepath.Join(project, "absent-claude.json") }
	mcp.OpenCodePaths = func() []string { return []string{filepath.Join(project, "opencode.jsonc")} }
	t.Cleanup(func() { mcp.CodexPath, mcp.ClaudeGlobalPath, mcp.OpenCodePaths = origC, origG, origOC })
	service := NewProviderService(t.Context(), "mcp-import")
	t.Cleanup(service.Close)
	return service, project
}

func TestMCPImportCandidatesReportOfferPathAndUnreadableSources(t *testing.T) {
	service, project := mcpImportFixture(t)
	result, err := service.MCPImportCandidates(protocol.MCPImportCandidatesParams{CWD: project})
	if err != nil {
		t.Fatal(err)
	}
	if result.Offered || !strings.HasSuffix(result.ConfigPath, "config.json") {
		t.Errorf("fresh host: offered=%t path=%q", result.Offered, result.ConfigPath)
	}
	states := map[string]string{}
	for _, c := range result.Candidates {
		states[c.Name] = c.State
	}
	if states["ahrefs"] != "native" || states["paper"] != "importable" || states["node_repl"] != "excluded" || states["proj"] != "importable" {
		t.Errorf("states = %v", states)
	}
	// Without a cwd the repository file is not read.
	result, err = service.MCPImportCandidates(protocol.MCPImportCandidatesParams{})
	if err != nil || len(result.Candidates) != 3 {
		t.Errorf("without cwd: %d candidates, err %v", len(result.Candidates), err)
	}
	if _, err := service.MCPImportCandidates(protocol.MCPImportCandidatesParams{CWD: "relative/dir"}); err == nil {
		t.Error("a relative cwd must be rejected")
	}
	// A broken source is reported by path, not fatal.
	broken := filepath.Join(project, "opencode.jsonc")
	if err := os.WriteFile(broken, []byte(`{broken`), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err = service.MCPImportCandidates(protocol.MCPImportCandidatesParams{CWD: project})
	if err != nil || result.Errors[broken] == "" {
		t.Fatalf("broken file: result.Errors = %v, err = %v", result.Errors, err)
	}
}

func TestMCPImportApplyWritesNativeEntriesAndRecordsTheOffer(t *testing.T) {
	service, project := mcpImportFixture(t)
	result, err := service.MCPImportApply(protocol.MCPImportApplyParams{CWD: project, Names: []string{"paper", "ahrefs"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(result.Imported, ",") != "paper" || result.Skipped["ahrefs"] != "already in Whip" {
		t.Errorf("result = %+v", result)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MCPServers["paper"].URL != "http://127.0.0.1:29979/mcp" || !mcp.FromConfigMap(cfg.MCPServers)["paper"].Trusted {
		t.Errorf("paper should be a native, trusted entry: %+v", cfg.MCPServers)
	}
	if cfg.MCPImport == nil || !cfg.MCPImport.Offered || len(cfg.MCPImport.Codex.Exclude) != 1 || len(cfg.Providers) != 1 {
		t.Errorf("apply must set offered and leave the rest alone: %+v", cfg.MCPImport)
	}
	listed, err := service.MCPImportCandidates(protocol.MCPImportCandidatesParams{CWD: project})
	if err != nil {
		t.Fatal(err)
	}
	if !listed.Offered {
		t.Error("offered should now be true")
	}
	for _, c := range listed.Candidates {
		if c.Name == "paper" && c.State != "native" {
			t.Errorf("paper should be native after import, got %s", c.State)
		}
	}
	if snapshot, err := service.ReadConfiguration(); err != nil || !snapshot.MCPImportOffered {
		t.Errorf("config.get must report the answered offer, got %+v %v", snapshot.MCPImportOffered, err)
	}
}

func TestMCPImportApplyValidatesBeforeWritingAndSkipsQuietly(t *testing.T) {
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
	// Skip for now: no names, offer recorded, config otherwise untouched.
	result, err := service.MCPImportApply(protocol.MCPImportApplyParams{CWD: project})
	if err != nil || len(result.Imported) != 0 {
		t.Fatalf("skip = %+v, %v", result, err)
	}
	if cfg, err = config.Load(); err != nil || !cfg.MCPImport.Offered || len(cfg.MCPServers) != 1 {
		t.Errorf("skip should only set offered: %+v %+v %v", cfg.MCPImport, cfg.MCPServers, err)
	}
	// A second skip changes nothing, so the file is not rewritten.
	path, _ := config.Path()
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.MCPImportApply(protocol.MCPImportApplyParams{CWD: project}); err != nil {
		t.Fatal(err)
	}
	if after, err := os.Stat(path); err != nil || !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() {
		t.Errorf("an unchanged apply must not rewrite config.json (%v)", err)
	}
}
