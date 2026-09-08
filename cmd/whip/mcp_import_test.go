package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/mcp"
)

// importFixture writes a healthy whip config plus a codex config with two
// servers, and points CodexPath at the fixture.
func importFixture(t *testing.T, mcpImport string) (wd string) {
	t.Helper()
	whipHome := t.TempDir()
	t.Setenv("WHIP_HOME", whipHome)
	wd = t.TempDir()
	cfgSrc := `{
  "defaultModel": "m1",
  "providers": { "a": { "baseUrl": "https://a", "api": "openai-completions" } },
  "models": { "m1": { "providers": ["a"] } }
  ` + mcpImport + `
}`
	if err := os.WriteFile(filepath.Join(whipHome, "config.json"), []byte(cfgSrc), 0o600); err != nil {
		t.Fatal(err)
	}
	codexFile := filepath.Join(wd, "codex.toml")
	if err := os.WriteFile(codexFile, []byte(
		"[mcp_servers.node_repl]\ncommand = \"/app/bin/node_repl\"\n[mcp_servers.paper]\nurl = \"http://127.0.0.1:29979/mcp\"\n",
	), 0o600); err != nil {
		t.Fatal(err)
	}
	orig := mcp.CodexPath
	mcp.CodexPath = func() string { return codexFile }
	t.Cleanup(func() { mcp.CodexPath = orig })
	// Isolate discovery from the developer's real global ~/.claude.json.
	origG := mcp.ClaudeGlobalPath
	mcp.ClaudeGlobalPath = func() string { return filepath.Join(wd, "absent-claude.json") }
	t.Cleanup(func() { mcp.ClaudeGlobalPath = origG })
	return wd
}

// chdir switches the process into dir for the test (discovery is cwd-based).
func chdir(t *testing.T, dir string) {
	t.Helper()
	t.Chdir(dir)
}

// captureStdout runs fn with os.Stdout redirected and returns what it printed.
// A file avoids filling a pipe before fn returns and the reader starts.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	out, err := os.CreateTemp(t.TempDir(), "stdout-*")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	origOut := os.Stdout
	os.Stdout = out
	defer func() { os.Stdout = origOut }()
	fn()
	if _, err := out.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	printed, err := io.ReadAll(out)
	if err != nil {
		t.Fatal(err)
	}
	return string(printed)
}

func TestMCPImportDryRunWritesNothing(t *testing.T) {
	wd := importFixture(t, "")
	chdir(t, wd)

	var runErr error
	printed := captureStdout(t, func() { runErr = mcpImportCLI([]string{"--dry-run"}) })
	if runErr != nil {
		t.Fatal(runErr)
	}
	if !strings.Contains(printed, "node_repl") || !strings.Contains(printed, "paper") {
		t.Errorf("dry-run should list both imported servers:\n%s", printed)
	}
	// The printed fragment must parse as the entry map.
	start := strings.Index(printed, "{")
	if start < 0 {
		t.Fatalf("no JSON fragment printed:\n%s", printed)
	}
	var fragment map[string]config.MCPServer
	if err := json.Unmarshal([]byte(strings.TrimSpace(printed[start:])), &fragment); err != nil {
		t.Errorf("fragment should parse as mcp entries: %v\n%s", err, printed[start:])
	}
	if fragment["paper"].URL != "http://127.0.0.1:29979/mcp" {
		t.Errorf("fragment lost the url: %+v", fragment["paper"])
	}
	// Nothing written.
	reloaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.MCPServers) != 0 {
		t.Errorf("dry-run must not mutate the config, got %+v", reloaded.MCPServers)
	}
}

func TestMCPImportAppliesAndIsIdempotent(t *testing.T) {
	wd := importFixture(t, `, "mcpImport": { "codex": { "exclude": ["node_repl"] } }`)
	chdir(t, wd)

	if err := mcpImportCLI(nil); err != nil {
		t.Fatal(err)
	}
	reloaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.MCPServers["paper"].URL != "http://127.0.0.1:29979/mcp" {
		t.Errorf("paper should be imported, got %+v", reloaded.MCPServers)
	}
	if _, ok := reloaded.MCPServers["node_repl"]; ok {
		t.Error("blocked servers are never imported")
	}
	// Second run: nothing left to import, config unchanged.
	var runErr error
	printed := captureStdout(t, func() { runErr = mcpImportCLI(nil) })
	if runErr != nil {
		t.Fatal(runErr)
	}
	if !strings.Contains(printed, "nothing to import") {
		t.Errorf("second run should be a no-op, got %q", printed)
	}
	reloaded2, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded2.MCPServers) != 1 {
		t.Errorf("config should hold exactly the imported entry, got %+v", reloaded2.MCPServers)
	}
}
