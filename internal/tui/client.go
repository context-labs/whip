package tui

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	bubbletea "github.com/charmbracelet/bubbletea"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/skills"
	"github.com/context-labs/whip/internal/update"
)

// ClientState is the synchronization state shared by every daemon-backed UI.
type ClientState uint8

const (
	ClientDisconnected ClientState = iota
	ClientReconnecting
	ClientSnapshotting
	ClientLive
)

func (s ClientState) String() string {
	switch s {
	case ClientDisconnected:
		return "disconnected"
	case ClientReconnecting:
		return "reconnecting"
	case ClientSnapshotting:
		return "snapshotting"
	case ClientLive:
		return "live"
	default:
		return "unknown"
	}
}

// Action gives one user gesture one stable command identity. Callers retain
// the value when a connection failure makes the outcome uncertain.
type Action struct {
	CommandID string
	Operation string
	RootID    string
	Payload   json.RawMessage
}

// ClientUpdate carries immutable daemon state into the Bubble Tea loop.
type ClientUpdate struct {
	State        ClientState
	StateChanged bool
	Snapshot     *session.RootSnapshot
	Event        *daemon.ProtocolEvent
	Err          error
}

// clientPresentation is the daemon-fed state needed to render a terminal. It
// deliberately has no provider client, tool registry, scheduler, store, or
// process handles.
type clientPresentation struct {
	modelID      string
	effort       string
	workingDir   string
	contextLimit int
	usage        llm.Usage
	messages     []llm.Message
	agents       []session.RuntimeAgent
	inbox        []session.InboxItem
	blackboard   []session.StateValue
	budgets      []session.SnapshotBudget
	capabilities []session.CapabilityRecord
	schedules    []session.Schedule
}

type daemonConnection interface {
	Command(context.Context, daemon.CommandParams) (daemon.CommandResult, error)
	Replay(context.Context, daemon.ReplayParams) (daemon.ReplayResult, error)
	Snapshot(context.Context, string) (session.RootSnapshot, error)
	Events() <-chan daemon.ProtocolEvent
	Done() <-chan struct{}
	Err() error
	Close() error
}

type clientConnector func(context.Context, map[string]int64) (daemonConnection, error)

var errReconnectForRoot = errors.New("reconnect to subscribe to created root")

// ClientOptions contains behavioral inputs only. Presentation state remains
// in the Bubble Tea model and survives every reconnect or snapshot.
type ClientOptions struct {
	ClientID   string
	PrivateKey ed25519.PrivateKey
	RootID     string
	Create     *daemon.CreateSession
	Connector  clientConnector
	RetryMin   time.Duration
	RetryMax   time.Duration
}

// Client owns a reconnecting protocol connection, never agent execution.
type Client struct {
	clientID   string
	privateKey ed25519.PrivateKey
	create     *daemon.CreateSession
	connect    clientConnector
	retryMin   time.Duration
	retryMax   time.Duration

	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	once    sync.Once
	started atomic.Bool

	mu      sync.RWMutex
	state   ClientState
	rootID  string
	cursor  int64
	conn    daemonConnection
	changed chan struct{}

	updates chan ClientUpdate
	nextID  atomic.Uint64
}

// Run starts the presentation-only TUI. Agent loops, persistence, schedulers,
// providers, permissions, and child processes remain in the daemon.
func Run(cfg *config.Config, modelName, provName, sysPrompt, resumeID string, _ bool, firstRun bool, initialPrompt string) (string, error) {
	stdin := bufio.NewReader(os.Stdin)
	if trusted, err := checkTrust(stdin); err != nil {
		return "", err
	} else if !trusted {
		return "", errors.New("folder not trusted")
	}
	if firstRun {
		if err := setupWizard(cfg, stdin); err != nil {
			return "", err
		}
	}

	_, modelConfig, apiID, err := cfg.Resolve(modelName, provName)
	if err != nil {
		return "", err
	}
	if modelName == "" {
		modelName = cfg.DefaultModel
	}
	if provName == "" {
		provName = cfg.DefaultProvider
		if provName == "" && len(modelConfig.Providers) > 0 {
			provName = modelConfig.Providers[0]
		}
	}
	home, err := config.Dir()
	if err != nil {
		return "", err
	}
	paths, err := daemon.Paths(home)
	if err != nil {
		return "", err
	}
	credentials, err := daemon.LoadOrCreateClientCredentials(daemon.SystemKeyStore(), "tui")
	if err != nil {
		return "", fmt.Errorf("TUI identity: %w", err)
	}
	connector := func(ctx context.Context, cursors map[string]int64) (daemonConnection, error) {
		return daemon.EnsureClient(ctx, paths, daemon.InitializeParams{
			ProtocolMajor: daemon.ProtocolMajor, BuildID: Version, ClientKind: "tui",
			ClientID: credentials.ClientID, Capabilities: []string{"commands", "events", "snapshots", "permissions"},
			Cursors: cursors,
		}, func() error { return daemon.LaunchSelfDaemon(paths) })
	}
	identityWarning, err := prepareTUIIdentity(connector, credentials, stdin)
	if err != nil {
		return "", err
	}
	var create *daemon.CreateSession
	if resumeID == "" {
		create = &daemon.CreateSession{CWD: cwd(), Model: modelName, Provider: provName}
	}
	client, err := NewClient(ClientOptions{
		ClientID: credentials.ClientID, PrivateKey: credentials.PrivateKey,
		RootID: resumeID, Create: create, Connector: connector,
	})
	if err != nil {
		return "", err
	}

	catalogs := config.LoadCatalogs()
	contextLimit := modelConfig.ContextWindow()
	if catalog, ok := catalogs[provName]; ok {
		contextLimit = max(contextLimit, catalog.ContextLength(apiID))
	}
	mouseOn := cfg.Mouse == nil || *cfg.Mouse
	showThinking := cfg.Thinking == nil || *cfg.Thinking
	m := &model{
		cfg: cfg, client: client, clientState: ClientDisconnected,
		clientView: clientPresentation{
			modelID: apiID, contextLimit: contextLimit,
			effort:   DefaultEffortFor(catalogs, provName, apiID, cfg.DefaultEffort),
			messages: []llm.Message{{Role: "system", Content: sysPrompt}},
		},
		modelName: modelName, provName: provName, sysPrompt: sysPrompt,
		input: newInput(), spin: spinner.New(spinner.WithSpinner(spinner.Dot)), follow: true, saved: 1, hoverIdx: -1,
		catalogs: catalogs, mouseOn: mouseOn, now: time.Now, showThinking: showThinking,
		chdir: os.Chdir, sidebarHide: cfg.Sidebar != nil && !*cfg.Sidebar,
		compactModel: cfg.CompactModel, compactProv: cfg.CompactProvider,
		skillScan:     func() []skills.Skill { return skills.Scan(skills.DefaultDirs()...) },
		initialPrompt: initialPrompt, cfgExtra: map[string]string{},
	}
	m.updateLatest = update.Pending(Version)
	m.themeHow = m.applyTheme(cfg.Theme)
	if cfg.UIMode == opencodeMode {
		m.applyUIMode(opencodeMode)
	}
	m.startupReport()
	m.append(dimStyle.Render("daemon: connecting…"))
	if identityWarning != "" {
		m.append(errStyle.Render(identityWarning))
	}

	options := []bubbletea.ProgramOption{}
	if cfg.UIMode == opencodeMode {
		options = append(options, bubbletea.WithAltScreen())
	}
	fmt.Fprint(os.Stdout, "\x1b[9999;1H")
	if m.mouseOn {
		enableClickWheelMouse(os.Stdout)
		if cfg.UIMode == opencodeMode {
			fmt.Fprint(os.Stdout, "\x1b[?1003h")
		}
	}
	if info, statErr := os.Stat(filepath.Join(home, "config.json")); statErr == nil {
		m.cfgMod = info.ModTime()
	}
	program := bubbletea.NewProgram(m, options...)
	m.prog = program
	client.Start()
	tuiRunning = true
	_, runErr := program.Run()
	tuiRunning = false
	closeErr := client.Close()
	if m.mouseOn {
		disableClickWheelMouse(os.Stdout)
	}
	return client.RootID(), errors.Join(runErr, closeErr)
}

