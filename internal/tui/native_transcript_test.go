package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

func nativeMessage(sequence int, role, text string) protocol.Message {
	return protocol.Message{ID: protocol.ID(fmt.Sprintf("message_%d", sequence)), SessionID: "owner", GroupID: "group", Sequence: protocol.Counter(sequence), Role: role, Parts: []protocol.Part{{Type: "text", Text: text}}}
}

func nativeObservation(sequence int, messages ...protocol.Message) client.Observation {
	return client.Observation{Epoch: "epoch", Snapshot: protocol.HistorySnapshot{SessionID: "owner", Revision: 1, ThroughSequence: protocol.Counter(sequence), MessageCount: protocol.Counter(sequence)}, Messages: messages, Cursor: client.ObservationCursor{After: protocol.Counter(sequence), Revision: new(protocol.Counter(1)), Epoch: "epoch"}}
}

func TestNativeTranscriptPreservesImportedProvenanceAndCanonicalParts(t *testing.T) {
	opening := nativeMessage(10, "user", "imported opening")
	opening.OpeningInput = true
	opening.Source = &protocol.MessageSource{SessionID: "source", MessageID: "original", Sequence: 30}
	opening.Parts = append(opening.Parts, protocol.Part{Type: "content", ReferenceID: "opaque_body"})
	page := nativeObservation(10, opening)
	v := nativeTranscript{owner: "owner"}
	if err := v.replace(protocol.HistoryPageResult{Snapshot: page.Snapshot, Messages: page.Messages, NextCursor: new(protocol.Counter(10))}); err != nil {
		t.Fatal(err)
	}
	opening.Source.SessionID = "mutated"
	opening.Parts[0].Text = "mutated"
	if got := v.messages[0]; !got.OpeningInput || got.GroupID != "group" || got.TurnID != nil || got.InputID != nil || got.Source.SessionID != "source" || got.Parts[0].Text != "imported opening" || !v.earlier {
		t.Fatalf("import changed: %+v", got)
	}
	call := nativeMessage(11, "assistant", "")
	call.Parts = []protocol.Part{{Type: "tool_call", Call: &protocol.ToolCall{ID: "call", Name: "execute", Arguments: []byte(`{"code":"print(1)"}`)}}}
	result := nativeMessage(12, "tool", "")
	result.Parts = []protocol.Part{{Type: "tool_result", Result: &protocol.ToolResult{CallID: "call", Output: "1\n"}}, {Type: "content", ReferenceID: "screenshot"}}
	if err := v.observe(nativeObservation(12, call, result)); err != nil {
		t.Fatal(err)
	}
	if text := nativeMessageText(v.messages[0]); text != "imported opening\n[attachment opaque_body]" {
		t.Fatal(text)
	}
	if text := nativeMessageText(v.messages[1]); !strings.Contains(text, "execute · call") || !strings.Contains(text, "print(1)") {
		t.Fatal(text)
	}
	if text := nativeMessageText(v.messages[2]); text != "result · call\n1\n\n[attachment screenshot]" {
		t.Fatal(text)
	}
	if v.messages[1].TurnID != nil || v.messages[2].TurnID != nil {
		t.Fatal("imported tool exchange fabricated local execution")
	}
}

func TestNativeTranscriptPreviewEpochRevisionAndSettlement(t *testing.T) {
	v := nativeTranscript{owner: "owner"}
	page := nativeObservation(1, nativeMessage(1, "user", "hello"))
	page.Preview = &protocol.MessagePreview{AttemptID: "attempt", TurnID: "turn", MessageID: "message_2", Text: "partial", Reasoning: "reason", Calls: []protocol.CallPreview{{Index: 0, Name: "execute", Arguments: "{"}}, Truncated: true}
	if err := v.observe(page); err != nil {
		t.Fatal(err)
	}
	if v.preview == nil || !v.preview.Truncated || len(v.preview.Calls) != 0 {
		t.Fatal(v.preview)
	}
	output := protocol.CellOutput{Epoch: "epoch", Preview: &protocol.CellOutputPreview{SessionID: "owner", TurnID: "turn", CellID: "cell", CallMessageID: "message_2", CallID: "call", HistoryRevision: 1, Revision: 1, Text: "live stdout"}}
	v.output(output)
	if v.cellOutput == nil {
		t.Fatal("current cell output discarded")
	}
	page = nativeObservation(2, nativeMessage(2, "assistant", "partial complete"))
	page.Preview = &protocol.MessagePreview{MessageID: "message_2", Text: "partial"}
	if err := v.observe(page); err != nil {
		t.Fatal(err)
	}
	if v.preview != nil {
		t.Fatal("committed message kept duplicate preview")
	}
	result := nativeMessage(3, "tool", "")
	result.Parts = []protocol.Part{{Type: "tool_result", Result: &protocol.ToolResult{CallID: "call", Output: "complete"}}}
	if err := v.observe(nativeObservation(3, result)); err != nil {
		t.Fatal(err)
	}
	v.output(output) // A raced earlier read must not revive settled output.
	if v.cellOutput != nil {
		t.Fatal("settled output revived")
	}
	page = nativeObservation(3)
	page.Epoch = "restart"
	if err := v.observe(page); err != nil {
		t.Fatal(err)
	}
	output.Preview.CallID = "new_call"
	v.output(output)
	if v.cellOutput != nil || len(v.messages) != 3 {
		t.Fatal("old process output survived or durable history disappeared")
	}
	page = nativeObservation(1, nativeMessage(1, "user", "retained"))
	page.Reset = true
	page.Snapshot.Revision, *page.Cursor.Revision = 2, 2
	if err := v.observe(page); err != nil {
		t.Fatal(err)
	}
	if len(v.messages) != 1 || v.snapshot.Revision != 2 {
		t.Fatal(v)
	}
	v.output(output)
	if v.cellOutput != nil {
		t.Fatal("old history output survived")
	}
}

