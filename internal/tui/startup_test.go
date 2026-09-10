package tui

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

type onboardingConnection struct {
	*fakeDaemonConnection
	host *setupTestHost
}

func (c *onboardingConnection) Call(ctx context.Context, method string, params, result any) error {
	switch method {
	case "provider.list", "provider.discover":
		p := params.(protocol.ProviderListParams)
		list, err := c.host.ListProvidersFor(ctx, p.Model, p.Provider)
		*result.(*protocol.ProviderList) = list
		return err
	case "provider.key.set":
		value, err := c.host.SetProviderKey(ctx, params.(daemon.ProviderKeySetup))
		*result.(*daemon.RuntimeConfiguration) = value
		return err
	case "query":
		query := params.(protocol.QueryParams)
		if query.Operation != "provider.catalogs" {
			return errors.New("unexpected query")
		}
		var p protocol.ProviderCatalogParams
		if err := json.Unmarshal(query.Payload, &p); err != nil {
			return err
		}
		value, err := c.host.ProviderCatalogsFor(ctx, p.Provider, p.Refresh)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(value)
		*result.(*protocol.QueryResult) = protocol.QueryResult{Result: raw}
		return err
	case "config.update":
		value, err := c.host.UpdateConfiguration(ctx, params.(daemon.ConfigurationUpdate))
		*result.(*daemon.RuntimeConfiguration) = value
		return err
	default:
		return errors.New("unexpected onboarding request: " + method)
	}
}

