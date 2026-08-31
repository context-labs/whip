package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/tools"
)

// Status is a server's lifecycle state, mirroring opencode's discriminated
// status union (mcp/index.ts): a server is always in exactly one state, and
// the TUI renders it directly.
type Status int

const (
	StatusDisabled   Status = iota // enabled: false, or an unsupported import
	StatusConnecting               // connect kicked off, not yet settled
	StatusReady                    // connected, tools listed
	StatusFailed                   // connect/list failed, or the session dropped
)

func (s Status) String() string {
	switch s {
	case StatusDisabled:
		return "disabled"
	case StatusConnecting:
		return "connecting"
	case StatusReady:
		return "ready"
	case StatusFailed:
		return "failed"
	}
	return "unknown"
}

// Server is the /mcp status view of one configured server.
type Server struct {
	Name   string // configured name
	Status Status
	Note   string // import notes (e.g. claude sse transport)
	Err    string // failure detail when Status == StatusFailed
	Tools  int    // tools contributed when ready
	Source string // config file the server was discovered from ("" when unknown)
}

// server holds one server's live state.
type server struct {
	name string
	cfg  ServerConfig

	status Status
	err    string
	note   string
	defs   []*sdkmcp.Tool
	instr  string // server instructions from initialize (opencode injects these)
	sess   *sdkmcp.ClientSession
	gen    int // increments per connect; a stale session's watcher no-ops
	stderr *ringBuffer

	// ready closes exactly once when the FIRST connect attempt settles
	// (ready or failed): the close-to-broadcast pattern from agent's
	// BackgroundTask — every waiter (tool calls, /mcp) wakes together with no
	// per-waiter state. Reconnects are a new attempt, not a new channel:
	// callers hold the manager, not the channel.
	ready   chan struct{}
	settled bool
	// calling is a 1-capacity semaphore serializing calls per server (many
	// servers are single-request-at-a-time over stdio).
	calling chan struct{}
	// reconnect requests a fresh connect attempt (used by /mcp reconnect).
	// Capacity 1; coalesces repeats while a connect is in flight.
	reconnect chan struct{}

	autoTries int // auto-reconnect attempts since the last successful connect
	runCtx    context.Context
	stop      context.CancelFunc

	mu sync.Mutex // guards status/err/defs/sess/cfg.Enabled
}

// disabled reports the server's current enable state. Enable/Disable flip
// cfg.Enabled while the run and auto-reconnect goroutines read it, so the
// read takes mu like any other mutable server field.
func (s *server) disabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.Disabled()
}

// autoReconnect caps the background reconnect attempts after an unexpected
// session drop (backoff doubles per try). Manual /mcp reconnect is unlimited.
const autoReconnectMax = 3

// autoReconnectDelay is the backoff between auto-reconnect attempts. Kept
// small for tests via a fast-path env read; tests use
// WHIP_TEST_MCP_BACKOFF_MS instead of racing a package var.
func autoReconnectDelay(try int) time.Duration {
	if ms, err := strconv.Atoi(os.Getenv("WHIP_TEST_MCP_BACKOFF_MS")); err == nil && ms >= 0 {
		return time.Duration(ms) * time.Millisecond
	}
	return time.Duration(1<<try) * time.Second // 1s, 2s, 4s
}

// kickAutoReconnect schedules a background reconnect after an unexpected
// drop, unless the manager is closing, the server is disabled, or we've
// already retried autoReconnectMax times in a row.
func (s *server) kickAutoReconnect(m *Manager) {
	m.onChangeMu.Lock()
	closing := m.closed
	m.onChangeMu.Unlock()
	s.mu.Lock()
	tries := s.autoTries
	ctx := s.runCtx
	s.mu.Unlock()
	if closing || s.disabled() || tries >= autoReconnectMax {
		return
	}
	m.launch("MCP auto-reconnect "+s.name, func() {
		timer := time.NewTimer(autoReconnectDelay(tries))
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		m.onChangeMu.Lock()
		closing := m.closed
		m.onChangeMu.Unlock()
		s.mu.Lock()
		gave := s.status == StatusReady || s.autoTries != tries // someone else recovered/retried
		s.mu.Unlock()
		if closing || gave || s.disabled() {
			return
		}
		s.mu.Lock()
		s.autoTries++
		s.mu.Unlock()
		select {
		case s.reconnect <- struct{}{}:
		default:
		}
	})
}

