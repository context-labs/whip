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
	message.Continuation = ResponseContinuation{AccountID: "account", Model: "model", Items: `[{"type":"reasoning","encrypted_content":"opaque"}]`}
	body, err := encodeResponses(Request{Model: "model", Messages: []Message{message}}, "account")
	plain = message
	plain.Presentation = nil
	baseline, baselineErr := encodeResponses(Request{Model: "model", Messages: []Message{plain}}, "account")
	if err != nil || baselineErr != nil || strings.Contains(string(body), "presentation") || strings.Contains(string(body), "private display") || string(body) != string(baseline) || message.Continuation != plain.Continuation {
		t.Fatalf("Responses mixed presentation with continuation: %s %v", body, err)
	}
}
