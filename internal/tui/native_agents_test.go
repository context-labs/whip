package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/protocol"
)

func nativeAgentChild(t *testing.T, m *nativeModel, parent protocol.ID, key string) protocol.Session {
	t.Helper()
	value := nativeMenuRPC[protocol.SpawnSessionResult](t, m.connection, "sessions.spawn", protocol.SpawnSessionParams{ParentID: parent, Name: "Reviewer " + key, Identity: protocol.RequestIdentity{ClientID: "agent-tree", RequestID: protocol.ID(key)}, GrantIDs: []protocol.ID{}, Parts: []protocol.Part{{Type: "text", Text: "child prompt"}}})
	if value.Session == nil {
		t.Fatal("spawned child missing")
	}
	return *value.Session
}

func TestNativeAgentTreeNavigationKeepsOwnerDraftsAndPendingWork(t *testing.T) {
	m, provider := nativeUIFixture(t)
	root := m.owner
	accepted := nativeUISubmit(t, m, "hold")
	select {
	case <-provider.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("root input not running")
	}
	child := nativeAgentChild(t, m, root.ID, "child")
	grandchild := nativeAgentChild(t, m, child.ID, "grandchild")
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
	m.polls = 0
	nativeUIRead(t, m)
	if m.agents == nil || m.agents.root != root.ID || len(m.agents.rows) != 3 {
		t.Fatal(m.agents, m.status)
	}
	depths := map[protocol.ID]int{}
	for _, row := range m.agents.rows {
		depths[row.id] = row.depth
		if row.id == child.ID && row.name != "Reviewer child" || row.id == grandchild.ID && row.name != "Reviewer grandchild" {
			t.Fatal("child display name lost", row)
		}
	}
	if depths[root.ID] != 0 || depths[child.ID] != 1 || depths[grandchild.ID] != 2 {
		t.Fatal(depths)
	}
	m.input.SetValue("unsent root draft")
	m.agentSelection = child.ID
	m.agentsFocus = true
	_, command := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if command == nil {
		t.Fatal(m.status)
	}
	_, read := m.Update(command())
	if read == nil {
		t.Fatal(m.status)
	}
	m.Update(read())
	if m.owner.ID != child.ID || m.input.Value() != "" {
		t.Fatal(m.owner.ID, m.input.Value())
	}
	m.input.SetValue("unsent child draft")
	m.agentsFocus = true
	_, command = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if command == nil {
		t.Fatal(m.status)
	}
	_, read = m.Update(command())
	if read == nil {
		t.Fatal(m.status)
	}
	m.Update(read())
	if m.owner.ID != root.ID || m.input.Value() != "unsent root draft" {
		t.Fatal(m.owner.ID, m.input.Value())
	}
	nativeNavigate(t, m, string(child.ID))
	if m.input.Value() != "unsent child draft" {
		t.Fatal("child draft lost", m.input.Value())
	}
	status, found, err := accepted.command.Check(t.Context())
	if err != nil || !found || status.Turn == nil || status.Turn.State != "running" {
		t.Fatal("navigation changed host execution", status, err)
	}
	close(provider.release)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := accepted.command.Wait(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestNativeAgentActivityAndControlsRespectTreeAndDoNotReplay(t *testing.T) {
	m, provider := nativeUIFixture(t)
	child := nativeAgentChild(t, m, m.owner.ID, "stopped")
	foreign := nativeNavigationRoot(t, m, "foreign", "Other")
	held, err := m.connection.PrepareInput("sessions.submit", protocol.SubmitParams{SessionID: child.ID, Identity: protocol.RequestIdentity{ClientID: "agent-tree", RequestID: "held"}, Source: "user", Parts: []protocol.Part{{Type: "text", Text: "hold"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := held.Send(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-provider.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("child hold not running")
	}
	nativeMenuRPC[protocol.Admission](t, m.connection, "sessions.submit", protocol.SubmitParams{SessionID: child.ID, Identity: protocol.RequestIdentity{ClientID: "agent-tree", RequestID: "queued"}, Source: "user", Parts: []protocol.Part{{Type: "text", Text: "queued before stop"}}})
	result := nativeUIControl(t, m, "/agents stop "+string(child.ID))
	if result.err != nil || result.retry != nil {
		t.Fatal(result)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := held.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	tree, err := readNativeAgents(t.Context(), m.connection, m.owner, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	var label string
	for _, row := range tree.rows {
		if row.id == child.ID {
			label = nativeAgentState(row)
		}
	}
	if !strings.Contains(label, "stopped") || !strings.Contains(label, "queued 1") {
		t.Fatal(label)
	}
	if result := nativeUIControl(t, m, "/agents stop "+string(foreign.ID)); result.err == nil || !strings.Contains(result.err.Error(), "outside") {
		t.Fatal("foreign control admitted", result)
	}
	if value := nativeMenuRPC[protocol.Session](t, m.connection, "sessions.get", protocol.SessionParams{SessionID: foreign.ID}); value.Lifecycle != "active" {
		t.Fatal(value)
	}
	if result := nativeUIControl(t, m, "/agents delete "+string(m.owner.ID)); result.err == nil {
		t.Fatal("root deletion hidden in child command")
	}
	deleted := nativeUIControl(t, m, "/agents delete "+string(child.ID))
	if deleted.err != nil || deleted.retry != nil {
		t.Fatal(deleted)
	}
	tree, err = readNativeAgents(t.Context(), m.connection, m.owner, m.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range tree.rows {
		if row.id == child.ID {
			t.Fatal("deleted child retained")
		}
	}
}

func TestNativeLayoutBoundsAndDraftCapacity(t *testing.T) {
	m, _ := nativeUIFixture(t)
	m.preferencesDirectory = t.TempDir()
	root := m.owner
	other := nativeNavigationRoot(t, m, "draft-other", "Other")
	m.drafts = map[protocol.ID]nativeDraft{}
	for i := range 16 {
		m.drafts[protocol.ID(fmt.Sprintf("draft-%d", i))] = nativeDraft{text: "saved"}
	}
	m.input.SetValue("current unsent")
	if err := m.attachSession(other); err == nil || m.owner.ID != root.ID || m.input.Value() != "current unsent" {
		t.Fatal("draft capacity silently lost text", err, m.owner)
	}
	m.drafts = nil
	for _, size := range [][2]int{{8, 4}, {79, 24}, {120, 30}, {160, 40}} {
		for _, dock := range []bool{false, true} {
			m.dock = dock
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			rows := strings.Split(m.View().Content, "\n")
			if len(rows) > size[1] {
				t.Fatalf("%v dock=%t height=%d", size, dock, len(rows))
			}
			for _, row := range rows {
				if ansi.StringWidth(row) > size[0] {
					t.Fatalf("%v dock=%t rowwidth=%d", size, dock, ansi.StringWidth(row))
				}
			}
		}
	}
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	m.input.SetValue("/sidebar")
	m.command("/sidebar")
	if m.sidebarVisible() {
		t.Fatal("sidebar toggle ignored")
	}
	prefs, err := readNativePreferences(m.preferencesDirectory)
	if err != nil || prefs.Sidebar == nil || *prefs.Sidebar {
		t.Fatal(prefs, err)
	}
	m.Update(tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	if !m.agentsFocus || !m.dock {
		t.Fatal("tree key did not expose dock")
	}
	if got := nativeAgentState(nativeAgentRow{lifecycle: "active"}); got != "active · activity unread" {
		t.Fatal("lifecycle fabricated running", got)
	}
}

type nativeAgentReadFixture struct{ call func(string, any, any) error }

func (f nativeAgentReadFixture) Call(_ context.Context, method string, params, result any) error {
	return f.call(method, params, result)
}

func TestNativeAgentTreeContinuesShortByteBoundedPagesAndRejectsWrongOwners(t *testing.T) {
	owner := protocol.Session{ID: "root", TreeID: "tree", Lifecycle: "active"}
	reads := 0
	fixture := nativeAgentReadFixture{call: func(method string, params, result any) error {
		switch method {
		case "sessions.list":
			reads++
			p := params.(protocol.ListSessionsParams)
			page := result.(*protocol.ListSessionsResult)
			if p.After == nil {
				page.Items = []protocol.Session{{ID: "child_a", ParentID: new(owner.ID), TreeID: owner.TreeID, Lifecycle: "active"}}
			} else if *p.After == "child_a" {
				page.Items = []protocol.Session{{ID: "child_b", ParentID: new(owner.ID), TreeID: owner.TreeID, Lifecycle: "stopped"}}
			}
		case "sessions.activity":
			p := params.(protocol.SessionParams)
			*result.(*protocol.SessionActivity) = protocol.SessionActivity{SessionID: p.SessionID, Lifecycle: "active"}
		default:
			t.Fatalf("unexpected method %s", method)
		}
		return nil
	}}
	tree, err := readNativeAgents(t.Context(), fixture, owner, owner.ID)
	if err != nil || reads != 3 || len(tree.rows) != 3 || tree.partial {
		t.Fatal("short page silently omitted owner", tree, reads, err)
	}
	fixture.call = func(method string, params, result any) error {
		if method == "sessions.list" {
			result.(*protocol.ListSessionsResult).Items = []protocol.Session{{ID: "foreign", TreeID: "other"}}
		}
		return nil
	}
	if _, err := readNativeAgents(t.Context(), fixture, owner, owner.ID); err == nil {
		t.Fatal("foreign tree row accepted")
	}
}
