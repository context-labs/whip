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
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	bubbletea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/update"
)

type ClientState = daemon.RootClientState

const (
	ClientDisconnected = daemon.RootDisconnected
	ClientReconnecting = daemon.RootReconnecting
	ClientSnapshotting = daemon.RootSnapshotting
	ClientLive         = daemon.RootLive
)

type Action = daemon.RootAction
type ClientUpdate = daemon.RootUpdate
type daemonConnection = daemon.RootConnection
type clientConnector = daemon.RootConnector
type ClientOptions = daemon.RootClientOptions
type Client = daemon.RootClient

func NewClient(options ClientOptions) (*Client, error) { return daemon.NewRootClient(options) }

// clientPresentation is the daemon-fed state needed to render a terminal. It
// deliberately has no provider client, tool registry, scheduler, store, or
// process handles.
type clientPresentation struct {
	modelID            string
	effort             string
	workingDir         string
	contextLimit       int
	usage              llm.Usage
	messages           []llm.Message
	agents             []session.RuntimeAgent
	inbox              []session.InboxItem
	blackboard         []session.StateValue
	budgets            []session.SnapshotBudget
	capabilities       []session.CapabilityRecord
	schedules          []session.Schedule
	permissions        []session.PermissionSnapshot
	presentation       []session.SnapshotEvent
	agentPresentations map[string][]session.SnapshotEvent
}

// Run starts the presentation-only TUI. Agent loops, persistence, schedulers,
// providers, permissions, and child processes remain in the daemon.
func Run(cfg *config.Config, modelName, provName, sysPrompt, resumeID string, cautious, firstRun bool, initialPrompt string) (string, error) {
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
		create = &daemon.CreateSession{Kind: session.SessionKindAgent, CWD: cwd(), Model: modelName, Provider: provName}
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
		input: newInput(), spin: spinner.New(spinner.WithSpinner(spinner.Dot)), follow: true,
		catalogs: catalogs, mouseOn: mouseOn, now: time.Now, showThinking: showThinking,
		sidebarHide:   cfg.Sidebar != nil && !*cfg.Sidebar,
		initialPrompt: initialPrompt, cfgExtra: map[string]string{},
		agentMessages: map[string][]llm.Message{},
	}
	m.updateLatest = update.Pending(Version)
	m.themeHow = m.applyTheme(cfg.Theme)
	loadUserThemes()
	m.applyOpencodeStyles()
	m.startupReport()
	m.append(dimStyle.Render("daemon: connecting…"))
	if identityWarning != "" {
		m.append(errStyle.Render(identityWarning))
	}

	// alt screen and mouse mode are View fields; the filter thins mouse motion
	options := []bubbletea.ProgramOption{bubbletea.WithFilter(newInputFilter().Filter)}
	if info, statErr := os.Stat(filepath.Join(home, "config.json")); statErr == nil {
		m.cfgMod = info.ModTime()
	}
	program := bubbletea.NewProgram(m, options...)
	m.prog = program
	client.Start()
	permissionCtx, cancelPermission := context.WithTimeout(context.Background(), 10*time.Second)
	if err := configureInteractiveSession(permissionCtx, client, cautious); err != nil {
		cancelPermission()
		_ = client.Close()
		return "", err
	}
	cancelPermission()
	tuiRunning = true
	_, runErr := program.Run()
	tuiRunning = false
	closeErr := client.Close()
	rootID := client.RootID()
	if resumeID == "" && !m.clientTouched {
		rootID = ""
	}
	return rootID, errors.Join(runErr, closeErr)
}

func configureInteractiveSession(ctx context.Context, client *Client, cautious bool) error {
	if err := client.WaitLive(ctx); err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	if cautious {
		action, err := client.NewAction("permission.mode", map[string]bool{"external_permissions": true})
		if err != nil {
			return fmt.Errorf("configure cautious permission mode: %w", err)
		}
		result, err := client.SetPermissionMode(ctx, action, true)
		if err != nil {
			return fmt.Errorf("configure cautious permission mode: %w", err)
		}
		if result.Status != "succeeded" {
			return fmt.Errorf("configure cautious permission mode: %s", result.Error)
		}
	}
	action, err := client.NewAction("session.autotitle", map[string]bool{"enabled": true})
	if err != nil {
		return fmt.Errorf("configure automatic titles: %w", err)
	}
	result, err := client.Command(ctx, action)
	if err != nil {
		return fmt.Errorf("configure automatic titles: %w", err)
	}
	if result.Status != "succeeded" {
		return fmt.Errorf("configure automatic titles: %s", result.Error)
	}
	return nil
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
			return clientUpdateMsg{ClientUpdate{State: ClientDisconnected, StateChanged: true, Err: netClosedError{}}, true}
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
	follow, offset, selection := m.follow, m.vp.YOffset(), m.sel
	selectedAgentID := ""
	if rows := m.runtimeAgentRows(); m.agentsFocus && m.agentSel >= 0 && m.agentSel < len(rows) {
		selectedAgentID = rows[m.agentSel].agent.ID
	}
	if m.sessionID != "" && m.sessionID != snapshot.RootID {
		m.agentOpen = ""
		m.agentMessages = map[string][]llm.Message{}
		m.terminalAgentID, m.terminalMarker = "", ""
		m.repl = nil // REPL history is per session
	}
	m.sessionID = snapshot.RootID
	m.clientCursor = snapshot.Cursor
	m.applyClientRoute(snapshot.Meta.Model, snapshot.Meta.Provider)
	m.goal, m.sessTitle = snapshot.Meta.Goal, snapshot.Meta.Title
	m.clientView.workingDir = snapshot.Meta.CWD
	m.applyStoredEffort(snapshot.Meta.Effort)
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
	m.clientView.permissions = append([]session.PermissionSnapshot(nil), snapshot.Permissions...)
	m.clientView.presentation = append([]session.SnapshotEvent(nil), snapshot.Presentation...)
	m.clientView.agentPresentations = snapshot.AgentPresentations
	m.replRebuild()
	if selectedAgentID != "" {
		for index, row := range m.runtimeAgentRows() {
			if row.agent.ID == selectedAgentID {
				m.agentSel = index
				break
			}
		}
	}
	m.rebuildClientTranscript()
	m.restoreTerminalMarker()
	m.applyClientPermissions(snapshot.Permissions)
	m.busy = m.visibleAgentBusy()
	m.input.SetValue(draft)
	m.input.CursorEnd()
	m.follow, m.sel = follow, selection
	m.refreshVP()
	if !follow {
		m.vp.SetYOffset(offset)
	}
}

