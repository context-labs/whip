package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/mcp"
)

func TestMCPCLIAddListRemove(t *testing.T) {
	wd := importFixture(t, "") // codex fixture provides node_repl + paper
	chdir(t, wd)

	// dispatch and argument validation
	if err := mcpCLI(nil, "v"); err == nil {
		t.Error("bare `whip mcp` should print usage")
	}
	if err := mcpCLI([]string{"bogus"}, "v"); err == nil {
		t.Error("unknown subcommand should error")
	}
	if err := mcpCLI([]string{"add"}, "v"); err == nil {
		t.Error("add without a name should error")
	}
	if err := mcpCLI([]string{"add", "x", "oops"}, "v"); err == nil {
		t.Error("add without -- or --url should error")
	}
	if err := mcpCLI([]string{"add", "bad", "--url", "ftp://x"}, "v"); err == nil {
		t.Error("a non-http url is an invalid server")
	}

	// add a stdio server and a remote server
	var err error
	out := captureStdout(t, func() { err = mcpCLI([]string{"add", "local", "--", "echo", "hi"}, "v") })
	if err != nil || !strings.Contains(out, `added mcp server "local"`) {
		t.Fatalf("add stdio: %v %q", err, out)
	}
	if err := mcpCLI([]string{"add", "remote", "--url", "http://127.0.0.1:9/mcp"}, "v"); err != nil {
		t.Fatalf("add remote: %v", err)
	}

	// list shows both new servers, the imported ones, and their sources
	out = captureStdout(t, func() { err = mcpCLI([]string{"list"}, "v") })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"local", "echo hi", "remote", "http://127.0.0.1:9/mcp", "paper", "whip config", "codex config"} {
		if !strings.Contains(out, want) {
			t.Errorf("list missing %q:\n%s", want, out)
		}
	}

	// remove: own entry works, imported and unknown names explain themselves
	if err := mcpCLI([]string{"remove", "local"}, "v"); err != nil {
		t.Fatalf("remove own server: %v", err)
	}
	out = captureStdout(t, func() { _ = mcpCLI([]string{"list"}, "v") })
	if strings.Contains(out, "local") {
		t.Errorf("removed server still listed:\n%s", out)
	}
	if err := mcpCLI([]string{"remove", "paper"}, "v"); err == nil || !strings.Contains(err.Error(), "edit that file") {
		t.Errorf("removing an imported server should point at its source file, got %v", err)
	}
	if err := mcpCLI([]string{"remove", "nosuch"}, "v"); err == nil || !strings.Contains(err.Error(), "no mcp server") {
		t.Errorf("removing an unknown server: %v", err)
	}
	if err := mcpCLI([]string{"remove"}, "v"); err == nil {
		t.Error("remove without a name should error")
	}
}

func TestMCPServeStopsCleanlyOnStdinEOF(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)
	useTestDaemon(t)
	t.Chdir(t.TempDir())
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	oldInput := os.Stdin
	os.Stdin = reader
	t.Cleanup(func() {
		os.Stdin = oldInput
		_ = reader.Close()
	})
	if err := mcpServe("test-version"); err != nil {
		t.Fatalf("mcp serve EOF = %v", err)
	}
}

func TestMCPCLIBlockedServer(t *testing.T) {
	wd := importFixture(t, `, "mcpImport": { "codex": { "exclude": ["node_repl"] } }`)
	chdir(t, wd)

	out := captureStdout(t, func() { _ = mcpCLI([]string{"list"}, "v") })
	if !strings.Contains(out, "node_repl") || !strings.Contains(out, "blocked") {
		t.Errorf("list should mark the excluded server blocked:\n%s", out)
	}
	if err := mcpCLI([]string{"remove", "node_repl"}, "v"); err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Errorf("removing a blocked server should explain the policy, got %v", err)
	}
	var err error
	_ = captureStdout(t, func() { err = mcpTestCLI("node_repl") })
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Errorf("testing a blocked server should explain the policy, got %v", err)
	}
}

