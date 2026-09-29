package protocol

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func TestPresentationProjectionAndWireBounds(t *testing.T) {
	p := &session.MessagePresentation{Version: 1, AttemptID: "attempt", Parts: []session.PresentationPart{{ID: "p0", Type: "reasoning", Text: "visible"}, {ID: "p1", Type: "text", Start: new(0), End: new(2)}, {ID: "p2", Type: "tool_call", CallIndex: new(0), CallID: "call"}}}
	message := MessageFromDomain(session.Message{ID: "message", SessionID: "owner", TurnID: "turn", GroupID: "turn", Sequence: 1, Role: session.Assistant, CreatedAt: time.Now(), Parts: []session.Part{{Type: "text", Text: "hi"}}, Presentation: p})
	value := SessionObservation{Snapshot: HistorySnapshot{SessionID: "owner", Revision: 1, ThroughSequence: 1, MessageCount: 1}, Epoch: "epoch", Messages: []Message{message}}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate("SessionObservation", raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"presentation"`) || strings.Contains(string(raw), "continuation") {
		t.Fatal(string(raw))
	}
	for _, mutate := range []func(*MessagePresentation){
		func(p *MessagePresentation) { p.Version = 2 },
		func(p *MessagePresentation) { p.Parts[1].Start = new(-1) },
		func(p *MessagePresentation) { p.Parts[2].CallIndex = new(16) },
		func(p *MessagePresentation) { p.Parts[0].CallID = new(ID("bad")) },
		func(p *MessagePresentation) { p.Parts = make([]PresentationPart, 129) },
	} {
		value.Messages[0].Presentation = PresentationFromDomain(p)
		mutate(value.Messages[0].Presentation)
		raw, err = json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := Validate("SessionObservation", raw); err == nil {
			t.Fatal("invalid presentation accepted", string(raw))
		}
	}
}