// Manager owns every MCP server connection. All methods are safe for
// concurrent use; tool calls for different servers proceed in parallel.
type Manager struct {
	servers  map[string]*server // keyed by configured name
	onChange func()             // optional redraw hook, like taskRegistry.OnChange

	// blocked holds servers an mcpImport policy filtered out. They never
	// connect, but stay visible in the status view so a gated import isn't
	// silent. Set at startup; read via Blocked.
	blocked []Server

	// connectTransport builds the transport for a server config. A var so
	// tests can substitute in-process transports without spawning processes.
	connectTransport func(ctx context.Context, cfg ServerConfig, stderr *ringBuffer) (sdkmcp.Transport, error)

	onChangeMu sync.Mutex // guards onChange, blocked, and closed (writes may race connect goroutines)
	closed     bool       // set by Close; connect() won't store new sessions after it
	started    bool
	stop       context.CancelFunc
	runCtx     context.Context
	stopStart  func() bool
	closeOnce  sync.Once
	workers    sync.WaitGroup
	launcher   func(string, func()) bool
	processes  *capability.ProcessManager
	rootID     string
	processCwd string
	processEnv map[string]string
}

// newServer builds one server's live state. Config errors become failed
// servers immediately; disabled entries never spawn (birth-settled, so their
// ready channel is already closed and tool calls never wait on them).
func newServer(name string, cfg ServerConfig) *server {
	s := &server{
		name:      name,
		cfg:       cfg,
		note:      cfg.Note,
		ready:     make(chan struct{}),
		calling:   make(chan struct{}, 1),
		reconnect: make(chan struct{}, 1),
	}
	if !cfg.Remote() {
		s.stderr = newRingBuffer(4096)
	}
	s.status = StatusConnecting
	if cfg.Disabled() {
		s.status = StatusDisabled
	} else if msg := cfg.Valid(); msg != "" {
		s.status = StatusFailed
		s.err = "invalid config: " + msg
	}
	if s.status != StatusConnecting {
		s.settled = true
		close(s.ready) // settled at birth; nothing will ever connect
	}
	return s
}

// NewManager builds a manager from merged server configs. Config errors
// become failed servers immediately; disabled entries never spawn.
func NewManager(cfgs map[string]ServerConfig) *Manager {
	ctx, stop := context.WithCancel(context.Background())
	m := &Manager{servers: map[string]*server{}, runCtx: ctx, stop: stop}
	m.connectTransport = m.defaultTransport
	for name, cfg := range cfgs {
		m.servers[name] = newServer(name, cfg)
	}
	return m
}

// SetLauncher routes MCP lifecycle goroutines through a daemon supervisor.
func (m *Manager) SetLauncher(launcher func(string, func()) bool) {
	m.onChangeMu.Lock()
	m.launcher = launcher
	m.onChangeMu.Unlock()
}

func (m *Manager) launch(kind string, work func()) bool {
	m.onChangeMu.Lock()
	if m.closed {
		m.onChangeMu.Unlock()
		return false
	}
	launcher := m.launcher
	m.workers.Add(1)
	m.onChangeMu.Unlock()
	run := func() {
		defer m.workers.Done()
		work()
	}
	if launcher != nil {
		if launcher(kind, run) {
			return true
		}
		m.workers.Done()
		return false
	}
	go run()
	return true
}

func (m *Manager) SetProcessOptions(processes *capability.ProcessManager, rootID, cwd string, env map[string]string) {
	m.onChangeMu.Lock()
	changedRoot := m.rootID != "" && m.rootID != rootID
	names := make([]string, 0, len(m.servers))
	if changedRoot && m.started {
		for name := range m.servers {
			names = append(names, name)
		}
	}
	m.processes, m.rootID, m.processCwd, m.processEnv = processes, rootID, cwd, maps.Clone(env)
	m.onChangeMu.Unlock()
	for _, name := range names {
		m.Reconnect(name)
	}
}

// AddServers folds new configs into a running manager and kicks connects for
// the enabled ones — the live half of toggling an import source on
// (LoadMergedFiltered re-discovers; the manager absorbs). A name that already
// exists is left alone: whip-owned entries and existing sessions win.
func (m *Manager) AddServers(_ context.Context, cfgs map[string]ServerConfig) {
	m.onChangeMu.Lock()
	if m.closed {
		m.onChangeMu.Unlock()
		return
	}
	var fresh []*server
	for name, cfg := range cfgs {
		if _, exists := m.servers[name]; exists {
			continue
		}
		s := newServer(name, cfg)
		s.runCtx, s.stop = context.WithCancel(m.runCtx)
		m.servers[name] = s
		if s.status == StatusConnecting {
			fresh = append(fresh, s)
		}
	}
	m.onChangeMu.Unlock()
	for _, s := range fresh {
		m.launch("MCP server "+s.name, func() { s.run(s.runCtx, m) })
	}
	m.fireOnChange()
}

