package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
)

// fakeServer is a scripted in-process LSP server on pipes: it parses frames,
// answers initialize, records didOpen/didChange versions, and pushes
// publishDiagnostics per test script. Shared by all package tests; no real
// gopls anywhere.
type fakeServer struct {
	t *testing.T

	clientToServer io.WriteCloser // manager writes here
	serverToClient io.ReadCloser  // manager reads here

	mu       sync.Mutex
	opened   map[string]int // uri -> latest version
	onChange chan string    // receives each touched uri
}

// push describes one scripted publishDiagnostics. Version semantics mirror
// gopls: pushes are stamped with the current document version (Version < 0
// means "unversioned", for servers that omit versions).
type push struct {
	uri     string // "" = echo the uri that triggered the script
	version int    // <0 = unversioned
	diags   []Diagnostic
}

// startFakeServer wires a manager to a scripted server. script, if non-nil,
// is called on every didOpen/didChange with the touched uri and version, and
// returns pushes to emit.
func startFakeServer(t *testing.T, script func(uri string, version int) []push) *fakeServer {
	t.Helper()
	f := &fakeServer{
		t:        t,
		opened:   map[string]int{},
		onChange: make(chan string, 64),
	}
	// Client stdin <- server reads; client stdout -> server writes.
	cStdinR, cStdinW := io.Pipe()
	cStdoutR, cStdoutW := io.Pipe()
	f.clientToServer = cStdinW
	f.serverToClient = cStdoutR
	go f.serve(cStdinR, cStdoutW, script)
	return f
}

// serve is the server-side loop: read frames, answer requests, run the
// script on document sync notifications.
func (f *fakeServer) serve(r io.Reader, w io.Writer, script func(string, int) []push) {
	// Server side reuses the client's frame reader/writer machinery.
	c := &client{
		stdin: w,
		out:   make(chan []byte, 64),
		dead:  make(chan struct{}),
	}
	go c.writeLoop()
	br := bufio.NewReader(r)
	for {
		body, err := readFrame(br)
		if err != nil {
			return
		}
		var msg rpcMessage
		if err := json.Unmarshal(body, &msg); err != nil {
			continue
		}
		switch msg.Method {
		case "initialize":
			c.send(rpcMessage{ID: msg.ID, Result: json.RawMessage(`{"capabilities":{}}`)})
		case "initialized":
			// handshake done; nothing to track
		case "textDocument/didOpen", "textDocument/didChange":
			var p struct {
				TextDocument struct {
					URI     string `json:"uri"`
					Version int    `json:"version"`
					Text    string `json:"text"`
				} `json:"textDocument"`
				ContentChanges []struct {
					Text string `json:"text"`
				} `json:"contentChanges"`
			}
			if err := json.Unmarshal(msg.Params, &p); err != nil {
				continue
			}
			uri, version := p.TextDocument.URI, p.TextDocument.Version
			f.mu.Lock()
			f.opened[uri] = version
			f.mu.Unlock()
			f.onChange <- uri
			if script != nil {
				for _, ps := range script(uri, version) {
					if ps.uri == "" {
						ps.uri = uri
					}
					msg := map[string]any{"uri": ps.uri, "diagnostics": diagsJSON(ps.diags)}
					if ps.version >= 0 {
						msg["version"] = ps.version
					}
					params, _ := json.Marshal(msg)
					c.send(rpcMessage{Method: "textDocument/publishDiagnostics", Params: params})
				}
			}
		default:
			if len(msg.ID) > 0 {
				c.send(rpcMessage{ID: msg.ID, Result: json.RawMessage("null")})
			}
		}
	}
}

// diagsJSON converts test Diagnostics to the wire shape (0-based positions).
func diagsJSON(ds []Diagnostic) []map[string]any {
	out := make([]map[string]any, len(ds))
	for i, d := range ds {
		out[i] = map[string]any{
			"range": map[string]any{
				"start": map[string]any{"line": d.Line - 1, "character": d.Col - 1},
				"end":   map[string]any{"line": d.Line - 1, "character": d.Col},
			},
			"severity": d.Severity,
			"message":  d.Message,
		}
	}
	return out
}

