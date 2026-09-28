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

func TestAutomaticTitleRPCPreservesPolicyAndImmutableEvidence(t *testing.T) {
	r, c := fixture(t)
	created := create(t, c)
	if !created.Root.Configuration.AutomaticTitle {
		t.Fatal("builtin policy was not exposed")
	}
	lookup := protocol.TreeParams{TreeID: created.Tree.ID}
	var absent protocol.AutomaticTitleDecision
	if err := c.Call(t.Context(), "trees.title_decision", lookup, &absent); err == nil {
		t.Fatal("reading an uninitialized tree created naming intent")
	}
	params := protocol.SubmitParams{Identity: protocol.RequestIdentity{ClientID: "title", RequestID: "first"}, SessionID: created.Root.ID, Source: "user", Parts: []protocol.Part{{Type: "text", Text: "  Name this useful authored conversation.  "}}}
	admitted := call[protocol.Admission](t, c, "sessions.submit", params)
	decision := call[protocol.AutomaticTitleDecision](t, c, "trees.title_decision", lookup)
	if decision.Reason != "eligible" || decision.ReceiptIdentity == nil || *decision.InputID != admitted.Input.ID || decision.Source != "Name this useful authored conversation." {
		t.Fatal(decision)
	}
	fallback := call[protocol.Tree](t, c, "trees.get", lookup)
	if fallback.Metadata.Title == nil || *fallback.Metadata.Title != decision.Source || fallback.Revision != decision.ExpectedRevision {
		t.Fatal("admission did not expose atomic fallback", fallback, decision)
	}
	configured := call[protocol.Session](t, c, "sessions.configure", protocol.UpdateConfigurationParams{SessionID: created.Root.ID, ExpectedRevision: created.Root.ConfigRevision, Patch: protocol.ConfigPatch{AutomaticTitle: new(false)}})
	if configured.Configuration.AutomaticTitle {
		t.Fatal("explicit false disappeared")
	}
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var completed protocol.Admission
	for {
		err := c.Call(ctx, "receipts.get", *decision.ReceiptIdentity, &completed)
		if err == nil && completed.Turn != nil && completed.Turn.FinishedAt != nil {
			break
		}
		var remote *client.Error
		if err != nil && (!errors.As(err, &remote) || remote.Kind != "NOT_FOUND") {
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("title did not settle", r.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	if completed.Input.Kind != "automatic_title" || completed.Input.Source != "agent" || len(completed.Input.Parts) != 0 || completed.Turn.Kind != "automatic_title" || completed.Turn.ConfigRevision != decision.ConfigRevision {
		t.Fatal("maintenance lost captured provenance", completed)
	}
	attempts := call[protocol.ModelAttemptsResult](t, c, "turns.attempts", protocol.ModelAttemptsParams{TurnID: completed.Turn.ID, Limit: 100})
	if len(attempts.Items) != 1 || attempts.Items[0].Request.Purpose != "automatic_title" || attempts.Items[0].MessageID != nil {
		t.Fatal(attempts)
	}
	resultParams := protocol.AutomaticTitleResultParams{TreeID: created.Tree.ID, AttemptID: attempts.Items[0].ID}
	result := call[protocol.AutomaticTitleResult](t, c, "trees.title_result", resultParams)
	if !result.Applied || result.Text != "ack: "+decision.Source {
		t.Fatal(result)
	}
	named := call[protocol.Tree](t, c, "trees.get", lookup)
	call[protocol.Tree](t, c, "trees.update", protocol.UpdateTreeParams{TreeID: created.Tree.ID, ExpectedRevision: named.Revision, Metadata: protocol.TreeMetadata{Title: nil, Pinned: true}})
	if !reflect.DeepEqual(result, call[protocol.AutomaticTitleResult](t, c, "trees.title_result", resultParams)) || !reflect.DeepEqual(decision, call[protocol.AutomaticTitleDecision](t, c, "trees.title_decision", lookup)) {
		t.Fatal("read projected mutable selected title or configuration")
	}
	resultParams.TreeID = "foreign"
	var foreign protocol.AutomaticTitleResult
	if err := c.Call(t.Context(), "trees.title_result", resultParams, &foreign); err == nil {
		t.Fatal("foreign result exposed")
	}
	history := call[protocol.HistoryResult](t, c, "sessions.history", protocol.HistoryParams{SessionID: created.Root.ID, Limit: 100})
	if len(history.Items) != 2 {
		t.Fatal("naming added history", history)
	}
}
