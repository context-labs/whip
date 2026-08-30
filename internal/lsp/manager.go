package lsp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/config"
)

// diagWait caps how long a write/edit tool call blocks for diagnostics
// (opencode blocks unbounded-ish behind a 3s timeout; we cap at 1.5s and
// return the tool result without diagnostics on timeout).
const diagWait = 1500 * time.Millisecond

// initTimeout bounds the initialize handshake.
const initTimeout = 10 * time.Second

// ServerSpec describes how to match and spawn one language server.
type ServerSpec struct {
	Command     []string          // argv; nil for a disabled entry
	Extensions  []string          // file extensions served, e.g. [".go"]
	RootMarkers []string          // files that mark a project root
	Env         map[string]string // extra env layered over whip's
	Disabled    bool
}

// builtinServers is the shipped registry. Adding a built-in is one row.
var builtinServers = map[string]ServerSpec{
	"gopls": {
		Command:     []string{"gopls"},
		Extensions:  []string{".go"},
		RootMarkers: []string{"go.work", "go.mod", "go.sum"},
	},
}

// FromConfigMap converts the config-file "lsp" block into specs merged over
// the built-ins: a user entry with disabled=true removes the built-in, an
// entry with a command replaces/extends it (extensions/rootMarkers default to
// the built-in's when omitted). Mirrors mcp.FromConfigMap semantics
// (internal/mcp/config.go:169).
func FromConfigMap(in map[string]config.LSPServer) map[string]ServerSpec {
	out := make(map[string]ServerSpec, len(builtinServers)+len(in))
	maps.Copy(out, builtinServers)
	for name, c := range in {
		existing := out[name]
		if c.Enabled != nil && !*c.Enabled {
			delete(out, name)
			continue
		}
		spec := existing
		if len(c.Command) > 0 {
			spec.Command = c.Command
		}
		if len(c.Extensions) > 0 {
			spec.Extensions = c.Extensions
		}
		if len(c.RootMarkers) > 0 {
			spec.RootMarkers = c.RootMarkers
		}
		if len(c.Env) > 0 {
			spec.Env = c.Env
		}
		out[name] = spec
	}
	return out
}

// Status is one row of the /lsp view.
type Status struct {
	Name  string // server id
	Root  string // workspace root once connected
	State string // "connected", "not started", "failed"
	Err   string // failure detail when State == "failed"
}

// Manager owns LSP server processes and the diagnostics cache.
//
// Concurrency (docs/concurrency.md): spawn dedup is a close-to-broadcast
// channel per server key (spawning); diagnostic waiters are channels closed
// by the publish handler, keyed by (path, version) — no per-waiter
// goroutines. mu guards the maps only and is never held across I/O; the
// publish handler runs on the client's read goroutine and only takes mu
// briefly to swap caches/close waiters.
type Manager struct {
	mu         sync.Mutex
	specs      map[string]ServerSpec
	clients    map[string]*clientState // key: id + "\x00" + root
	broken     map[string]string       // key -> error message
	spawning   map[string]chan struct{}
	diags      map[string][]Diagnostic    // abs path -> latest pushed set
	waiters    map[string][]chan struct{} // abs path -> pending wakes
	keyer      spawnKeyer                 // nil = findRoot (production)
	closed     bool
	processes  *capability.ProcessManager
	rootID     string
	workspace  string
	processEnv map[string]string
}

type clientState struct {
	cli     *client
	cmd     *exec.Cmd
	process *capability.Process
	root    string
	docs    map[string]int // abs path -> last sent version
}

func (m *Manager) SetProcessOptions(processes *capability.ProcessManager, rootID, workspace string, env map[string]string) {
	m.mu.Lock()
	var clients []*clientState
	if m.rootID != "" && m.rootID != rootID {
		for _, client := range m.clients {
			clients = append(clients, client)
		}
		m.clients = map[string]*clientState{}
		m.broken = map[string]string{}
		m.diags = map[string][]Diagnostic{}
	}
	m.processes, m.rootID, m.workspace, m.processEnv = processes, rootID, workspace, maps.Clone(env)
	m.mu.Unlock()
	for _, client := range clients {
		client.kill()
	}
}

// spawnKeyer resolves the spawn key (server id + root) for a file; nil =
// findRoot. Tests override it to pin pipe-attached clients to one key.
type spawnKeyer func(serverID, abs string, markers []string) string

