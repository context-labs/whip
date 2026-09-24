package tui

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/context-labs/whip/internal/protocol"
)

// Startup is presentation state inside the normal TUI, not another program.
// Host RPCs work before a session exists; execution waits for its preparation.
type sessionStartup struct {
	ctx                          context.Context
	effort                       string
	cautious, yolo               bool
	checked, creating, preparing bool
}

type sessionPreparedMsg struct {
	owner *sessionStartup
	err   error
}

func (m *model) setupContext() context.Context {
	if m.runContext != nil {
		return m.runContext
	}
	return context.Background() // Models constructed directly in headless tests.
}

func (m *model) beforeSession() bool { return m.startup != nil && m.sessionID == "" }

func (m *model) advanceStartup() tea.Cmd {
	if m.clientClosed {
		return nil
	}
	s := m.startup
	if m.beforeSession() {
		if s.checked || s.creating {
			return nil
		}
		s.checked = true
		cmd := m.openProviderSetup()
		if m.providerSetup != nil {
			m.providerSetup.automatic = true
		}
		return cmd
	}
	if s.preparing {
		return nil
	}
	s.preparing = true
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
		defer cancel()
		err := configureInteractiveSession(ctx, client, s.cautious, s.yolo)
		if err == nil && s.effort != "" {
			action, actionErr := client.NewAction("session.effort", protocol.EffortParams{Effort: s.effort})
			err = actionErr
			if err == nil {
				result, callErr := client.Command(ctx, action)
				err = callErr
				if err == nil && result.Status != "succeeded" {
					err = fmt.Errorf("configure reasoning effort: %s", result.Error)
				}
			}
		}
		return sessionPreparedMsg{owner: s, err: err}
	}
}

func (m *model) startFirstSession(model, provider string) tea.Cmd {
	if m.startup.creating {
		return nil
	}
	if err := m.client.StartSession(model, provider); err != nil {
		return m.toastError(err.Error())
	}
	m.startup.creating = true
	m.modelName, m.provName = model, provider
	m.clientState = ClientReconnecting
	return nil
}

func (m *model) clientReady() tea.Cmd {
	if m.clientState != ClientLive {
		return nil
	}
	var commands []tea.Cmd
	if !m.historyRequested {
		m.historyRequested = true
		commands = append(commands, m.requestHostSkills())
		_, catalogs := m.submitClientAction("provider.catalogs", map[string]string{}, "")
		_, history := m.submitClientAction("history.user.list", map[string]string{}, "")
		commands = append(commands, catalogs, history)
	}
	if m.initialPrompt != "" {
		text := m.initialPrompt
		m.initialPrompt = ""
		if m.input.Value() != text {
			return tea.Batch(commands...)
		}
		m.input.Reset()
		_, command := m.submitClientAction("submit", map[string]string{"text": text}, text)
		commands = append(commands, command)
	}
	return tea.Batch(commands...)
}
