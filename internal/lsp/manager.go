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
func FromConfigMap(in map[string]Config) map[string]ServerSpec {
	out := make(map[string]ServerSpec, len(builtinServers)+len(in))
	for name, spec := range builtinServers {
		out[name] = cloneSpec(spec)
	}
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
		out[name] = cloneSpec(spec)
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
	mu             sync.Mutex
	specs          map[string]ServerSpec
	clients        map[string]*clientState // key: id + "\x00" + root
	broken         map[string]string       // key -> error message
	spawning       map[string]chan struct{}
	diags          map[string][]Diagnostic    // abs path -> latest pushed set
	waiters        map[string][]chan struct{} // abs path -> pending wakes
	keyer          spawnKeyer                 // nil = findRoot (production)
	closed         bool
	processes      *capability.ProcessManager
	rootID         string
	workspace      string
	processEnv     map[string]string
	ctx            context.Context
	cancel         context.CancelFunc
	calls          sync.WaitGroup
	spawns         sync.WaitGroup
	closedDone     chan struct{}
	callSlot       chan struct{}
	slots          chan struct{}
	truncated      map[string]bool
	workspaceInfo  os.FileInfo
	generation     uint64
	cacheTruncated bool
}

type clientState struct {
	cli      *client
	cmd      *exec.Cmd
	process  *capability.Process
	root     string
	docs     map[string]int // abs path -> last sent version
	docBytes map[string]int
	bytes    int
	release  func()
	killOnce sync.Once
}

func (m *Manager) SetProcessOptions(processes *capability.ProcessManager, rootID, workspace string, env map[string]string) {
	if canonical, err := filepath.EvalSymlinks(workspace); err == nil {
		workspace = canonical
	}
	identity, _ := os.Stat(workspace)
	m.mu.Lock()
	m.generation++
	var clients []*clientState
	if m.rootID != "" && (m.rootID != rootID || m.workspace != workspace || m.processes != processes) {
		for _, client := range m.clients {
			clients = append(clients, client)
		}
		m.clients = map[string]*clientState{}
		m.broken = map[string]string{}
		m.diags = map[string][]Diagnostic{}
	}
	m.processes, m.rootID, m.workspace, m.processEnv = processes, rootID, workspace, maps.Clone(env)
	m.workspaceInfo = identity
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
	copied := make(map[string]ServerSpec, len(specs))
	for name, spec := range specs {
		copied[name] = cloneSpec(spec)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		specs:    copied,
		clients:  map[string]*clientState{},
		broken:   map[string]string{},
		spawning: map[string]chan struct{}{},
		diags:    map[string][]Diagnostic{},
		waiters:  map[string][]chan struct{}{},
		ctx:      ctx, cancel: cancel, closedDone: make(chan struct{}), callSlot: make(chan struct{}, 1), truncated: map[string]bool{},
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
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0) //nolint:gosec // Legacy caller validates the path; this compatibility read rejects links/special files and bounds content.
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxDocumentBytes {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(file, maxDocumentBytes+1))
	if err != nil || len(data) > maxDocumentBytes {
		return ""
	}
	result, err := m.Diagnostics(ctx, path, string(data))
	if err != nil {
		return ""
	}
	return result.Output
}

// clientFor resolves a client for the file, spawning on demand. Spawn dedup:
// concurrent touches for the same (server, root) share one spawn via a
// close-to-broadcast channel — losers wait on <-ch, the winner closes it
// after registering clients[key] or broken[key].
func (m *Manager) clientFor(ctx context.Context, abs string) (*clientState, error) {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(m.ctx, cancel)
	defer func() { stop(); cancel() }()
	ext := filepath.Ext(abs)
	var name string
	var spec ServerSpec
	m.mu.Lock()
	workspace, generation := m.workspace, m.generation
	for _, candidate := range slices.Sorted(maps.Keys(m.specs)) {
		s := m.specs[candidate]
		if !s.Disabled && slices.Contains(s.Extensions, ext) {
			name, spec = candidate, s
			break
		}
	}
	m.mu.Unlock()
	if name == "" || len(spec.Command) == 0 {
		return nil, nil //nolint:nilnil // No enabled server covers this file.
	}
	root := findRoot(filepath.Dir(abs), spec.RootMarkers, workspace)
	if m.keyer != nil {
		root = m.keyer(name, abs, spec.RootMarkers)
	}
	key := name + "\x00" + root
	for {
		m.mu.Lock()
		if m.closed || m.generation != generation {
			m.mu.Unlock()
			return nil, errors.New("language server scope closed or changed")
		}
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
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if len(m.clients)+len(m.broken)+len(m.spawning) >= 16 {
			m.mu.Unlock()
			return nil, errors.New("language server workspace process capacity reached")
		}
		ch := make(chan struct{})
		m.spawning[key] = ch
		m.spawns.Add(1)
		m.mu.Unlock()
		cs, err := m.spawn(ctx, key, name, spec, root)
		m.mu.Lock()
		stale := m.closed || m.generation != generation
		if !stale {
			if err == nil {
				m.clients[key] = cs
			} else if !errors.Is(err, context.Canceled) {
				m.broken[key] = diagnosticText(err.Error())
			}
		}
		delete(m.spawning, key)
		close(ch)
		m.mu.Unlock()
		if stale && cs != nil {
			cs.kill()
			cs = nil
		}
		m.spawns.Done()
		if stale {
			return nil, errors.New("language server scope closed or changed")
		}
		return cs, err
	}
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
	cs := &clientState{root: root, docs: map[string]int{}, docBytes: map[string]int{}}
	if m.slots != nil {
		select {
		case m.slots <- struct{}{}:
			cs.release = func() { <-m.slots }
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			return nil, errors.New("language server host process capacity reached")
		}
	}
	started := false
	defer func() {
		if !started && cs.release != nil {
			cs.release()
		}
	}()
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
			_ = stdin.Close()
			_ = stdout.Close()
			return nil, err
		}
		cs.cmd = cmd
	}

	started = true
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
	if err := cs.cli.notifyContext(ctx, "initialized", map[string]any{}); err != nil {
		cs.kill()
		return nil, err
	}
	return cs, nil
}