func TestNativeTranscriptInvalidPagesDoNotPartiallyChangeDisplay(t *testing.T) {
	for name, edit := range map[string]func(*client.Observation){
		"owner":                     func(p *client.Observation) { p.Messages[0].SessionID = "other" },
		"revision":                  func(p *client.Observation) { p.Snapshot.Revision = 2 },
		"regression":                func(p *client.Observation) { p.Messages[0].Sequence = 1 },
		"old_identity_new_sequence": func(p *client.Observation) { p.Messages[0].ID = "message_1" },
		"duplicate_identity":        func(p *client.Observation) { p.Messages = append(p.Messages, p.Messages[0]) },
		"out_of_snapshot":           func(p *client.Observation) { p.Messages[0].Sequence = 3 },
		"retired":                   func(p *client.Observation) { p.Messages[0].RetiredBy = new(protocol.ID("edit")) },
		"oversized_message":         func(p *client.Observation) { p.Messages[0].Parts[0].Text = strings.Repeat("x", nativeHistoryBytes) },
		"oversized_preview": func(p *client.Observation) {
			p.Preview = &protocol.MessagePreview{Text: strings.Repeat("x", (128<<10)+1)}
		},
	} {
		t.Run(name, func(t *testing.T) {
			v := nativeTranscript{owner: "owner"}
			if err := v.observe(nativeObservation(1, nativeMessage(1, "user", "original"))); err != nil {
				t.Fatal(err)
			}
			page := nativeObservation(2, nativeMessage(2, "assistant", "new"))
			edit(&page)
			if err := v.observe(page); err == nil {
				t.Fatal("invalid observation accepted")
			}
			if len(v.messages) != 1 || v.snapshot.ThroughSequence != 1 || v.messages[0].Parts[0].Text != "original" {
				t.Fatal("failed observation changed visible history")
			}
		})
	}
}

func TestNativeTranscriptBoundsKeepWholeMessagesAndExplicitHistoryGap(t *testing.T) {
	v := nativeTranscript{owner: "owner"}
	for i := 1; i <= nativeHistoryMessages+5; i++ {
		if err := v.observe(nativeObservation(i, nativeMessage(i, "user", "entry"))); err != nil {
			t.Fatal(err)
		}
	}
	if len(v.messages) != nativeHistoryMessages || !v.earlier || v.messages[0].Sequence != 6 {
		t.Fatal(len(v.messages), v.earlier, v.messages[0])
	}
	body := strings.Repeat("界", (2<<20)/3)
	for i := nativeHistoryMessages + 6; i < nativeHistoryMessages+12; i++ {
		if err := v.observe(nativeObservation(i, nativeMessage(i, "assistant", body))); err != nil {
			t.Fatal(err)
		}
	}
	if v.bytes > nativeHistoryBytes || !v.earlier || len(v.messages) > 4 {
		t.Fatal(v.bytes, len(v.messages))
	}
	for _, message := range v.messages {
		if message.Parts[0].Text != body {
			t.Fatal("bounded display cut a canonical body")
		}
	}
	page := nativeObservation(1, nativeMessage(1, "user", "oldest"))
	if err := v.replace(protocol.HistoryPageResult{Snapshot: page.Snapshot, Messages: page.Messages}); err != nil {
		t.Fatal(err)
	}
	if v.earlier || len(v.messages) != 1 {
		t.Fatal("explicit page replacement retained a stale gap")
	}
}