type identityConnection interface {
	IdentityStatus(context.Context) (daemon.IdentityStatusResult, error)
	EnrollIdentity(context.Context, ed25519.PrivateKey, bool, string, ed25519.PrivateKey) (daemon.IdentityResult, error)
}

func prepareTUIIdentity(connector clientConnector, credentials daemon.ClientCredentials, stdin *bufio.Reader) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	connection, err := connector(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = connection.Close() }()
	identity, ok := connection.(identityConnection)
	if !ok {
		return "permission approvals unavailable: daemon does not support identities", nil
	}
	status, err := identity.IdentityStatus(ctx)
	if err != nil {
		return "", err
	}
	if status.Paired {
		return "", nil
	}
	if !status.EnrollmentOpen {
		return "permission approvals unavailable: this TUI identity is not paired", nil
	}
	if !askYN(stdin, os.Stdout, "Pair this terminal as the first human permission approver?", false) {
		return "permission approvals disabled until a human identity is paired", nil
	}
	if _, err := identity.EnrollIdentity(ctx, credentials.PrivateKey, true, "", nil); err != nil {
		return "", fmt.Errorf("pair TUI identity: %w", err)
	}
	return "", nil
}

func NewClient(options ClientOptions) (*Client, error) {
	if options.ClientID == "" || options.Connector == nil {
		return nil, errors.New("TUI client requires an identity and connector")
	}
	if options.RootID == "" && options.Create == nil {
		return nil, errors.New("TUI client requires a root or session template")
	}
	if options.RootID != "" && options.Create != nil {
		return nil, errors.New("TUI client cannot resume and create simultaneously")
	}
	if options.RetryMin <= 0 {
		options.RetryMin = 25 * time.Millisecond
	}
	if options.RetryMax < options.RetryMin {
		options.RetryMax = time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{
		clientID: options.ClientID, privateKey: append(ed25519.PrivateKey(nil), options.PrivateKey...),
		rootID: options.RootID, create: options.Create,
		connect: options.Connector, retryMin: options.RetryMin, retryMax: options.RetryMax,
		ctx: ctx, cancel: cancel, done: make(chan struct{}), state: ClientDisconnected,
		changed: make(chan struct{}), updates: make(chan ClientUpdate, daemon.MaxOutboundEnvelopes),
	}, nil
}

func (c *Client) Start() {
	c.once.Do(func() {
		c.started.Store(true)
		go c.run()
	})
}

func (c *Client) Close() error {
	c.cancel()
	select {
	case <-c.done:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("TUI client did not stop")
	}
}

func (c *Client) Updates() <-chan ClientUpdate { return c.updates }

func (c *Client) State() ClientState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

func (c *Client) RootID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.rootID
}

func (c *Client) Cursor() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cursor
}

// SwitchRoot changes the subscribed session only after a successful daemon
// action. Closing the current connection makes the run loop perform the full
// replay-or-snapshot synchronization before enabling commands on the new root.
func (c *Client) SwitchRoot(rootID string) error {
	if strings.TrimSpace(rootID) == "" {
		return errors.New("session root is required")
	}
	c.mu.Lock()
	c.rootID, c.cursor, c.create = rootID, 0, nil
	c.state = ClientSnapshotting
	connection := c.conn
	c.notifyLocked()
	c.mu.Unlock()
	c.emit(ClientUpdate{State: ClientSnapshotting, StateChanged: true})
	if connection != nil {
		return connection.Close()
	}
	return nil
}

func (c *Client) NewAction(operation string, payload any) (Action, error) {
	if operation == "" {
		return Action{}, errors.New("action operation is required")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return Action{}, err
	}
	id := c.nextID.Add(1)
	c.mu.RLock()
	rootID := c.rootID
	c.mu.RUnlock()
	return Action{
		CommandID: c.clientID + "-" + strconv.FormatUint(id, 10),
		Operation: operation, RootID: rootID,
		Payload: raw,
	}, nil
}