// RemoveServers tears down and forgets servers by name — the live half of
// toggling an import source off. Sessions close, auto-reconnect stops (the
// gen bump invalidates stale watchers), and the servers vanish from Tools()
// and Statuses() immediately. Names that aren't live are ignored (whip-owned
// entries are the caller's to keep).
func (m *Manager) RemoveServers(names ...string) {
	m.onChangeMu.Lock()
	var doomed []*server
	for _, name := range names {
		if s, ok := m.servers[name]; ok {
			doomed = append(doomed, s)
			delete(m.servers, name)
		}
	}
	m.onChangeMu.Unlock()
	for _, s := range doomed {
		s.mu.Lock()
		if s.stop != nil {
			s.stop()
		}
		s.cfg.Enabled = new(false)
		old := s.sess
		s.sess, s.defs = nil, nil
		s.gen++
		s.mu.Unlock()
		if old != nil {
			_ = old.Close()
		}
	}
	if len(doomed) > 0 {
		m.fireOnChange()
	}
}

// SetOnChange installs a callback fired whenever a server's status changes
// (the TUI uses it to redraw). Safe to call any time; the callback runs from
// connect goroutines, so keep it cheap and non-blocking.
func (m *Manager) SetOnChange(fn func()) {
	m.onChangeMu.Lock()
	m.onChange = fn
	m.onChangeMu.Unlock()
}

// FireOnChangeForTest invokes the installed callback (tests only).
func (m *Manager) FireOnChangeForTest() { m.fireOnChange() }

func (m *Manager) fireOnChange() {
	m.onChangeMu.Lock()
	fn := m.onChange
	m.onChangeMu.Unlock()
	if fn != nil {
		fn()
	}
}

// Start kicks concurrent connects for every connecting server. Each server
// owns its lifecycle goroutine, which also services later reconnect requests.
// ctx cancellation aborts initial connects (e.g. shutdown during startup).
func (m *Manager) Start(ctx context.Context) {
	m.onChangeMu.Lock()
	if m.started || m.closed {
		m.onChangeMu.Unlock()
		return
	}
	m.started = true
	startCtx, cancelStart := context.WithCancel(m.runCtx)
	m.stopStart = context.AfterFunc(ctx, cancelStart)
	servers := make([]*server, 0, len(m.servers))
	for _, s := range m.servers {
		if s.status == StatusConnecting {
			s.runCtx, s.stop = context.WithCancel(startCtx)
			servers = append(servers, s)
		}
	}
	m.onChangeMu.Unlock()
	for _, s := range servers {
		m.launch("MCP server "+s.name, func() { s.run(s.runCtx, m) })
	}
}

func (m *Manager) defaultTransport(ctx context.Context, cfg ServerConfig, stderr *ringBuffer) (sdkmcp.Transport, error) {
	if cfg.Remote() {
		return defaultTransport(ctx, cfg, stderr)
	}
	if len(cfg.Command) == 0 {
		return nil, errors.New("MCP command is empty")
	}
	m.onChangeMu.Lock()
	processes, rootID, cwd := m.processes, m.rootID, m.processCwd
	env := make(map[string]string, len(m.processEnv)+len(cfg.Env))
	maps.Copy(env, m.processEnv)
	m.onChangeMu.Unlock()
	if processes == nil {
		return defaultTransport(ctx, cfg, stderr)
	}
	if cfg.Cwd != "" {
		baseCwd := cwd
		cwd = cfg.Cwd
		if !filepath.IsAbs(cwd) {
			cwd = filepath.Join(baseCwd, cwd)
		}
	}
	maps.Copy(env, cfg.Env)
	return &managedTransport{processes: processes, rootID: rootID, cwd: cwd, env: env, command: cfg.Command, stderr: stderr}, nil
}

type managedTransport struct {
	processes *capability.ProcessManager
	rootID    string
	cwd       string
	env       map[string]string
	command   []string
	stderr    io.Writer
}

