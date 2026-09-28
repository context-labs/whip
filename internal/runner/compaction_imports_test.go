package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

func importedMessages(raw []session.Message) []session.Message {
	result := slices.Clone(raw)
	for i := range result {
		message := &result[i]
		message.Source = &session.MessageSource{SessionID: "source", MessageID: message.ID, Sequence: message.Sequence}
		message.ID = session.MessageID("import_" + string(message.ID))
		message.GroupID = session.HistoryGroupID("import_" + string(message.GroupID))
		message.TurnID = ""
		message.InputID = nil
	}
	return result
}

func TestImportedCompactionPinsPartialGroupAndReleasesWholeGroup(t *testing.T) {
	ledger := newCompactionLedger(importedMessages(longExchange(false)))
	before := slices.Clone(ledger.raw)
	provider := providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
		if request.Purpose != "compaction" || len(request.Tools) != 0 {
			t.Fatal("history maintenance invoked ordinary execution", request)
		}
		opening := 0
		for _, message := range request.Messages {
			if reflect.DeepEqual(message.Parts, before[0].Parts) {
				opening++
			}
		}
		if opening != 1 {
			t.Fatalf("helper lost or duplicated imported opening: %d", opening)
		}
		return model.Response{Parts: []session.Part{{Type: "text", Text: "summary"}}}, nil
	})
	r, err := New(provider, ledger, ledger, nil, forbiddenExecutor{}, nil, nil, ledger, nil)
	if err != nil {
		t.Fatal(err)
	}
	turn := session.Turn{ID: "maintenance", SessionID: "owner", Kind: session.CompactInput}
	outcome, err := r.Run(t.Context(), turn, session.Configuration{})
	if err != nil || outcome.State != session.Succeeded || len(ledger.specs) != 3 {
		t.Fatalf("outcome=%+v err=%v attempts=%d", outcome, err, len(ledger.specs))
	}
	for _, draft := range ledger.drafts {
		if !slices.Equal(draft.PinnedMessageIDs, []session.MessageID{before[0].ID}) {
			t.Fatalf("partial imported group lost its opening pin: %+v", draft)
		}
	}
	// A new runner must restore an imported pin without a fabricated local input.
	restarted, err := New(provider, ledger, ledger, nil, forbiddenExecutor{}, nil, nil, ledger, nil)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := restarted.selection(t.Context(), "owner")
	if err != nil || selected.through() != 247 || len(selected.pins) != 1 || selected.pins[0].InputID != nil || selected.pins[0].TurnID != "" {
		t.Fatalf("restored selection=%+v err=%v", selected, err)
	}
	request := model.Request{}
	if _, err := restarted.rawContext(t.Context(), "owner", &request, selected); err != nil {
		t.Fatal(err)
	}
	if len(request.Messages) != 5 || !reflect.DeepEqual(request.Messages[1].Parts, before[0].Parts) {
		t.Fatalf("selected context lost its exact opening: %+v", request.Messages)
	}
	for i, message := range request.Messages[2:] {
		if message.ID != before[247+i].ID || !reflect.DeepEqual(message.Parts, before[247+i].Parts) {
			t.Fatalf("selected context split or changed the latest tool exchange: %+v", message)
		}
	}
	folds := 3
	selected, err = restarted.foldToBoundary(t.Context(), turn, session.Configuration{}, selected, 250, &folds)
	if err != nil || selected.through() != 250 || len(selected.pins) != 0 || len(selected.base.PinnedMessageIDs) != 0 {
		t.Fatalf("fully covered imported group retained a pin: selection=%+v err=%v", selected, err)
	}
	if _, err := restarted.rawContext(t.Context(), "owner", &request, selected); err != nil || len(request.Messages) != 1 {
		t.Fatalf("fully covered context=%+v err=%v", request.Messages, err)
	}
	if !reflect.DeepEqual(ledger.raw, before) || len(ledger.messages) != 0 || len(ledger.specs) != 4 {
		t.Fatal("compaction altered imported evidence or invented conversation work")
	}
	for _, spec := range ledger.specs {
		if spec.TurnID != turn.ID || spec.Request.Purpose != "compaction" {
			t.Fatalf("attempt is not owned by current maintenance: %+v", spec)
		}
	}
}

