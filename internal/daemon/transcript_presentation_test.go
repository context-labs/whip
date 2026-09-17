package daemon

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/rlm"
)

func TestTranscriptPresentationOrderAndOperations(t *testing.T) {
	node := &AgentSession{turn: turnJournal{TurnID: "turn"}}
	node.recordTranscriptMessage(llm.Message{Role: "user", Content: "hello"})
	thought := node.presentationPart("reasoning", "", "Inspect first")
	text := node.presentationPart("text", "", "Hello 🌍")
	tool := node.presentationPart("tool", "call", "")
	node.recordTranscriptMessage(llm.Message{Role: "assistant", Content: "Hello 🌍", ToolCalls: []llm.ToolCall{{ID: "call"}}})
	before := node.turnJournal().Messages[1].Presentation
	for _, id := range []string{"1", "2"} {
		node.recordHostPresentation(rlm.HostCall{CallID: "call", InvocationID: id, Module: "files", Operation: "read", Display: &llm.OperationDisplay{Target: id}})
	}
	node.recordHostPresentation(rlm.HostCall{CallID: "call", InvocationID: "2", Module: "files", Operation: "read", Status: "failed", Err: "missing"})
	node.recordHostPresentation(rlm.HostCall{CallID: "call", InvocationID: "1", Module: "files", Operation: "read", Status: "completed"})
	node.finishPresentation()
	p := node.turnJournal().Messages[1].Presentation
	if len(p.Parts) != 3 || p.Parts[0].ID != thought || p.Parts[1].ID != text || p.Parts[2].ID != tool || p.Parts[1].End != len("Hello 🌍") {
		t.Fatalf("order: %+v", p)
	}
	if len(before.Parts[2].Hosts) != 0 {
		t.Fatal("mutated a published journal")
	}
	hosts := p.Parts[2].Hosts
	if len(hosts) != 2 || hosts[0].InvocationID != "1" || hosts[0].Status != "completed" || hosts[1].Status != "failed" {
		t.Fatalf("hosts: %+v", hosts)
	}
	encoded, err := json.Marshal(node.turn.Messages[1])
	if err != nil {
		t.Fatal(err)
	}
	var restored llm.Message
	if err := json.Unmarshal(encoded, &restored); err != nil || restored.Presentation.Parts[0].Text != "Inspect first" {
		t.Fatalf("restore: %v %+v", err, restored)
	}
}

func TestPresentationDoesNotReinsertEvictedCompletions(t *testing.T) {
	node := &AgentSession{turn: turnJournal{TurnID: "turn"}}
	node.presentationPart("tool", "call", "")
	node.recordTranscriptMessage(llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call"}}})
	for i := range 130 {
		node.recordHostPresentation(rlm.HostCall{CallID: "call", InvocationID: fmt.Sprint(i), Module: "files", Operation: "read"})
	}
	node.recordHostPresentation(rlm.HostCall{CallID: "call", InvocationID: "0", Module: "files", Operation: "read", Status: "completed"})
	p := node.turn.Messages[0].Presentation.Parts[0]
	if len(p.Hosts) != 128 || p.Hosts[0].InvocationID != "2" || p.Hosts[127].InvocationID != "129" || p.Omitted != 2 {
		t.Fatalf("evicted start moved to tail: %+v", p)
	}
}

func TestPresentationInterruptedProseFollowsLastRecord(t *testing.T) {
	node := &AgentSession{turn: turnJournal{TurnID: "turn"}}
	node.recordTranscriptMessage(llm.Message{Role: "user", Content: "request"})
	node.presentationPart("text", "", "Partial 🌍")
	node.finishPresentation()
	p := node.turn.Messages[0].Presentation.Parts[0]
	if p.Kind != "text" || p.Text != "Partial 🌍" || p.Start != 0 || p.End != 0 {
		t.Fatalf("orphan prose: %+v", p)
	}
	if node.turn.Messages[0].Content != "request" {
		t.Fatal("changed canonical body")
	}
}

func TestTranscriptPresentationDiscardOrphanAndBounds(t *testing.T) {
	node := &AgentSession{turn: turnJournal{TurnID: "turn"}}
	node.recordTranscriptMessage(llm.Message{Role: "user", Content: "hello"})
	first := node.presentationPart("reasoning", "", "discarded")
	node.discardPresentation()
	last := node.presentationPart("reasoning", "", strings.Repeat("🌍", 20000))
	if first == last {
		t.Fatal("retry reused part identity")
	}
	node.finishPresentation()
	p := node.turn.Messages[0].Presentation
	encoded, _ := json.Marshal(p)
	if len(encoded) > 64<<10 || len(p.Parts) != 1 || p.Parts[0].ID != last || p.Parts[0].Omitted == 0 {
		t.Fatalf("orphan/bound: bytes=%d %+v", len(encoded), p)
	}
}