func (t *managedTransport) Connect(ctx context.Context) (sdkmcp.Connection, error) {
	stderr := t.stderr
	if stderr == nil {
		stderr = io.Discard
	}
	process, stdin, stdout, err := t.processes.StartPiped(context.WithoutCancel(ctx), t.rootID, t.command[0], t.command[1:], capability.ProcessOptions{
		Cwd: t.cwd, Env: t.env, Stderr: stderr,
	})
	if err != nil {
		return nil, err
	}
	transport := &sdkmcp.IOTransport{Reader: stdout, Writer: &managedInput{WriteCloser: stdin, process: process}}
	return transport.Connect(ctx)
}

type managedInput struct {
	io.WriteCloser
	process *capability.Process
	once    sync.Once
	err     error
}

func (w *managedInput) Close() error {
	w.once.Do(func() {
		closeErr := w.WriteCloser.Close()
		done := make(chan error, 1)
		go func() { done <- w.process.Wait() }()
		select {
		case waitErr := <-done:
			_ = w.process.Kill()
			w.err = errors.Join(closeErr, waitErr)
		case <-time.After(3 * time.Second):
			killErr := w.process.Kill()
			w.err = errors.Join(closeErr, killErr, <-done)
		}
	})
	return w.err
}

// run is the per-server lifecycle goroutine: one connect attempt, then it
// services reconnect requests until the process exits (Close kills sessions;
// the goroutine parks on reconnect thereafter — it has no work but also no
// cost, and whip exits rather than idles servers). A reconnect queued while
// a connect was in flight is dropped when that connect just succeeded — the
// user asked for a fresh connection and already has one.
func (s *server) run(ctx context.Context, m *Manager) {
	s.connect(ctx, m)
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.reconnect:
		}
		if s.disabled() {
			s.setState(m, StatusDisabled, "")
			continue
		}
		s.mu.Lock()
		ready := s.status == StatusReady
		s.mu.Unlock()
		if ready {
			continue
		}
		s.connect(ctx, m) // manager lifetime, not any single turn
	}
}

func (s *server) connect(ctx context.Context, m *Manager) {
	// Birth-settled servers (disabled/invalid) never run a lifecycle
	// goroutine, so connect is only reachable for servers allowed to try.
	s.mu.Lock()
	s.status = StatusConnecting
	s.mu.Unlock()
	m.fireOnChange()
	s.mu.Lock()
	cfg, startGen := s.cfg, s.gen // snapshot under mu: RemoveServers mutates cfg
	s.mu.Unlock()
	timeout := cfg.StartupTimeoutDuration()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	transport, err := m.connectTransport(ctx, cfg, s.stderr)
	if err == nil {
		client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "whip", Title: "whip"}, nil)
		var sess *sdkmcp.ClientSession
		sess, err = client.Connect(ctx, transport, nil)
		if err == nil {
			var listed *sdkmcp.ListToolsResult
			listed, err = sess.ListTools(ctx, nil)
			if err == nil {
				m.onChangeMu.Lock()
				closed := m.closed
				_, stillOurs := m.servers[s.name]
				m.onChangeMu.Unlock()
				s.mu.Lock()
				removed := s.gen != startGen
				s.mu.Unlock()
				if closed || removed || !stillOurs {
					// Manager closing, or this server was removed mid-connect
					// (import source toggled off): don't store the session.
					_ = sess.Close()
					return
				}
				var instr string
				if ir := sess.InitializeResult(); ir != nil {
					instr = strings.TrimSpace(ir.Instructions)
				}
				s.mu.Lock()
				s.defs = listed.Tools
				s.instr = instr
				s.sess = sess
				s.gen++
				s.autoTries = 0
				gen := s.gen
				s.mu.Unlock()
				s.setState(m, StatusReady, "")
				// Watch for a dropped session: mark failed so tool calls stop
				// being routed (opencode's client.onclose → status failed,
				// guarded by a client-identity check, index.ts:443). The gen
				// counter is the same check: a watcher from an older connect
				// must not tear down the newer session.
				m.launch("MCP connection watcher "+s.name, func() {
					_ = sess.Wait()
					m.onChangeMu.Lock()
					closing := m.closed
					m.onChangeMu.Unlock()
					s.mu.Lock()
					stale := s.gen != gen
					if !stale {
						s.sess = nil
						s.defs = nil
						s.instr = ""
					}
					s.mu.Unlock()
					if !stale && !closing {
						s.setState(m, StatusFailed, "connection closed")
						s.kickAutoReconnect(m)
					}
				})
				return
			}
			_ = sess.Close()
		}
	}
	msg := err.Error()
	if ctx.Err() == context.DeadlineExceeded {
		msg = fmt.Sprintf("timed out after %s", timeout)
	}
	if s.stderr != nil {
		if tail := strings.TrimSpace(s.stderr.String()); tail != "" {
			msg += " — stderr: " + tail
		}
	}
	s.setState(m, StatusFailed, msg)
}