func (c *Client) Command(ctx context.Context, action Action) (daemon.CommandResult, error) {
	if action.CommandID == "" || action.Operation == "" || action.RootID == "" {
		return daemon.CommandResult{}, errors.New("action identity and operation are required")
	}
	for {
		c.mu.RLock()
		state, connection := c.state, c.conn
		c.mu.RUnlock()
		if state != ClientLive || connection == nil {
			if !c.started.Load() {
				return daemon.CommandResult{}, fmt.Errorf("behavioral commands are disabled while client is %s", state)
			}
			if !c.waitForLive(ctx) {
				if err := ctx.Err(); err != nil {
					return daemon.CommandResult{}, err
				}
				return daemon.CommandResult{}, c.ctx.Err()
			}
			continue
		}
		result, err := connection.Command(ctx, daemon.CommandParams{
			CommandID: action.CommandID, Scope: string(session.CommandScopeRoot), RootID: action.RootID,
			Operation: action.Operation, Payload: action.Payload,
		})
		if err == nil {
			return result, nil
		}
		if ctx.Err() != nil {
			return daemon.CommandResult{}, ctx.Err()
		}
		select {
		case <-connection.Done():
			continue
		default:
			return daemon.CommandResult{}, err
		}
	}
}

type permissionConnection interface {
	DecidePermission(context.Context, ed25519.PrivateKey, daemon.PermissionDecision) (daemon.PermissionDecisionResult, error)
}

func (c *Client) DecidePermission(ctx context.Context, action Action, permissionID string, allow bool, reason string) (daemon.PermissionDecisionResult, error) {
	if action.CommandID == "" || action.Operation != "permission.decide" || action.RootID == "" || permissionID == "" {
		return daemon.PermissionDecisionResult{}, errors.New("permission action requires stable command and permission identities")
	}
	if len(c.privateKey) != ed25519.PrivateKeySize {
		return daemon.PermissionDecisionResult{}, errors.New("this TUI identity cannot approve permissions")
	}
	for {
		c.mu.RLock()
		state, connection := c.state, c.conn
		c.mu.RUnlock()
		if state != ClientLive || connection == nil {
			if !c.started.Load() {
				return daemon.PermissionDecisionResult{}, fmt.Errorf("permission decisions are disabled while client is %s", state)
			}
			if !c.waitForLive(ctx) {
				if err := ctx.Err(); err != nil {
					return daemon.PermissionDecisionResult{}, err
				}
				return daemon.PermissionDecisionResult{}, c.ctx.Err()
			}
			continue
		}
		privileged, ok := connection.(permissionConnection)
		if !ok {
			return daemon.PermissionDecisionResult{}, errors.New("this TUI identity cannot approve permissions")
		}
		result, err := privileged.DecidePermission(ctx, c.privateKey, daemon.PermissionDecision{
			CommandID: action.CommandID, RootID: action.RootID, PermissionID: permissionID, Allow: allow, Reason: reason,
		})
		if err == nil {
			return result, nil
		}
		if ctx.Err() != nil {
			return daemon.PermissionDecisionResult{}, ctx.Err()
		}
		select {
		case <-connection.Done():
			continue
		default:
			return daemon.PermissionDecisionResult{}, err
		}
	}
}

func (c *Client) waitForLive(ctx context.Context) bool {
	for {
		c.mu.RLock()
		live := c.state == ClientLive && c.conn != nil
		changed := c.changed
		c.mu.RUnlock()
		if live {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-c.ctx.Done():
			return false
		case <-changed:
		}
	}
}

func (c *Client) Snapshot(ctx context.Context) (session.RootSnapshot, error) {
	c.mu.RLock()
	state, rootID, connection := c.state, c.rootID, c.conn
	c.mu.RUnlock()
	if state != ClientLive || connection == nil || rootID == "" {
		return session.RootSnapshot{}, fmt.Errorf("snapshot unavailable while client is %s", state)
	}
	return connection.Snapshot(ctx, rootID)
}

func (c *Client) run() {
	defer close(c.done)
	defer close(c.updates)
	delay := c.retryMin
	for c.ctx.Err() == nil {
		c.transition(ClientReconnecting, nil)
		rootID, cursor := c.position()
		cursors := map[string]int64{}
		if rootID != "" {
			cursors[rootID] = cursor
		}
		connection, err := c.connect(c.ctx, cursors)
		if err != nil {
			c.transition(ClientDisconnected, err)
			if !c.retry(delay) {
				return
			}
			delay = min(delay*2, c.retryMax)
			continue
		}
		delay = c.retryMin
		c.setConnection(connection)
		if err := c.synchronize(connection); err != nil {
			_ = connection.Close()
			c.clearConnection(connection)
			if errors.Is(err, errReconnectForRoot) {
				continue
			}
			c.transition(ClientDisconnected, err)
			continue
		}
		c.transition(ClientLive, nil)
		if !c.consume(connection) {
			return
		}
		c.clearConnection(connection)
		c.transition(ClientDisconnected, connection.Err())
	}
}

func (c *Client) synchronize(connection daemonConnection) error {
	rootID, cursor := c.position()
	if rootID == "" {
		payload, err := json.Marshal(c.create)
		if err != nil {
			return err
		}
		result, err := connection.Command(c.ctx, daemon.CommandParams{
			CommandID: c.clientID + "-session", Scope: string(session.CommandScopeDaemon),
			Operation: "session.create", Payload: payload,
		})
		if err != nil {
			return err
		}
		if result.Status != "succeeded" || result.Output == "" {
			return fmt.Errorf("session creation is %s: %s", result.Status, result.Error)
		}
		rootID = result.Output
		c.mu.Lock()
		c.rootID = rootID
		c.cursor = 0
		c.mu.Unlock()
		return errReconnectForRoot
	}

	c.transition(ClientSnapshotting, nil)
	if cursor > 0 {
		replay, err := connection.Replay(c.ctx, daemon.ReplayParams{RootID: rootID, Cursor: cursor})
		if err != nil {
			return err
		}
		if !replay.Expired {
			for i := range replay.Events {
				c.emitEvent(replay.Events[i])
			}
			return nil
		}
	}
	snapshot, err := connection.Snapshot(c.ctx, rootID)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.cursor = snapshot.Cursor
	c.mu.Unlock()
	c.emit(ClientUpdate{Snapshot: &snapshot})
	return nil
}

