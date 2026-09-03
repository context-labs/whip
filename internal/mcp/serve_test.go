package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

// TestServeInProcess drives Serve without a subprocess: Serve's
// StdioTransport is hardwired to os.Stdin/os.Stdout, so the test swaps both
// for pipes and speaks MCP through them with the SDK client. The
// WHIP_TEST_SELFHOST-gated tests cover the real-subprocess path; this one
// keeps Serve covered in plain CI.
func TestServeInProcess(t *testing.T) {
	store, err := session.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rootID, err := store.Create(session.SessionKindAgent, cwd, "mcp-test", "local")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := store.EnsureAuthority(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	services := tools.NewServices()
	if err := services.BindDispatcher(store, store.Workspaces(), store.Processes(), authority); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(services.Close)

	inR, inW, err := os.Pipe() // server stdin
	if err != nil {
		t.Fatal(err)
	}
	outR, outW, err := os.Pipe() // server stdout
	if err != nil {
		t.Fatal(err)
	}
	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = inR, outW

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, "test", services) }()

	// Restore stdio only after Serve has returned, so the swap can't race
	// the server's reads under -race.
	t.Cleanup(func() {
		cancel()
		_ = inW.Close()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("Serve did not return after stdin close + cancel")
		}
		os.Stdin, os.Stdout = oldIn, oldOut
	})

	cli := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0"}, nil)
	cs, err := cli.Connect(ctx, &sdkmcp.IOTransport{Reader: outR, Writer: inW}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = cs.Close() }()

	list, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	names := map[string]bool{}
	for _, tool := range list.Tools {
		names[tool.Name] = true
	}
	if len(list.Tools) != 4 || !names["read"] || names["rlm_exec"] {
		t.Fatalf("served tools = %v (want whip's 4 restricted tools)", names)
	}

	res, err := cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "read",
		Arguments: map[string]any{"path": "serve.go", "limit": 3},
	})
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	txt, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok || !strings.Contains(txt.Text, "package mcp") {
		t.Fatalf("read via MCP = %#v", res.Content)
	}
	if res.IsError {
		t.Fatal("successful tool result marked as an error")
	}

	// Tool errors must come back as tool output, not protocol failures.
	res, err = cs.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "read",
		Arguments: map[string]any{"path": "does-not-exist.xyz"},
	})
	if err != nil {
		t.Fatalf("failing tool call should not be a protocol error: %v", err)
	}
	txt, ok = res.Content[0].(*sdkmcp.TextContent)
	if !ok || !strings.HasPrefix(txt.Text, "Error: ") {
		t.Fatalf("tool error surfaced as %#v", res.Content)
	}
	if !res.IsError {
		t.Fatal("failed tool result was not marked as an error")
	}
}