// setState transitions status and wakes every waiter on the first settle.
func (s *server) setState(m *Manager, st Status, errMsg string) {
	s.mu.Lock()
	firstSettle := !s.settled
	if st != StatusConnecting {
		s.settled = true
	}
	s.status, s.err = st, errMsg
	s.mu.Unlock()
	if firstSettle && st != StatusConnecting {
		close(s.ready)
	}
	logf("server %s -> %s %s", s.name, st, errMsg)
	m.fireOnChange()
}

// Tools returns the current agent-facing tool set: one tools.Tool per listed
// MCP tool on every ready server. Cheap to call per turn.
func (m *Manager) Tools() []tools.Tool {
	m.onChangeMu.Lock()
	servers := make([]*server, 0, len(m.servers))
	for _, s := range m.servers {
		servers = append(servers, s)
	}
	m.onChangeMu.Unlock()
	var out []tools.Tool
	for _, s := range servers {
		s.mu.Lock()
		defs, sess := s.defs, s.sess
		s.mu.Unlock()
		if sess == nil {
			continue
		}
		for _, d := range defs {
			out = append(out, s.bridge(d))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Def.Function.Name < out[j].Def.Function.Name })
	return out
}

// bridge converts one listed MCP tool into the agent-loop tools.Tool. The
// name follows claude-code's mcp__server__tool convention; the schema passes
// through verbatim with the object-typed shape providers require (opencode
// forces type:object + properties, catalog.ts convertTool).
func (s *server) bridge(d *sdkmcp.Tool) tools.Tool {
	name := ToolName(s.name, d.Name)
	schema := normalizeSchema(d.InputSchema)
	desc := d.Description
	if d.Title != "" && desc == "" {
		desc = d.Title
	}
	return tools.Tool{
		Def: llm.NewTool(name, fmt.Sprintf("[MCP %s] %s", s.name, desc), schema),
		Run: func(ctx context.Context, args json.RawMessage) (string, error) {
			return s.call(ctx, d.Name, args)
		},
	}
}

// call runs one tool call against the session, serialized per server and
// bounded by the configured tool timeout. Errors become error strings for
// the model via tools.Execute — never loop-aborting (opencode throws and
// converts to an output-error tool part; whip's "Error: …" convention is
// the same shape).
// connectGrace caps how long a tool call waits for a still-connecting server
// before reporting back. A call should never park the turn for the full
// startup timeout — the model can retry, and the server may still land.
const connectGrace = 5 * time.Second

func (s *server) call(ctx context.Context, tool string, args json.RawMessage) (string, error) {
	// Fail fast: a server whose first connect already settled (ready/failed/
	// disabled) never waits on the channel at all.
	s.mu.Lock()
	settled, sess, status, errMsg := s.settled, s.sess, s.status, s.err
	s.mu.Unlock()
	if !settled {
		// Still connecting: wait out the grace period, not the full timeout.
		grace, cancel := context.WithTimeout(ctx, connectGrace)
		select {
		case <-s.ready:
		case <-grace.Done():
			cancel()
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", fmt.Errorf("mcp server %q is still connecting — retry in a moment (/mcp shows status)", s.name)
		}
		cancel()
		s.mu.Lock()
		sess, status, errMsg = s.sess, s.status, s.err
		s.mu.Unlock()
	}
	if sess == nil {
		switch status {
		case StatusFailed:
			if errMsg != "" {
				return "", fmt.Errorf("mcp server %q unavailable: %s (/mcp %s reconnect)", s.name, errMsg, s.name)
			}
			return "", fmt.Errorf("mcp server %q unavailable (/mcp %s reconnect)", s.name, s.name)
		case StatusDisabled:
			return "", fmt.Errorf("mcp server %q is disabled (/mcp %s enable)", s.name, s.name)
		default:
			return "", fmt.Errorf("mcp server %q is %s", s.name, status)
		}
	}
	// Serialize calls per server.
	select {
	case s.calling <- struct{}{}:
		defer func() { <-s.calling }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.ToolTimeoutDuration())
	defer cancel()
	var argMap map[string]any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &argMap); err != nil {
			return "", fmt.Errorf("invalid tool arguments: %w", err)
		}
	}
	res, err := sess.CallTool(ctx, &sdkmcp.CallToolParams{Name: tool, Arguments: argMap})
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("mcp tool %s timed out after %s", tool, s.cfg.ToolTimeoutDuration())
		}
		return "", err
	}
	return flattenResult(res), nil
}