func (c *Client) consume(connection daemonConnection) bool {
	for {
		select {
		case <-c.ctx.Done():
			_ = connection.Close()
			return false
		case <-connection.Done():
			return true
		case event, ok := <-connection.Events():
			if !ok {
				return true
			}
			c.emitEvent(event)
		}
	}
}

func (c *Client) emitEvent(event daemon.ProtocolEvent) {
	c.mu.Lock()
	if event.RootID != c.rootID || event.Seq <= c.cursor {
		c.mu.Unlock()
		return
	}
	c.cursor = event.Seq
	c.mu.Unlock()
	c.emit(ClientUpdate{Event: &event})
}

func (c *Client) transition(state ClientState, err error) {
	c.mu.Lock()
	c.state = state
	c.notifyLocked()
	c.mu.Unlock()
	c.emit(ClientUpdate{State: state, StateChanged: true, Err: err})
}

func (c *Client) emit(update ClientUpdate) {
	select {
	case <-c.ctx.Done():
	case c.updates <- update:
	}
}

func (c *Client) position() (string, int64) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.rootID, c.cursor
}

func (c *Client) setConnection(connection daemonConnection) {
	c.mu.Lock()
	c.conn = connection
	c.notifyLocked()
	c.mu.Unlock()
}

func (c *Client) clearConnection(connection daemonConnection) {
	c.mu.Lock()
	if c.conn == connection {
		c.conn = nil
		c.notifyLocked()
	}
	c.mu.Unlock()
}

func (c *Client) notifyLocked() {
	if c.changed != nil {
		close(c.changed)
	}
	c.changed = make(chan struct{})
}

func (c *Client) retry(delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-c.ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

type clientUpdateMsg struct {
	ClientUpdate
	closed bool
}
type clientSnapshotMsg struct {
	snapshot session.RootSnapshot
	err      error
}
type clientCommandMsg struct {
	action Action
	result daemon.CommandResult
	err    error
}
type clientPermissionMsg struct {
	action       Action
	permissionID string
	result       daemon.PermissionDecisionResult
	err          error
}
type clientTerminalMsg struct {
	action Action
	result daemon.CommandResult
	err    error
}

func waitClientUpdate(client *Client) bubbletea.Cmd {
	return func() bubbletea.Msg {
		update, ok := <-client.Updates()
		if !ok {
			return clientUpdateMsg{ClientUpdate: ClientUpdate{State: ClientDisconnected, StateChanged: true, Err: netClosedError{}}, closed: true}
		}
		return clientUpdateMsg{ClientUpdate: update}
	}
}

type netClosedError struct{}

func (netClosedError) Error() string { return "daemon client closed" }

func (m *model) requestClientSnapshot() bubbletea.Cmd {
	return func() bubbletea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		snapshot, err := m.client.Snapshot(ctx)
		return clientSnapshotMsg{snapshot: snapshot, err: err}
	}
}

func (m *model) applyClientSnapshot(snapshot session.RootSnapshot) {
	if m.sessionID != "" && snapshot.RootID == m.sessionID && snapshot.Cursor < m.clientCursor {
		return
	}
	draft := m.input.Value()
	follow, offset, selection := m.follow, m.vp.YOffset, m.sel
	m.sessionID = snapshot.RootID
	m.clientCursor = snapshot.Cursor
	m.sessionMode = snapshot.Meta.Mode
	m.modelName, m.provName = snapshot.Meta.Model, snapshot.Meta.Provider
	m.goal, m.sessTitle = snapshot.Meta.Goal, snapshot.Meta.Title
	if m.cfg != nil {
		if _, _, apiID, err := m.cfg.Resolve(snapshot.Meta.Model, snapshot.Meta.Provider); err == nil {
			m.clientView.modelID = apiID
		}
	}
	m.clientView.workingDir = snapshot.Meta.CWD
	if snapshot.Meta.Effort != "" {
		m.clientView.effort = snapshot.Meta.Effort
	}
	usage := llm.Usage{PromptTokens: snapshot.Meta.UsageIn, CompletionTokens: snapshot.Meta.UsageOut}
	if snapshot.Meta.UsageCached > 0 {
		usage.PromptTokensDetails = &struct {
			CachedTokens int `json:"cached_tokens"`
		}{CachedTokens: snapshot.Meta.UsageCached}
	}
	m.clientView.usage = usage
	m.clientView.messages = append([]llm.Message{{Role: "system", Content: m.sysPrompt}}, snapshot.Messages...)
	m.clientView.agents = append([]session.RuntimeAgent(nil), snapshot.Agents...)
	m.clientView.inbox = append([]session.InboxItem(nil), snapshot.Inbox...)
	m.clientView.blackboard = append([]session.StateValue(nil), snapshot.Blackboard...)
	m.clientView.budgets = append([]session.SnapshotBudget(nil), snapshot.Budgets...)
	m.clientView.capabilities = append([]session.CapabilityRecord(nil), snapshot.Capabilities...)
	m.clientView.schedules = append([]session.Schedule(nil), snapshot.Schedules...)
	m.blocks, m.msgBlock = nil, nil
	m.clientTerminalID, m.iactive = "", nil
	m.seedTranscript(snapshot.Messages, 1)
	for _, event := range snapshot.Presentation {
		_, _ = m.applyClientStream(event.Kind, event.Payload)
	}
	for _, runtimeAgent := range snapshot.Agents {
		if runtimeAgent.ParentID == "" {
			continue
		}
		m.blocks = append(m.blocks, block{kind: blockText, text: dimStyle.Render(runtimeAgentLine(runtimeAgent))})
	}
	m.applyClientPermissions(snapshot.Permissions)
	m.busy = m.clientInFlight > 0
	for _, item := range snapshot.Inbox {
		if item.Status == "queued" || item.Status == "running" {
			m.busy = true
			break
		}
	}
	m.input.SetValue(draft)
	m.input.CursorEnd()
	m.follow, m.sel = follow, selection
	m.refreshVP()
	if !follow {
		m.vp.SetYOffset(offset)
	}
}

