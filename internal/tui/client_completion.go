package tui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/context-labs/whip/internal/daemon"
)

type clientCompletion struct {
	value      string
	rootID     string
	agentID    string
	tab        bool
	cancel     context.CancelFunc
	ready      bool
	candidates []cand
}

type clientCompletionMsg struct {
	request *clientCompletion
	result  daemon.CompletionResult
	err     error
}

func completionKind(value string, explicit bool) (kind, prefix, head string) {
	index := strings.LastIndexAny(value, " \n")
	head, token := value[:index+1], value[index+1:]
	switch {
	case strings.HasPrefix(token, "@"):
		return "mention", token[1:], head
	case strings.HasPrefix(token, "$"):
		return "skill", token[1:], head
	case !strings.HasPrefix(value, "/") && explicit:
		return "path", token, head
	default:
		return "", "", head
	}
}

func (m *model) completionCandidates(value string, explicit bool) (string, []cand) {
	kind, prefix, head := completionKind(value, explicit)
	if kind == "" {
		if m.hostCompletion != nil && m.hostCompletion.cancel != nil {
			m.hostCompletion.cancel()
			m.hostCompletion = nil
		}
		return completions(value, m.modelCands(), m.providerCands(), nil, effortCandsFor(m.effortsFor()))
	}
	if current := m.hostCompletion; current != nil && current.value == value && current.rootID == m.sessionID && current.agentID == m.agentOpen {
		if explicit {
			current.tab = true
		}
		if current.ready {
			return head, current.candidates
		}
		return head, nil
	}
	if m.hostCompletion != nil && m.hostCompletion.cancel != nil {
		m.hostCompletion.cancel()
	}
	if m.client == nil || m.prog == nil {
		m.hostCompletion = nil
		return head, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	request := &clientCompletion{value: value, rootID: m.sessionID, agentID: m.agentOpen, tab: explicit, cancel: cancel}
	m.hostCompletion = request
	client, program := m.client, m.prog
	go func() {
		defer cancel()
		if !explicit {
			timer := time.NewTimer(80 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		result, err := client.CompleteWorkspace(ctx, daemon.CompletionParams{RootID: request.rootID, AgentID: request.agentID, Kind: kind, Prefix: prefix, Limit: 64})
		if ctx.Err() == context.Canceled && err != nil {
			return
		}
		program.Send(clientCompletionMsg{request: request, result: result, err: err})
	}()
	return head, nil
}

func (m *model) applyHostCompletion(msg clientCompletionMsg) (tea.Model, tea.Cmd) {
	if m.hostCompletion != msg.request || m.input.Value() != msg.request.value || m.sessionID != msg.request.rootID || m.agentOpen != msg.request.agentID {
		return m, nil
	}
	if msg.err != nil {
		m.hostCompletion = nil
		if msg.request.tab {
			return m, m.toastError("host completion: " + msg.err.Error())
		}
		return m, nil
	}
	request := msg.request
	request.ready = true
	request.candidates = make([]cand, len(msg.result.Candidates))
	for i, candidate := range msg.result.Candidates {
		request.candidates[i] = cand{Text: candidate.Text, Desc: candidate.Description}
	}
	if len(request.candidates) == 0 {
		m.menu = nil
		return m, nil
	}
	_, _, head := completionKind(request.value, request.tab)
	m.menu = &menu{head: head, cands: request.candidates}
	if request.tab {
		m.menuCycle(0)
	}
	return m, nil
}

// Skill discovery belongs to the execution host, including startup diagnostics.
type clientSkillsMsg struct {
	rootID   string
	warnings []string
}

func (m *model) requestHostSkills() tea.Cmd {
	client, rootID := m.client, m.sessionID
	if client != nil && rootID == "" {
		rootID = client.RootID()
	}
	if client == nil || rootID == "" {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		result, err := client.CompleteWorkspace(ctx, daemon.CompletionParams{RootID: rootID, Kind: "skill", Limit: 64})
		if err != nil {
			return nil
		}
		return clientSkillsMsg{rootID: rootID, warnings: result.Warnings}
	}
}
