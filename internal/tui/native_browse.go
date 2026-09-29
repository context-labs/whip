package tui

import (
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

// Historical pages are separate from the live observer. Moving through them
// never consumes an observation cursor or accumulates the entire transcript.
type nativeBrowse struct {
	transcript nativeTranscript
	earlier    bool
	later      bool
}

type nativeBrowseResult struct {
	request, generation uint64
	params              protocol.HistoryPageParams
	page                protocol.HistoryPageResult
	err                 error
}

func (m *nativeModel) latest() {
	m.browseRequest++
	m.browsing, m.browse, m.follow = false, nil, true
	m.status = "Following the latest conversation."
	m.refresh()
}

func (m *nativeModel) browseHistory(direction string) tea.Cmd {
	if !m.ready || m.browsing {
		return nil
	}
	v := &m.history
	if m.browse != nil {
		v = &m.browse.transcript
	}
	if len(v.messages) == 0 || direction == "backward" && (m.browse == nil && !v.earlier || m.browse != nil && !m.browse.earlier) {
		m.status = "The beginning of this conversation is already displayed."
		return nil
	}
	if direction == "forward" && (m.browse == nil || !m.browse.later) {
		m.latest()
		return nil
	}
	cursor := v.messages[0].Sequence
	if direction == "forward" {
		cursor = v.messages[len(v.messages)-1].Sequence
	}
	params := protocol.HistoryPageParams{SessionID: m.owner.ID, Direction: direction, Cursor: new(cursor), ExpectedRevision: new(v.snapshot.Revision), Limit: 64}
	m.browseRequest++
	m.browsing = true
	m.input.Reset()
	request, generation, handle := m.browseRequest, m.generation, m.handle
	return func() tea.Msg {
		result := nativeBrowseResult{request: request, generation: generation, params: params}
		ctx, done, err := m.work.begin()
		if err != nil {
			result.err = err
			return result
		}
		defer done()
		result.page, result.err = handle.History(ctx, params)
		return result
	}
}

func (m *nativeModel) applyBrowse(value nativeBrowseResult) {
	if value.request != m.browseRequest || value.generation != m.generation {
		return
	}
	m.browsing = false
	if value.err != nil {
		if rejection, ok := errors.AsType[*client.Error](value.err); ok && rejection.Kind == "CONFLICT" {
			m.latest()
			m.status = "History changed; the older page was closed."
			return
		}
		m.status = "History page unavailable: " + value.err.Error() + ". /latest returns to live history."
		return
	}
	browse, err := checkedNativeBrowse(value.params, value.page)
	if err == nil && browse.transcript.snapshot.Revision != m.history.snapshot.Revision {
		err = errors.New("history changed while loading the page")
	}
	if err != nil {
		m.status = "History page rejected: " + err.Error()
		return
	}
	if len(browse.transcript.messages) == 0 {
		m.status = "No more messages in that direction. /latest returns to live history."
		return
	}
	m.browse, m.follow = browse, false
	m.notice = ""
	m.status = "Browsing history · /older · /newer · /latest"
	m.refresh()
	if value.params.Direction == "backward" {
		m.vp.GotoBottom()
	} else {
		m.vp.SetYOffset(0)
	}
}

func checkedNativeBrowse(params protocol.HistoryPageParams, page protocol.HistoryPageResult) (*nativeBrowse, error) {
	if params.ExpectedRevision == nil || params.Cursor == nil || page.Snapshot.Revision != *params.ExpectedRevision || params.Direction != "forward" && params.Direction != "backward" || len(page.Messages) > params.Limit {
		return nil, errors.New("history page revision or bounds mismatch")
	}
	value := &nativeBrowse{transcript: nativeTranscript{owner: params.SessionID}}
	if err := value.transcript.replace(page); err != nil {
		return nil, err
	}
	for _, message := range page.Messages {
		if params.Direction == "backward" && message.Sequence >= *params.Cursor || params.Direction == "forward" && message.Sequence <= *params.Cursor {
			return nil, errors.New("history page crossed its requested cursor")
		}
	}
	if len(page.Messages) == 0 {
		if page.NextCursor != nil {
			return nil, errors.New("empty history page has a continuation")
		}
		return value, nil
	}
	first, last := page.Messages[0].Sequence, page.Messages[len(page.Messages)-1].Sequence
	value.earlier, value.later = true, last < page.Snapshot.ThroughSequence
	if params.Direction == "backward" {
		value.earlier = page.NextCursor != nil
	}
	expected := last
	if params.Direction == "backward" {
		expected = first
	}
	if page.NextCursor != nil && *page.NextCursor != expected {
		return nil, fmt.Errorf("history continuation does not match %s page boundary", params.Direction)
	}
	return value, nil
}