func runtimeAgentLine(value session.RuntimeAgent) string {
	phase := value.LifecyclePhase
	if phase == "" {
		phase = value.Status
	}
	line := fmt.Sprintf("⚙ %s — %s", value.ID, phase)
	if value.BlockingReason != "" {
		line += " · blocked: " + value.BlockingReason
	}
	if value.TerminalCause != "" {
		line += " · terminal: " + value.TerminalCause
	}
	if len(value.AllowedControls) > 0 {
		line += " · controls: " + strings.Join(value.AllowedControls, ", ")
	}
	return line
}

func (m *model) displayModelID() string {
	if m.client != nil {
		return m.clientView.modelID
	}
	if m.agent != nil {
		return m.agent.Model
	}
	return ""
}

func (m *model) displayEffort() string {
	if m.client != nil {
		return m.clientView.effort
	}
	if m.agent != nil {
		return m.agent.Effort
	}
	return ""
}

func (m *model) displayUsage() llm.Usage {
	if m.client != nil {
		return m.clientView.usage
	}
	if m.agent != nil {
		return m.agent.Usage()
	}
	return llm.Usage{}
}

func (m *model) displayMessages() []llm.Message {
	if m.client != nil {
		return m.clientView.messages
	}
	if m.agent != nil {
		return m.agent.Messages
	}
	return nil
}

func (m *model) displayContextLimit() int {
	if m.client != nil {
		return m.clientView.contextLimit
	}
	if m.agent != nil {
		return m.agent.ContextLimit
	}
	return 0
}

func (m *model) applyClientStream(kind string, payload []byte) (bool, bubbletea.Cmd) {
	if !strings.HasPrefix(kind, "stream.") {
		return false, nil
	}
	var event daemon.StreamEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		m.append(errStyle.Render("daemon stream: " + err.Error()))
		return true, nil
	}
	switch kind {
	case "stream.terminal.started":
		m.clientTerminalID = event.ID
		m.iactive = &interactive{}
		return true, nil
	case "stream.terminal.output":
		if m.clientTerminalID == event.ID && m.iactive != nil {
			m.iactive.output += event.Text
			m.iactive.await = false
		}
		return true, nil
	case "stream.terminal.awaiting":
		if m.clientTerminalID == event.ID && m.iactive != nil {
			m.iactive.await = true
			m.iactive.awaitcd, _ = strconv.Atoi(event.Text)
		}
		return true, nil
	case "stream.terminal.completed":
		if m.clientTerminalID == event.ID {
			m.clientTerminalID = ""
			m.iactive = nil
			m.interrupt1 = false
		}
		return true, nil
	}
	var message bubbletea.Msg
	switch kind {
	case "stream.text":
		message = textMsg(event.Text)
	case "stream.reasoning":
		message = thinkMsg(event.Text)
	case "stream.tool.call":
		message = toolCallMsg{id: event.ID, name: event.Name, args: event.Args}
	case "stream.tool.started":
		message = toolStartMsg{id: event.ID, name: event.Name, args: event.Args}
	case "stream.tool.output":
		message = toolOutputMsg{id: event.ID, text: event.Text}
	case "stream.tool.completed":
		message = toolEndMsg{id: event.ID, name: event.Name, result: event.Result}
	case "stream.notice":
		message = noticeMsg(event.Text)
	default:
		m.append(dimStyle.Render("daemon sent unsupported stream event " + kind))
		return true, nil
	}
	_, command := m.Update(message)
	return true, command
}

func (m *model) submitClientAction(operation string, payload any, echo string) (bubbletea.Model, bubbletea.Cmd) {
	if m.clientState != ClientLive {
		m.append(errStyle.Render("daemon is " + m.clientState.String() + " — command not sent"))
		return m, nil
	}
	action, err := m.client.NewAction(operation, payload)
	if err != nil {
		m.append(errStyle.Render("command: " + err.Error()))
		return m, nil
	}
	if echo != "" {
		m.appendRaw(blockUser, linkifyFilePaths(echo, realFileExists))
	}
	m.clientInFlight++
	m.busy = true
	m.turnStart = m.nowFn()
	return m, func() bubbletea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		result, commandErr := m.client.Command(ctx, action)
		return clientCommandMsg{action: action, result: result, err: commandErr}
	}
}

func (m *model) openClientNamePrompt(label, value, operation string, cut int) {
	m.clientPromptOp, m.clientPromptCut = operation, cut
	m.openNamePrompt(label, value, func(string) {})
}