// fakeSpec returns a ServerSpec whose Command makes spawn fail; tests that
// need a live manager attach the fake directly via newPipeManager below.
func fakeSpec() ServerSpec {
	return ServerSpec{Command: []string{"nonexistent-lsp-binary-xyzzy"}, Extensions: []string{".go"}, RootMarkers: []string{"go.mod"}}
}

// pipeManager builds a Manager whose gopls client talks to the fake server
// over pipes, bypassing spawn (which execs real binaries). All files resolve
// to one pinned key so clientFor finds the attached client.
func pipeManager(f *fakeServer) *Manager {
	m := NewManager(map[string]ServerSpec{"gopls": fakeSpec()})
	m.keyer = func(serverID, abs string, markers []string) string { return "/froot" }
	cs := &clientState{root: "/froot", docs: map[string]int{}}
	cs.cli = newClient(f.clientToServer, f.serverToClient, func(uri string, version int, diags []Diagnostic) {
		m.publish("gopls\x00/froot", uri, version, diags)
	})
	m.clients["gopls\x00/froot"] = cs
	return m
}

// --- tests ---

func TestWaitDiagnosticsEditedFile(t *testing.T) {
	f := startFakeServer(t, func(uri string, version int) []push {
		return []push{{version: version, diags: []Diagnostic{
			{Line: 3, Col: 2, Severity: SeverityError, Message: "undefined: foo"},
			{Line: 7, Col: 1, Severity: SeverityWarning, Message: "x declared and not used"},
			{Line: 9, Col: 1, Severity: SeverityHint, Message: "hint dropped"},
		}}}
	})
	m := pipeManager(f)
	defer m.Close()

	dir := t.TempDir()
	writeFile(t, dir+"/main.go", "package main\n")
	out := m.WaitDiagnostics(context.Background(), dir+"/main.go")
	want := "\n\n<diagnostics file=\"" + dir + "/main.go\">\nERROR [3:2] undefined: foo\nWARN [7:1] x declared and not used\n</diagnostics>"
	if out != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

func TestWaitDiagnosticsSiblingErrors(t *testing.T) {
	f := startFakeServer(t, func(uri string, version int) []push {
		return []push{
			{version: version, diags: nil}, // edited file clean
			{uri: strings.TrimSuffix(uri, "main.go") + "other.go", version: -1, diags: []Diagnostic{
				{Line: 42, Col: 9, Severity: SeverityError, Message: "cannot use x (int) as string"},
			}},
		}
	})
	m := pipeManager(f)
	defer m.Close()

	dir := t.TempDir()
	writeFile(t, dir+"/main.go", "package main\n")
	writeFile(t, dir+"/other.go", "package main\n")
	out := m.WaitDiagnostics(context.Background(), dir+"/main.go")
	if want := "<diagnostics file=\"" + dir + "/other.go\">\nERROR [42:9]"; !strings.Contains(out, want) {
		t.Fatalf("missing sibling block; got %q", out)
	}
	if !strings.Contains(out, "errors in file") {
		t.Fatalf("missing sibling note; got %q", out)
	}
}

func TestWaitDiagnosticsDidChangeVersions(t *testing.T) {
	f := startFakeServer(t, func(uri string, version int) []push {
		return []push{{version: version, diags: []Diagnostic{
			{Line: version, Col: 1, Severity: SeverityError, Message: fmt.Sprintf("err v%d", version)},
		}}}
	})
	m := pipeManager(f)
	defer m.Close()

	dir := t.TempDir()
	p := dir + "/main.go"
	writeFile(t, p, "package main\n")
	m.WaitDiagnostics(context.Background(), p)
	writeFile(t, p, "package main\n\n")
	out := m.WaitDiagnostics(context.Background(), p)
	if !strings.Contains(out, "err v2") {
		t.Fatalf("second touch should wait for version 2 diagnostics; got %q", out)
	}
	f.mu.Lock()
	if f.opened[fileURI(p)] != 2 {
		t.Fatalf("server saw version %d, want 2", f.opened[fileURI(p)])
	}
	f.mu.Unlock()
}

func TestWaitDiagnosticsTimeout(t *testing.T) {
	f := startFakeServer(t, func(uri string, version int) []push { return nil }) // never pushes
	m := pipeManager(f)
	defer m.Close()

	dir := t.TempDir()
	writeFile(t, dir+"/main.go", "package main\n")
	start := time.Now()
	out := m.WaitDiagnostics(context.Background(), dir+"/main.go")
	if out != "" {
		t.Fatalf("want empty on timeout, got %q", out)
	}
	if took := time.Since(start); took > diagWait+500*time.Millisecond {
		t.Fatalf("wait overran cap: %s", took)
	}
}

func TestWaitDiagnosticsCancel(t *testing.T) {
	f := startFakeServer(t, func(uri string, version int) []push { return nil })
	m := pipeManager(f)
	defer m.Close()

	dir := t.TempDir()
	writeFile(t, dir+"/main.go", "package main\n")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan string, 1)
	go func() { done <- m.WaitDiagnostics(ctx, dir+"/main.go") }()
	<-f.onChange // ensure the touch landed before cancelling
	cancel()
	select {
	case out := <-done:
		if out != "" {
			t.Fatalf("cancelled wait should return empty, got %q", out)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled wait hung")
	}
}

func TestWaitDiagnosticsUnmatchedFile(t *testing.T) {
	m := NewManager(map[string]ServerSpec{"gopls": fakeSpec()})
	defer m.Close()
	if out := m.WaitDiagnostics(context.Background(), "/tmp/readme.txt"); out != "" {
		t.Fatalf("unmatched extension should be a no-op, got %q", out)
	}
}

func TestSpawnBrokenCached(t *testing.T) {
	m := NewManager(map[string]ServerSpec{"gopls": fakeSpec()})
	defer m.Close()
	dir := t.TempDir()
	writeFile(t, dir+"/go.mod", "module x\n")
	writeFile(t, dir+"/main.go", "package main\n")
	if out := m.WaitDiagnostics(context.Background(), dir+"/main.go"); out != "" {
		t.Fatalf("broken spawn should yield no diagnostics, got %q", out)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.broken) != 1 {
		t.Fatalf("expected one broken entry, got %v", m.broken)
	}
}

func TestFromConfigMapMerge(t *testing.T) {
	disabled := false
	got := FromConfigMap(map[string]config.LSPServer{
		"gopls":  {Enabled: &disabled},
		"custom": {Command: []string{"my-lsp"}, Extensions: []string{".ml"}},
	})
	if _, ok := got["gopls"]; ok {
		t.Fatal("disabled gopls should be removed")
	}
	if got["custom"].Command[0] != "my-lsp" {
		t.Fatalf("custom server missing: %+v", got)
	}
	// extension defaulting: override command only, keep built-in extensions
	got = FromConfigMap(map[string]config.LSPServer{"gopls": {Command: []string{"/opt/gopls"}}})
	if got["gopls"].Extensions[0] != ".go" || got["gopls"].Command[0] != "/opt/gopls" {
		t.Fatalf("merge lost built-in defaults: %+v", got["gopls"])
	}
}

func TestRootWalk(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir+"/go.mod", "module x\n")
	writeFile(t, dir+"/sub/deep/main.go", "package main\n")
	if root := findRoot(dir+"/sub/deep", []string{"go.mod"}); root != dir {
		t.Fatalf("root = %q, want %q", root, dir)
	}
	if root := findRoot(dir+"/sub/deep", []string{"nope.marker"}); root != dir+"/sub/deep" {
		t.Fatalf("fallback root = %q, want file dir", root)
	}
	if root := findRoot(dir+"/sub/deep", []string{"go.mod"}, dir+"/sub"); root != dir+"/sub/deep" {
		t.Fatalf("bounded root escaped workspace: %q", root)
	}
}

// --- helpers ---

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// numGoroutines reports the runtime goroutine count (leak sanity in tests).
func numGoroutines() int { return runtime.NumGoroutine() }
