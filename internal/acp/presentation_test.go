package acp

import (
	"encoding/json"
	"strings"
	"testing"

	acp "github.com/coder/acp-go-sdk"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

func TestNativePresentationCommitsPreviewByIdentity(t *testing.T) {
	var updates []acp.SessionUpdate
	p := presentation{emit: func(value acp.SessionUpdate) error { updates = append(updates, value); return nil }}
	page := client.Observation{
		SessionObservation: protocol.SessionObservation{Epoch: "epoch", Preview: &protocol.MessagePreview{AttemptID: "attempt", MessageID: "message", TurnID: "turn", Text: "Hel", Reasoning: "Thinking", Calls: []protocol.CallPreview{{ID: "partial", Name: "read", Arguments: "{"}}}},
	}
	if err := p.observe(page, false); err != nil {
		t.Fatal(err)
	}
	page.Preview.Text = "Hello"
	if err := p.observe(page, false); err != nil {
		t.Fatal(err)
	}
	page.Messages = []protocol.Message{{ID: "message", Role: "assistant", Parts: []protocol.Part{{Type: "text", Text: "Hello!"}}}}
	page.Preview = nil
	if err := p.observe(page, false); err != nil {
		t.Fatal(err)
	}
	var text, thought strings.Builder
	for _, update := range updates {
		if update.AgentMessageChunk != nil {
			text.WriteString(update.AgentMessageChunk.Content.Text.Text)
		}
		if update.AgentThoughtChunk != nil {
			thought.WriteString(update.AgentThoughtChunk.Content.Text.Text)
		}
		if update.ToolCall != nil {
			t.Fatal("incomplete preview created a canonical call")
		}
	}
	if text.String() != "Hello!" || thought.String() != "Thinking" || p.preview != nil {
		t.Fatalf("text=%q thought=%q preview=%+v", text.String(), thought.String(), p.preview)
	}
}

func TestNativePresentationReplacesInterruptedPreviewAndRejectsRewind(t *testing.T) {
	var updates []acp.SessionUpdate
	p := presentation{emit: func(value acp.SessionUpdate) error { updates = append(updates, value); return nil }}
	page := client.Observation{
		SessionObservation: protocol.SessionObservation{Epoch: "first", Preview: &protocol.MessagePreview{AttemptID: "attempt", MessageID: "message", Text: "uncommitted"}},
	}
	if err := p.observe(page, false); err != nil {
		t.Fatal(err)
	}
	page.Epoch = "second"
	page.Preview = nil
	if err := p.observe(page, false); err != nil {
		t.Fatal(err)
	}
	if len(updates) != 2 || updates[1].AgentThoughtChunk == nil || !strings.Contains(updates[1].AgentThoughtChunk.Content.Text.Text, "not committed") {
		t.Fatalf("missing explicit interruption: %+v", updates)
	}
	if err := p.observe(client.Observation{Reset: true}, false); err == nil {
		t.Fatal("rewind silently replayed append-only editor history")
	}
}

func TestNativePresentationRetainsImportedToolExchangeAndExplicitFailure(t *testing.T) {
	var updates []acp.SessionUpdate
	p := presentation{emit: func(value acp.SessionUpdate) error { updates = append(updates, value); return nil }}
	// Imported history has no native turn or input links. The explicit result
	// flag is authoritative even when text has no retired Error: prefix.
	messages := []protocol.Message{
		{ID: "opening", GroupID: "imported", Role: "user", OpeningInput: true, Parts: []protocol.Part{{Type: "text", Text: "do it"}}},
		{ID: "call", GroupID: "imported", Role: "assistant", Parts: []protocol.Part{{Type: "tool_call", Call: &protocol.ToolCall{ID: "tool", Name: "execute", Arguments: json.RawMessage(`{"code":"print(1)"}`)}}}},
		{ID: "result", GroupID: "imported", Role: "tool", Parts: []protocol.Part{{Type: "tool_result", Result: &protocol.ToolResult{CallID: "tool", Output: "confirmed failure", IsError: true}}}},
	}
	for _, message := range messages {
		if err := p.message(message, true); err != nil {
			t.Fatal(err)
		}
	}
	if len(updates) != 3 || updates[0].UserMessageChunk == nil || updates[1].ToolCall.Kind != acp.ToolKindExecute || *updates[2].ToolCallUpdate.Status != acp.ToolCallStatusFailed {
		t.Fatalf("updates=%+v", updates)
	}
	value := endToolCall("ok", "write", `{"path":"/x","content":"x"}`, "Error: is ordinary file content", false)
	if *value.ToolCallUpdate.Status != acp.ToolCallStatusCompleted || len(value.ToolCallUpdate.Content) != 2 {
		t.Fatalf("guessed failure from result text: %+v", value)
	}
}

func TestNativePresentationDoesNotAdvancePreviewPastUnreadHistory(t *testing.T) {
	var updates []acp.SessionUpdate
	p := presentation{emit: func(value acp.SessionUpdate) error { updates = append(updates, value); return nil }}
	page := client.Observation{
		Cursor:             client.ObservationCursor{After: 1},
		SessionObservation: protocol.SessionObservation{Snapshot: protocol.HistorySnapshot{ThroughSequence: 2}, Preview: &protocol.MessagePreview{Text: "future"}},
	}
	if err := p.observe(page, false); err != nil {
		t.Fatal(err)
	}
	if len(updates) != 0 || p.preview != nil {
		t.Fatal("preview skipped unread canonical history")
	}
}

func TestNativePresentationToolImagesRetainResultContent(t *testing.T) {
	var updates []acp.SessionUpdate
	var reads []protocol.ID
	p := presentation{emit: func(value acp.SessionUpdate) error { updates = append(updates, value); return nil }, content: func(id protocol.ID) (acp.ContentBlock, error) {
		reads = append(reads, id)
		return acp.ContentBlock{Image: &acp.ContentBlockImage{Type: "image", MimeType: "image/png", Data: "aW1hZ2U="}}, nil
	}}
	if err := p.message(protocol.Message{Role: "assistant", Parts: []protocol.Part{{Type: "tool_call", Call: &protocol.ToolCall{ID: "shot", Name: "execute", Arguments: json.RawMessage(`{}`)}}}}, false); err != nil {
		t.Fatal(err)
	}
	if err := p.message(protocol.Message{Role: "tool", Parts: []protocol.Part{{Type: "tool_result", Result: &protocol.ToolResult{CallID: "shot", Output: "captured"}}, {Type: "content", ReferenceID: "image1"}, {Type: "content", ReferenceID: "image2"}}}, false); err != nil {
		t.Fatal(err)
	}
	if len(reads) != 2 || reads[0] != "image1" || reads[1] != "image2" || len(updates) != 4 || len(updates[2].ToolCallUpdate.Content) != 2 || len(updates[3].ToolCallUpdate.Content) != 3 {
		t.Fatalf("reads=%v updates=%+v", reads, updates)
	}
	if updates[3].ToolCallUpdate.Content[0].Content.Content.Text.Text != "captured" {
		t.Fatal("image update replaced canonical result text")
	}
}