func (m *model) applyClientRoute(modelName, providerName string) {
	if modelName != "" {
		m.modelName = modelName
	}
	if providerName != "" {
		m.provName = providerName
	}
	if m.cfg == nil {
		return
	}
	m.clientView.modelID, m.clientView.contextLimit = "", 0
	_, modelConfig, apiID, err := m.cfg.Resolve(m.modelName, m.provName)
	if err != nil {
		return
	}
	m.clientView.modelID = apiID
	m.clientView.contextLimit = modelConfig.ContextWindow()
	if catalog, ok := m.catalogs[m.provName]; ok {
		m.clientView.contextLimit = max(m.clientView.contextLimit, catalog.ContextLength(apiID))
	}
}

func (m *model) applyStoredEffort(stored string) {
	switch stored {
	case "off":
		m.clientView.effort = ""
	case "":
		if m.cfg == nil {
			m.clientView.effort = ""
			return
		}
		m.clientView.effort = DefaultEffortFor(m.catalogs, m.provName, m.displayModelID(), m.cfg.DefaultEffort)
	default:
		m.clientView.effort = stored
	}
}

func (m *model) rebuildClientTranscript() {
	expanded := map[string]bool{}
	for _, value := range m.blocks {
		if value.expanded {
			expanded[blockExpansionKey(value)] = true
		}
	}
	m.blocks, m.msgBlock = nil, nil
	m.clientTerminalID, m.iactive = "", nil
	m.current, m.curThink, m.inMsg, m.inThink = "", "", false, false
	messages := m.clientView.messages
	base := 1
	presentation := m.clientView.presentation
	if m.agentOpen != "" {
		messages = m.agentMessages[m.agentOpen]
		base = 0
		presentation = m.clientView.agentPresentations[m.agentOpen]
	}
	m.seedTranscript(messages, base)
	for _, item := range m.clientView.inbox {
		if item.AgentID != m.visibleAgentID() {
			continue
		}
		if text := pendingUserText(item); text != "" {
			m.appendRaw(blockUser, linkifyFilePathsAt(text, m.completionRoot()))
		}
	}
	for _, event := range presentation {
		_, _ = m.applyClientStream(event.Kind, event.Payload)
	}
	for i := range m.blocks {
		if expanded[blockExpansionKey(m.blocks[i])] {
			m.blocks[i].expanded, m.blocks[i].stale = true, true
		}
	}
}

func pendingUserText(item session.InboxItem) string {
	switch item.Kind {
	case "submit", "steer":
		return string(item.Payload.Inline)
	case "submit.parts", "steer.parts":
		var payload daemon.SubmitPayload
		if json.Unmarshal(item.Payload.Inline, &payload) != nil {
			return ""
		}
		if payload.Text != "" {
			return payload.Text
		}
		if len(payload.Parts) > 0 {
			return "[image attachment]"
		}
	}
	return ""
}

func blockExpansionKey(value block) string {
	if value.toolID != "" {
		return fmt.Sprintf("%d:%s", value.kind, value.toolID)
	}
	return fmt.Sprintf("%d:%s", value.kind, value.text)
}

func (m *model) restoreTerminalMarker() {
	if m.terminalMarker != "" && m.terminalAgentID == m.visibleAgentID() {
		if len(m.blocks) > 0 && m.blocks[len(m.blocks)-1].text == m.terminalMarker {
			return
		}
		m.append(m.terminalMarker)
	}
}

func (m *model) recordTurnFailure(action Action, message string) {
	if message == "" {
		message = "turn failed"
	}
	agentID := m.rootAgentID()
	if action.Operation == "agent.submit" {
		var payload struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(action.Payload, &payload) == nil && payload.ID != "" {
			agentID = payload.ID
		}
	}
	m.clientTurnError = message
	m.terminalAgentID = agentID
	m.terminalMarker = errStyle.Render("error: " + message)
	m.restoreTerminalMarker()
}

func runtimeAgentLine(value session.RuntimeAgent) string {
	phase := value.LifecyclePhase
	if phase == "" {
		phase = value.Status
	}
	name := value.Name
	if name == "" {
		name = shortAgentID(value.ID)
	} else {
		name += " (" + shortAgentID(value.ID) + ")"
	}
	line := fmt.Sprintf("⚙ %s — %s", name, phase)
	if value.BlockingReason != "" {
		line += " · blocked: " + value.BlockingReason
	}
	if value.TerminalCause != "" {
		line += " · terminal: " + value.TerminalCause
	}
	if value.PendingMail > 0 {
		line += fmt.Sprintf(" · mail %d", value.PendingMail)
	}
	return line
}

// shortAgentID abbreviates "<root>:<suffix>" ids to "ba06…c16d" for one-line
// displays; agentDetails keeps the full id.
func shortAgentID(id string) string {
	if colon := strings.LastIndexByte(id, ':'); colon >= 0 {
		id = id[colon+1:]
	}
	if len(id) > 12 {
		id = id[:4] + "…" + id[len(id)-4:]
	}
	return id
}

func (m *model) displayModelID() string {
	return m.clientView.modelID
}

func (m *model) displayEffort() string {
	return m.clientView.effort
}

func (m *model) displayUsage() llm.Usage {
	return m.clientView.usage
}

func (m *model) displayMessages() []llm.Message {
	return m.clientView.messages
}

func (m *model) displayContextLimit() int {
	return m.clientView.contextLimit
}

