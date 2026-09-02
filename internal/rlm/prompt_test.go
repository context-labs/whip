package rlm

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/llm"
)

func TestFocusedHistoryBoundsSummaryAndRecentExchanges(t *testing.T) {
	history := []llm.Message{{Role: "system", Content: "original"}, {Role: "system", Content: "Summary of the conversation so far:\n\n" + strings.Repeat("s", maxSummaryBytes)}}
	for index := range 6 {
		history = append(history,
			llm.Message{Role: "user", Content: string(rune('a' + index))},
			llm.Message{Role: "assistant", Content: string(rune('A' + index))},
			llm.Message{Role: "tool", Content: strings.Repeat("corpus", 10_000)},
		)
	}
	focused := FocusedHistory(history)
	if len(focused) != 9 {
		t.Fatalf("focused messages = %d, want summary + four exchanges", len(focused))
	}
	if len(focused[0].Content) != maxSummaryBytes {
		t.Fatalf("summary bytes = %d", len(focused[0].Content))
	}
	if focused[1].Content != "c" || focused[len(focused)-1].Content != "F" {
		t.Fatalf("recent range = %q ... %q", focused[1].Content, focused[len(focused)-1].Content)
	}
	history = append(history, llm.Message{Role: "user", Content: strings.Repeat("界", maxFocusedMessageBytes)})
	focused = FocusedHistory(history)
	if content := focused[len(focused)-1].Content; len(content) > maxFocusedMessageBytes || !utf8.ValidString(content) || !strings.Contains(content, "history handle") {
		t.Fatalf("oversized focused content was not bounded safely: bytes=%d", len(content))
	}
}

func TestBuildPromptReferencesHandleWithoutInliningCorpus(t *testing.T) {
	prompt := BuildPrompt("/workspace", &ContextHandle{ReferenceID: "ref-history", Size: 1 << 20, Source: "history"})
	if !strings.Contains(prompt, "ref-history") || !strings.Contains(prompt, "rlm_exec") || strings.Contains(prompt, strings.Repeat("x", 100)) {
		t.Fatalf("prompt = %q", prompt)
	}
}