func TestMCPTestCLIUnknownAndDisabled(t *testing.T) {
	whipHome := t.TempDir()
	t.Setenv("WHIP_HOME", whipHome)
	chdir(t, t.TempDir()) // no .mcp.json in the working directory
	orig := mcp.CodexPath
	mcp.CodexPath = func() string { return filepath.Join(whipHome, "no-codex.toml") }
	t.Cleanup(func() { mcp.CodexPath = orig })

	cfg := `{
  "defaultModel": "m1",
  "providers": { "a": { "baseUrl": "https://a", "api": "openai-completions" } },
  "models": { "m1": { "providers": ["a"] } },
  "mcp": { "off": { "command": ["true"], "enabled": false } }
}`
	if err := os.WriteFile(filepath.Join(whipHome, "config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := mcpCLI([]string{"test"}, "v"); err == nil {
		t.Error("`mcp test` without a name should print usage")
	}
	var err error
	_ = captureStdout(t, func() { err = mcpTestCLI("nosuch") })
	if err == nil || !strings.Contains(err.Error(), "no mcp server named") {
		t.Errorf("unknown server: %v", err)
	}
	// a disabled server reports without ever launching the command
	out := captureStdout(t, func() { err = mcpTestCLI("off") })
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Errorf("disabled server should error: %v", err)
	}
	if !strings.Contains(out, "disabled") {
		t.Errorf("disabled status not printed:\n%s", out)
	}
	// list marks it disabled too
	out = captureStdout(t, func() { _ = mcpCLI([]string{"list"}, "v") })
	if !strings.Contains(out, "off") || !strings.Contains(out, "disabled") {
		t.Errorf("list should show the disabled server:\n%s", out)
	}
}

// mcpHome isolates WHIP_HOME (with the given "mcp" block appended to a
// healthy config), an empty working directory, and a missing codex file.
func mcpHome(t *testing.T, mcpBlock string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)
	chdir(t, t.TempDir())
	orig := mcp.CodexPath
	mcp.CodexPath = func() string { return filepath.Join(home, "no-codex.toml") }
	t.Cleanup(func() { mcp.CodexPath = orig })
	origG := mcp.ClaudeGlobalPath
	mcp.ClaudeGlobalPath = func() string { return filepath.Join(home, "no-claude.json") }
	t.Cleanup(func() { mcp.ClaudeGlobalPath = origG })

	cfg := `{
  "defaultModel": "m1",
  "providers": { "a": { "baseUrl": "https://a", "api": "openai-completions" } },
  "models": { "m1": { "providers": ["a"] } }` + mcpBlock + `
}`
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

// With nothing configured anywhere, list says so instead of printing nothing.
func TestMCPCLIListEmpty(t *testing.T) {
	mcpHome(t, "")
	var err error
	out := captureStdout(t, func() { err = mcpCLI([]string{"list"}, "v") })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no MCP servers configured") {
		t.Fatalf("empty list should say so, got %q", out)
	}
}

// mcpCLI routes `test` and `import` to their handlers (and validates their
// arguments) rather than falling through to the unknown-subcommand error.
func TestMCPCLIRoutesTestAndImport(t *testing.T) {
	mcpHome(t, "")

	var err error
	_ = captureStdout(t, func() { err = mcpCLI([]string{"test", "nosuch"}, "v") })
	if err == nil || !strings.Contains(err.Error(), "no mcp server named") {
		t.Errorf("`mcp test <name>` should reach the doctor, got %v", err)
	}

	out := captureStdout(t, func() { err = mcpCLI([]string{"import", "--dry-run"}, "v") })
	if err != nil || !strings.Contains(out, "nothing to import") {
		t.Errorf("`mcp import --dry-run` should reach the importer, got %v %q", err, out)
	}
	if err := mcpImportCLI([]string{"--bogus"}); err == nil {
		t.Error("an unknown import flag should error")
	}
}

// A server that is neither stdio nor remote fails at birth: the doctor
// reports the failure with its note and the file to fix, without launching
// anything or waiting for a connect timeout.
func TestMCPTestCLIInvalidServerFails(t *testing.T) {
	mcpHome(t, `,
  "mcp": { "broken": { "note": "imported without a command" } }`)

	var err error
	out := captureStdout(t, func() { err = mcpTestCLI("broken") })
	if err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("an invalid server should fail, got %v", err)
	}
	for _, want := range []string{"✗ failed", "neither command nor url", "note: imported without a command", "config:"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor output missing %q:\n%s", want, out)
		}
	}
}

