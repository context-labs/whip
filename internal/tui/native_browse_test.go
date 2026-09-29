package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

func TestNativeHistoryBrowsingKeepsLiveObservationAndBoundsEachPage(t *testing.T) {
	m, _ := nativeUIFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	var last *client.InputCommand
	for i := range 40 {
		var err error
		last, err = m.handle.Submission(protocol.SubmitParams{Source: "user", Delivery: "queued", Identity: protocol.RequestIdentity{ClientID: "history", RequestID: protocol.ID(fmt.Sprintf("input_%d", i))}, Parts: []protocol.Part{{Type: "text", Text: fmt.Sprintf("body %d", i)}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := last.Send(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := last.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	m.observer = nil
	nativeUIRead(t, m)
	if len(m.history.messages) != 64 || !m.history.earlier || m.history.messages[0].Sequence != 17 {
		t.Fatal("initial tail not bounded", len(m.history.messages), m.history.snapshot)
	}
	observer := m.observer
	older := m.command("/older")
	if older == nil {
		t.Fatal(m.status)
	}
	m.Update(older())
	if m.browse == nil || len(m.browse.transcript.messages) != 16 || m.browse.earlier || !m.browse.later || m.browse.transcript.messages[0].Sequence != 1 || m.observer != observer {
		t.Fatal("older page consumed or replaced live observation", m.status, m.browse)
	}
	input, err := m.handle.Submission(protocol.SubmitParams{Source: "user", Delivery: "queued", Identity: protocol.RequestIdentity{ClientID: "history", RequestID: "later"}, Parts: []protocol.Part{{Type: "text", Text: "new while browsing"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := input.Send(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := input.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	nativeUIRead(t, m)
	if m.history.messages[len(m.history.messages)-1].Sequence != 82 || len(m.browse.transcript.messages) != 16 || strings.Contains(nativeDisplayText(strings.Join(m.rows, "\n")), "new while browsing") {
		t.Fatal("live append was lost or rewritten into older page")
	}
	newer := m.command("/newer")
	if newer == nil {
		t.Fatal(m.status)
	}
	m.Update(newer())
	if len(m.browse.transcript.messages) != 64 || m.browse.transcript.messages[0].Sequence != 17 || !m.browse.earlier || !m.browse.later {
		t.Fatal("forward page invalid", m.browse)
	}
	// Returning to live while an older-page read is in flight invalidates only
	// that read; it must not drop a concurrent canonical observation.
	older = m.command("/older")
	stale := older()
	m.command("/latest")
	m.Update(stale)
	if m.browse != nil || m.browsing || m.observer != observer || !strings.Contains(nativeDisplayText(strings.Join(m.rows, "\n")), "new while browsing") {
		t.Fatal("late browse reply replaced live history")
	}
	if m.renderCache.bytes > nativeRenderBytes || m.history.bytes > nativeHistoryBytes {
		t.Fatal("display window exceeded bound")
	}
}

func TestNativeHistoryBrowseRejectsOwnerRevisionAndCursorTampering(t *testing.T) {
	params := protocol.HistoryPageParams{SessionID: "owner", Direction: "backward", Cursor: new(protocol.Counter(10)), ExpectedRevision: new(protocol.Counter(1)), Limit: 2}
	page := protocol.HistoryPageResult{Snapshot: nativeObservation(12).Snapshot, Messages: []protocol.Message{nativeMessage(8, "user", "eight"), nativeMessage(9, "assistant", "nine")}, NextCursor: new(protocol.Counter(8))}
	if _, err := checkedNativeBrowse(params, page); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*protocol.HistoryPageResult){
		func(p *protocol.HistoryPageResult) { p.Snapshot.SessionID = "other" },
		func(p *protocol.HistoryPageResult) { p.Snapshot.Revision = 2 },
		func(p *protocol.HistoryPageResult) { p.NextCursor = new(protocol.Counter(7)) },
		func(p *protocol.HistoryPageResult) {
			p.Messages = []protocol.Message{nativeMessage(10, "user", "outside")}
		},
		func(p *protocol.HistoryPageResult) { p.Messages = nil },
	} {
		copyValue := page
		mutate(&copyValue)
		if _, err := checkedNativeBrowse(params, copyValue); err == nil {
			t.Fatal("forged history page accepted", copyValue)
		}
	}
	m := &nativeModel{history: nativeTranscript{owner: "owner", snapshot: nativeObservation(12).Snapshot}, input: newInput(), width: 80, height: 24, browseRequest: 1}
	m.applyBrowse(nativeBrowseResult{request: 1, params: params, page: page})
	if m.browse == nil {
		t.Fatal(m.status)
	}
	replacement := protocol.HistoryPageResult{Snapshot: protocol.HistorySnapshot{SessionID: "owner", Revision: 2, ThroughSequence: 1, MessageCount: 1}, Messages: []protocol.Message{nativeMessage(1, "user", "retained")}}
	m.Update(nativeRead{page: &replacement})
	if m.browse != nil || !strings.Contains(m.status, "History changed") || len(m.history.messages) != 1 {
		t.Fatal("retired page remained visible", m.status)
	}
	m.browse = &nativeBrowse{}
	m.applyBrowse(nativeBrowseResult{request: m.browseRequest, err: &client.Error{Kind: "CONFLICT"}})
	if m.browse != nil || !strings.Contains(m.status, "History changed") || len(m.history.messages) != 1 {
		t.Fatal("known conflict left a stale page or discarded live history", m.status)
	}
}
