package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/mcpconfig"
)

// importFixture writes a healthy whipcode config plus a codex config with two
// servers, and points CodexPath at the fixture.
func importFixture(t *testing.T, excluded bool) (wd, directory string) {
	t.Helper()
	wd = t.TempDir()
	codexFile := filepath.Join(wd, "codex.toml")
	if err := os.WriteFile(codexFile, []byte("[mcp_servers.node_repl]\ncommand = \"/app/bin/node_repl\"\n[mcp_servers.paper]\nurl = \"http://127.0.0.1:29979/mcp\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	original, global := mcp.CodexPath, mcp.ClaudeGlobalPath
	mcp.CodexPath = func() string { return codexFile }
	mcp.ClaudeGlobalPath = func() string { return filepath.Join(wd, "absent-claude.json") }
	t.Cleanup(func() { mcp.CodexPath = original; mcp.ClaudeGlobalPath = global })
	directory = useNativeAuth(t, func(directory string) {
		host := config.Default()
		if excluded {
			host.MCP.Imports.Codex = &mcpconfig.ImportSource{Exclude: []string{"node_repl"}}
		}
		if err := config.Save(directory, host); err != nil {
			t.Fatal(err)
		}
	})
	return wd, directory
}

// chdir switches the process into dir for the test (discovery is cwd-based).
func chdir(t *testing.T, dir string) {
	t.Helper()
	t.Chdir(dir)
}

// captureStdout runs fn with os.Stdout redirected and returns what it printed.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	// A pipe fills before fn returns if the command prints more than its buffer.
	outFile, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer outFile.Close()
	origOut := os.Stdout
	os.Stdout = outFile
	defer func() { os.Stdout = origOut }()
	fn()
	out, err := os.ReadFile(outFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestCaptureStdoutLargeOutput(t *testing.T) {
	want := strings.Repeat("x", 1<<20)
	got := captureStdout(t, func() {
		if _, err := os.Stdout.WriteString(want); err != nil {
			t.Fatal(err)
		}
	})
	if got != want {
		t.Fatalf("captured %d bytes, want %d", len(got), len(want))
	}
}

func TestMCPImportDryRunWritesNothing(t *testing.T) {
	wd, directory := importFixture(t, false)
	chdir(t, wd)

	var runErr error
	printed := captureStdout(t, func() { runErr = mcpImportCLI([]string{"--dry-run"}) })
	if runErr != nil {
		t.Fatal(runErr)
	}
	if !strings.Contains(printed, "node_repl") || !strings.Contains(printed, "paper") {
		t.Errorf("dry-run should list both imported servers:\n%s", printed)
	}
	if strings.Contains(printed, "/app/bin/node_repl") || strings.Contains(printed, "127.0.0.1:29979") {
		t.Fatal("dry run exposed private connection details", printed)
	}
	// Nothing written.
	reloaded, err := config.Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.MCP.Servers) != 0 {
		t.Errorf("dry-run must not mutate the config, got %+v", reloaded.MCP.Servers)
	}
}

func TestMCPImportAppliesAndIsIdempotent(t *testing.T) {
	wd, directory := importFixture(t, true)
	chdir(t, wd)

	if err := mcpImportCLI(nil); err != nil {
		t.Fatal(err)
	}
	reloaded, err := config.Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.MCP.Servers["paper"].URL != "http://127.0.0.1:29979/mcp" {
		t.Errorf("paper should be imported, got %+v", reloaded.MCP.Servers)
	}
	if _, ok := reloaded.MCP.Servers["node_repl"]; ok {
		t.Error("blocked servers are never imported")
	}
	// Materialized entries are native: no import provenance, so the host
	// trusts them like hand-written ones. Import is the trust path.
	if entry := reloaded.MCP.Servers["paper"]; entry.Origin != "" || entry.Source != "" {
		t.Errorf("imported entry kept import provenance: %+v", entry)
	}
	if !mcp.NativeConfigs(reloaded.MCP.Servers, "fixture")["paper"].Trusted {
		t.Error("an imported entry must be trusted once it lives in whip's own config")
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
	reloaded2, err := config.Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded2.MCP.Servers) != 1 {
		t.Errorf("config should hold exactly the imported entry, got %+v", reloaded2.MCP.Servers)
	}
}