// flattenResult renders a CallToolResult as model-facing text (pure).
// Text content is concatenated; binary/resource parts become placeholders
// (ponytail: feed images to vision models); structured content is appended
// as JSON when no text exists (opencode catalog.ts does the same). IsError
// prefixes "Error: " so the model sees failure, per the MCP spec's own
// guidance that tool errors belong in content.
func flattenResult(res *sdkmcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		switch c := c.(type) {
		case *sdkmcp.TextContent:
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(c.Text)
		case *sdkmcp.ImageContent:
			fmt.Fprintf(&b, "\n[image content omitted: %s, %d bytes]", c.MIMEType, len(c.Data))
		case *sdkmcp.AudioContent:
			fmt.Fprintf(&b, "\n[audio content omitted: %s, %d bytes]", c.MIMEType, len(c.Data))
		case *sdkmcp.EmbeddedResource:
			if c.Resource != nil && c.Resource.Text != "" {
				fmt.Fprintf(&b, "\n[resource %s]\n%s", c.Resource.URI, c.Resource.Text)
			} else if c.Resource != nil {
				fmt.Fprintf(&b, "\n[binary resource omitted: %s, %d bytes]", c.Resource.URI, len(c.Resource.Blob))
			}
		case *sdkmcp.ResourceLink:
			fmt.Fprintf(&b, "\n[resource link: %s (%s)]", c.URI, c.Name)
		}
	}
	out := b.String()
	if out == "" && res.StructuredContent != nil {
		if data, err := json.MarshalIndent(res.StructuredContent, "", "  "); err == nil {
			out = string(data)
		}
	}
	if out == "" {
		out = "(no output)"
	}
	if res.IsError {
		out = "Error: " + out
	}
	return tools.Truncate(out)
}

// normalizeSchema passes the server's input schema through as a JSON string,
// coercing it into the object shape providers require (opencode forces
// type:object + properties:{} + additionalProperties:false).
// normalizeSchema renders an MCP tool's input schema as a JSON object with
// type:"object" and a properties key (some servers omit one or both). The
// input map is COPIED, never mutated: d.InputSchema is shared across every
// Manager.Tools() call, and the race detector caught concurrent writes to it
// (fatal error: concurrent map writes) when two settles interleaved.
func normalizeSchema(schema any) string {
	src, ok := schema.(map[string]any)
	if !ok || src == nil {
		return `{"type":"object","properties":{}}`
	}
	m := make(map[string]any, len(src)+2)
	maps.Copy(m, src)
	m["type"] = "object"
	if _, ok := m["properties"]; !ok {
		m["properties"] = map[string]any{}
	}
	data, err := json.Marshal(m)
	if err != nil {
		return `{"type":"object","properties":{}}`
	}
	return string(data)
}