func (m *model) thinKey(msg bubbletea.KeyMsg) (bubbletea.Model, bubbletea.Cmd) {
	if m.permDialog != nil && m.permDialog.daemon != nil {
		return m.thinPermissionKey(msg)
	}
	if m.iactive != nil && m.clientTerminalID != "" {
		return m.thinInteractiveKey(msg)
	}
	if m.namePrompt != nil {
		if m.clientPromptOp == "" {
			switch msg.Type {
			case bubbletea.KeyEsc, bubbletea.KeyCtrlC:
				m.closeNamePrompt()
				return m, nil
			case bubbletea.KeyEnter:
				onOK := m.namePrompt.onOK
				value := strings.TrimSpace(m.input.Value())
				m.closeNamePrompt()
				onOK(value)
				return m, nil
			default:
				var command bubbletea.Cmd
				m.input, command = m.input.Update(msg)
				return m, command
			}
		}
		switch msg.Type {
		case bubbletea.KeyEsc, bubbletea.KeyCtrlC:
			m.closeNamePrompt()
			m.clientPromptOp, m.clientPromptCut = "", 0
			return m, nil
		case bubbletea.KeyEnter:
			value, operation, cut := strings.TrimSpace(m.input.Value()), m.clientPromptOp, m.clientPromptCut
			m.closeNamePrompt()
			m.clientPromptOp, m.clientPromptCut = "", 0
			if value == "" {
				m.append(errStyle.Render("a name is required"))
				return m, nil
			}
			payload := map[string]any{"args": value}
			if cut > 0 {
				payload["cut"] = cut
			}
			return m.submitClientAction(operation, payload, "")
		default:
			var command bubbletea.Cmd
			m.input, command = m.input.Update(msg)
			return m, command
		}
	}
	if m.palette != nil {
		return m.paletteKey(msg)
	}
	if m.picker != nil {
		return m.pickerKey(msg)
	}
	if m.rew != nil {
		return m.rewindKey(msg)
	}
	if m.mpicker != nil {
		return m.modelPickerKey(msg)
	}
	children := m.runtimeChildren()
	if m.tasksFocus && len(children) == 0 {
		m.tasksFocus = false
	}
	if m.tasksFocus {
		switch msg.Type {
		case bubbletea.KeyEsc, bubbletea.KeyCtrlT:
			m.tasksFocus = false
			return m, nil
		case bubbletea.KeyUp:
			if m.taskSel == 0 {
				m.tasksFocus = false
			} else {
				m.taskSel--
			}
			return m, nil
		case bubbletea.KeyDown:
			m.taskSel = min(m.taskSel+1, len(children)-1)
			return m, nil
		case bubbletea.KeyCtrlX:
			if len(children) == 0 {
				m.tasksFocus = false
				return m, nil
			}
			child := children[min(m.taskSel, len(children)-1)]
			return m.submitClientAction("agent.control", map[string]string{"args": "stop " + child.ID}, "")
		case bubbletea.KeyEnter:
			if len(children) > 0 {
				m.append(dimStyle.Render(runtimeAgentLine(children[min(m.taskSel, len(children)-1)])))
			}
			return m, nil
		default:
			m.tasksFocus = false
		}
	}
	switch msg.Type {
	case bubbletea.KeyCtrlC:
		if m.busy && m.clientState == ClientLive {
			if !m.interrupt1 {
				m.interrupt1 = true
				return m, nil
			}
			return m.submitClientAction("cancel", map[string]any{}, "")
		}
		if m.quit1 {
			m.quit1 = false
			return m, bubbletea.Quit
		}
		m.quit1 = true
		return m, bubbletea.Tick(2*time.Second, func(time.Time) bubbletea.Msg { return quitArmMsg{} })
	case bubbletea.KeyEsc:
		if m.menu != nil {
			if m.menu.cyc {
				m.input.SetValue(m.menu.base)
			}
			m.menu = nil
			return m, nil
		}
		if m.busy && strings.TrimSpace(m.input.Value()) == "" && m.clientState == ClientLive {
			return m.submitClientAction("cancel", map[string]any{}, "")
		}
		if strings.TrimSpace(m.input.Value()) != "" {
			if m.escClr {
				m.escClr = false
				m.hist = append(m.hist, strings.TrimSpace(m.input.Value()))
				m.histIdx = len(m.hist)
				m.input.Reset()
				m.append(dimStyle.Render("draft cleared — ↑ recalls it"))
				return m, nil
			}
			m.escClr = true
			return m, bubbletea.Tick(time.Second, func(time.Time) bubbletea.Msg { return escArmMsg{} })
		}
		if m.esc1 {
			m.esc1 = false
			m.openRewind()
			return m, nil
		}
		m.esc1 = true
		return m, bubbletea.Tick(time.Second, func(time.Time) bubbletea.Msg { return escArmMsg{} })
	case bubbletea.KeyPgUp, bubbletea.KeyPgDown:
		var command bubbletea.Cmd
		m.vp, command = m.vp.Update(msg)
		m.follow = m.vp.AtBottom()
		return m, command
	case bubbletea.KeyCtrlO:
		m.toggleThinking()
		return m, nil
	case bubbletea.KeyCtrlE:
		for i := len(m.blocks) - 1; i >= 0; i-- {
			if m.blocks[i].kind == blockTool {
				m.blocks[i].toggle()
				m.refreshVP()
				break
			}
		}
		return m, nil
	case bubbletea.KeyCtrlP:
		m.openThinPalette()
		return m, nil
	case bubbletea.KeyTab:
		if m.menu != nil {
			m.menuCycle(1)
		} else {
			m.openMenu()
		}
		return m, nil
	case bubbletea.KeyShiftTab:
		if m.menu != nil {
			m.menuCycle(-1)
		}
		return m, nil
	case bubbletea.KeyCtrlK:
		return m.thinCommand("/clear")
	case bubbletea.KeyCtrlT:
		if len(children) > 0 {
			m.tasksFocus = true
			m.clampTaskSel()
		}
		return m, nil
	case bubbletea.KeyDown:
		if m.menu != nil {
			m.menu.idx = (m.menu.idx + 1) % len(m.menu.cands)
			return m, nil
		}
		if strings.TrimSpace(m.input.Value()) == "" && len(children) > 0 {
			m.tasksFocus = true
			m.taskSel = 0
			return m, nil
		}
		m.histNext()
		return m, nil
	case bubbletea.KeyUp:
		if m.menu != nil {
			m.menu.idx = (m.menu.idx + len(m.menu.cands) - 1) % len(m.menu.cands)
			return m, nil
		}
		m.histPrev()
		return m, nil
	case bubbletea.KeyEnter:
		if m.menu != nil {
			candidate := m.menu.cands[m.menu.idx]
			if m.menu.cyc && m.menu.head == "" && execNow[candidate.Text] {
				m.menu = nil
				m.input.Reset()
				return m.thinCommand(candidate.Text)
			}
			if m.menu.cyc {
				m.acceptPreview()
				return m, nil
			}
			if m.menu.head == "" && execNow[candidate.Text] {
				m.menu = nil
				m.input.Reset()
				return m.thinCommand(candidate.Text)
			}
			if m.accept() {
				return m, nil
			}
		}
		text := strings.TrimSpace(m.input.Value())
		if text == "" {
			return m, nil
		}
		if m.clientState != ClientLive {
			m.append(errStyle.Render("daemon is " + m.clientState.String() + " — draft preserved"))
			return m, nil
		}
		m.input.Reset()
		m.hist = append(m.hist, text)
		m.histIdx = len(m.hist)
		if strings.HasPrefix(text, "/") {
			return m.thinCommand(text)
		}
		if strings.HasPrefix(text, "!") {
			return m.submitClientAction("shell.run", map[string]any{"command": strings.TrimSpace(strings.TrimPrefix(text, "!"))}, text)
		}
		return m.submitClientAction("submit", map[string]string{"text": text}, text)
	}
	var command bubbletea.Cmd
	m.input, command = m.input.Update(msg)
	m.refreshMenu()
	return m, command
}

