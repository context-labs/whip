package llm

import (
	"strings"
	"testing"
)

// "off" is the explicit no-reasoning level everywhere in whip. Both request
// encoders translate it into an omitted parameter; a level passes through.
func TestOffEffortOmitsReasoningParameter(t *testing.T) {
	client := New("https://example.test/v1", "key")
	chat, err := client.encodeChatRequest(Request{Model: "m", ReasoningEffort: "off"})
	if err != nil || strings.Contains(string(chat), "reasoning_effort") {
		t.Fatalf("chat off = %s %v", chat, err)
	}
	chat, err = client.encodeChatRequest(Request{Model: "m", ReasoningEffort: "high"})
	if err != nil || !strings.Contains(string(chat), `"reasoning_effort":"high"`) {
		t.Fatalf("chat high = %s %v", chat, err)
	}
	responses, err := encodeResponses(Request{Model: "m", ReasoningEffort: "off"}, "")
	if err != nil || strings.Contains(string(responses), `"reasoning"`) {
		t.Fatalf("responses off = %s %v", responses, err)
	}
	responses, err = encodeResponses(Request{Model: "m", ReasoningEffort: "high"}, "")
	if err != nil || !strings.Contains(string(responses), `"effort":"high"`) {
		t.Fatalf("responses high = %s %v", responses, err)
	}
}
