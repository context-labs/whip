package tui

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/protocol"
)

type nativeInputRecall struct {
	owner           protocol.ID
	revision        protocol.Counter
	entries         []nativeDraft
	index           int
	draft           nativeDraft
	loaded, loading bool
}

func (m *nativeModel) captureDraft() nativeDraft {
	text := m.input.Value()
	return nativeDraft{text: text, pastes: m.livePastes(text), images: m.liveImages(text), design: m.draftDesign}
}

func (m *nativeModel) applyDraft(draft nativeDraft) {
	m.input.SetValue(draft.text)
	m.pastes, m.images, m.draftDesign = maps.Clone(draft.pastes), maps.Clone(draft.images), draft.design
	m.sizeInput()
}

// Local recall is an expendable bounded presentation cache, not a second input
// ledger. Original parts remain in the journal/host; recalling never resends.
func (m *nativeModel) rememberDraft(draft nativeDraft) bool {
	if draft.text == "" || draft.bytes() > nativeDraftLimit {
		return false
	}
	if nativeRecallSecret(draft.text) {
		return false
	}
	m.recall = nil
	m.recallLocal = slices.DeleteFunc(m.recallLocal, func(old nativeDraft) bool { return reflect.DeepEqual(old, draft) })
	m.recallLocal = append(m.recallLocal, draft)
	bytes := 0
	start := len(m.recallLocal)
	for start > 0 && len(m.recallLocal)-start < 32 {
		next := m.recallLocal[start-1].bytes()
		if bytes+next > 1<<20 {
			break
		}
		bytes += next
		start--
	}
	m.recallLocal = slices.Clone(m.recallLocal[start:])
	return true
}

func (m *nativeModel) recallKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	name := key.String()
	if name != "up" && name != "down" {
		return nil, false
	}
	if m.recall != nil && (m.recall.owner != m.owner.ID || m.recall.revision != m.history.snapshot.Revision) {
		m.recall = nil
		m.status = "History changed; the displayed draft was kept. Press Up again to start a fresh recall."
		return nil, true
	}
	if name == "up" {
		now := time.Now()
		previous := m.recallUpAt
		m.recallUpAt = now
		if m.input.Line() != 0 || m.input.LineInfo().RowOffset != 0 {
			return nil, false
		}
		if !previous.IsZero() && now.Sub(previous) < 300*time.Millisecond {
			return nil, true
		}
	} else if m.input.Line() != m.input.LineCount()-1 || m.input.LineInfo().RowOffset < m.input.LineInfo().Height-1 {
		return nil, false
	}
	if m.attachment != nil || m.attachmentBusy {
		m.status = "Resolve the pending image upload before recalling another draft."
		return nil, true
	}
	if m.recall == nil {
		if name == "down" {
			return nil, true
		}
		draft := m.captureDraft()
		if draft.bytes() > nativeDraftLimit {
			m.status = "Save or shorten this draft before recalling history; it exceeds 256 KiB."
			return nil, true
		}
		m.recall = &nativeInputRecall{owner: m.owner.ID, revision: m.history.snapshot.Revision, entries: slices.Clone(m.recallLocal), index: len(m.recallLocal), draft: draft}
	}
	r := m.recall
	if name == "up" && r.index == 0 && !r.loaded {
		return m.loadRecall(), true
	}
	if name == "up" {
		r.index = max(r.index-1, 0)
	} else {
		r.index = min(r.index+1, len(r.entries))
	}
	if r.index == len(r.entries) {
		m.applyDraft(r.draft)
		m.recall = nil
	} else {
		m.applyDraft(r.entries[r.index])
		m.status = "Input recalled as an unsent draft. Down restores the newer draft; Enter is a new explicit submission."
	}
	return nil, true
}

func (m *nativeModel) escapeKey() tea.Cmd {
	if m.input.Value() != "" {
		if !m.escapeAt.IsZero() && time.Since(m.escapeAt) <= time.Second {
			m.escapeAt = time.Time{}
			if !m.rememberDraft(m.captureDraft()) {
				m.status = "Draft kept: it cannot be safely retained for recall."
				return nil
			}
			m.applyDraft(nativeDraft{})
			m.status = "Draft cleared locally; Up recalls its original text and attachments. Nothing was sent or cancelled."
			return nil
		}
		m.escapeAt = time.Now()
		m.status = "Press Escape again within one second to clear this draft; Up will recall it."
		return nil
	}
	if m.owner.ParentID != nil {
		return m.returnToRoot()
	}
	if m.ready && !m.cancelling && m.activity.ActiveTurn != nil {
		return m.cancelTurn(m.activity.ActiveTurn.ID)
	}
	if !m.escapeAt.IsZero() && time.Since(m.escapeAt) <= time.Second {
		m.escapeAt = time.Time{}
		return m.openHistoryDialog(false, 0)
	}
	m.escapeAt = time.Now()
	m.status = "Press Escape again within one second to inspect rewind boundaries."
	return nil
}

func (m *nativeModel) returnToRoot() tea.Cmd {
	if !m.navigationAllowed() {
		return nil
	}
	owner := m.owner
	return m.navigationRead("Return to root", func(ctx context.Context) nativeControlResult {
		seen := map[protocol.ID]bool{}
		for depth := 0; owner.ParentID != nil; depth++ {
			if depth >= 32 || seen[owner.ID] {
				return nativeControlResult{err: errors.New("agent ancestry exceeds its bound or contains a cycle")}
			}
			seen[owner.ID] = true
			var parent protocol.Session
			if err := m.connection.Call(ctx, "sessions.get", protocol.SessionParams{SessionID: *owner.ParentID}, &parent); err != nil {
				return nativeControlResult{err: err}
			}
			if parent.ID != *owner.ParentID || parent.TreeID != owner.TreeID {
				return nativeControlResult{err: errors.New("agent parent identity mismatch")}
			}
			owner = parent
		}
		return nativeControlResult{attach: &owner}
	})
}

func (m *nativeModel) interruptKey() tea.Cmd {
	now := time.Now()
	if m.ready && m.activity.ActiveTurn != nil {
		m.quitArmed = false
		target := m.activity.ActiveTurn.ID
		if m.interruptTarget == target && now.Sub(m.interruptAt) <= 2*time.Second && !m.cancelling {
			m.interruptTarget = ""
			return m.cancelTurn(target)
		}
		m.interruptTarget, m.interruptAt = target, now
		m.status = "Press Ctrl+C again within two seconds to cancel this exact turn. /quit detaches and leaves host work running."
		return nil
	}
	m.interruptTarget = ""
	if m.quitArmed && now.Sub(m.quitAt) <= 2*time.Second {
		return tea.Quit
	}
	m.quitArmed, m.quitAt = true, now
	m.status = "Press Ctrl+C again within two seconds to detach. Host work continues."
	return nil
}
