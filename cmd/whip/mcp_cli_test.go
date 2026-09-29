package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/mcpconfig"
)

func TestMCPCLIAddListRemove(t *testing.T) {
	wd, _ := importFixture(t, false) // codex fixture provides node_repl + paper
	chdir(t, wd)

	// dispatch and argument validation
	if err := mcpCLI(nil, "v"); err == nil {
		t.Error("bare `whipcode mcp` should print usage")
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
	for _, want := range []string{"local", "remote", "paper", "not_started", "codex"} {
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
	useNativeAuth(t, nil)
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
	wd, _ := importFixture(t, true)
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
	mcpHome(t, `, "mcp":{"off":{"command":["false"],"enabled":false}}`)
	if err := mcpCLI([]string{"test"}, "v"); err == nil {
		t.Fatal("missing name accepted")
	}
	if err := mcpTestCLI("nosuch"); err == nil || !strings.Contains(err.Error(), "no mcp server named") {
		t.Fatal(err)
	}
	var err error
	out := captureStdout(t, func() { err = mcpTestCLI("off") })
	if err == nil || !strings.Contains(err.Error(), "disabled") || !strings.Contains(out, "disabled") {
		t.Fatal(err, out)
	}
	out = captureStdout(t, func() { err = mcpCLI([]string{"list"}, "v") })
	if err != nil || !strings.Contains(out, "off") || !strings.Contains(out, "disabled") {
		t.Fatal(err, out)
	}
}

// mcpHome isolates WHIPCODE_HOME (with the given "mcp" block appended to a
// healthy config), an empty working directory, and a missing codex file.
func mcpHome(t *testing.T, block string) string {
	t.Helper()
	t.Chdir(t.TempDir())
	var values struct {
		MCP     map[string]mcpconfig.Server `json:"mcp"`
		Imports mcpconfig.Import            `json:"mcpImport"`
	}
	if err := json.Unmarshal([]byte(`{"fixture":true`+block+`}`), &values); err != nil {
		t.Fatal(err)
	}
	return useNativeAuth(t, func(directory string) {
		host := config.Default()
		host.MCP.Servers = values.MCP
		host.MCP.Imports = values.Imports
		if err := config.Save(directory, host); err != nil {
			t.Fatal(err)
		}
	})
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
	mcpHome(t, `, "mcp":{"broken":{"command":["/definitely-not-present-whip-mcp"],"note":"fixture command unavailable"}}`)
	var err error
	out := captureStdout(t, func() { err = mcpTestCLI("broken") })
	if err == nil || !strings.Contains(err.Error(), "failed") || !strings.Contains(out, "✗ failed") || !strings.Contains(out, "source:") {
		t.Fatal(err, out)
	}
}

// Every subcommand that needs the config reports a broken one instead of
// panicking on a nil config.
func TestMCPCLIUnreadableConfig(t *testing.T) {
	previous := connectNativeRuntime
	connectNativeRuntime = func(context.Context) (*client.Client, error) { return nil, errors.New("host unavailable") }
	t.Cleanup(func() { connectNativeRuntime = previous })

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
	if errOut == "" {
		t.Errorf("a broken project file should be reported on stderr, got %q", errOut)
	}
	if !strings.Contains(out, "ok") {
		t.Errorf("the working servers should still be listed:\n%s", out)
	}
}

// TestMCPServeHelperProcess is not a test: re-executed as a child process it
// is a real `whipcode mcp serve` stdio server for the doctor to talk to.
func TestMCPServeHelperProcess(t *testing.T) {
	if os.Getenv("WHIP_MCP_SERVE_HELPER") != "1" {
		t.Skip("helper process, run only by TestMCPTestCLIReady")
	}
	useNativeAuth(t, nil)
	if err := mcpServe("fixture"); err != nil {
		t.Fatal(err)
	}
}

// The doctor's happy path: connect to a real stdio MCP server (this test
// binary re-executed as `whipcode mcp serve`), report the timing and the tools.
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
	if !strings.Contains(out, "— 10 tools") {
		t.Errorf("doctor should report the complete served-tool count:\n%s", out)
	}
	wantPreview := "  tools: bash, browser_allow_preview_port, browser_attach, browser_detach, browser_list_tabs, …"
	if !strings.Contains(out, wantPreview+"\n") {
		t.Errorf("doctor should list the first five sorted tools and explicit truncation:\n%s", out)
	}
	if strings.Contains(out, "read") || strings.Contains(out, "browser_run") {
		t.Errorf("doctor preview must remain bounded rather than listing all tools:\n%s", out)
	}
}

// When the config can't be written back, add/remove/import all report the
// failure instead of claiming success.
func TestMCPCLISaveFailures(t *testing.T) {
	home := mcpHome(t, `,
  "mcp": { "mine": { "command": ["true"] } },
  "mcpImport": { "project": { "enabled": true } }`)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// an importable project server (the project source is opted in above),
	// so import has something to write
	if werr := os.WriteFile(filepath.Join(wd, ".mcp.json"),
		[]byte(`{"mcpServers":{"proj":{"command":"true"}}}`), 0o600); werr != nil {
		t.Fatal(werr)
	}
	if err := os.WriteFile(filepath.Join(home, config.FileName), []byte("invalid host configuration"), 0o600); err != nil {
		t.Fatal(err)
	}

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