func (m *model) thinInteractiveKey(msg bubbletea.KeyMsg) (bubbletea.Model, bubbletea.Cmd) {
	if msg.Type == bubbletea.KeyCtrlC {
		if !m.interrupt1 {
			m.interrupt1 = true
			return m, nil
		}
		return m.submitClientAction("cancel", map[string]any{}, "")
	}
	var input []byte
	switch msg.Type {
	case bubbletea.KeyEsc:
		input = []byte{0x1b}
	case bubbletea.KeyEnter, bubbletea.KeyCtrlJ:
		input = []byte("\r")
	case bubbletea.KeyTab:
		input = []byte("\t")
	case bubbletea.KeyBackspace, bubbletea.KeyDelete:
		input = []byte{0x7f}
	case bubbletea.KeyUp, bubbletea.KeyDown, bubbletea.KeyLeft, bubbletea.KeyRight:
		input = []byte(arrowBytes(msg.Type))
	case bubbletea.KeyRunes, bubbletea.KeySpace:
		if msg.Alt {
			input = append(input, 0x1b)
		}
		if msg.Type == bubbletea.KeySpace && len(msg.Runes) == 0 {
			input = append(input, ' ')
		} else {
			input = append(input, []byte(string(msg.Runes))...)
		}
	}
	if len(input) == 0 {
		return m, nil
	}
	action, err := m.client.NewAction("terminal.input", map[string]any{"id": m.clientTerminalID, "bytes": input})
	if err != nil {
		m.append(errStyle.Render("terminal input: " + err.Error()))
		return m, nil
	}
	return m, func() bubbletea.Msg {
		result, commandErr := m.client.Command(context.Background(), action)
		return clientTerminalMsg{action: action, result: result, err: commandErr}
	}
}

func (m *model) thinPermissionKey(msg bubbletea.KeyMsg) (bubbletea.Model, bubbletea.Cmd) {
	dialog := m.permDialog
	if dialog == nil || dialog.daemon == nil || dialog.deciding {
		return m, nil
	}
	decide := func(allow bool, reason string) (bubbletea.Model, bubbletea.Cmd) {
		action, err := m.client.NewAction("permission.decide", map[string]any{
			"permission_id": dialog.daemon.ID, "allow": allow, "reason": reason,
		})
		if err != nil {
			m.append(errStyle.Render("permission: " + err.Error()))
			return m, nil
		}
		dialog.deciding = true
		m.clientInFlight++
		m.busy = true
		permissionID := dialog.daemon.ID
		return m, func() bubbletea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			result, decisionErr := m.client.DecidePermission(ctx, action, permissionID, allow, reason)
			return clientPermissionMsg{action: action, permissionID: permissionID, result: result, err: decisionErr}
		}
	}
	if dialog.rejecting {
		switch msg.Type {
		case bubbletea.KeyEnter:
			return decide(false, strings.TrimSpace(dialog.rejectIn))
		case bubbletea.KeyEsc:
			dialog.rejecting, dialog.rejectIn = false, ""
		case bubbletea.KeyBackspace:
			if len(dialog.rejectIn) > 0 {
				dialog.rejectIn = dialog.rejectIn[:len(dialog.rejectIn)-1]
			}
		case bubbletea.KeyRunes, bubbletea.KeySpace:
			dialog.rejectIn += string(msg.Runes)
			if msg.Type == bubbletea.KeySpace {
				dialog.rejectIn += " "
			}
		}
		return m, nil
	}
	switch msg.Type {
	case bubbletea.KeyLeft, bubbletea.KeyUp, bubbletea.KeyRight, bubbletea.KeyDown:
		dialog.sel = (dialog.sel + 1) % 2
	case bubbletea.KeyEnter:
		if dialog.sel == 0 {
			return decide(true, "")
		}
		dialog.rejecting = true
	case bubbletea.KeyRunes:
		switch string(msg.Runes) {
		case "a", "A":
			return decide(true, "")
		case "r":
			dialog.rejecting = true
		}
	case bubbletea.KeyEsc:
		return decide(false, "rejected without a reason")
	}
	return m, nil
}

func (m *model) openThinPalette() {
	commandItem := func(title, category, command string, suggested bool) paletteItem {
		return paletteItem{
			title: title, category: category, suggested: suggested,
			dynDesc: func(*model) string { return strings.TrimSpace(strings.TrimPrefix(command, "/")) },
			dynHint: func(*model) string { return command },
			run: func(value *model) (bubbletea.Model, bubbletea.Cmd) {
				value.palette = nil
				return value.thinCommand(command)
			},
		}
	}
	items := []paletteItem{
		commandItem("Model", "Agent", "/model", true),
		commandItem("Reasoning effort", "Agent", "/effort "+nextEffort(m.effortsFor(), m.displayEffort()), false),
		commandItem("Computer use", "Agent", "/computer-use status", false),
		commandItem("Resume session", "Session", "/resume", true),
		commandItem("Rewind conversation", "Session", "/rewind", true),
		commandItem("Fork session", "Session", "/fork", false),
		commandItem("Rename session", "Session", "/rename", false),
		commandItem("Clear conversation", "Session", "/clear", false),
		commandItem("Compact session", "Session", "/compact", true),
		commandItem("Context doctor", "Session", "/context-doctor", false),
		commandItem("Subagents", "Session", "/subagents", false),
		commandItem("Schedules", "Session", "/schedule list", false),
		commandItem("MCPs", "Session", "/mcp list", false),
		commandItem("Browser", "Session", "/browser status", false),
		{
			title: "Thinking tokens", category: "Display",
			dynDesc: func(*model) string { return "show or hide streamed reasoning" },
			dynHint: func(*model) string { return "ctrl+o" },
			run: func(value *model) (bubbletea.Model, bubbletea.Cmd) {
				value.palette = nil
				value.toggleThinking()
				return value, nil
			},
		},
		commandItem("Mouse capture", "Display", "/mouse", false),
		commandItem("Help", "App", "/help", false),
		{
			title: "Quit", category: "App",
			dynDesc: func(*model) string { return "exit whip" },
			dynHint: func(*model) string { return "/quit" },
			run: func(value *model) (bubbletea.Model, bubbletea.Cmd) {
				value.palette = nil
				return value, bubbletea.Quit
			},
		},
	}
	m.palette = &palette{all: items}
	m.palette.applyFilter(m)
}

