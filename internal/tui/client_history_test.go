package tui

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

func TestPagedHistoryUsesRawSequencesForRewindAndKeepsRenderIndices(t *testing.T) {
	m := compactCmdModel()
	m.sessionID = "root"
	m.clientView.messages = m.snapshotHistory(session.RootSnapshot{
		RootID: "root", HistoryRevision: 4, FirstMessageSeq: 91,
		MessageSeqs: []int{91, 94}, Omitted: map[string]bool{"messages": true}, Messages: []llm.Message{
			{Role: "user", Content: "first", Authored: true}, {Role: "user", Content: "second", Authored: true},
		},
	})
	entries := m.rewindEntries()
	if len(entries) != 2 || entries[0].cut != 91 || entries[1].cut != 94 || entries[0].index != 1 || entries[1].index != 2 {
		t.Fatalf("rewind entries=%+v", entries)
	}
}

func TestOlderHistoryPagePreservesSnapshotOverlapAndRejectsStaleRevision(t *testing.T) {
	m := compactCmdModel()
	m.sessionID = "root"
	snapshot := session.RootSnapshot{
		RootID: "root", HistoryRevision: 2, FirstMessageSeq: 100, MessageSeqs: []int{100, 101},
		Omitted: map[string]bool{"messages": true}, Messages: []llm.Message{{Role: "user", Content: "newer", Authored: true}, {Role: "assistant", Content: "latest"}},
	}
	m.clientView.messages = m.snapshotHistory(snapshot)
	request := *m.historyState("root")
	old := llm.Message{Role: "user", Content: "older", Authored: true}
	duplicate := llm.Message{Role: "user", Content: "incorrect duplicate"}
	_, cmd := m.applyOlderHistory(clientHistoryMsg{request: request, page: session.BoundedTranscriptPage{
		HistoryRevision: 2, NextSeq: 90, ThroughSeq: 101, HasMore: true,
		Messages: []session.TranscriptPageEntry{{Seq: 90, Message: &old}, {Seq: 100, Message: &duplicate}},
	}})
	if cmd != nil || len(m.clientView.messages) != 4 || m.clientView.messages[1].RawSequence != 90 || m.clientView.messages[2].Content != "newer" {
		t.Fatal("page duplicated or replaced snapshot messages")
	}
	m.clientView.messages = m.snapshotHistory(snapshot)
	if len(m.clientView.messages) != 4 || m.historyState("root").before != 90 {
		t.Fatal("overlapping snapshot discarded loaded older page")
	}
	snapshot.HistoryRevision = 3
	m.clientView.messages = m.snapshotHistory(snapshot)
	before := append([]llm.Message{}, m.clientView.messages...)
	m.applyOlderHistory(clientHistoryMsg{request: request, page: session.BoundedTranscriptPage{HistoryRevision: 2, Messages: []session.TranscriptPageEntry{{Seq: 90, Message: &old}}}})
	if !reflect.DeepEqual(before, m.clientView.messages) {
		t.Fatal("pre-rewind page changed post-rewind view")
	}
}

func TestChildHistoryPagingDoesNotChangeRootModelView(t *testing.T) {
	m := compactCmdModel()
	m.sessionID, m.agentOpen = "root", "child"
	m.clientView.messages = []llm.Message{{Role: "system"}, {Role: "user", Content: "root only", RawSequence: 1}}
	before := append([]llm.Message{}, m.clientView.messages...)
	m.agentMessages = map[string][]llm.Message{"child": {{Role: "assistant", Content: "child recent", RawSequence: 50}}}
	m.setHistoryPage("child", session.BoundedTranscriptPage{HistoryRevision: 2, NextSeq: 50, ThroughSeq: 50, HasMore: true})
	request := *m.historyState("child")
	older := llm.Message{Role: "user", Content: "child older"}
	m.applyOlderHistory(clientHistoryMsg{request: request, page: session.BoundedTranscriptPage{
		HistoryRevision: 2, NextSeq: 1, ThroughSeq: 50,
		Messages: []session.TranscriptPageEntry{{Seq: 1, Message: &older}},
	}})
	if !reflect.DeepEqual(before, m.clientView.messages) {
		t.Fatal("child inspection changed root transcript")
	}
	if len(m.agentMessages["child"]) != 2 {
		t.Fatal("child page not retained")
	}
}

func TestLargeHistoryMessageExplicitlyIdentifiesContentReference(t *testing.T) {
	messages := pageMessages(session.BoundedTranscriptPage{Messages: []session.TranscriptPageEntry{{Seq: 7, Body: &session.RuntimeValue{ReferenceID: "body-7", Size: 1 << 20}}}})
	if len(messages) != 1 || messages[0].RawSequence != 7 || !strings.Contains(messages[0].TextContent(), "preview omitted; content reference body-7") {
		t.Fatalf("large-body presentation=%+v", messages)
	}
}

type historyConnection struct {
	*fakeDaemonConnection
	requests chan daemon.HistoryPageParams
}

func (c *historyConnection) HistoryPage(ctx context.Context, p daemon.HistoryPageParams) (session.BoundedTranscriptPage, error) {
	c.requests <- p
	return session.BoundedTranscriptPage{HistoryRevision: *p.Revision, NextSeq: 1, Messages: []session.TranscriptPageEntry{}}, nil
}

func TestScrollHistoryRequestsOneBoundedPage(t *testing.T) {
	snapshot := session.RootSnapshot{
		RootID: "root", HistoryRevision: 7, FirstMessageSeq: 100, MessageSeqs: []int{100},
		Omitted: map[string]bool{"messages": true}, Meta: session.Meta{ID: "root"}, Messages: []llm.Message{{Role: "user", Content: "recent"}},
	}
	connection := &historyConnection{fakeDaemonConnection: newFakeDaemonConnection(snapshot), requests: make(chan daemon.HistoryPageParams, 1)}
	client, err := NewClient(ClientOptions{
		ClientID: "tui", RootID: "root", RetryMin: time.Millisecond, RetryMax: time.Millisecond,
		Connector: func(context.Context, map[string]int64) (daemonConnection, error) { return connection, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	client.Start()
	defer client.Close()
	waitClientState(t, client, ClientLive)
	m := compactCmdModel()
	m.client, m.sessionID = client, "root"
	m.clientView.messages = m.snapshotHistory(snapshot)
	command := m.requestOlderHistory()
	if command == nil || m.requestOlderHistory() != nil {
		t.Fatal("page admission did not limit in-flight reads")
	}
	result := command().(clientHistoryMsg)
	if result.err != nil {
		t.Fatal(result.err)
	}
	request := <-connection.requests
	if request.RootID != "root" || request.AgentID != "root" || request.BeforeSeq != 100 || request.Limit != 64 || request.MaxBytes != 256<<10 || *request.Revision != 7 || !request.Recent {
		t.Fatalf("unbounded or incorrect request=%+v", request)
	}
	if len(connection.commands) != 0 {
		t.Fatal("history query entered execution command path")
	}
}

func TestLargeUserHistoryPreservesRewindCut(t *testing.T) {
	m := modelCmdModel()
	m.clientView.messages = pageMessages(session.BoundedTranscriptPage{Messages: []session.TranscriptPageEntry{{Seq: 77, Role: "user", Authored: true, Body: &session.RuntimeValue{ReferenceID: "large-user"}}}})
	entries := m.rewindEntries()
	if len(entries) != 1 || entries[0].cut != 77 {
		t.Fatalf("large user omitted from rewind: %+v", entries)
	}
}
