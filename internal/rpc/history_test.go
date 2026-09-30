package rpc_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

func TestRewindRPCPreservesIdentityAndRejectsStaleHistoryAtEveryDepth(t *testing.T) {
	for _, depth := range []string{"root", "child"} {
		t.Run(depth, func(t *testing.T) {
			r, c := fixture(t)
			tree := create(t, c)
			owner := tree.Root.ID
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			if depth == "child" {
				spawned := call[protocol.SpawnSessionResult](t, c, "sessions.spawn", protocol.SpawnSessionParams{
					Identity: protocol.RequestIdentity{ClientID: "history", RequestID: "child"},
					ParentID: owner, Parts: []protocol.Part{{Type: "text", Text: "first"}},
				})
				owner = spawned.Session.ID
				waitHistoryTurn(t, c, "child")
			} else {
				submitHistoryTurn(t, c, owner, "first")
			}
			first := call[protocol.HistoryResult](t, c, "sessions.history", protocol.HistoryParams{SessionID: owner, Limit: 100})
			submitHistoryTurn(t, c, owner, "second")
			suffix := call[protocol.HistoryResult](t, c, "sessions.history", protocol.HistoryParams{SessionID: owner, After: first.Snapshot.ThroughSequence, Limit: 100})
			page := call[protocol.HistoryResult](t, c, "sessions.history", protocol.HistoryParams{SessionID: owner, Limit: 1})
			if len(page.Items) != 1 || page.Snapshot.Revision != 1 || page.Snapshot.ThroughSequence != 4 || page.Snapshot.MessageCount != 4 {
				t.Fatalf("snapshot must describe the whole active history, not just the page: %+v", page)
			}
			opening := page.Items[0]
			if !opening.OpeningInput || opening.GroupID == "" || opening.TurnID == nil || opening.InputID == nil || opening.Source != nil {
				t.Fatalf("native opening provenance=%+v", opening)
			}
			editParams := protocol.RewindParams{
				EditID: "edit", SessionID: owner, ExpectedRevision: page.Snapshot.Revision,
				ObservedThrough: page.Snapshot.ThroughSequence, KeepThrough: first.Snapshot.ThroughSequence,
			}
			stale := editParams
			stale.ObservedThrough = first.Snapshot.ThroughSequence
			requireHistoryError(t, c, "sessions.rewind", stale, "CONFLICT")
			split := editParams
			split.KeepThrough = 1
			requireHistoryError(t, c, "sessions.rewind", split, "INVALID")
			edit := call[protocol.HistoryEdit](t, c, "sessions.rewind", editParams)
			if edit.Revision != 2 || edit.KeepThrough != 2 || edit.ObservedThrough != 4 || len(edit.Digest) != 64 {
				t.Fatalf("edit=%+v", edit)
			}
			changed := editParams
			changed.KeepThrough = 0
			requireHistoryError(t, c, "sessions.rewind", changed, "CONFLICT")
			for _, method := range []string{"sessions.history", "sessions.observe"} {
				requireHistoryError(t, c, method, protocol.HistoryParams{SessionID: owner, After: 1, Limit: 100, ExpectedRevision: new(protocol.Counter(1))}, "CONFLICT")
			}
			requireHistoryError(t, c, "context.list", protocol.ContextHistoryParams{SessionID: owner, ThroughSequence: 4, Limit: 100, ExpectedRevision: new(protocol.Counter(1))}, "CONFLICT")
			requireHistoryError(t, c, "context.search", protocol.SearchHistoryParams{SessionID: owner, ThroughSequence: 4, Query: "second", Limit: 100, ExpectedRevision: new(protocol.Counter(1))}, "CONFLICT")
			current := call[protocol.SessionObservation](t, c, "sessions.observe", protocol.HistoryParams{SessionID: owner, Limit: 100, ExpectedRevision: new(edit.Revision)})
			if current.Snapshot.Revision != 2 || current.Snapshot.ThroughSequence != 2 || len(current.Messages) != 2 {
				t.Fatalf("rewound observation=%+v", current)
			}
			retired := call[protocol.ReadHistoryResult](t, c, "context.read", protocol.ReadHistoryParams{SessionID: owner, MessageID: suffix.Items[0].ID, Length: 65536})
			if retired.Message.RetiredBy == nil || *retired.Message.RetiredBy != edit.ID || retired.Message.RetiredRevision == nil || *retired.Message.RetiredRevision != edit.Revision {
				t.Fatalf("exact evidence lost retirement provenance: %+v", retired.Message)
			}
			call[protocol.Session](t, c, "sessions.lifecycle", protocol.LifecycleParams{SessionID: owner, Lifecycle: "active"})
			submitHistoryTurn(t, c, owner, "third")
			retried := call[protocol.HistoryEdit](t, c, "sessions.rewind", editParams)
			if !reflect.DeepEqual(edit, retried) {
				t.Fatalf("exact retry changed immutable edit: %+v / %+v", edit, retried)
			}
			latest := call[protocol.HistoryResult](t, c, "sessions.history", protocol.HistoryParams{SessionID: owner, Limit: 100})
			if latest.Snapshot.Revision != 2 || latest.Snapshot.ThroughSequence != 6 || len(latest.Items) != 4 || latest.Items[2].Sequence != 5 {
				t.Fatalf("retry removed new work or reused a retired sequence: %+v", latest)
			}
			turn := call[protocol.Turn](t, c, "turns.get", protocol.TurnParams{TurnID: *latest.Items[2].TurnID})
			if turn.HistoryRevision != 2 {
				t.Fatalf("turn lost captured history revision: %+v", turn)
			}
		})
	}
}

func submitHistoryTurn(t *testing.T, c *client.Client, owner, request protocol.ID) {
	t.Helper()
	call[protocol.Admission](t, c, "sessions.submit", protocol.SubmitParams{
		Identity: protocol.RequestIdentity{ClientID: "history", RequestID: request}, SessionID: owner,
		Source: "user", Parts: []protocol.Part{{Type: "text", Text: string(request)}},
	})
	waitHistoryTurn(t, c, request)
}

func waitHistoryTurn(t *testing.T, c *client.Client, request protocol.ID) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result, err := c.Wait(ctx, protocol.RequestIdentity{ClientID: "history", RequestID: request})
	if err != nil || result.Turn == nil || result.Turn.State != "succeeded" {
		t.Fatalf("turn=%+v err=%v", result.Turn, err)
	}
}

func requireHistoryError(t *testing.T, c *client.Client, method string, params any, kind string) {
	t.Helper()
	var result any
	err := c.Call(t.Context(), method, params, &result)
	var remote *client.Error
	if !errors.As(err, &remote) || remote.Kind != kind {
		t.Fatalf("%s: want %s, got %v", method, kind, err)
	}
}
