package tui

import (
	"context"
	"fmt"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

type clientHistoryPage struct {
	rootID   string
	agentID  string
	revision int64
	before   int
	through  int
	hasMore  bool
	loading  bool
	epoch    uint64
}

type clientHistoryMsg struct {
	request clientHistoryPage
	page    session.BoundedTranscriptPage
	err     error
}

func pageMessages(page session.BoundedTranscriptPage) []llm.Message {
	messages := make([]llm.Message, 0, len(page.Messages))
	for _, entry := range page.Messages {
		var message llm.Message
		if entry.Message != nil {
			message = *entry.Message
		} else if entry.Body != nil {
			message = llm.Message{Role: entry.Role, Authored: entry.Authored, SentAt: entry.SentAt, Content: fmt.Sprintf("[Large message %d: preview omitted; content reference %s]", entry.Seq, entry.Body.ReferenceID)}
		} else {
			continue
		}
		message.RawSequence = entry.Seq
		messages = append(messages, message)
	}
	return messages
}

func (m *model) historyState(id string) *clientHistoryPage {
	if m.historyPages == nil {
		m.historyPages = make(map[string]*clientHistoryPage)
	}
	return m.historyPages[id]
}

func (m *model) setHistoryPage(id string, page session.BoundedTranscriptPage) {
	previous := m.historyState(id)
	epoch := uint64(1)
	if previous != nil {
		epoch = previous.epoch + 1
	}
	m.historyPages[id] = &clientHistoryPage{
		rootID: m.sessionID, agentID: id, revision: page.HistoryRevision,
		before: page.NextSeq, through: page.ThroughSeq, hasMore: page.HasMore, epoch: epoch,
	}
}

func (m *model) snapshotHistory(snapshot session.RootSnapshot) []llm.Message {
	messages := append([]llm.Message{}, snapshot.Messages...)
	for i := range messages {
		if i < len(snapshot.MessageSeqs) {
			messages[i].RawSequence = snapshot.MessageSeqs[i]
		}
	}
	previous := m.historyState(snapshot.RootID)
	if previous != nil && previous.revision != snapshot.HistoryRevision {
		m.agentMessages = make(map[string][]llm.Message)
		m.agentOpen = ""
		m.rew = nil
		m.historyPages = make(map[string]*clientHistoryPage)
	}
	first := snapshot.FirstMessageSeq
	preserve := previous != nil && previous.revision == snapshot.HistoryRevision && first > 0 &&
		slices.ContainsFunc(m.clientView.messages, func(message llm.Message) bool { return message.RawSequence == first })
	state := &clientHistoryPage{
		rootID: snapshot.RootID, agentID: snapshot.RootID, revision: snapshot.HistoryRevision,
		before: first, hasMore: snapshot.Omitted["messages"], through: -1, epoch: 1,
	}
	if previous != nil {
		state.epoch = previous.epoch + 1
	}
	if preserve {
		prefix := []llm.Message{}
		for _, message := range m.clientView.messages {
			if message.RawSequence > 0 && message.RawSequence < first {
				prefix = append(prefix, message)
			}
		}
		messages = append(prefix, messages...)
		state.before, state.hasMore, state.loading, state.epoch = previous.before, previous.hasMore, previous.loading, previous.epoch
	}
	m.historyPages[snapshot.RootID] = state
	return append([]llm.Message{{Role: "system"}}, messages...)
}

// Each explicit scroll at the oldest loaded row fetches at most 64 messages
// and 256 KiB. History is never eagerly drained in a loop.
func (m *model) requestOlderHistory() tea.Cmd {
	id := m.sessionID
	if m.agentOpen != "" {
		id = m.agentOpen
	}
	state := m.historyState(id)
	if m.client == nil || state == nil || state.loading || !state.hasMore {
		return nil
	}
	state.loading = true
	request, client := *state, m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		revision := request.revision
		page, err := client.HistoryPage(ctx, daemon.HistoryPageParams{
			RootID: request.rootID, AgentID: request.agentID,
			BeforeSeq: request.before, ThroughSeq: request.through, Revision: &revision, Limit: 64, MaxBytes: 256 << 10, Recent: true,
		})
		return clientHistoryMsg{request: request, page: page, err: err}
	}
}

func (m *model) applyOlderHistory(msg clientHistoryMsg) (tea.Model, tea.Cmd) {
	state := m.historyState(msg.request.agentID)
	if state == nil || state.rootID != m.sessionID || state.epoch != msg.request.epoch || state.revision != msg.request.revision {
		return m, nil
	}
	state.loading = false
	if msg.err != nil {
		return m, m.toastError("older history: " + msg.err.Error())
	}
	if msg.page.HistoryRevision != state.revision {
		return m, m.requestClientSnapshot()
	}
	messages := pageMessages(msg.page)
	var current []llm.Message
	root := state.agentID == m.sessionID
	if root {
		current = m.clientView.messages
	} else {
		current = m.agentMessages[state.agentID]
	}
	seen := make(map[int]bool, len(current))
	for _, message := range current {
		seen[message.RawSequence] = true
	}
	merged := make([]llm.Message, 0, len(current)+len(messages))
	if root {
		merged = append(merged, llm.Message{Role: "system"})
	}
	for _, message := range messages {
		if !seen[message.RawSequence] {
			merged = append(merged, message)
		}
	}
	for _, message := range current {
		if message.RawSequence > 0 || !root {
			merged = append(merged, message)
		}
	}
	// Page entries are chronological; sorting also handles a snapshot that
	// arrives while an earlier read is in flight without duplicating messages.
	slices.SortStableFunc(merged, func(a, b llm.Message) int { return a.RawSequence - b.RawSequence })
	state.before, state.through, state.hasMore = msg.page.NextSeq, msg.page.ThroughSeq, msg.page.HasMore
	if root {
		m.clientView.messages = merged
	} else {
		m.agentMessages[state.agentID] = merged
	}
	visible := (root && m.agentOpen == "") || m.agentOpen == state.agentID
	if !visible {
		return m, nil
	}
	oldLines, offset := m.vp.TotalLineCount(), m.vp.YOffset()
	m.follow, m.sel = false, nil
	m.rebuildClientTranscript()
	m.refreshVP()
	m.vp.SetYOffset(offset + max(0, m.vp.TotalLineCount()-oldLines))
	if m.rew != nil {
		cut := m.rew.entries[m.rew.sel].cut
		m.rew.entries = m.rewindEntries()
		for i, entry := range m.rew.entries {
			if entry.cut == cut {
				m.rew.sel = i
				break
			}
		}
	}
	return m, nil
}
