package model

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
)

func TestPresentationRetainsOrderAndStableSlots(t *testing.T) {
	a := NewPresentationAccumulator("attempt_1")
	a.Append(Chunk{Reasoning: "consider"})
	a.Append(Chunk{Text: "é"})
	a.Append(Chunk{Call: &CallChunk{Index: 0, Name: "execute", Arguments: `{"code":"`}})
	before := a.Snapshot()
	a.Append(Chunk{Reasoning: "then"})
	a.Append(Chunk{Text: "done"})
	a.Append(Chunk{Call: &CallChunk{Index: 0, ID: "call_1", Arguments: `print(1)"}`}})
	a.Append(Chunk{Call: &CallChunk{Index: 1, ID: "call_2", Name: "execute", Arguments: `{"code":"print(2)"}`}})
	parts := []session.Part{{Type: "text", Text: "édone"}, {Type: "tool_call", Call: &session.ToolCall{ID: "call_1", Name: "execute", Arguments: json.RawMessage(`{"code":"print(1)"}`)}}, {Type: "tool_call", Call: &session.ToolCall{ID: "call_2", Name: "execute", Arguments: json.RawMessage(`{"code":"print(2)"}`)}}}
	committed := a.Successful(parts)
	if err := committed.ValidateMessage(parts); err != nil {
		t.Fatal(err)
	}
	kinds := []string{}
	for _, part := range committed.Parts {
		kinds = append(kinds, part.Type)
	}
	if !reflect.DeepEqual(kinds, []string{"reasoning", "text", "tool_call", "reasoning", "text", "tool_call"}) {
		t.Fatal(kinds)
	}
	if committed.Parts[2].ID != before.Presentation.Parts[2].ID || committed.Parts[2].CallID != "call_1" || *committed.Parts[1].End != 2 {
		t.Fatal(committed)
	}
	if committed.Parts[1].Text != "" || committed.Parts[2].Call != nil {
		t.Fatal("canonical output duplicated", committed)
	}
	failed := a.Failed()
	if err := failed.Validate(); err != nil {
		t.Fatal(err)
	}
	if failed.Parts[1].Text != "é" || failed.Parts[1].Start != nil || failed.Parts[2].Call.Arguments != `{"code":"print(1)"}` {
		t.Fatal(failed)
	}
	before.Presentation.Parts[0].Text = "observer mutation"
	*before.Presentation.Parts[1].End = 0
	if fresh := a.Snapshot(); fresh.Presentation.Parts[0].Text != "consider" || *fresh.Presentation.Parts[1].End != 2 {
		t.Fatal("snapshot aliases accumulator", fresh)
	}
}

func TestPresentationBoundsAndFinalMismatch(t *testing.T) {
	a := NewPresentationAccumulator("attempt_2")
	for range session.MaxPresentationParts + 1 {
		a.Append(Chunk{Text: "é", Reasoning: "\x00"})
	}
	live := a.Snapshot()
	failed := a.Failed()
	raw, _ := json.Marshal(failed)
	if !live.Truncated || len(raw) > session.MaxPresentationBytes || failed.Validate() != nil || !utf8.ValidString(live.Text) {
		t.Fatal(live, len(raw))
	}
	b := NewPresentationAccumulator("attempt_3")
	b.Append(Chunk{Reasoning: strings.Repeat("\x00", 32<<10)})
	b.Append(Chunk{Text: "preview"})
	final := []session.Part{{Type: "text", Text: "canonical"}}
	p := b.Successful(final)
	if err := p.ValidateMessage(final); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(p)
	if len(raw) > session.MaxPresentationBytes || !p.Truncated {
		t.Fatal(len(raw), p)
	}
	last := p.Parts[len(p.Parts)-1]
	if last.Type != "text" || *last.Start != 0 || *last.End != len("canonical") {
		t.Fatal(last)
	}
	c := NewPresentationAccumulator("attempt_4")
	c.Append(Chunk{Text: strings.Repeat("€", MaxPreviewBytes/3+1)})
	if value := c.Failed(); value.Validate() != nil || !utf8.ValidString(value.Parts[0].Text) || !value.Truncated {
		t.Fatal(value)
	}
}
