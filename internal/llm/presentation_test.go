package llm

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBoundPresentationRetainsRecentHostsWithoutMutatingHistory(t *testing.T) {
	t.Parallel()
	index := 2
	value := &TranscriptPresentation{
		Version:       1,
		DesignContext: &DesignContextPresentation{ScreenshotPartIndex: &index},
		Parts:         []PresentationPart{{ID: "part", Kind: "tool", Hosts: []PresentationHost{}}},
	}
	for i := range 129 {
		value.Parts[0].Hosts = append(value.Parts[0].Hosts, PresentationHost{InvocationID: strconv.Itoa(i), Name: "read"})
	}
	bounded := BoundPresentation(value, 128<<10)
	part := &bounded.Parts[0]
	if len(part.Hosts) != 128 || part.Hosts[0].InvocationID != "1" || part.Omitted != 1 {
		t.Fatalf("recent hosts or omission receipt changed: %+v", part)
	}
	part.Hosts[0].Name = "changed"
	*bounded.DesignContext.ScreenshotPartIndex = 9
	if value.Parts[0].Hosts[1].Name != "read" || len(value.Parts[0].Hosts) != 129 || index != 2 {
		t.Fatal("bounded presentation changed previously retained history")
	}
}

func TestBoundPresentationTrimsReasoningAndOversizedIdentities(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("思", 30000)
	value := &TranscriptPresentation{Version: 1, Parts: []PresentationPart{{ID: "reason", Text: text}}}
	bounded := BoundPresentation(value, 64<<10)
	encoded, err := json.Marshal(bounded)
	if err != nil || len(encoded) > 64<<10 || len(bounded.Parts) != 1 {
		t.Fatalf("reasoning exceeds presentation budget: bytes=%d err=%v", len(encoded), err)
	}
	if bounded.Parts[0].Omitted == 0 || !utf8.ValidString(bounded.Parts[0].Text) || value.Parts[0].Text != text {
		t.Fatal("reasoning trim lost omission metadata, split UTF-8, or mutated history")
	}
	oversized := &TranscriptPresentation{Version: 1, Parts: []PresentationPart{{ID: strings.Repeat("x", 1024)}}}
	trimmed := BoundPresentation(oversized, 256)
	if len(trimmed.Parts) != 0 || trimmed.Omitted != 1 || len(oversized.Parts) != 1 {
		t.Fatal("oversized identity was not omitted independently")
	}
	if got := PresentationExcerpt("short", 128); got != "short" {
		t.Fatalf("short excerpt changed: %q", got)
	}
}

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
