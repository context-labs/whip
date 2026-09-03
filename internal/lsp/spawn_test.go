package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/capability"
)

// kill0 probes whether pid is alive (signal 0).
func kill0(pid int) error { return syscall.Kill(pid, 0) }

// TestHelperProcess is the real-process fake server: the test binary
// re-execs itself with GO_LSP_FAKE=1 and serves scripted LSP over stdio.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_LSP_FAKE") != "1" {
		return
	}
	// Minimal server: answer initialize, push one diagnostic per didOpen,
	// then serve forever (the parent's Close must SIGKILL us).
	br := bufio.NewReader(os.Stdin)
	write := func(msg rpcMessage) {
		body, _ := json.Marshal(msg)
		fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(body), body)
	}
	for {
		body, err := readFrame(br)
		if err != nil {
			os.Exit(0)
		}
		var msg rpcMessage
		if json.Unmarshal(body, &msg) != nil {
			continue
		}
		switch msg.Method {
		case "initialize":
			write(rpcMessage{ID: msg.ID, Result: json.RawMessage(`{"capabilities":{}}`)})
		case "textDocument/didOpen":
			var p struct {
				TextDocument struct {
					URI     string `json:"uri"`
					Version int    `json:"version"`
				} `json:"textDocument"`
			}
			_ = json.Unmarshal(msg.Params, &p)
			params, _ := json.Marshal(map[string]any{
				"uri":     p.TextDocument.URI,
				"version": p.TextDocument.Version,
				"diagnostics": diagsJSON([]Diagnostic{
					{Line: 1, Col: 1, Severity: SeverityError, Message: "from real process"},
				}),
			})
			write(rpcMessage{Method: "textDocument/publishDiagnostics", Params: params})
		default:
			if len(msg.ID) > 0 {
				write(rpcMessage{ID: msg.ID, Result: json.RawMessage("null")})
			}
		}
	}
}

// fakeExecSpec spawns the test binary as the language server.
func fakeExecSpec() ServerSpec {
	return ServerSpec{
		Command:     []string{os.Args[0], "-test.run=^TestHelperProcess$"},
		Extensions:  []string{".go"},
		RootMarkers: []string{"go.mod"},
		Env:         map[string]string{"GO_LSP_FAKE": "1"},
	}
}

// TestRealProcessLifecycle exercises spawn (exec, Setpgid, initialize
// handshake over real pipes), a live WaitDiagnostics round trip, and Close
// (SIGKILL of the process group, goroutine exit). Covers what pipeManager
// bypasses.
func TestRealProcessLifecycle(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir+"/go.mod", "module x\n")
	writeFile(t, dir+"/main.go", "package main\n")
	processes := capability.NewProcessManager()
	m := NewManager(map[string]ServerSpec{"fake": fakeExecSpec()})
	m.SetProcessOptions(processes, "root", dir, nil)

	before := runtime.NumGoroutine()
	out := m.WaitDiagnostics(context.Background(), dir+"/main.go")
	if out == "" || !strings.Contains(out, "from real process") {
		t.Fatalf("real-process diagnostics missing: %q", out)
	}

	// Capture the child pid, then switch root authority and assert the old
	// server is reaped before a fresh one is launched.
	m.mu.Lock()
	var pid int
	for _, cs := range m.clients {
		if cs.process != nil {
			pid = cs.process.PID()
		}
	}
	m.mu.Unlock()
	if pid == 0 {
		t.Fatal("no spawned client")
	}
	assertDead := func(pid int) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if err := kill0(pid); err != nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("process %d survived scope close", pid)
	}
	m.SetProcessOptions(processes, "root-two", dir, nil)
	assertDead(pid)
	if out := m.WaitDiagnostics(context.Background(), dir+"/main.go"); !strings.Contains(out, "from real process") {
		t.Fatalf("diagnostics after root switch: %q", out)
	}
	m.mu.Lock()
	for _, cs := range m.clients {
		if cs.process != nil {
			pid = cs.process.PID()
		}
	}
	m.mu.Unlock()
	m.Close()
	if err := processes.Close(); err != nil {
		t.Fatal(err)
	}
	assertDead(pid)
	time.Sleep(100 * time.Millisecond)
	if after := runtime.NumGoroutine(); after > before+2 {
		t.Fatalf("goroutines grew across spawn+close: before=%d after=%d", before, after)
	}

	// A second manager reusing the spec spawns fresh (broken entries are
	// per-manager, so this is cheap to assert via Statuses).
	if sts := m.Statuses(); len(sts) != 1 || sts[0].Name != "fake" {
		t.Fatalf("statuses after close: %+v", sts)
	}
}