// NewManager builds a manager from merged specs (see FromConfigMap).
func NewManager(specs map[string]ServerSpec) *Manager {
	return &Manager{
		specs:    specs,
		clients:  map[string]*clientState{},
		broken:   map[string]string{},
		spawning: map[string]chan struct{}{},
		diags:    map[string][]Diagnostic{},
		waiters:  map[string][]chan struct{}{},
	}
}

// WaitDiagnostics touches path (didOpen/didChange at current disk content),
// waits up to diagWait for the server to push diagnostics for the new
// version, and returns the rendered block for the tool output — including
// sibling files the edit broke. Returns "" when no server covers the file,
// the server failed, the wait timed out, or there is nothing to report.
// Bounded by ctx: ctrl+c during a turn cancels the wait.
//
// WaitDiagnostics must not be called concurrently for the same path (the
// agent's per-path file lock already guarantees this: writes/edits to one
// path serialize, so their diagnostic waits do too).
func (m *Manager) WaitDiagnostics(ctx context.Context, path string) string {
	if m == nil {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	cs, err := m.clientFor(ctx, abs)
	if err != nil || cs == nil {
		return ""
	}
	data, err := os.ReadFile(abs) //nolint:gosec // G304: abs is a workspace file the LSP already opened
	if err != nil {
		return ""
	}

	// Snapshot the edited file's diagnostics up front; the wait below runs
	// until a push for this file arrives (any push: even an identical
	// diagnostic list proves the server re-evaluated this content) or the
	// budget/ctx expires. Related pushes (sibling files) are batched by the
	// server, so they're in the cache by the time the edited file's push
	// wakes us.
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ""
	}
	before, hadBefore := m.diags[abs]
	cs.docs[abs]++
	version := cs.docs[abs]
	wch := make(chan struct{})
	pushed := false
	m.waiters[abs] = append(m.waiters[abs], wch)
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.waiters, abs)
		m.mu.Unlock()
	}()

	uri := fileURI(abs)
	if version == 1 {
		cs.cli.notify("textDocument/didOpen", map[string]any{
			"textDocument": map[string]any{
				// languageId: servers match on extension anyway; gopls only
				// accepts "go". Trim the dot and go.
				"uri": uri, "languageId": strings.TrimPrefix(filepath.Ext(abs), "."), "version": version, "text": string(data),
			},
		})
	} else {
		cs.cli.notify("textDocument/didChange", map[string]any{
			"textDocument":   map[string]any{"uri": uri, "version": version},
			"contentChanges": []map[string]any{{"text": string(data)}},
		})
	}

	deadline := time.Now().Add(diagWait)
	for {
		m.mu.Lock()
		edited, ok := m.diags[abs]
		m.mu.Unlock()
		// A push that arrived since snapshot — whether or not the message
		// list differs (a clean file pushes an identical empty list) — means
		// the server evaluated this exact content. Related pushes (sibling
		// files) usually batch, but frames can land a tick apart: give
		// trailing pushes one more wake or up to 50ms before rendering so
		// sibling errors make the same tool result.
		arrived := pushed || ok != hadBefore || !diagsEqual(before, edited)
		if arrived {
			// One grace window, then out: the select below consumes a push
			// that arrives during the window, so no second-arrival loop
			// iteration is needed (and none happens — the path always breaks).
			m.mu.Lock()
			wch = make(chan struct{})
			m.waiters[abs] = append(m.waiters[abs], wch)
			m.mu.Unlock()
			graceFor := min(50*time.Millisecond, time.Until(deadline)) // cap is a cap
			grace := time.NewTimer(graceFor)
			select {
			case <-wch:
				grace.Stop()
				m.mu.Lock()
				closing := m.closed
				m.mu.Unlock()
				if closing {
					return ""
				}
				// The push that woke the grace window is the one the wait
				// exists for; the grace path breaks below either way, so no
				// pushed = true re-marking is needed here.
			case <-ctx.Done():
				grace.Stop()
				return ""
			case <-grace.C:
			}
			break
		}
		remain := time.Until(deadline)
		if remain <= 0 {
			break
		}
		timer := time.NewTimer(remain)
		select {
		case <-wch:
			timer.Stop()
			// Manager.Close also closes waiter channels — that wake is a
			// shutdown signal, not a push.
			m.mu.Lock()
			closing := m.closed
			m.mu.Unlock()
			if closing {
				return ""
			}
			pushed = true
			// publish closed and removed this waiter; register a fresh one
			// for the next push before re-checking the cache.
			m.mu.Lock()
			wch = make(chan struct{})
			m.waiters[abs] = append(m.waiters[abs], wch)
			m.mu.Unlock()
		case <-ctx.Done():
			timer.Stop()
			return ""
		case <-timer.C:
		}
	}

	m.mu.Lock()
	siblings := siblingErrors(abs, m.diags)
	edited := append([]Diagnostic(nil), m.diags[abs]...)
	m.mu.Unlock()
	return Report(abs, edited, siblings)
}

