package tui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

func liveQueueModel(t *testing.T) (*model, *fakeDaemonConnection) {
	t.Helper()
	snapshot := session.RootSnapshot{RootID: "root", Meta: session.Meta{ID: "root"}, Agents: []session.RuntimeAgent{{ID: "root-agent", RootID: "root", LifecyclePhase: "running"}}}
	connection := newFakeDaemonConnection(snapshot)
	client, err := NewClient(ClientOptions{
		ClientID: "tui", RootID: "root",
		Connector: func(context.Context, map[string]int64) (daemonConnection, error) { return connection, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	client.Start()
	t.Cleanup(func() { _ = client.Close() })
	waitClientState(t, client, ClientLive)
	m := &model{
		cfg: &config.Config{}, client: client, clientState: ClientLive, input: newInput(), now: time.Now,
		clientView: clientPresentation{agents: snapshot.Agents}, busy: true,
		agentMessages: map[string][]llm.Message{},
	}
	return m, connection
}

// clientCommandFrom runs a key handler's command and digs the client command
// out of any batch (the input widget batches its blink command alongside).
func clientCommandFrom(t *testing.T, command tea.Cmd) clientCommandMsg {
	t.Helper()
	if command == nil {
		t.Fatal("no command was produced")
	}
	pending := []tea.Cmd{command}
	for len(pending) > 0 {
		next := pending[0]
		pending = pending[1:]
		if next == nil {
			continue
		}
		switch msg := next().(type) {
		case clientCommandMsg:
			return msg
		case tea.BatchMsg:
			pending = append(pending, msg...)
		}
	}
	t.Fatal("command did not produce a client command")
	return clientCommandMsg{}
}

func TestTypedInputWhileBusySteersThroughDaemon(t *testing.T) {
	m, _ := liveQueueModel(t)
	m.input.SetValue("first follow-up")
	next, command := m.thinKey(keyMsg(tea.KeyEnter))
	m = next.(*model)
	if command == nil || m.input.Value() != "" {
		t.Fatalf("typed text while busy was not sent: command=%v draft=%q", command != nil, m.input.Value())
	}
	if message := clientCommandFrom(t, command); message.action.Operation != "steer" {
		t.Fatalf("enter while busy sent %q, want steer", message.action.Operation)
	}
	m.input.SetValue("/compact")
	next, command = m.thinKey(keyMsg(tea.KeyEnter))
	m = next.(*model)
	if command != nil || m.input.Value() != "/compact" {
		t.Fatalf("mutating command while busy: command=%v draft=%q", command != nil, m.input.Value())
	}
}

func TestOnlyReadOnlyCommandsRunDuringGeneration(t *testing.T) {
	for _, text := range []string{"/help", "/theme", "/pwd", "/effort", "/mcp status", "/agents list"} {
		if !clientCommandRunsWhileBusy(text) {
			t.Errorf("%q should run while busy", text)
		}
	}
	for _, text := range []string{"/cd /tmp", "/effort high", "/model next", "/compact"} {
		if clientCommandRunsWhileBusy(text) {
			t.Errorf("%q should queue while busy", text)
		}
	}
}

func TestTurnFailureIsVisibleOnce(t *testing.T) {
	m, _ := liveQueueModel(t)
	m.clientView.inbox = []session.InboxItem{{AgentID: "root-agent", Seq: 1, Kind: "submit", Status: "running"}}
	action, err := m.client.NewAction("submit", map[string]string{"text": "bad"})
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := m.Update(clientCommandMsg{action: action, result: daemon.CommandResult{Status: "failed", Error: "provider unavailable"}})
	m = updated.(*model)
	if m.busy || strings.Count(m.transcriptText(), "provider unavailable") != 1 {
		t.Fatalf("after command failure busy=%v transcript=%q", m.busy, m.transcriptText())
	}
	payload, _ := json.Marshal(session.LifecycleEvent{
		RootID: "root", AgentID: "root-agent", InboxSeq: 1, Status: "failed", Error: "provider unavailable", Acknowledged: []int64{1},
	})
	if handled, _ := m.applyClientLifecycle("turn.failed", payload); !handled {
		t.Fatal("turn failure lifecycle was not handled")
	}
	if strings.Count(m.transcriptText(), "provider unavailable") != 1 || m.visibleAgentBusy() {
		t.Fatalf("lifecycle duplicated failure or left root running: phase=%q transcript=%q",
			m.clientView.agents[0].LifecyclePhase, m.transcriptText())
	}
}

func TestAutoApprovedPermissionRendersOneDimLineForTheVisibleAgent(t *testing.T) {
	m, _ := liveQueueModel(t)
	m.clientView.agents = append(m.clientView.agents, session.RuntimeAgent{ID: "child", ParentID: "root-agent", LifecyclePhase: "running"})
	hidden, _ := json.Marshal(session.LifecycleEvent{AgentID: "child", Operation: "bash", Command: "go test ./...", Rule: "go test", RuleSource: "tree"})
	if handled, _ := m.applyClientLifecycle("permission.auto_approved", hidden); !handled || strings.Contains(m.transcriptText(), "auto-approved") {
		t.Fatalf("hidden agent's auto-approval handled=%v transcript=%q", handled, m.transcriptText())
	}
	visible, _ := json.Marshal(session.LifecycleEvent{AgentID: "root-agent", Operation: "bash", Command: "ls -la", Rule: "ls", RuleSource: "global"})
	if handled, _ := m.applyClientLifecycle("permission.auto_approved", visible); !handled {
		t.Fatal("auto-approved lifecycle was not handled")
	}
	if !strings.Contains(m.transcriptText(), "(auto-approved bash ls -la by global rule ls)") {
		t.Fatalf("transcript = %q", m.transcriptText())
	}
}

func TestLifecycleReducerTracksQueuedRunningAndTerminalInbox(t *testing.T) {
	m, _ := liveQueueModel(t)
	m.agentOpen = "child"
	m.clientView.agents = append(m.clientView.agents, session.RuntimeAgent{ID: "child", ParentID: "root-agent", LifecyclePhase: "idle"})

	queued, _ := json.Marshal(session.LifecycleEvent{AgentID: "child", InboxSeq: 7, InboxKind: "submit", Status: "queued"})
	if handled, _ := m.applyClientLifecycle("inbox.queued", queued); !handled || !m.visibleAgentBusy() {
		t.Fatalf("queued inbox was not reduced: %+v", m.clientView.inbox)
	}
	started, _ := json.Marshal(session.LifecycleEvent{AgentID: "child", Status: "running"})
	if handled, _ := m.applyClientLifecycle("agent.turn.started", started); !handled || m.clientView.inbox[0].Status != "running" {
		t.Fatalf("started inbox was not reduced: %+v", m.clientView.inbox)
	}
	finished, _ := json.Marshal(session.LifecycleEvent{AgentID: "child", Status: "succeeded", Acknowledged: []int64{7}})
	if handled, _ := m.applyClientLifecycle("agent.turn.succeeded", finished); !handled {
		t.Fatal("terminal lifecycle was not handled")
	}
	if m.visibleAgentBusy() || len(m.clientView.inbox) != 0 {
		t.Fatalf("terminal inbox remained busy: phase=%q inbox=%+v", m.clientView.agents[1].LifecyclePhase, m.clientView.inbox)
	}
}

func TestRecursiveAgentTreeAndScopedStreams(t *testing.T) {
	m := &model{
		input: newInput(), agentMessages: map[string][]llm.Message{},
		clientView: clientPresentation{agents: []session.RuntimeAgent{
			{ID: "root-agent"},
			{ID: "b", ParentID: "root-agent", Name: "beta"},
			{ID: "a", ParentID: "root-agent", Name: "alpha"},
			{ID: "grandchild", ParentID: "a", Name: "nested"},
		}},
	}
	rows := m.runtimeAgentRows()
	if len(rows) != 4 || rows[0].agent.ID != "root-agent" || rows[0].agent.Name != "root" || rows[0].depth != 0 ||
		rows[1].agent.ID != "a" || rows[1].depth != 1 || rows[2].agent.ID != "grandchild" || rows[2].depth != 2 || rows[3].agent.ID != "b" {
		t.Fatalf("root then depth-first rows=%+v", rows)
	}
	m.agentOpen = "a"
	rootPayload, _ := json.Marshal(daemon.StreamEvent{AgentID: "root-agent", Text: "root-only"})
	childPayload, _ := json.Marshal(daemon.StreamEvent{AgentID: "a", Text: "child-only"})
	_, _ = m.applyClientStream("stream.text", rootPayload)
	if m.current != "" {
		t.Fatalf("root stream contaminated child view: %q", m.current)
	}
	_, _ = m.applyClientStream("stream.text", childPayload)
	if m.current != "child-only" {
		t.Fatalf("selected child stream=%q", m.current)
	}
}

func TestAgentTreeSelectionSurvivesSnapshotInsertion(t *testing.T) {
	m := &model{
		input: newInput(), sessionID: "root", clientCursor: 1, agentsFocus: true, agentSel: 2, // root, b, [c]
		agentMessages: map[string][]llm.Message{},
		clientView: clientPresentation{agents: []session.RuntimeAgent{
			{ID: "root-agent"},
			{ID: "b", ParentID: "root-agent"},
			{ID: "c", ParentID: "root-agent"},
		}},
	}
	m.applyClientSnapshot(session.RootSnapshot{
		RootID: "root", Cursor: 2, Meta: session.Meta{ID: "root"},
		Agents: []session.RuntimeAgent{
			{ID: "root-agent"},
			{ID: "a", ParentID: "root-agent"},
			{ID: "b", ParentID: "root-agent"},
			{ID: "c", ParentID: "root-agent"},
		},
	})
	rows := m.runtimeAgentRows()
	if m.agentSel >= len(rows) || rows[m.agentSel].agent.ID != "c" {
		t.Fatalf("selection moved after insertion: index=%d rows=%+v", m.agentSel, rows)
	}
}

func TestScopedStreamsRemainAvailableAcrossPaneNavigation(t *testing.T) {
	m := &model{
		input: newInput(), agentOpen: "child",
		agentMessages: map[string][]llm.Message{"child": nil},
		clientView: clientPresentation{
			agents:             []session.RuntimeAgent{{ID: "root-agent"}, {ID: "child", ParentID: "root-agent"}},
			agentPresentations: map[string][]session.SnapshotEvent{},
		},
	}
	payload, _ := json.Marshal(daemon.StreamEvent{AgentID: "root-agent", Text: "root progress\n"})
	event := daemon.ProtocolEvent{Seq: 4, Kind: "stream.text", Payload: payload}
	m.recordClientStream(event)
	if handled, _ := m.applyClientStream(event.Kind, event.Payload); !handled || strings.Contains(m.transcriptText(), "root progress") {
		t.Fatalf("root stream contaminated selected child: %q", m.transcriptText())
	}
	m.closeAgent()
	if strings.Count(m.transcriptText(), "root progress") != 1 {
		t.Fatalf("root stream was lost or duplicated after navigation: %q", m.transcriptText())
	}
}

func TestTerminalAgentViewRetainsDraftReadOnly(t *testing.T) {
	m := &model{
		clientState: ClientLive, input: newInput(),
		agentOpen:     "child",
		agentMessages: map[string][]llm.Message{},
		clientView: clientPresentation{agents: []session.RuntimeAgent{
			{ID: "root-agent"},
			{ID: "child", ParentID: "root-agent", Status: "stopped", LifecyclePhase: "terminal", TerminalCause: "stopped", AllowedControls: []string{"agent.delete"}},
		}},
	}
	m.input.SetValue("do not lose me")
	next, command := m.thinKey(keyMsg(tea.KeyEnter))
	m = next.(*model)
	if command != nil || m.input.Value() != "do not lose me" {
		t.Fatalf("terminal submit command=%v draft=%q", command != nil, m.input.Value())
	}
	details := m.agentDetails()
	if !strings.Contains(m.transcriptText(), "read-only") || !strings.Contains(details, "read-only") ||
		!strings.Contains(details, "terminal cause stopped") || !strings.Contains(details, "controls agent.delete") {
		t.Fatalf("terminal agent did not explain read-only state: transcript=%q details=%q", m.transcriptText(), m.agentDetails())
	}
}

func TestEscapeLeavesRunningAgentViewWithoutCancellingIt(t *testing.T) {
	m, _ := liveQueueModel(t)
	m.agentOpen = "child"
	m.clientView.agents = append(m.clientView.agents, session.RuntimeAgent{
		ID: "child", ParentID: "root-agent", LifecyclePhase: "running",
	})
	m.busy = true
	next, command := m.thinKey(keyMsg(tea.KeyEsc))
	m = next.(*model)
	if command != nil || m.agentOpen != "" {
		t.Fatalf("escape cancelled instead of closing child view: command=%v agent=%q", command != nil, m.agentOpen)
	}
	m.agentOpen = "child"
	m.busy = true
	next, command = m.thinKey(ctrlKey('c'))
	m = next.(*model)
	if command != nil || !m.interrupt1 {
		t.Fatal("first ctrl+c did not arm child cancellation")
	}
	next, command = m.thinKey(ctrlKey('c'))
	m = next.(*model)
	if command == nil {
		t.Fatal("second ctrl+c did not cancel child turn")
	}
	message := command().(clientCommandMsg)
	if message.action.Operation != "agent.turn.cancel" || !strings.Contains(string(message.action.Payload), "child") {
		t.Fatalf("child cancellation action=%+v", message.action)
	}
}

func TestSessionMetadataEventsUpdateHeaderRouteAndContext(t *testing.T) {
	m := &model{
		cfg: &config.Config{
			Providers: map[string]config.Provider{"next-provider": {BaseURL: "https://example.test"}},
			Models: map[string]config.Model{"next-model": {
				ID: "api-next", Providers: []string{"next-provider"}, Context: 128,
			}},
		},
		input: newInput(), agentMessages: map[string][]llm.Message{},
		catalogs: map[string]config.Catalog{"next-provider": {
			Models: []config.ModelInfoLite{{ID: "api-next", ContextLength: 256}},
		}},
	}
	payload, _ := json.Marshal(daemon.SessionUpdateEvent{
		Title: "renamed", Model: "next-model", Provider: "next-provider", Effort: "off", EffortChanged: true, WorkingDir: "/tmp/project",
	})
	if handled, _ := m.applyClientLifecycle("session.model.updated", payload); !handled {
		t.Fatal("session metadata event was not handled")
	}
	if m.sessTitle != "renamed" || m.modelName != "next-model" || m.provName != "next-provider" ||
		m.displayModelID() != "api-next" || m.displayEffort() != "" || m.displayContextLimit() != 256 ||
		m.clientView.workingDir != "/tmp/project" {
		t.Fatalf("metadata was not reduced: title=%q route=%s@%s id=%q effort=%q context=%d cwd=%q",
			m.sessTitle, m.modelName, m.provName, m.displayModelID(), m.displayEffort(), m.displayContextLimit(), m.clientView.workingDir)
	}
}

func TestSnapshotDistinguishesInheritedEffortFromExplicitOff(t *testing.T) {
	m := &model{
		cfg: &config.Config{DefaultEffort: "high"}, input: newInput(),
		agentMessages: map[string][]llm.Message{},
		clientView:    clientPresentation{agentPresentations: map[string][]session.SnapshotEvent{}},
	}
	snapshot := session.RootSnapshot{RootID: "root", Meta: session.Meta{ID: "root", Model: "model", Provider: "provider"}}
	m.applyClientSnapshot(snapshot)
	if m.displayEffort() != "high" {
		t.Fatalf("inherited effort=%q, want high", m.displayEffort())
	}
	snapshot.Cursor++
	snapshot.Meta.Effort = "off"
	m.applyClientSnapshot(snapshot)
	if m.displayEffort() != "" {
		t.Fatalf("explicit off effort=%q", m.displayEffort())
	}
}

func TestSnapshotPreservesQueuesFocusModalAndToolExpansion(t *testing.T) {
	encode := func(event daemon.StreamEvent) []byte {
		payload, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		return payload
	}
	presentation := []session.SnapshotEvent{
		{Seq: 1, Kind: "stream.tool.started", Payload: encode(daemon.StreamEvent{AgentID: "root-agent", ID: "tool", Name: "read", Args: `{"path":"a.go"}`})},
		{Seq: 2, Kind: "stream.tool.completed", Payload: encode(daemon.StreamEvent{AgentID: "root-agent", ID: "tool", Name: "read", Result: "result"})},
	}
	snapshot := session.RootSnapshot{
		RootID: "root", Cursor: 2, Meta: session.Meta{ID: "root", Model: "kimi-k3-fast", Provider: "inference"},
		Agents: []session.RuntimeAgent{{ID: "root-agent"}}, Presentation: presentation,
	}
	m := compactCmdModel()
	m.sessionID = "root"
	m.applyClientSnapshot(snapshot)
	for index := range m.blocks {
		if m.blocks[index].kind == blockTool {
			m.blocks[index].expanded = true
		}
	}
	m.agentsFocus = true
	m.agentSel = 3
	m.openThinPalette()
	m.palette.idx = 2
	m.input.SetValue("draft")
	m.follow = false

	snapshot.Cursor = 3
	m.applyClientSnapshot(snapshot)
	if !m.agentsFocus || m.agentSel != 3 {
		t.Fatalf("snapshot changed local focus: focus=%t agent=%d", m.agentsFocus, m.agentSel)
	}
	if m.palette == nil || m.palette.idx != 2 || m.input.Value() != "draft" || m.follow {
		t.Fatalf("snapshot changed modal/draft/follow: palette=%+v draft=%q follow=%t", m.palette, m.input.Value(), m.follow)
	}
	expanded := false
	for _, value := range m.blocks {
		expanded = expanded || value.kind == blockTool && value.expanded
	}
	if !expanded {
		t.Fatal("snapshot collapsed an expanded tool row")
	}
}

func TestSnapshotRebuildsInProgressUserInputAndChildPresentation(t *testing.T) {
	stream, _ := json.Marshal(daemon.StreamEvent{AgentID: "child", Text: "working\n"})
	m := &model{
		input: newInput(), sysPrompt: "system", agentOpen: "child",
		agentMessages: map[string][]llm.Message{},
		clientView: clientPresentation{
			agents:             []session.RuntimeAgent{{ID: "root-agent"}, {ID: "child", ParentID: "root-agent", LifecyclePhase: "running"}},
			agentPresentations: map[string][]session.SnapshotEvent{"child": {{Kind: "stream.text", Payload: stream}}},
			inbox:              []session.InboxItem{{AgentID: "child", Seq: 4, Kind: "submit", Status: "running", Payload: session.RuntimeValue{Inline: []byte("inspect this")}}},
		},
	}
	m.rebuildClientTranscript()
	if transcript := m.transcriptText(); strings.Count(transcript, "inspect this") != 1 || !strings.Contains(transcript, "working") {
		t.Fatalf("running transcript was not rebuilt exactly once: %q", transcript)
	}

	m.openAgent(daemon.AgentTranscriptResult{
		Cursor:       10,
		Agent:        session.RuntimeAgent{ID: "child", ParentID: "root-agent", LifecyclePhase: "running"},
		Inbox:        []session.InboxItem{{AgentID: "child", Seq: 5, Kind: "submit", Status: "queued", Payload: session.RuntimeValue{Inline: []byte("follow up")}}},
		Presentation: []session.SnapshotEvent{{Seq: 9, Kind: "stream.text", Payload: stream}},
	})
	if transcript := m.transcriptText(); strings.Count(transcript, "follow up") != 1 || strings.Contains(transcript, "inspect this") {
		t.Fatalf("agent transcript did not replace partial state: %q", transcript)
	}
}