func TestCompactionToolExchangesUseHistoryGroups(t *testing.T) {
	for _, origin := range []string{"native", "imported"} {
		for _, pairing := range []string{"same group", "different groups"} {
			t.Run(origin+"/"+pairing, func(t *testing.T) {
				raw := []session.Message{
					{ID: "opening", SessionID: "owner", GroupID: "first", TurnID: "first", OpeningInput: true, InputID: new(session.InputID("input")), Sequence: 1, Role: session.User, Parts: []session.Part{{Type: "text", Text: "opening"}}},
					{ID: "call", SessionID: "owner", GroupID: "first", TurnID: "first", Sequence: 2, Role: session.Assistant, Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "shared-call", Name: "execute", Arguments: json.RawMessage(`{}`)}}}},
					{ID: "result", SessionID: "owner", GroupID: "first", TurnID: "first", Sequence: 3, Role: session.Tool, Parts: []session.Part{{Type: "tool_result", Result: &session.ToolResult{CallID: "shared-call", Output: "result"}}}},
					{ID: "answer", SessionID: "owner", GroupID: "first", TurnID: "first", Sequence: 4, Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: "answer"}}},
				}
				if pairing == "different groups" {
					raw[2].GroupID, raw[2].TurnID = "second", "second"
				} else {
					// Reusing a call ID in a later complete exchange is valid.
					for _, message := range slices.Clone(raw) {
						message.ID = session.MessageID("second_" + string(message.ID))
						message.GroupID, message.TurnID = "second", "second"
						message.Sequence += 4
						raw = append(raw, message)
					}
				}
				if origin == "imported" {
					raw = importedMessages(raw)
				}
				ledger := newCompactionLedger(raw)
				r := &Runner{compactions: ledger}
				snapshot, err := ledger.HistorySnapshot(t.Context(), "owner")
				if err != nil {
					t.Fatal(err)
				}
				selection := contextSelection{snapshot: snapshot}
				boundary, splitErr := r.splitBoundary(t.Context(), "owner", selection)
				request := model.Request{}
				through, _, prefixErr := r.compactionPrefix(t.Context(), "owner", &request, selection, snapshot.ThroughSequence)
				if pairing == "different groups" {
					if splitErr == nil || prefixErr == nil {
						t.Fatalf("paired a call across history groups: split=%v prefix=%v", splitErr, prefixErr)
					}
				} else if splitErr != nil || prefixErr != nil || boundary != 7 || through != 8 || len(request.Messages) != 8 {
					t.Fatalf("valid exchanges: boundary=%d through=%d messages=%d split=%v prefix=%v", boundary, through, len(request.Messages), splitErr, prefixErr)
				}
			})
		}
	}
}

func TestImportedCompactionKeepsWholeBatchAcrossPagesAndRecentGroups(t *testing.T) {
	raw := compactionMessages(1, 98)
	raw = append(raw,
		session.Message{ID: "calls", SessionID: "owner", GroupID: "old1", TurnID: "old1", Sequence: 99, Role: session.Assistant, Parts: []session.Part{
			{Type: "tool_call", Call: &session.ToolCall{ID: "a", Name: "execute", Arguments: json.RawMessage(`{}`)}},
			{Type: "tool_call", Call: &session.ToolCall{ID: "b", Name: "execute", Arguments: json.RawMessage(`{}`)}},
		}},
	)
	for i, id := range []string{"a", "b"} {
		raw = append(raw, session.Message{ID: session.MessageID("result_" + id), SessionID: "owner", GroupID: "old1", TurnID: "old1", Sequence: int64(100 + i), Role: session.Tool, Parts: []session.Part{{Type: "tool_result", Result: &session.ToolResult{CallID: id, Output: "exact result"}}}})
	}
	raw = importedMessages(raw)
	// Retain four distinct recent groups, mixing imported and native history.
	for i, message := range compactionMessages(4, 1) {
		message.Sequence += 101
		message.ID = session.MessageID(fmt.Sprintf("tail%d", message.Sequence))
		message.TurnID = session.TurnID(message.ID)
		message.GroupID = session.HistoryGroupID(message.ID)
		if i%2 == 0 {
			message = importedMessages([]session.Message{message})[0]
		}
		raw = append(raw, message)
	}
	ledger := newCompactionLedger(raw)
	var sizes []int
	r, err := New(providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
		sizes = append(sizes, len(request.Messages))
		if len(sizes) == 2 {
			if len(request.Messages) != 5 || request.Messages[2].Parts[0].Call.ID != "a" || request.Messages[2].Parts[1].Call.ID != "b" || request.Messages[3].Parts[0].Result.CallID != "a" || request.Messages[4].Parts[0].Result.CallID != "b" {
				t.Fatal("page boundary split imported batch", request.Messages)
			}
		}
		return model.Response{Parts: []session.Part{{Type: "text", Text: "summary"}}}, nil
	}), ledger, ledger, nil, forbiddenExecutor{}, nil, nil, ledger, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := r.Run(t.Context(), session.Turn{ID: "maintenance", SessionID: "owner", Kind: session.CompactInput}, session.Configuration{})
	if err != nil || outcome.State != session.Succeeded || !slices.Equal(sizes, []int{98, 5}) {
		t.Fatalf("outcome=%+v err=%v helper sizes=%v", outcome, err, sizes)
	}
	first := ledger.values[session.CompactionID(string(ledger.specs[0].ID)+"_summary")]
	last := ledger.values[*ledger.head.CompactionID]
	if first.ThroughSequence != 98 || !slices.Equal(first.PinnedMessageIDs, []session.MessageID{raw[0].ID}) || last.ThroughSequence != 101 || len(last.PinnedMessageIDs) != 0 {
		t.Fatalf("wrong batch coverage or pin lifetime: first=%+v last=%+v", first, last)
	}
	selection, err := r.selection(t.Context(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	request := model.Request{}
	if _, err := r.rawContext(t.Context(), "owner", &request, selection); err != nil || len(request.Messages) != 5 {
		t.Fatalf("recent group context=%+v err=%v", request.Messages, err)
	}
	for i, message := range request.Messages[1:] {
		if message.ID != raw[101+i].ID {
			t.Fatal("lost a recent imported or native group", message)
		}
	}
}