// diagsEqual compares two diagnostic sets (order-sensitive: servers push
// ordered lists).
func diagsEqual(a, b []Diagnostic) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// clientFor resolves a client for the file, spawning on demand. Spawn dedup:
// concurrent touches for the same (server, root) share one spawn via a
// close-to-broadcast channel — losers wait on <-ch, the winner closes it
// after registering clients[key] or broken[key].
func (m *Manager) clientFor(ctx context.Context, abs string) (*clientState, error) {
	ext := filepath.Ext(abs)
	var name string
	var spec ServerSpec
	m.mu.Lock()
	workspace := m.workspace
	for n, s := range m.specs {
		if slices.Contains(s.Extensions, ext) {
			name, spec = n, s
		}
		if name != "" {
			break
		}
	}
	m.mu.Unlock()
	if name == "" || spec.Disabled || len(spec.Command) == 0 {
		return nil, nil //nolint:nilnil // nil client = no server for this file (or disabled); caller treats that as "no LSP available", not an error
	}
	root := findRoot(filepath.Dir(abs), spec.RootMarkers, workspace)
	if m.keyer != nil {
		root = m.keyer(name, abs, spec.RootMarkers)
	}

	m.mu.Lock()
	key := name + "\x00" + root
	if cs, ok := m.clients[key]; ok {
		m.mu.Unlock()
		return cs, nil
	}
	if msg, bad := m.broken[key]; bad {
		m.mu.Unlock()
		return nil, errors.New(msg)
	}
	if ch, ok := m.spawning[key]; ok {
		m.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if cs, ok := m.clients[key]; ok {
			return cs, nil
		}
		return nil, errors.New(m.broken[key])
	}
	ch := make(chan struct{})
	m.spawning[key] = ch
	m.mu.Unlock()

	cs, err := m.spawn(ctx, key, name, spec, root)

	m.mu.Lock()
	delete(m.spawning, key)
	if err != nil {
		// A caller-side cancel (ctrl+c mid-spawn) must not poison the server:
		// the process may be fine. Only genuine spawn/handshake failures are
		// remembered as broken.
		if !errors.Is(err, context.Canceled) {
			m.broken[key] = err.Error()
		}
	} else {
		m.clients[key] = cs
	}
	close(ch) // wake all deduped waiters
	m.mu.Unlock()
	return cs, err
}

// spawn starts the server process and runs the initialize handshake.
func (m *Manager) spawn(ctx context.Context, key, name string, spec ServerSpec, root string) (*clientState, error) {
	if _, err := exec.LookPath(spec.Command[0]); err != nil {
		return nil, fmt.Errorf("%s not on PATH", spec.Command[0])
	}
	m.mu.Lock()
	processes, rootID := m.processes, m.rootID
	env := make(map[string]string, len(m.processEnv)+len(spec.Env))
	maps.Copy(env, m.processEnv)
	m.mu.Unlock()
	var stdin io.WriteCloser
	var stdout io.ReadCloser
	var err error
	cs := &clientState{root: root, docs: map[string]int{}}
	if processes != nil {
		maps.Copy(env, spec.Env)
		cs.process, stdin, stdout, err = processes.StartPiped(context.WithoutCancel(ctx), rootID, spec.Command[0], spec.Command[1:], capability.ProcessOptions{
			Cwd: root, Env: env, Stderr: io.Discard,
		})
		if err != nil {
			return nil, err
		}
	} else {
		// WithoutCancel: the server belongs to the manager, not one tool turn.
		cmd := exec.CommandContext(context.WithoutCancel(ctx), spec.Command[0], spec.Command[1:]...)
		cmd.Dir = root
		cmd.Env = os.Environ()
		for k, v := range spec.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		stdin, err = cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		stdout, err = cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		cs.cmd = cmd
	}

	cs.cli = newClient(stdin, stdout, func(uri string, version int, diags []Diagnostic) {
		m.publish(key, uri, version, diags)
	})

	initCtx, cancel := context.WithTimeout(ctx, initTimeout)
	defer cancel()
	// ponytail: didChange always sends full text, which gopls (and every
	// server worth configuring) accepts; if a stricter server ever rejects
	// it, parse capabilities.textDocumentSync.change from the result.
	err = cs.cli.request(initCtx, "initialize", map[string]any{
		"processId": os.Getpid(),
		"rootUri":   fileURI(root),
		"workspaceFolders": []map[string]any{
			{"name": "workspace", "uri": fileURI(root)},
		},
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"synchronization":    map[string]any{"didOpen": true, "didChange": true},
				"publishDiagnostics": map[string]any{"versionSupport": true},
			},
		},
	}, nil)
	if err != nil {
		cs.kill()
		return nil, fmt.Errorf("initialize: %w", err)
	}
	cs.cli.notify("initialized", map[string]any{})
	return cs, nil
}

