package lsp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/capability"
)

func TestBoundedFrames(t *testing.T) {
	for name, wire := range map[string]string{
		"oversized":   "Content-Length: 1048577\r\n\r\n",
		"duplicate":   "Content-Length: 1\r\nContent-Length: 1\r\n\r\na",
		"huge header": strings.Repeat("X", maxHeaderBytes) + "\r\n\r\n",
		"negative":    "Content-Length: -1\r\n\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := readFrame(bufio.NewReader(strings.NewReader(wire))); err == nil {
				t.Fatal("unbounded frame accepted")
			}
		})
	}
}

func TestSendCancellationAndJoinedPumps(t *testing.T) {
	serverRead, clientWrite := io.Pipe()
	clientRead, serverWrite := io.Pipe()
	defer serverRead.Close()
	defer serverWrite.Close()
	client := newClient(clientWrite, clientRead, nil)
	for range cap(client.out) + 1 {
		client.notify("test", nil)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := client.request(ctx, "test", nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled request: %v", err)
	}
	client.shutdown()
	client.wait()
}

func TestScopedDiagnosticCacheAndVersion(t *testing.T) {
	f := startFakeServer(t, func(string, int) []push { return nil })
	m := pipeManager(f)
	defer m.Close()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m.SetProcessOptions(nil, "root", dir, nil)
	file := filepath.Join(dir, "main.go")
	writeFile(t, file, "package main")
	key := "gopls\x00/froot"
	m.clients[key].docs[file] = 2
	outside := filepath.Join(t.TempDir(), "private.go")
	writeFile(t, outside, "package secret")
	diag := []Diagnostic{{Line: 1, Col: 1, Severity: SeverityError, Message: "fresh"}}
	m.publish(key, fileURI(outside), 2, diag)
	m.publish(key, "file://remote"+file, 2, diag)
	m.publish(key, fileURI(file), 1, diag)
	if len(m.diags) != 0 {
		t.Fatal("outside/remote/stale diagnostics cached")
	}
	m.publish(key, fileURI(file), 2, diag)
	if m.diags[file][0].Message != "fresh" {
		t.Fatal("fresh diagnostic missing")
	}
	many := make([]Diagnostic, maxPerFile+3)
	for i := range many {
		many[i] = Diagnostic{Severity: SeverityError, Message: strings.Repeat("界", 200)}
	}
	m.publish(key, fileURI(file), 2, many)
	if len(m.diags[file]) != maxPerFile || !m.truncated[file] || !utf8.ValidString(m.diags[file][0].Message) || len(m.diags[file][0].Message) > maxMsgLen+3 {
		t.Fatal("diagnostic bounds not applied")
	}
	for i := range maxDiagnosticFiles + 2 {
		path := filepath.Join(dir, fmt.Sprintf("f%d.go", i))
		writeFile(t, path, "package x")
		m.publish(key, fileURI(path), 0, diag)
	}
	if len(m.diags) != maxDiagnosticFiles || !m.cacheTruncated {
		t.Fatal("cache count not bounded")
	}
}

func TestDocumentAggregateBound(t *testing.T) {
	f := startFakeServer(t, func(string, int) []push { return nil })
	m := pipeManager(f)
	defer m.Close()
	cs := m.clients["gopls\x00/froot"]
	cs.bytes = maxDocumentsBytes
	path := filepath.Join(t.TempDir(), "main.go")
	writeFile(t, path, "package main")
	result, err := m.Diagnostics(t.Context(), path, "package main")
	if err != nil || result.State != "unavailable" || !result.Truncated {
		t.Fatalf("aggregate: %+v %v", result, err)
	}
}

func TestPoolAuthorityLifetimeAndEphemeralIsolation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	writeFile(t, path, "package main")
	processes := capability.NewProcessManager()
	defer processes.Close()
	pool := NewPool(processes)
	defer pool.Close()
	spec := fakeExecSpec()
	values := map[string]Config{"fake": {Command: spec.Command, Extensions: spec.Extensions, RootMarkers: spec.RootMarkers, Env: spec.Env}}
	result, err := pool.Diagnostics(t.Context(), "root", dir, path, "package main", values, false, pool.Generation())
	if err != nil || !strings.Contains(result.Output, "real process") {
		t.Fatalf("ephemeral: %+v %v", result, err)
	}
	if len(pool.active) != 0 || len(pool.slots) != 0 {
		t.Fatal("one-use diagnostics retained process authority")
	}
	generation := pool.Generation()
	result, err = pool.Diagnostics(t.Context(), "root", dir, path, "package main", values, true, generation)
	if err != nil || result.State != "ready" {
		t.Fatalf("retained: %+v %v", result, err)
	}
	if len(pool.active) != 1 || len(pool.slots) != 1 {
		t.Fatal("standing manager missing")
	}
	pool.RetireAll()
	if len(pool.active) != 0 || len(pool.slots) != 0 {
		t.Fatal("retired process survived")
	}
	if _, err = pool.Diagnostics(t.Context(), "root", dir, path, "package main", values, true, generation); err == nil {
		t.Fatal("stale authority started server")
	}
}

func TestCloseJoinsInitializeRace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	writeFile(t, path, "package main")
	marker := filepath.Join(dir, "spawned")
	spec := fakeExecSpec()
	spec.Env["GO_LSP_BLOCK_INITIALIZE"] = marker
	processes := capability.NewProcessManager()
	defer processes.Close()
	m := NewManager(map[string]ServerSpec{"fake": spec})
	m.SetProcessOptions(processes, "root", dir, nil)
	t.Cleanup(m.Close)
	done := make(chan struct{})
	go func() { defer close(done); _, _ = m.Diagnostics(t.Context(), path, "package main") }()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("server did not initialize")
		case <-tick.C:
		}
	}
	m.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close did not join the diagnostic caller")
	}
	if len(m.clients) != 0 || len(m.spawning) != 0 {
		t.Fatal("closed manager published initializing server")
	}
}

func TestConfigIsolationAndBounds(t *testing.T) {
	if err := ValidateConfig(map[string]Config{"custom": {Command: []string{"server"}, Extensions: []string{".x"}, RootMarkers: []string{"../escape"}}}); err == nil {
		t.Fatal("escaping marker accepted")
	}
	original := FromConfigMap(nil)
	original["gopls"].Command[0] = "changed"
	if FromConfigMap(nil)["gopls"].Command[0] != "gopls" {
		t.Fatal("built-in declarations aliased")
	}
}