func (m *model) recordClientStream(event daemon.ProtocolEvent) {
	if !strings.HasPrefix(event.Kind, "stream.") {
		return
	}
	var payload daemon.StreamEvent
	if json.Unmarshal(event.Payload, &payload) != nil {
		return
	}
	value := session.SnapshotEvent{Seq: event.Seq, Kind: event.Kind, Payload: append([]byte(nil), event.Payload...)}
	owner := payload.AgentID
	if owner == "" {
		owner = m.rootAgentID()
	}
	m.replApplySeq(owner, event.Kind, payload, event.Seq)
	if owner == "" || owner == m.rootAgentID() {
		m.clientView.presentation = append(m.clientView.presentation, value)
		return
	}
	if m.clientView.agentPresentations == nil {
		m.clientView.agentPresentations = make(map[string][]session.SnapshotEvent)
	}
	m.clientView.agentPresentations[owner] = append(m.clientView.agentPresentations[owner], value)
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
	owner := event.AgentID
	if owner == "" {
		owner = m.rootAgentID()
	}
	if owner != m.visibleAgentID() {
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
	case "stream.cell.host":
		return true, nil // the REPL panel consumes it; the transcript shows the cell
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
	case "stream.usage":
		var usage daemon.UsageEvent
		if err := json.Unmarshal([]byte(event.Result), &usage); err != nil {
			m.append(errStyle.Render("daemon usage: " + err.Error()))
			return true, nil
		}
		m.lastResp = usage.Usage
		if usage.Size > 0 {
			m.clientView.contextLimit = usage.Size
		}
		return true, nil
	case "stream.plan":
		var plan daemon.PlanEvent
		if err := json.Unmarshal([]byte(event.Result), &plan); err != nil {
			m.append(errStyle.Render("daemon plan: " + err.Error()))
			return true, nil
		}
		m.plan = append(m.plan[:0], plan.Items...)
		return true, nil
	default:
		m.append(dimStyle.Render("daemon sent unsupported stream event " + kind))
		return true, nil
	}
	_, command := m.Update(message)
	return true, command
}

func (m *model) rootAgentID() string {
	for _, value := range m.clientView.agents {
		if value.ParentID == "" {
			return value.ID
		}
	}
	return ""
}

func (m *model) visibleAgentID() string {
	if m.agentOpen != "" {
		return m.agentOpen
	}
	return m.rootAgentID()
}

func (m *model) visibleAgentBusy() bool {
	id := m.visibleAgentID()
	for _, value := range m.clientView.agents {
		if value.ID == id && value.LifecyclePhase == "running" {
			return true
		}
	}
	for _, item := range m.clientView.inbox {
		if item.AgentID == id && (item.Status == "queued" || item.Status == "running") {
			return true
		}
	}
	return false
}

func (m *model) setAgentLifecycle(id, phase string) bool {
	for index := range m.clientView.agents {
		if m.clientView.agents[index].ID == id {
			m.clientView.agents[index].LifecyclePhase = phase
			return true
		}
	}
	return false
}

func (m *model) upsertInbox(agentID string, seq int64, kind, status string) {
	if agentID == "" || seq < 1 {
		return
	}
	for index := range m.clientView.inbox {
		item := &m.clientView.inbox[index]
		if item.AgentID == agentID && item.Seq == seq {
			if kind != "" {
				item.Kind = kind
			}
			if status != "" {
				item.Status = status
			}
			return
		}
	}
	m.clientView.inbox = append(m.clientView.inbox, session.InboxItem{
		RootID: m.sessionID, AgentID: agentID, Seq: seq, Kind: kind, Status: status,
	})
}

func (m *model) startAgentInbox(agentID string, inboxSeq int64) {
	if inboxSeq > 0 {
		m.upsertInbox(agentID, inboxSeq, "", "running")
		return
	}
	claimed := 0
	for index := range m.clientView.inbox {
		item := &m.clientView.inbox[index]
		if item.AgentID == agentID && item.Status == "queued" && claimed < session.MaxInboxBatch {
			item.Status = "running"
			claimed++
		}
	}
}

func (m *model) finishAgentInbox(agentID string, acknowledged []int64) {
	consumed := make(map[int64]bool, len(acknowledged))
	for _, seq := range acknowledged {
		consumed[seq] = true
	}
	kept := m.clientView.inbox[:0]
	for _, item := range m.clientView.inbox {
		if item.AgentID != agentID {
			kept = append(kept, item)
			continue
		}
		if consumed[item.Seq] {
			continue
		}
		if item.Status == "running" {
			continue
		}
		kept = append(kept, item)
	}
	m.clientView.inbox = kept
}

func (m *model) replaceAgentInbox(agentID string, inbox []session.InboxItem) {
	kept := m.clientView.inbox[:0]
	for _, item := range m.clientView.inbox {
		if item.AgentID != agentID {
			kept = append(kept, item)
		}
	}
	m.clientView.inbox = append(kept, inbox...)
}

func mergePresentation(snapshot, current []session.SnapshotEvent, cursor int64) []session.SnapshotEvent {
	merged := append([]session.SnapshotEvent(nil), snapshot...)
	for _, event := range current {
		if event.Seq > cursor {
			merged = append(merged, event)
		}
	}
	return merged
}

func (m *model) openAgent(result daemon.AgentTranscriptResult) {
	m.agentOpen = result.Agent.ID
	m.agentMessages[result.Agent.ID] = append([]llm.Message(nil), result.Messages...)
	if m.clientView.agentPresentations == nil {
		m.clientView.agentPresentations = make(map[string][]session.SnapshotEvent)
	}
	m.clientView.agentPresentations[result.Agent.ID] = mergePresentation(
		result.Presentation, m.clientView.agentPresentations[result.Agent.ID], result.Cursor,
	)
	m.replRebuild()
	m.replaceAgentInbox(result.Agent.ID, result.Inbox)
	m.plan = nil
	m.rebuildClientTranscript()
	m.restoreTerminalMarker()
	m.busy = m.visibleAgentBusy()
	m.turnStart = time.Time{}
	if m.busy {
		m.turnStart = m.nowFn()
	}
	m.follow = true
	m.refreshVP()
}

func (m *model) closeAgent() {
	if m.agentOpen == "" {
		return
	}
	m.agentOpen = ""
	m.plan = nil
	m.rebuildClientTranscript()
	m.restoreTerminalMarker()
	m.busy = m.visibleAgentBusy()
	m.turnStart = time.Time{}
	if m.busy {
		m.turnStart = m.nowFn()
	}
	m.follow = true
	m.refreshVP()
}

func (m *model) cancelVisibleTurn() (bubbletea.Model, bubbletea.Cmd) {
	if m.agentOpen != "" {
		return m.submitClientAction("agent.turn.cancel", map[string]string{"id": m.agentOpen}, "")
	}
	return m.submitClientAction("cancel", map[string]any{}, "")
}