func (m *model) openThinThemePalette() {
	var items []paletteItem
	for _, theme := range []string{"auto", "light", "dark"} {
		theme := theme
		items = append(items, paletteItem{
			title: "Theme: " + theme, category: "Display",
			dynDesc: func(*model) string { return "switch terminal colors to " + theme },
			dynHint: func(*model) string { return "/theme " + theme },
			run: func(value *model) (bubbletea.Model, bubbletea.Cmd) {
				value.palette = nil
				value.setTheme(theme)
				return value, nil
			},
		})
	}
	m.palette = &palette{all: items}
	m.palette.applyFilter(m)
}

func (m *model) thinCommand(text string) (bubbletea.Model, bubbletea.Cmd) {
	fields := strings.Fields(text)
	name := strings.TrimPrefix(fields[0], "/")
	args := strings.TrimSpace(strings.TrimPrefix(text, fields[0]))
	switch name {
	case "help", "mouse", "export", "report":
		return m.command(text)
	case "auth":
		m.authCommand(fields[1:])
		return m, nil
	case "me":
		return m, m.openMe()
	case "memory":
		m.memoryCommand(fields[1:])
		return m, nil
	case "theme":
		if args == "" {
			m.openThinThemePalette()
			return m, nil
		}
		return m.command(text)
	case "quit", "exit", "q":
		return m, bubbletea.Quit
	case "goal-from-context":
		if args != "" {
			window, parseErr := strconv.Atoi(args)
			if parseErr != nil || window < 2 {
				m.append(errStyle.Render("usage: /goal-from-context [n] — n ≥ 2 messages of context"))
				return m, nil
			}
		}
		return m.submitClientAction("goal.from-context", map[string]string{"args": args}, "")
	case "model", "model-for-session":
		if args == "" {
			m.openModelPicker(name == "model-for-session")
			return m, nil
		}
		return m.submitClientAction("session.model", map[string]string{"args": args}, "")
	case "rewind":
		if args == "" {
			m.openRewind()
			return m, nil
		}
		return m.submitClientAction("history.rewind", map[string]string{"args": args}, "")
	case "resume":
		if args == "" {
			return m.submitClientAction("session.list", map[string]string{}, "")
		}
		return m.submitClientAction("session.open", map[string]string{"args": args}, "")
	case "rename":
		if args == "" {
			m.openClientNamePrompt("✎ session name:", m.sessTitle, "session.rename", 0)
			return m, nil
		}
		return m.submitClientAction("session.rename", map[string]string{"args": args}, "")
	case "fork":
		if args == "" {
			suggestion := strings.TrimSpace(m.sessTitle)
			if suggestion == "" {
				suggestion = "session"
			}
			m.openClientNamePrompt("⑂ fork name:", suggestion+" (fork)", "session.fork", 0)
			return m, nil
		}
		return m.submitClientAction("session.fork", map[string]string{"args": args}, "")
	case "computer", "computer-use":
		fields := strings.Fields(args)
		if len(fields) > 0 && fields[0] != "status" && fields[0] != "allow" && fields[0] != "deny" {
			instruction := computerUseInstruction(args)
			return m.submitClientAction("submit", map[string]string{"text": instruction}, args)
		}
		return m.submitClientAction("computer.control", map[string]string{"args": args}, "")
	case "subagent":
		if args == "" {
			m.append(errStyle.Render("/subagent [-m model[@provider]] <prompt>"))
			return m, nil
		}
		if fields := strings.Fields(args); len(fields) == 2 && fields[0] == "stop" {
			return m.submitClientAction("agent.control", map[string]string{"args": args}, "")
		}
		return m.submitClientAction("agent.start", map[string]string{"args": args}, "")
	case "goal":
		switch args {
		case "":
			if m.goal == "" {
				m.append(dimStyle.Render("no goal set — /goal <text> to set one"))
			} else {
				m.append(dimStyle.Render("◎ goal: " + m.goal))
			}
			return m, nil
		case "resume":
			if m.goal == "" {
				m.append(errStyle.Render("no goal to resume — set one with /goal <text>"))
				return m, nil
			}
			return m.submitClientAction("submit", map[string]string{"text": goalContinuePrompt(m.goal)}, "")
		case "clear":
			return m.submitClientAction("goal.set", map[string]string{"args": "clear"}, "")
		default:
			return m.submitClientAction("goal.run", map[string]string{"args": args}, "")
		}
	case "steer":
		if args == "" {
			m.append(errStyle.Render("/steer <message>"))
			return m, nil
		}
		return m.submitClientAction("steer", map[string]string{"text": args}, args)
	}
	operations := map[string]string{
		"schedule": "schedule.manage",
		"cd":       "workspace.set", "pwd": "workspace.inspect",
		"effort": "session.effort", "clear": "history.clear", "compact": "history.compact",
		"tasks": "agents.list", "subagents": "agents.list",
		"mcp": "mcp.control", "lsp": "lsp.control",
		"browser": "browser.control", "context-doctor": "context.inspect",
		"budget": "budget.cap", "capability": "capability.revoke", "delete-agent": "agent.delete",
	}
	operation, ok := operations[name]
	if !ok {
		m.append(errStyle.Render("unknown command: /" + name))
		return m, nil
	}
	return m.submitClientAction(operation, map[string]string{"args": args}, "")
}

var _ daemonConnection = (*daemon.Client)(nil)