// publish handles one publishDiagnostics push: swap the cache entry and wake
// waiters whose (path, version) is at-or-before this push, or whose server
// omitted the version. Runs on the client's read goroutine — no I/O here.
func (m *Manager) publish(key, uri string, version int, diags []Diagnostic) {
	path := uriPath(uri)
	if path == "" || len(path) > 4096 {
		return
	}
	m.mu.Lock()
	workspace, identity := m.workspace, m.workspaceInfo
	m.mu.Unlock()
	if workspace != "" {
		canonical, err := filepath.EvalSymlinks(path)
		current, statErr := os.Stat(workspace)
		if err != nil || statErr != nil || identity == nil || !os.SameFile(identity, current) || !within(workspace, canonical) {
			return
		}
		path = canonical
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cs := m.clients[key]
	if m.closed || cs == nil || (version > 0 && cs.docs[path] > 0 && version != cs.docs[path]) {
		return
	}
	if _, exists := m.diags[path]; !exists && len(m.diags) >= maxDiagnosticFiles {
		m.cacheTruncated = true
		if len(m.waiters[path]) == 0 {
			return
		}
		evict := slices.Sorted(maps.Keys(m.diags))[0]
		delete(m.diags, evict)
		delete(m.truncated, evict)
	}
	truncated := len(diags) > maxPerFile
	bounded := make([]Diagnostic, min(len(diags), maxPerFile))
	for i, value := range diags[:len(bounded)] {
		message := diagnosticText(value.Message)
		truncated = truncated || message != value.Message
		value.Message = message
		bounded[i] = value
	}
	m.diags[path], m.truncated[path] = bounded, truncated
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
				if cs.cli.isDead() {
					st.State, st.Err = "failed", "language server connection closed"
				}
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
		<-m.closedDone
		return
	}
	m.closed = true
	m.cancel()
	clients := slices.Collect(maps.Values(m.clients))
	m.mu.Unlock()
	var joined sync.WaitGroup
	for _, cs := range clients {
		joined.Go(cs.kill)
	}
	joined.Wait()
	m.spawns.Wait()
	m.calls.Wait()
	close(m.closedDone)
}

// kill joins the bounded shutdown exchange, transport pumps, and process tree.
func (cs *clientState) kill() {
	cs.killOnce.Do(func() {
		if cs.release != nil {
			defer cs.release()
		}
		if cs.cmd != nil || cs.process != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = cs.cli.request(ctx, "shutdown", nil, nil)
			_ = cs.cli.notifyContext(ctx, "exit", nil)
			cancel()
		}
		cs.cli.shutdown()
		if cs.process != nil {
			_ = cs.process.Kill()
			_ = cs.process.Wait()
		}
		if cs.cmd != nil {
			if cs.cmd.Process != nil {
				_ = syscall.Kill(-cs.cmd.Process.Pid, syscall.SIGKILL)
			}
			_ = cs.cmd.Wait()
		}
		cs.cli.wait()
	})
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
	if err != nil || u.Scheme != "file" || u.Host != "" || u.RawQuery != "" || u.Fragment != "" || !filepath.IsAbs(u.Path) || filepath.Clean(u.Path) != u.Path {
		return ""
	}
	return u.Path
}