func (m *model) applyClientLifecycle(kind string, payload []byte) (bool, bubbletea.Cmd) {
	if strings.HasPrefix(kind, "session.") && strings.HasSuffix(kind, ".updated") {
		var event daemon.SessionUpdateEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			return false, nil
		}
		if event.Title != "" {
			m.sessTitle = event.Title
		}
		if event.Model != "" || event.Provider != "" {
			m.applyClientRoute(event.Model, event.Provider)
		}
		if event.EffortChanged {
			m.applyStoredEffort(event.Effort)
		}
		if event.WorkingDir != "" {
			m.clientView.workingDir = event.WorkingDir
		}
		return true, nil
	}
	var event session.LifecycleEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return false, nil
	}
	rootAgent := m.rootAgentID()
	isRoot := event.AgentID == "" || rootAgent == "" || event.AgentID == rootAgent
	switch kind {
	case "inbox.queued":
		m.upsertInbox(event.AgentID, event.InboxSeq, event.InboxKind, "queued")
		return true, nil
	case "inbox.consumed":
		m.finishAgentInbox(event.AgentID, []int64{event.InboxSeq})
		return true, nil
	case "scratch.restored":
		m.replRestart(event.AgentID, len(event.Restored), len(event.NotRestored))
		if event.AgentID == m.visibleAgentID() {
			line := "(worker restarted; scratch restored: " + strings.Join(event.Restored, ", ")
			if len(event.Restored) == 0 {
				line = "(worker restarted; no scratch restored"
			}
			if len(event.NotRestored) > 0 {
				parts := make([]string, 0, len(event.NotRestored))
				for _, item := range event.NotRestored {
					parts = append(parts, item.Name+" ("+item.Reason+")")
				}
				line += "; not restored: " + strings.Join(parts, ", ")
			}
			m.append(dimStyle.Render(line + ")"))
		}
		return true, nil
	case "turn.started":
		phase := event.Phase
		if phase == "" {
			phase = "running"
		}
		m.setAgentLifecycle(rootAgent, phase)
		m.startAgentInbox(rootAgent, event.InboxSeq)
		if m.agentOpen != "" {
			return true, nil
		}
		m.busy = true
		m.clientTurnError = ""
		m.terminalAgentID, m.terminalMarker = "", ""
		m.plan = nil
		if m.turnStart.IsZero() {
			m.turnStart = m.nowFn()
		}
		return true, nil
	case "turn.succeeded", "turn.failed", "turn.cancelled", "turn.interrupted":
		if !isRoot {
			return true, nil
		}
		phase := event.Phase
		if phase == "" {
			phase = "idle"
		}
		m.setAgentLifecycle(rootAgent, phase)
		m.finishAgentInbox(rootAgent, event.Acknowledged)
		if m.agentOpen != "" {
			_, transcriptCommand := m.submitClientAction("agent.transcript", map[string]string{"id": rootAgent}, "")
			return true, transcriptCommand
		}
		m.flushThink()
		m.flushCurrent()
		m.busy = false
		m.interrupt1 = false
		m.turnStart = time.Time{}
		m.plan = nil
		m.clientTurnError = event.Error
		m.terminalAgentID, m.terminalMarker = rootAgent, ""
		switch kind {
		case "turn.failed":
			if event.Error == "" {
				event.Error = "turn failed"
			}
			m.terminalMarker = errStyle.Render("error: " + event.Error)
		case "turn.cancelled":
			m.terminalMarker = dimStyle.Render("(interrupted)")
		case "turn.interrupted":
			m.terminalMarker = dimStyle.Render("(interrupted — effects may be uncertain)")
		}
		m.restoreTerminalMarker()
		_, transcriptCommand := m.submitClientAction("agent.transcript", map[string]string{"id": rootAgent}, "")
		return true, transcriptCommand
	case "agent.turn.started", "agent.turn.succeeded", "agent.turn.failed", "agent.turn.cancelled", "agent.turn.interrupted":
		phase := event.Phase
		if phase == "" && kind == "agent.turn.started" {
			phase = "running"
		} else if phase == "" {
			phase = "idle"
		}
		if !m.setAgentLifecycle(event.AgentID, phase) {
			return true, m.requestClientSnapshot()
		}
		if event.AgentID != m.agentOpen {
			return true, nil
		}
		if kind == "agent.turn.started" {
			m.startAgentInbox(event.AgentID, event.InboxSeq)
			m.busy = true
			m.clientTurnError = ""
			m.terminalAgentID, m.terminalMarker = "", ""
			m.plan = nil
			if m.turnStart.IsZero() {
				m.turnStart = m.nowFn()
			}
			return true, nil
		}
		m.finishAgentInbox(event.AgentID, event.Acknowledged)
		m.flushThink()
		m.flushCurrent()
		m.busy = false
		m.interrupt1 = false
		m.turnStart = time.Time{}
		m.plan = nil
		m.clientTurnError = event.Error
		m.terminalAgentID, m.terminalMarker = event.AgentID, ""
		switch kind {
		case "agent.turn.failed":
			if event.Error == "" {
				event.Error = "turn failed"
			}
			m.terminalMarker = errStyle.Render("error: " + event.Error)
		case "agent.turn.cancelled":
			m.terminalMarker = dimStyle.Render("(interrupted)")
		case "agent.turn.interrupted":
			m.terminalMarker = dimStyle.Render("(interrupted — effects may be uncertain)")
		}
		m.restoreTerminalMarker()
		_, transcriptCommand := m.submitClientAction("agent.transcript", map[string]string{"id": event.AgentID}, "")
		return true, transcriptCommand
	case "command.queued", "command.running", "command.succeeded", "command.failed", "command.cancelled", "command.interrupted",
		"command.control.queued":
		return true, nil
	case "session.reload.failed":
		if event.Error == "" {
			event.Error = "session reload failed"
		}
		m.append(errStyle.Render("session reload: " + event.Error))
		return true, nil
	case "permission.auto_approved":
		if event.AgentID == m.visibleAgentID() {
			m.append(dimStyle.Render("(auto-approved " + event.Operation + " " + event.Command + " by " + event.RuleSource + " rule " + event.Rule + ")"))
		}
		return true, nil
	}
	return false, nil
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
		m.appendRaw(blockUser, linkifyFilePathsAt(echo, m.completionRoot()))
	}
	if operation == "submit" || operation == "steer" || operation == "agent.submit" {
		m.clientTouched = true
	}
	m.clientInFlight++
	if operation == "submit" || operation == "steer" || operation == "agent.submit" {
		m.busy = true
		m.turnStart = m.nowFn()
	}
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