// Config returns a server's normalized config (the live definition, including
// imported claude/codex entries) — the TUI persists this when toggling
// enabled so whip's own config stays self-contained. ok is false for
// unknown names.
func (m *Manager) Config(name string) (ServerConfig, bool) {
	m.onChangeMu.Lock()
	s, ok := m.servers[name]
	m.onChangeMu.Unlock()
	if !ok {
		return ServerConfig{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg, true
}

// Disable tears down a server's live session without touching config (the
// caller persists enabled:false). Reconnect on a disabled server is refused
// by run().
func (m *Manager) Disable(name string) bool {
	m.onChangeMu.Lock()
	s, ok := m.servers[name]
	m.onChangeMu.Unlock()
	if !ok {
		return false
	}
	s.mu.Lock()
	s.cfg.Enabled = new(false)
	old := s.sess
	s.sess, s.defs = nil, nil
	s.gen++
	s.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	s.setState(m, StatusDisabled, "")
	return true
}

// Enable clears a persisted disable and reconnects.
func (m *Manager) Enable(name string) bool {
	m.onChangeMu.Lock()
	s, ok := m.servers[name]
	m.onChangeMu.Unlock()
	if !ok {
		return false
	}
	s.mu.Lock()
	s.cfg.Enabled = nil
	s.mu.Unlock()
	return m.Reconnect(name)
}

// InstructionsBlock renders the <mcp_instructions> system-prompt section:
// ready servers' initialize instructions, name-sorted ("" when none publish
// any). Servers that publish instructions are telling the model how to use
// their tools — injecting them (opencode does, session/system.ts) improves
// usage quality, not just availability.
func (m *Manager) InstructionsBlock() string {
	m.onChangeMu.Lock()
	servers := make([]*server, 0, len(m.servers))
	for _, s := range m.servers {
		servers = append(servers, s)
	}
	m.onChangeMu.Unlock()
	type entry struct{ name, text string }
	var instr []entry
	for _, s := range servers {
		s.mu.Lock()
		ready, text := s.sess != nil, s.instr
		s.mu.Unlock()
		if ready && text != "" {
			instr = append(instr, entry{s.name, text})
		}
	}
	if len(instr) == 0 {
		return ""
	}
	sort.Slice(instr, func(i, j int) bool { return instr[i].name < instr[j].name })
	var b strings.Builder
	b.WriteString("\n<mcp_instructions>\n")
	for _, e := range instr {
		fmt.Fprintf(&b, "<server name=%q>\n%s\n</server>\n", e.name, e.text)
	}
	b.WriteString("</mcp_instructions>")
	return b.String()
}

// Probe connects a single server for `whip mcp test`: builds a throwaway
// manager with just that entry, starts it, waits for the first settle, and
// returns the outcome with tool names. A doctor visit, not a residency.
type ProbeResult struct {
	Server
	Elapsed   time.Duration
	ToolNames []string // agent-facing names (mcp__name__tool), first 5 + "…"
}

func Probe(ctx context.Context, name string, cfg ServerConfig) ProbeResult {
	start := time.Now()
	m := NewManager(map[string]ServerConfig{name: cfg})
	defer m.Close()
	processes := capability.NewProcessManager()
	defer processes.Close()
	cwd, _ := os.Getwd()
	m.SetProcessOptions(processes, "mcp-probe", cwd, nil)
	m.Start(ctx)
	s := m.servers[name] // ponytail: no lock — this manager never leaves Probe, so AddServers/RemoveServers can't reach it
	select {
	case <-s.ready:
	case <-ctx.Done():
		return ProbeResult{Name: name, Status: StatusFailed, Err: ctx.Err().Error(), Elapsed: time.Since(start)}
	}
	st := m.Statuses()[0]
	res := ProbeResult{Server: st, Elapsed: time.Since(start)}
	for _, t := range m.Tools() {
		res.ToolNames = append(res.ToolNames, t.Def.Function.Name)
		if len(res.ToolNames) == 5 {
			res.ToolNames = append(res.ToolNames, "…")
			break
		}
	}
	return res
}

// SetBlocked records the servers an import policy filtered out (already
// disabled+noted ServerConfigs). Called once at startup, before Start.
func (m *Manager) SetBlocked(cfgs map[string]ServerConfig) {
	m.onChangeMu.Lock()
	defer m.onChangeMu.Unlock()
	m.blocked = make([]Server, 0, len(cfgs))
	for name, c := range cfgs {
		m.blocked = append(m.blocked, Server{Name: name, Status: StatusDisabled, Note: c.Note, Source: c.Source})
	}
	sort.Slice(m.blocked, func(i, j int) bool { return m.blocked[i].Name < m.blocked[j].Name })
}

// Blocked returns the name-sorted snapshot of policy-filtered servers.
func (m *Manager) Blocked() []Server {
	m.onChangeMu.Lock()
	defer m.onChangeMu.Unlock()
	return append([]Server(nil), m.blocked...)
}

// BlockedByPolicy reports whether name was filtered out by the mcpImport
// policy (vs merely disabled), so /mcp enable can point at the right fix.
func (m *Manager) BlockedByPolicy(name string) bool {
	m.onChangeMu.Lock()
	defer m.onChangeMu.Unlock()
	for _, b := range m.blocked {
		if b.Name == name {
			return true
		}
	}
	return false
}

// Statuses returns a stable, name-sorted snapshot for /mcp.
func (m *Manager) Statuses() []Server {
	m.onChangeMu.Lock()
	servers := make([]*server, 0, len(m.servers))
	for _, s := range m.servers {
		servers = append(servers, s)
	}
	m.onChangeMu.Unlock()
	out := make([]Server, 0, len(servers))
	for _, s := range servers {
		s.mu.Lock()
		out = append(out, Server{Name: s.name, Status: s.status, Note: s.note, Err: s.err, Tools: len(s.defs), Source: s.cfg.Source})
		s.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Reconnect requests a fresh connect for a server (drops a live session
// first). Returns false for unknown names.
func (m *Manager) Reconnect(name string) bool {
	m.onChangeMu.Lock()
	s, ok := m.servers[name]
	m.onChangeMu.Unlock()
	if !ok {
		return false
	}
	s.mu.Lock()
	old := s.sess
	s.sess, s.defs = nil, nil
	s.status = StatusConnecting
	s.gen++
	s.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	select {
	case s.reconnect <- struct{}{}:
	default: // a reconnect is already queued
	}
	return true
}

// Close shuts every session down. Stdio transports terminate their child
// process on Close (stdin closes first, then the scoped process is killed).
func (m *Manager) Close() {
	m.closeOnce.Do(func() {
		m.onChangeMu.Lock()
		m.closed = true
		m.onChange = nil
		stop := m.stop
		stopStart := m.stopStart
		servers := make([]*server, 0, len(m.servers))
		for _, s := range m.servers {
			servers = append(servers, s)
		}
		m.onChangeMu.Unlock()
		if stop != nil {
			stop()
		}
		if stopStart != nil {
			stopStart()
		}
		for _, s := range servers {
			s.mu.Lock()
			sess := s.sess
			s.sess, s.defs = nil, nil
			s.mu.Unlock()
			if sess != nil {
				_ = sess.Close()
			}
		}
		m.workers.Wait()
	})
}

// defaultTransport builds the SDK transport for a config: CommandTransport
// (stdio, own process group, merged env, captured stderr) or
// StreamableClientTransport (remote, header-injecting client).
func defaultTransport(ctx context.Context, cfg ServerConfig, stderr *ringBuffer) (sdkmcp.Transport, error) {
	if cfg.Remote() {
		// Header values may be secret references ("$VAR"/"${VAR}"/"!cmd") —
		// resolve them at connect time (the point of use) so configs hold only
		// references and resolved secrets never reach the log or session store.
		// Unresolvable references drop the header: the connect then fails
		// cleanly instead of sending the literal reference upstream.
		headers := make(map[string]string, len(cfg.Headers))
		for k, v := range cfg.Headers {
			rv, err := config.ResolveSecret(v)
			if err != nil {
				logf("header %s: %v (dropped)", k, err)
				continue
			}
			headers[k] = rv
		}
		return &sdkmcp.StreamableClientTransport{
			Endpoint:   cfg.URL,
			HTTPClient: &http.Client{Transport: headerTransport(headers)},
			// ponytail: the standalone SSE stream would deliver server-initiated
			// notifications (tool list changes); request-response is enough for v1
			DisableStandaloneSSE: true,
		}, nil
	}
	if len(cfg.Command) == 0 {
		return nil, errors.New("MCP command is empty")
	}
	// WithoutCancel: ctx is connect()'s startup-timeout context, cancelled the
	// moment connect returns — binding the command to it would SIGKILL every
	// stdio server right after a successful connect. The process must live
	// until the session is closed (CommandTransport terminates it then).
	cmd := exec.CommandContext(context.WithoutCancel(ctx), cfg.Command[0], cfg.Command[1:]...)
	// Inherit whip's environment and layer the server's vars on top (opencode
	// does the same — users expect $PATH etc. to work).
	cmd.Env = append(os.Environ(), envPairs(cfg.Env)...)
	if cfg.Cwd != "" {
		cmd.Dir = cfg.Cwd
	}
	if stderr != nil {
		cmd.Stderr = stderr
	}
	// Own process group: detached grandchildren of the server die with it.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return &sdkmcp.CommandTransport{Command: cmd, TerminateDuration: 3 * time.Second}, nil
}

func envPairs(env map[string]string) []string {
	pairs := make([]string, 0, len(env))
	for k, v := range env {
		pairs = append(pairs, k+"="+v)
	}
	return pairs
}

// headerTransport injects static headers (e.g. Authorization) into every
// request of a remote server's HTTP client.
type headerTransport map[string]string

func (h headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	for k, v := range h {
		req.Header.Set(k, v)
	}
	return http.DefaultTransport.RoundTrip(req)
}

// logf mirrors config.LogEvent for MCP lifecycle events (connect failures,
// status transitions) so "why didn't my server come up?" is answerable from
// ~/.whip/whip.log.
func logf(format string, args ...any) {
	config.LogEvent("mcp", fmt.Sprintf(format, args...))
}
