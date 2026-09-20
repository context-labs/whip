package agent

import (
	"testing"

	"github.com/context-labs/whip/internal/llm"
)

func TestDesignContextReachesAuthoredHistoryButNotProvider(t *testing.T) {
	srv := textServer(t, func(_ int, req llm.Request) string {
		for _, message := range req.Messages {
			if message.Presentation != nil {
				t.Error("display metadata reached provider")
			}
		}
		return "done"
	})
	defer srv.Close()
	ag := newTestAgent(llm.New(srv.URL, "k"), "m", 1000, "sys")
	p := &llm.TranscriptPresentation{Version: 1, DesignContext: &llm.DesignContextPresentation{ContextAttachmentID: "context", ElementCount: 1, Elements: []llm.DesignContextElement{{Label: "Button"}}, ContextPartIndex: 1}}
	var recorded llm.Message
	_, err := ag.TurnParts(t.Context(), "fix this", []llm.ContentPart{{Type: "text", Text: "full raw evidence"}}, Events{InputPresentation: p, OnMessage: func(message llm.Message) int {
		if message.Role == "user" {
			recorded = message
		}
		return 1
	}})
	if err != nil {
		t.Fatal(err)
	}
	if recorded.Presentation == nil || recorded.Presentation.DesignContext.ContextPartIndex != 1 || recorded.Parts[0].Text != "full raw evidence" {
		t.Fatalf("journal lost provenance or evidence: %+v", recorded)
	}
	found := false
	for _, message := range ag.MessagesSnapshot() {
		if message.Authored {
			found = true
			if message.Presentation == nil {
				t.Fatal("live history lost provenance")
			}
		}
	}
	if !found {
		t.Fatal("authored message missing")
	}
}