// publish handles one publishDiagnostics push: swap the cache entry and wake
// waiters whose (path, version) is at-or-before this push, or whose server
// omitted the version. Runs on the client's read goroutine — no I/O here.
func (m *Manager) publish(key, uri string, version int, diags []Diagnostic) {
	path := uriPath(uri)
	if path == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.diags[path] = diags
	// Wake everyone waiting on this file regardless of push version: a stale
	// push is harmless (the waiter re-checks the cache and re-registers), a
	// missed one costs the full timeout. (ponytail: version matching could
	// skip stale wakes; the re-check already covers it.)
	for _, ch := range m.waiters[path] {
		close(ch)
	}
	delete(m.waiters, path)
}

// Statuses renders the /lsp rows: every configured server plus state.
func (m *Manager) Statuses() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Status, 0, len(m.specs))
	names := make([]string, 0, len(m.specs))
	for n := range m.specs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		st := Status{Name: n, State: "not started"}
		for key, cs := range m.clients {
			if strings.HasPrefix(key, n+"\x00") {
				st.State = "connected"
				st.Root = cs.root
			}
		}
		for key, msg := range m.broken {
			if strings.HasPrefix(key, n+"\x00") {
				st.State = "failed"
				st.Err = msg
			}
		}
		out = append(out, st)
	}
	return out
}

// Close shuts every server down (shutdown/exit then kill) and wakes all
// waiters.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	clients := make([]*clientState, 0, len(m.clients))
	for _, cs := range m.clients {
		clients = append(clients, cs)
	}
	for wk, chans := range m.waiters {
		for _, ch := range chans {
			close(ch)
		}
		delete(m.waiters, wk)
	}
	m.mu.Unlock()
	for _, cs := range clients {
		cs.kill()
	}
}

// kill tries the polite LSP shutdown then SIGKILLs the process group.
func (cs *clientState) kill() {
	if cs.cmd == nil && cs.process == nil { // pipe-attached test client: no process to kill
		cs.cli.shutdown()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = cs.cli.request(ctx, "shutdown", nil, nil) // best effort
	cs.cli.notify("exit", nil)
	cs.cli.shutdown()
	if c, ok := cs.cli.stdin.(io.Closer); ok {
		_ = c.Close()
	}
	if cs.process != nil {
		_ = cs.process.Kill()
		_ = cs.process.Wait()
		return
	}
	if cs.cmd.Process != nil {
		_ = syscall.Kill(-cs.cmd.Process.Pid, syscall.SIGKILL)
	}
	_ = cs.cmd.Wait()
}

// findRoot walks up from dir looking for any marker, falling back to dir
// itself (opencode's NearestRoot falls back to the project dir the same way,
// server.ts:32-79).
func findRoot(dir string, markers []string, boundary ...string) string {
	stop := ""
	if len(boundary) > 0 {
		stop = filepath.Clean(boundary[0])
	}
	for d := dir; ; d = filepath.Dir(d) {
		for _, mkr := range markers {
			if _, err := os.Stat(filepath.Join(d, mkr)); err == nil {
				return d
			}
		}
		if stop != "" && d == stop {
			return dir
		}
		parent := filepath.Dir(d)
		if parent == d {
			return dir
		}
	}
}

// fileURI renders an absolute path as a file:// URI.
func fileURI(path string) string {
	return "file://" + (&url.URL{Path: path}).String()
}

// uriPath parses a file:// URI back to a path; "" for non-file URIs.
// url.Parse already percent-decodes u.Path — do NOT unescape again (a path
// containing a literal % would corrupt or drop).
func uriPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	return u.Path
}
