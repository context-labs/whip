package rpc_test

import (
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func TestForkRPCImportsEveryDepthAndRetainsDeletedAdmission(t *testing.T) {
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
				call[protocol.Session](t, c, "sessions.lifecycle", protocol.LifecycleParams{SessionID: tree.Root.ID, Lifecycle: "stopped"})
				waitHistoryTurn(t, c, "child")
			} else {
				submitHistoryTurn(t, c, owner, "first")
			}
			history := call[protocol.HistoryResult](t, c, "sessions.history", protocol.HistoryParams{SessionID: owner, Limit: 100})
			source := call[protocol.Session](t, c, "sessions.get", protocol.SessionParams{SessionID: owner})
			params := protocol.ForkParams{
				ForkID: "fork", SessionID: owner, ExpectedHistoryRevision: history.Snapshot.Revision,
				ExpectedConfigRevision: source.ConfigRevision, ObservedThrough: history.Snapshot.ThroughSequence,
				KeepThrough: history.Snapshot.ThroughSequence, Title: new("Branch"),
			}
			bad := params
			bad.ExpectedConfigRevision++
			requireHistoryError(t, c, "sessions.fork", bad, "CONFLICT")
			bad = params
			bad.ExpectedHistoryRevision++
			requireHistoryError(t, c, "sessions.fork", bad, "CONFLICT")
			bad = params
			bad.ObservedThrough++
			requireHistoryError(t, c, "sessions.fork", bad, "CONFLICT")
			bad = params
			bad.KeepThrough = 1
			requireHistoryError(t, c, "sessions.fork", bad, "INVALID")
			result := call[protocol.ForkResult](t, c, "sessions.fork", params)
			if result.Deleted || result.Root == nil || result.Tree == nil || result.Root.ParentID != nil || result.Root.ID == owner || result.Root.HistoryRevision != 1 || result.Root.ConfigRevision != 1 || *result.Tree.Metadata.Title != "Branch" {
				t.Fatalf("fork did not create an independent root: %+v", result)
			}
			imported := call[protocol.HistoryResult](t, c, "sessions.history", protocol.HistoryParams{SessionID: result.Root.ID, Limit: 100})
			if len(imported.Items) != len(history.Items) {
				t.Fatalf("imported %d messages, want %d", len(imported.Items), len(history.Items))
			}
			for i, message := range imported.Items {
				original := history.Items[i]
				if message.TurnID != nil || message.InputID != nil || message.Source == nil || message.Source.SessionID != owner || message.Source.MessageID != original.ID || message.ID == original.ID || message.GroupID == original.GroupID || !reflect.DeepEqual(message.Parts, original.Parts) {
					t.Fatalf("imported message lost content or fabricated execution: %+v", message)
				}
			}
			call[protocol.Session](t, c, "sessions.lifecycle", protocol.LifecycleParams{SessionID: owner, Lifecycle: "stopped"})
			call[protocol.DeleteResult](t, c, "sessions.delete", protocol.SessionParams{SessionID: owner})
			if retry := call[protocol.ForkResult](t, c, "sessions.fork", params); !reflect.DeepEqual(retry.Fork, result.Fork) || retry.Root == nil {
				t.Fatalf("source deletion changed fork receipt: %+v", retry)
			}
			submitHistoryTurn(t, c, result.Root.ID, "continued")
			continued := call[protocol.HistoryResult](t, c, "sessions.history", protocol.HistoryParams{SessionID: result.Root.ID, Limit: 100})
			if len(continued.Items) != len(imported.Items)+2 || continued.Items[len(imported.Items)].Source != nil || continued.Items[len(imported.Items)].TurnID == nil {
				t.Fatalf("fork did not continue using local execution: %+v", continued)
			}
			call[protocol.Session](t, c, "sessions.lifecycle", protocol.LifecycleParams{SessionID: result.Root.ID, Lifecycle: "stopped"})
			call[protocol.DeleteResult](t, c, "sessions.delete", protocol.SessionParams{SessionID: result.Root.ID})
			deleted := call[protocol.ForkResult](t, c, "sessions.fork", params)
			if !deleted.Deleted || deleted.Root != nil || deleted.Tree != nil || !reflect.DeepEqual(deleted.Fork, result.Fork) {
				t.Fatalf("retry recreated a deleted fork: %+v", deleted)
			}
			params.Title = new("different")
			requireHistoryError(t, c, "sessions.fork", params, "CONFLICT")
		})
	}
}