func (m *model) thinKey(msg bubbletea.KeyPressMsg) (bubbletea.Model, bubbletea.Cmd) {
	if d := m.topDialog(); d != nil { // what is drawn on top owns the keyboard, even over a permission or name prompt
		return d.key(m, msg)
	}
	if m.permDialog != nil && m.permDialog.daemon != nil {
		return m.thinPermissionKey(msg)
	}
	if m.iactive != nil && m.clientTerminalID != "" {
		return m.thinInteractiveKey(msg)
	}
	if m.namePrompt != nil {
		if m.clientPromptOp == "" {
			switch msg.String() {
			case "esc", "ctrl+c":
				m.closeNamePrompt()
				return m, nil
			case "enter":
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
		switch msg.String() {
		case "esc", "ctrl+c":
			m.closeNamePrompt()
			m.clientPromptOp, m.clientPromptCut = "", 0
			return m, nil
		case "enter":
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
	if m.rew != nil {
		return m.rewindKey(msg)
	}
	if key := msg.String(); key == "ctrl+j" || key == "alt+enter" || key == "shift+enter" {
		maxHeight := m.input.MaxHeight
		m.input.MaxHeight = 0
		var command bubbletea.Cmd
		m.input, command = m.input.Update(bubbletea.KeyPressMsg{Code: 'j', Mod: bubbletea.ModCtrl})
		m.input.MaxHeight = maxHeight
		m.input.SetHeight(maxHeight)
		value := m.input.Value()
		m.input.SetValue(value)
		m.input.CursorEnd()
		m.refreshMenu()
		return m, command
	}
	{ // ctrl+x leader chords
		if !m.leaderAt.IsZero() && m.nowFn().Sub(m.leaderAt) < 2*time.Second {
			m.leaderAt = time.Time{}
			if msg.String() == "esc" {
				return m, nil
			}
			if next, command, handled := m.ocLeaderChord(msg.String()); handled {
				return next, command
			}
		} else if msg.String() == "ctrl+x" && !m.agentsFocus { // focused tree: ctrl+x stops the agent
			m.leaderAt = m.nowFn()
			return m, nil
		}
	}
	children := m.runtimeChildren()
	if m.agentsFocus && len(children) == 0 {
		m.agentsFocus = false
	}
	if m.agentsFocus {
		switch msg.String() {
		case "ctrl+t":
			m.agentsFocus = false
			return m, nil
		case "esc": // esc means "back": drop focus and leave an open child
			m.agentsFocus = false
			if m.agentOpen != "" && strings.TrimSpace(m.input.Value()) == "" {
				m.closeAgent()
			}
			return m, nil
		case "up":
			if m.agentSel == 0 {
				m.agentsFocus = false
			} else {
				m.agentSel--
			}
			return m, nil
		case "down":
			m.agentSel = min(m.agentSel+1, len(children)-1)
			return m, nil
		case "ctrl+x":
			if len(children) == 0 {
				m.agentsFocus = false
				return m, nil
			}
			child := children[min(m.agentSel, len(children)-1)]
			if child.ParentID == "" {
				return m, nil // the root is not stoppable from the tree
			}
			return m.submitClientAction("agent.control", map[string]string{"args": "stop " + child.ID}, "")
		case "enter":
			if len(children) > 0 {
				child := children[min(m.agentSel, len(children)-1)]
				m.agentsFocus = false
				if child.ParentID == "" { // the root row: back to the main transcript
					m.closeAgent()
					return m, nil
				}
				return m.submitClientAction("agent.transcript", map[string]string{"id": child.ID}, "")
			}
			return m, nil
		default:
			m.agentsFocus = false
		}
	}
	switch msg.String() {
	case "ctrl+c":
		if m.busy && m.clientState == ClientLive {
			if !m.interrupt1 {
				m.interrupt1 = true
				return m, nil
			}
			return m.cancelVisibleTurn()
		}
		if m.quit1 {
			m.quit1 = false
			return m, bubbletea.Quit
		}
		m.quit1 = true
		return m, bubbletea.Tick(2*time.Second, func(time.Time) bubbletea.Msg { return quitArmMsg{} })
	case "esc":
		if m.menu != nil {
			if m.menu.cyc {
				m.input.SetValue(m.menu.base)
			}
			m.menu = nil
			return m, nil
		}
		if m.agentOpen != "" && strings.TrimSpace(m.input.Value()) == "" {
			m.closeAgent()
			return m, nil
		}
		if m.busy && strings.TrimSpace(m.input.Value()) == "" && m.clientState == ClientLive {
			return m.cancelVisibleTurn()
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
	case "pgup", "pgdown":
		var command bubbletea.Cmd
		m.vp, command = m.vp.Update(msg)
		m.follow = m.vp.AtBottom()
		return m, command
	case "ctrl+v":
		return m, pasteImageCmd
	case "ctrl+o":
		m.toggleThinking()
		return m, nil
	case "ctrl+e":
		for i := len(m.blocks) - 1; i >= 0; i-- {
			if m.blocks[i].kind == blockTool {
				m.blocks[i].toggle()
				m.refreshVP()
				break
			}
		}
		return m, nil
	case "ctrl+p":
		m.openThinPalette()
		return m, nil
	case "tab":
		if m.menu != nil {
			m.menuCycle(1)
		} else {
			m.openMenu()
		}
		return m, nil
	case "shift+tab":
		if m.menu != nil {
			m.menuCycle(-1)
		}
		return m, nil
	case "ctrl+k":
		return m.thinCommand("/clear")
	case "ctrl+t":
		if len(children) > 0 {
			m.agentsFocus = true
			m.agentSel = max(m.agentSel, m.firstChildSel())
			m.clampAgentSel()
		}
		return m, nil
	case "down":
		if m.menu != nil {
			m.menu.idx = (m.menu.idx + 1) % len(m.menu.cands)
			return m, nil
		}
		if strings.TrimSpace(m.input.Value()) == "" && len(children) > 0 {
			m.agentsFocus = true
			m.agentSel = m.firstChildSel()
			return m, nil
		}
		if !m.cursorOnLastLine() {
			var command bubbletea.Cmd
			m.input, command = m.input.Update(msg)
			return m, command
		}
		m.histNext()
		return m, nil
	case "up":
		if m.menu != nil {
			m.menu.idx = (m.menu.idx + len(m.menu.cands) - 1) % len(m.menu.cands)
			return m, nil
		}
		if !m.cursorOnFirstLine() {
			m.lastUp = m.nowFn()
			var command bubbletea.Cmd
			m.input, command = m.input.Update(msg)
			return m, command
		}
		if m.nowFn().Sub(m.lastUp) < 300*time.Millisecond {
			m.lastUp = m.nowFn()
			return m, nil
		}
		m.lastUp = m.nowFn()
		m.histPrev()
		return m, nil
	case "enter":
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
		if m.pasteBuf != "" {
			placeholder := fmt.Sprintf("[Pasted ~%d lines]", strings.Count(m.pasteBuf, "\n")+1)
			text = strings.Replace(text, placeholder, strings.TrimSpace(m.pasteBuf), 1)
			m.pasteBuf = ""
		}
		if m.clientState != ClientLive {
			m.append(errStyle.Render("daemon is " + m.clientState.String() + " — draft preserved"))
			return m, nil
		}
		if text != "" && !strings.HasPrefix(text, "/") && !strings.HasPrefix(text, "!") && m.visibleAgentReadOnly() {
			m.append(errStyle.Render("this agent is read-only because it has stopped"))
			return m, nil
		}
		if m.busy {
			switch {
			case text != "" && clientCommandRunsWhileBusy(text):
				if !strings.HasPrefix(text, "/auth ") {
					m.hist = append(m.hist, text)
					m.histIdx = len(m.hist)
				}
				m.input.Reset()
				return m.thinCommand(text)
			case strings.HasPrefix(text, "/") || strings.HasPrefix(text, "!"):
				// Mutating commands and shell wait for the turn; the draft stays.
				m.append(dimStyle.Render("(waits for the current turn — press enter again when idle)"))
				return m, nil
			case text != "":
				// Typed text is durable daemon work: it steers the running turn
				// at its next loop boundary, or runs as the next turn if the
				// turn ends first. alt+enter stays the newline binding.
				if !strings.HasPrefix(text, "/auth ") {
					m.hist = append(m.hist, text)
					m.histIdx = len(m.hist)
				}
				m.input.Reset()
				if m.agentOpen != "" {
					return m.submitClientAction("agent.submit", map[string]string{"id": m.agentOpen, "text": text, "delivery": "steer"}, text)
				}
				return m.submitClientAction("steer", daemon.SubmitPayload{Text: text}, text)
			default:
				return m, nil
			}
		}
		if text == "" {
			return m, nil
		}
		m.input.Reset()
		if !strings.HasPrefix(text, "/auth ") {
			m.hist = append(m.hist, text)
			m.histIdx = len(m.hist)
		}
		if strings.HasPrefix(text, "/") {
			return m.thinCommand(text)
		}
		if command, ok := strings.CutPrefix(text, "!"); ok {
			return m.submitClientAction("shell.run", map[string]any{"command": strings.TrimSpace(command)}, text)
		}
		if m.agentOpen != "" {
			return m.submitClientAction("agent.submit", map[string]string{"id": m.agentOpen, "text": text}, text)
		}
		return m.submitClientAction("submit", map[string]string{"text": text}, text)
	}
	var command bubbletea.Cmd
	m.input, command = m.input.Update(msg)
	m.refreshMenu()
	return m, command
}

func clientCommandRunsWhileBusy(text string) bool {
	fields := strings.Fields(text)
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		return false
	}
	name := strings.TrimPrefix(fields[0], "/")
	args := strings.TrimSpace(strings.TrimPrefix(text, fields[0]))
	switch name {
	case "help", "theme", "mouse", "pwd", "fork", "agents", "permissions", "report", "export", "me", "memory", "context-doctor":
		return true
	case "effort":
		return args == ""
	case "goal":
		return args == "" || args == "clear" || strings.HasPrefix(args, "rounds ")
	case "mcp", "lsp", "browser", "schedule":
		return args == "" || args == "list" || args == "status"
	default:
		return false
	}
}

// thinPaste handles bracketed paste: a big paste collapses to a placeholder
// (expanded on submit, see paste.go) when configured; otherwise it goes to
// the textarea like typed text.
func (m *model) thinPaste(msg bubbletea.PasteMsg) (bubbletea.Model, bubbletea.Cmd) {
	if m.cfg != nil && m.cfg.CollapsePaste != nil && *m.cfg.CollapsePaste {
		if lines := strings.Count(msg.Content, "\n"); lines >= 2 {
			m.pasteBuf = msg.Content
			m.input.SetValue(m.input.Value() + fmt.Sprintf("[Pasted ~%d lines]", lines+1))
			m.input.CursorEnd()
			m.growInput()
			return m, nil
		}
	}
	var command bubbletea.Cmd
	m.input, command = m.input.Update(msg)
	m.growInput()
	return m, command
}

func (m *model) thinInteractiveKey(msg bubbletea.KeyPressMsg) (bubbletea.Model, bubbletea.Cmd) {
	if msg.String() == "ctrl+c" {
		if !m.interrupt1 {
			m.interrupt1 = true
			return m, nil
		}
		return m.cancelVisibleTurn()
	}
	var input []byte
	switch msg.String() {
	case "esc":
		input = []byte{0x1b}
	case "enter", "ctrl+j":
		input = []byte("\r")
	case "tab":
		input = []byte("\t")
	case "backspace", "delete":
		input = []byte{0x7f}
	case "up", "down", "left", "right":
		input = []byte(arrowBytes(msg.Code))
	default:
		if msg.Text != "" {
			if msg.Mod.Contains(bubbletea.ModAlt) {
				input = append(input, 0x1b)
			}
			input = append(input, msg.Text...)
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
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result, commandErr := m.client.Command(ctx, action)
		return clientTerminalMsg{action: action, result: result, err: commandErr}
	}
}

func (m *model) thinPermissionKey(msg bubbletea.KeyPressMsg) (bubbletea.Model, bubbletea.Cmd) {
	dialog := m.permDialog
	if dialog == nil || dialog.daemon == nil || dialog.deciding {
		return m, nil
	}
	decide := func(allow bool, reason, remember string) (bubbletea.Model, bubbletea.Cmd) {
		action, err := m.client.NewAction("permission.decide", map[string]any{
			"permission_id": dialog.daemon.ID, "allow": allow, "reason": reason, "remember": remember,
		})
		if err != nil {
			m.append(errStyle.Render("permission: " + err.Error()))
			return m, nil
		}
		dialog.deciding = true
		m.clientInFlight++
		permissionID := dialog.daemon.ID
		return m, func() bubbletea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			result, decisionErr := m.client.DecidePermission(ctx, action, permissionID, allow, reason, remember)
			return clientPermissionMsg{action: action, permissionID: permissionID, result: result, err: decisionErr}
		}
	}
	if dialog.rejecting {
		switch msg.String() {
		case "enter":
			return decide(false, strings.TrimSpace(dialog.rejectIn), "")
		case "esc":
			dialog.rejecting, dialog.rejectIn = false, ""
		case "backspace":
			if len(dialog.rejectIn) > 0 {
				dialog.rejectIn = dialog.rejectIn[:len(dialog.rejectIn)-1]
			}
		default:
			dialog.rejectIn += msg.Text
		}
		return m, nil
	}
	options := len(permOptions(dialog.daemon))
	switch msg.String() {
	case "left", "up":
		dialog.sel = (dialog.sel + options - 1) % options
	case "right", "down":
		dialog.sel = (dialog.sel + 1) % options
	case "enter":
		switch {
		case dialog.sel == 0:
			return decide(true, "", "")
		case dialog.sel == 1 && dialog.daemon.Rule != "":
			return decide(true, "", "tree")
		default:
			dialog.rejecting = true
		}
	default:
		switch msg.Text {
		case "a", "A":
			return decide(true, "", "")
		case "t", "T":
			if dialog.daemon.Rule != "" {
				return decide(true, "", "tree")
			}
		case "r":
			dialog.rejecting = true
		}
	case "esc", "ctrl+c":
		return decide(false, "rejected without a reason", "")
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
		{
			title: "Reasoning effort", category: "Agent",
			dynDesc: func(value *model) string { return "current: " + effortLabel(value.displayEffort()) },
			dynHint: func(*model) string { return "/effort" },
			run: func(value *model) (bubbletea.Model, bubbletea.Cmd) {
				value.openThinEffortPalette()
				return value, nil
			},
		},
		{
			title: "Authentication", category: "Agent",
			dynDesc: func(*model) string { return "configure Inference.net or OpenRouter" },
			dynHint: func(*model) string { return "/auth" },
			run: func(value *model) (bubbletea.Model, bubbletea.Cmd) {
				value.openThinAuthPalette()
				return value, nil
			},
		},
		commandItem("Computer use", "Agent", "/computer-use status", false),
		{
			title: "Session", category: "Session",
			dynDesc: func(value *model) string { return value.sessTitle },
			dynHint: func(*model) string { return "resume · fork · rename · clear" },
			run: func(value *model) (bubbletea.Model, bubbletea.Cmd) {
				value.openThinSessionPalette()
				return value, nil
			},
		},
		commandItem("Resume session", "Session", "/resume", true),
		commandItem("Rewind conversation", "Session", "/rewind", true),
		commandItem("Fork session", "Session", "/fork", false),
		commandItem("Rename session", "Session", "/rename", false),
		commandItem("Clear conversation", "Session", "/clear", false),
		{
			title: "Compact session", category: "Session", suggested: true,
			dynDesc: func(*model) string { return "run, configure, retry, or inspect compaction" },
			dynHint: func(*model) string { return "/compact" },
			run: func(value *model) (bubbletea.Model, bubbletea.Cmd) {
				value.openThinCompactPalette()
				return value, nil
			},
		},
		commandItem("Context doctor", "Session", "/context-doctor", false),
		commandItem("Agents", "Session", "/agents", false),
		commandItem("Permission rules", "Session", "/permissions", false),
		commandItem("Schedules", "Session", "/schedule list", false),
		{
			title: "MCPs", category: "Session",
			dynDesc: func(*model) string { return "servers and imported configurations" },
			dynHint: func(*model) string { return "/mcp" },
			run: func(value *model) (bubbletea.Model, bubbletea.Cmd) {
				value.openThinMCPPalette()
				return value, nil
			},
		},
		commandItem("Language servers", "Session", "/lsp status", false),
		{
			title: "Browser", category: "Session",
			dynDesc: func(*model) string { return "status and driver" },
			dynHint: func(*model) string { return "/browser" },
			run: func(value *model) (bubbletea.Model, bubbletea.Cmd) {
				value.openThinBrowserPalette()
				return value, nil
			},
		},
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
		{
			title: "Theme", category: "Display",
			dynDesc: func(value *model) string {
				current := ""
				if value.cfg != nil {
					current = value.cfg.Theme
				}
				if current == "" {
					current = "auto"
				}
				return "current: " + current
			},
			dynHint: func(*model) string { return "/theme" },
			run: func(value *model) (bubbletea.Model, bubbletea.Cmd) {
				value.openThinThemePalette()
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

func (m *model) openThinSessionPalette() {
	commands := []struct{ title, command string }{
		{"Resume a session", "/resume"},
		{"Fork this session", "/fork"},
		{"Rename this session", "/rename"},
		{"Clear this conversation", "/clear"},
	}
	m.openCommandSubpalette("Session", commands)
}

func (m *model) openThinAuthPalette() {
	commands := []struct{ title, command string }{
		{"Sign in to Inference.net", "/auth inference-net"},
		{"Configure OpenRouter key", "/auth openrouter"},
	}
	m.openCommandSubpalette("Authentication", commands)
}

func (m *model) openThinCompactPalette() {
	commands := []struct{ title, command string }{
		{"Compact now", "/compact"},
		{"Use automatic default", "/compact off"},
		{"Retry latest compaction", "/compact retry"},
		{"View compaction log", "/compact log"},
	}
	for _, item := range buildModelItems(m.cfg) {
		commands = append(commands, struct{ title, command string }{
			title:   "Use " + item.model + " @ " + item.provider,
			command: "/compact " + item.model + " " + item.provider,
		})
	}
	m.openCommandSubpalette("Compaction", commands)
}

func (m *model) openThinMCPPalette() {
	commands := []struct{ title, command string }{
		{"MCP server status", "/mcp status"},
		{"MCP import status", "/mcp import status"},
		{"Enable Claude imports", "/mcp import claude on"},
		{"Disable Claude imports", "/mcp import claude off"},
		{"Enable Codex imports", "/mcp import codex on"},
		{"Disable Codex imports", "/mcp import codex off"},
	}
	var servers []string
	if m.cfg != nil {
		servers = make([]string, 0, len(m.cfg.MCPServers))
		for name := range m.cfg.MCPServers {
			servers = append(servers, name)
		}
	}
	slices.Sort(servers)
	for _, name := range servers {
		commands = append(commands,
			struct{ title, command string }{"Reconnect " + name, "/mcp " + name + " reconnect"},
			struct{ title, command string }{"Enable " + name, "/mcp " + name + " enable"},
			struct{ title, command string }{"Disable " + name, "/mcp " + name + " disable"},
		)
	}
	m.openCommandSubpalette("MCP", commands)
}

func (m *model) openCommandSubpalette(category string, commands []struct{ title, command string }) {
	items := make([]paletteItem, 0, len(commands))
	for _, item := range commands {
		item := item
		items = append(items, paletteItem{
			title: item.title, category: category,
			dynHint: func(*model) string { return item.command },
			run: func(value *model) (bubbletea.Model, bubbletea.Cmd) {
				value.palette = nil
				return value.thinCommand(item.command)
			},
		})
	}
	m.palette = &palette{all: items}
	m.palette.applyFilter(m)
}

func (m *model) openThinThemePalette() {
	loadUserThemes()
	names := themeNames()
	items := make([]paletteItem, 0, len(names))
	for _, theme := range names {
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

func (m *model) openThinEffortPalette() {
	items := make([]paletteItem, 0, len(m.effortsFor()))
	for _, level := range m.effortsFor() {
		level := level
		items = append(items, paletteItem{
			title: "Effort: " + effortLabel(level), category: "Agent",
			dynDesc: func(value *model) string {
				if value.displayEffort() == level {
					return "current"
				}
				return "switch reasoning level"
			},
			run: func(value *model) (bubbletea.Model, bubbletea.Cmd) {
				value.palette = nil
				return value.submitClientAction("session.effort", map[string]string{
					"args": effortLabel(level), "persist_default": "true",
				}, "")
			},
		})
	}
	m.palette = &palette{all: items}
	m.palette.applyFilter(m)
}

func (m *model) openThinBrowserPalette() {
	commands := []struct{ title, command string }{
		{"Browser status", "/browser status"},
		{"Use Rod driver", "/browser driver rod"},
		{"Use chromedp driver", "/browser driver chromedp"},
	}
	items := make([]paletteItem, 0, len(commands))
	for _, item := range commands {
		item := item
		items = append(items, paletteItem{
			title: item.title, category: "Browser",
			dynHint: func(*model) string { return item.command },
			run: func(value *model) (bubbletea.Model, bubbletea.Cmd) {
				value.palette = nil
				return value.thinCommand(item.command)
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
		if len(fields) == 1 {
			m.openThinAuthPalette()
			return m, nil
		}
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
	case "repl":
		m.replPanel = !m.replPanel
		m.recalcWidth()
		if !m.sidebarVisible() {
			m.append(dimStyle.Render("(the REPL panel needs a terminal at least 120 columns wide)"))
		}
		return m, nil
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
	case "effort":
		if args == "" {
			m.openThinEffortPalette()
			return m, nil
		}
		level, ok := parseEffort(m.effortsFor(), args)
		if !ok {
			levels := m.effortsFor()
			names := make([]string, len(levels))
			for index := range levels {
				names[index] = effortLabel(levels[index])
			}
			m.append(errStyle.Render("unknown effort level; " + m.modelName + " supports: " + strings.Join(names, ", ")))
			return m, nil
		}
		return m.submitClientAction("session.effort", map[string]string{
			"args": effortLabel(level), "persist_default": "true",
		}, "")
	case "model", "model-for-session":
		if args == "refresh" {
			return m.submitClientAction("provider.catalogs", map[string]string{}, "")
		}
		if args == "" {
			m.openModelPicker(name == "model-for-session")
			return m, nil
		}
		resolved, err := m.resolveModelCommandArgs(args)
		if err != nil {
			m.append(errStyle.Render(err.Error()))
			return m, nil
		}
		return m.submitClientAction("session.model", map[string]string{"args": resolved, "persist_default": strconv.FormatBool(name == "model")}, "")
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
	case "agents":
		parts := strings.Fields(args)
		if len(parts) == 0 || (len(parts) == 1 && parts[0] == "list") {
			return m.submitClientAction("agents.list", map[string]string{}, "")
		}
		if len(parts) == 2 && parts[0] == "stop" {
			return m.submitClientAction("agent.control", map[string]string{"args": parts[1]}, "")
		}
		if len(parts) == 2 && parts[0] == "delete" {
			return m.submitClientAction("agent.delete", map[string]string{"args": parts[1]}, "")
		}
		if len(parts) == 4 && parts[0] == "budget" {
			return m.submitClientAction("budget.cap", map[string]string{"args": strings.Join(parts[1:], " ")}, "")
		}
		if len(parts) == 2 && parts[0] == "revoke" {
			return m.submitClientAction("capability.revoke", map[string]string{"args": parts[1]}, "")
		}
		m.append(errStyle.Render("usage: /agents [list|stop <id>|delete <id>|budget <id> <kind> <limit>|revoke <capability-id>]"))
		return m, nil
	case "permissions":
		parts := strings.Fields(args)
		if len(parts) == 0 || (len(parts) == 1 && parts[0] == "list") {
			return m.submitClientAction("permission.rules", map[string]string{}, "")
		}
		if len(parts) == 2 && parts[0] == "forget" {
			return m.submitClientAction("permission.forget", map[string]string{"args": parts[1]}, "")
		}
		m.append(errStyle.Render("usage: /permissions [list|forget <id>]"))
		return m, nil
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
	case "compact":
		switch args {
		case "log":
			return m.submitClientAction("history.compact.log", map[string]string{}, "")
		case "retry":
			return m.submitClientAction("history.compact.retry", map[string]string{}, "")
		case "off":
			return m.submitClientAction("compaction.configure", map[string]string{"args": "off"}, "")
		case "":
			return m.submitClientAction("history.compact", map[string]string{}, "")
		default:
			return m.submitClientAction("compaction.configure", map[string]string{"args": args}, "")
		}
	}
	operations := map[string]string{
		"schedule": "schedule.manage",
		"cd":       "workspace.set", "pwd": "workspace.inspect",
		"clear": "history.clear",
		"mcp":   "mcp.control", "lsp": "lsp.control",
		"browser": "browser.control", "context-doctor": "context.audit",
	}
	operation, ok := operations[name]
	if !ok {
		m.append(errStyle.Render("unknown command: /" + name))
		return m, nil
	}
	return m.submitClientAction(operation, map[string]string{"args": args}, "")
}

var _ daemonConnection = (*daemon.Client)(nil)