// Every subcommand that needs the config reports a broken one instead of
// panicking on a nil config.
func TestMCPCLIUnreadableConfig(t *testing.T) {
	unusableHome(t)
	chdir(t, t.TempDir())

	if err := mcpCLI([]string{"list"}, "v"); err == nil {
		t.Error("list with an unreadable config should error")
	}
	if err := mcpTestCLI("any"); err == nil {
		t.Error("test with an unreadable config should error")
	}
	if err := mcpImportCLI(nil); err == nil {
		t.Error("import with an unreadable config should error")
	}
}

// captureStderr runs fn with os.Stderr redirected and returns what it wrote.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()
	fn()
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// A project .mcp.json that doesn't parse is reported on stderr and never
// aborts the listing of the servers that do parse.
func TestMCPCLIListReportsBrokenProjectFile(t *testing.T) {
	mcpHome(t, `,
  "mcp": { "ok": { "command": ["true"] } }`)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wd, ".mcp.json"), []byte("{ not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out string
	errOut := captureStderr(t, func() {
		out = captureStdout(t, func() {
			if err := mcpCLI([]string{"list"}, "v"); err != nil {
				t.Error(err)
			}
		})
	})
	if !strings.Contains(errOut, ".mcp.json") {
		t.Errorf("a broken project file should be reported on stderr, got %q", errOut)
	}
	if !strings.Contains(out, "ok") {
		t.Errorf("the working servers should still be listed:\n%s", out)
	}
}

// TestMCPServeHelperProcess is not a test: re-executed as a child process it
// is a real `whip mcp serve` stdio server for the doctor to talk to.
func TestMCPServeHelperProcess(t *testing.T) {
	if os.Getenv("WHIP_MCP_SERVE_HELPER") != "1" {
		t.Skip("helper process, run only by TestMCPTestCLIReady")
	}
	useTestDaemon(t)
	if err := mcpCLI([]string{"serve"}, "helper"); err != nil {
		fmt.Fprintln(os.Stderr, "serve:", err)
	}
}

// The doctor's happy path: connect to a real stdio MCP server (this test
// binary re-executed as `whip mcp serve`), report the timing and the tools.
func TestMCPTestCLIReady(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Skip("no test binary path")
	}
	mcpHome(t, fmt.Sprintf(`,
  "mcp": { "self": { "command": [%q, "-test.run=^TestMCPServeHelperProcess$"], "env": { "WHIP_MCP_SERVE_HELPER": "1" }, "startupTimeout": 20 } }`, self))

	out := captureStdout(t, func() {
		if terr := mcpTestCLI("self"); terr != nil {
			t.Errorf("a live server should probe clean: %v", terr)
		}
	})
	if !strings.Contains(out, "✓ connected") {
		t.Fatalf("doctor should report the connection:\n%s", out)
	}
	if !strings.Contains(out, "tools:") || !strings.Contains(out, "read") {
		t.Errorf("doctor should list the served tools:\n%s", out)
	}
}

// When the config can't be written back, add/remove/import all report the
// failure instead of claiming success.
func TestMCPCLISaveFailures(t *testing.T) {
	home := mcpHome(t, `,
  "mcp": { "mine": { "command": ["true"] } }`)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// an importable project server, so import has something to write
	if werr := os.WriteFile(filepath.Join(wd, ".mcp.json"),
		[]byte(`{"mcpServers":{"proj":{"command":"true"}}}`), 0o600); werr != nil {
		t.Fatal(werr)
	}
	freezeHome(t, home)

	if err := mcpCLI([]string{"add", "new", "--", "true"}, "v"); err == nil {
		t.Error("add should report an unwritable config")
	}
	if err := mcpCLI([]string{"remove", "mine"}, "v"); err == nil {
		t.Error("remove should report an unwritable config")
	}
	if err := mcpImportCLI(nil); err == nil {
		t.Error("import should report an unwritable config")
	}
}