func TestOnboardingStaysInNormalTUIUntilExplicitFirstSend(t *testing.T) {
	h := &setupTestHost{list: setupFixture(), catalogs: setupDefaultCatalogs()}
	connections := make(chan *onboardingConnection, 4)
	client, err := NewClient(ClientOptions{
		ClientID: "onboarding", DeferCreate: true,
		Create: &daemon.CreateSession{Kind: session.SessionKindAgent, CWD: t.TempDir()},
		Connector: func(context.Context, map[string]int64) (daemonConnection, error) {
			c := &onboardingConnection{fakeDaemonConnection: newFakeDaemonConnection(session.RootSnapshot{RootID: "first-root"}), host: h}
			c.commandFunc = func(p daemon.CommandParams) (daemon.CommandResult, error) {
				return daemon.CommandResult{Status: "succeeded", Output: "first-root"}, nil
			}
			connections <- c
			return c, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	client.Start()
	waitClientState(t, client, ClientLive)
	<-connections
	m := compactCmdModel()
	m.client, m.clientState = client, ClientLive
	m.modelName, m.provName = "", ""
	m.runContext = t.Context()
	m.startup = &sessionStartup{ctx: t.Context(), yolo: true}
	m.historyRequested = true // Auxiliary bootstrap queries have separate protocol tests.
	m.initialPrompt = "launch draft"
	m.input.SetValue(m.initialPrompt)
	m.Update(mkWinSize(100, 36))
	_, cmd := m.Update(m.advanceStartup()())
	if cmd != nil || m.initialPrompt != "" || m.providerSetup == nil || client.RootID() != "" {
		t.Fatal("setup created execution or retained an automatic launch submission")
	}
	view := ansi.Strip(viewStr(m))
	if !strings.Contains(view, "Connect a provider") || !strings.Contains(view, "launch draft") || !strings.Contains(view, "ctrl+p") {
		t.Fatalf("connection dialog is outside the normal composer/frame:\n%s", view)
	}
	_, cmd = m.Update(setupKey("esc"))
	if cmd != nil || m.providerSetup != nil || m.input.Value() != "launch draft" || client.RootID() != "" {
		t.Fatal("Esc exited the TUI or lost the normal draft")
	}
	draft := "Inspect this repository\nand explain the worker."
	m.input.Reset()
	m.Update(tea.PasteMsg{Content: draft})
	_, cmd = m.Update(setupKey("enter"))
	m.Update(cmd())
	if m.input.Value() != draft || client.RootID() != "" || m.providerSetup == nil {
		t.Fatal("sending an unconfigured draft did not reopen setup safely")
	}
	m.Update(setupKey("down"))
	m.Update(setupKey("enter"))
	m.Update(tea.PasteMsg{Content: "fixture-secret"})
	if strings.Contains(ansi.Strip(viewStr(m)), "fixture-secret") || m.input.Value() != draft {
		t.Fatal("credential input escaped the dialog")
	}
	_, cmd = m.Update(setupKey("enter"))
	_, cmd = m.Update(cmd()) // Save key, then refresh inventory.
	for i := 0; cmd != nil; i++ {
		if i > 5 {
			t.Fatal("onboarding did not finish")
		}
		_, cmd = m.Update(cmd())
	}
	if m.providerSetup != nil || m.input.Value() != draft {
		t.Fatalf("auto selection did not return to composer: setup=%+v draft=%q", m.providerSetup, m.input.Value())
	}
	var prepare tea.Cmd
	for prepare == nil {
		update := nextClientUpdate(t, client)
		_, next := m.Update(clientUpdateMsg{ClientUpdate: update})
		if update.StateChanged && update.State == ClientLive {
			batch := next().(tea.BatchMsg)
			prepare = batch[0]
		}
	}
	if m.startup == nil || !m.startup.preparing {
		t.Fatal("execution became available before preparation")
	}
	_, blocked := m.submitClientAction("submit", map[string]string{"text": "too early"}, "")
	if blocked == nil || m.busy {
		t.Fatal("early submission was not gated")
	}
	m.Update(prepare())
	if m.startup != nil || m.input.Value() != draft || m.busy {
		t.Fatal("preparation sent or lost the draft")
	}
	connection := <-connections
	connection.mu.Lock()
	operations := make([]string, len(connection.commands))
	for i, command := range connection.commands {
		operations[i] = command.Operation
	}
	var selected daemon.CreateSession
	var selectedEffort protocol.EffortParams
	if len(connection.commands) == 4 {
		_ = json.Unmarshal(connection.commands[0].Payload, &selected)
		_ = json.Unmarshal(connection.commands[3].Payload, &selectedEffort)
	}
	connection.mu.Unlock()
	if selected.Model != "z-ai/glm-5.3" || selected.Provider != "openrouter" || selectedEffort.Effort != "max" {
		t.Fatalf("first session lost selection: %+v %+v", selected, selectedEffort)
	}
	if strings.Join(operations, ",") != "session.create,permission.mode,session.autotitle,session.effort" {
		t.Fatalf("unexpected preparation/submission order: %v", operations)
	}
	_, cmd = m.Update(setupKey("enter"))
	message := clientCommandFrom(t, cmd)
	if message.err != nil || message.action.Operation != "submit" || !strings.Contains(string(message.action.Payload), "Inspect this repository") {
		t.Fatalf("first explicit send failed: %+v", message)
	}
}

func TestOnboardingClosedDialogIgnoresLateCredentialReply(t *testing.T) {
	m := authTestModel(t)
	m.startup = &sessionStartup{ctx: t.Context()}
	m.runContext = t.Context()
	m.input.SetValue("keep the original prompt")
	h := &setupTestHost{list: setupFixture()}
	s := newTestSetup(t, h, true)
	s.list = h.list
	m.providerSetup = s
	s.connect(h.list.Providers[1])
	s.input.SetValue("fixture-key")
	pending := s.keypress(setupKey("enter"))
	m.Update(ctrlKey('c'))
	if m.providerSetup != nil {
		t.Fatal("Ctrl+C did not dismiss the dialog")
	}
	m.Update(pending())
	if m.providerSetup != nil || m.input.Value() != "keep the original prompt" || m.busy {
		t.Fatal("late credential response restored setup or submitted the draft")
	}
}

func TestOnboardingEmptyComposerRetriesFailedPreparation(t *testing.T) {
	m, _ := liveQueueModel(t)
	m.busy, m.sessionID = false, "root"
	m.startup = &sessionStartup{ctx: t.Context(), preparing: true}
	m.Update(sessionPreparedMsg{owner: m.startup, err: errors.New("temporary failure")})
	_, command := m.Update(setupKey("enter"))
	if command == nil || !m.startup.preparing || m.input.Value() != "" {
		t.Fatal("empty composer cannot retry preparation")
	}
}

func TestOnboardingPreparationAcrossDisconnectPreservesLaunchPrompt(t *testing.T) {
	m := compactCmdModel()
	m.sessionID = "root"
	m.startup = &sessionStartup{ctx: t.Context()}
	m.initialPrompt = "launch prompt"
	m.input.SetValue(m.initialPrompt)
	m.clientState = ClientDisconnected
	_, command := m.Update(sessionPreparedMsg{owner: m.startup})
	if command != nil || m.initialPrompt != "launch prompt" || m.input.Value() != "launch prompt" || m.busy {
		t.Fatal("disconnected preparation consumed or submitted the launch prompt")
	}
}

func TestOnboardingTerminalFailureShowsCauseAndPreservesDraft(t *testing.T) {
	m := compactCmdModel()
	m.Update(mkWinSize(100, 30))
	m.startup = &sessionStartup{ctx: t.Context(), creating: true}
	m.input.SetValue("keep the draft")
	m.Update(clientUpdateMsg{ClientUpdate: ClientUpdate{
		State: ClientDisconnected, StateChanged: true, Err: errors.New("session creation rejected"),
	}, closed: true})
	view := ansi.Strip(viewStr(m))
	if !m.clientClosed || m.startup.creating || !strings.Contains(view, "session creation rejected") || !strings.Contains(view, "relaunch") {
		t.Fatalf("terminal failure left an endless loading state:\n%s", view)
	}
	m.thinCommand("/connect")
	if m.providerSetup != nil || m.input.Value() != "keep the draft" {
		t.Fatal("closed client reopened a dead connection flow or cleared draft")
	}
	_, quit := m.thinCommand("/quit")
	if _, ok := quit().(tea.QuitMsg); !ok {
		t.Fatal("terminal failure cannot be exited")
	}
}

func TestOnboardingModelEntryPointsOpenProviderChooser(t *testing.T) {
	for _, entry := range []string{"/model", "/model-for-session", "leader"} {
		t.Run(entry, func(t *testing.T) {
			m := compactCmdModel()
			m.startup = &sessionStartup{ctx: t.Context()}
			m.input.SetValue("draft")
			if entry == "leader" {
				m.ocLeaderChord("m")
			} else {
				m.thinCommand(entry)
			}
			if m.providerSetup == nil || m.input.Value() != "draft" || strings.Contains(m.transcriptText(), "config.json") {
				t.Fatal("pre-session model command bypassed provider chooser")
			}
			m.providerSetup.cancel()
		})
	}
}

func TestOnboardingEditedLaunchPromptWaitsForExplicitSend(t *testing.T) {
	m := compactCmdModel()
	m.clientState, m.historyRequested = ClientLive, true
	m.initialPrompt = "original launch prompt"
	m.input.SetValue("edited launch prompt")
	if command := m.clientReady(); command != nil || m.initialPrompt != "" || m.input.Value() != "edited launch prompt" || m.busy {
		t.Fatal("readiness sent the stale launch prompt or discarded an edit")
	}
}
