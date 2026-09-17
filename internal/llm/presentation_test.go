package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPresentationDoesNotEnterProviderOrAccounting(t *testing.T) {
	message := Message{Role: "assistant", Content: "hello", Presentation: &TranscriptPresentation{Version: 1, Parts: []PresentationPart{{ID: "p", Kind: "reasoning", Text: "private display"}}}}
	plain := message
	plain.Presentation = nil
	if EstimateTokens([]Message{message}) != EstimateTokens([]Message{plain}) {
		t.Fatal("presentation counted as model context")
	}
	wire, _ := json.Marshal(stripAuthored([]Message{message}))
	if strings.Contains(string(wire), "presentation") || strings.Contains(string(wire), "private display") {
		t.Fatal(string(wire))
	}
	if message.Presentation == nil {
		t.Fatal("stripping mutated transcript")
	}
}
