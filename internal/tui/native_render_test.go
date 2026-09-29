package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/protocol"
)

func TestNativeMarkdownPreservesRichTextWithoutLocalPathLinksOrTerminalControls(t *testing.T) {
	text := "# Heading\n\n**bold** and `code`\n\n- first\n- second\n\n```go\nfmt.Println(42)\n```\n\n[host path](file:///private/host/secret)\n\n\x1b]0;owned\a\x1b[2Jlast"
	rendered := nativeMarkdown(text, 45)
	plain := ansi.Strip(rendered)
	for _, want := range []string{"Heading", "bold", "code", "first", "second", "fmt.Println(42)", "host path", "last"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q in %q", want, plain)
		}
	}
	if strings.Contains(plain, "**bold**") || strings.Contains(plain, "```go") || !strings.Contains(rendered, "\x1b[") || strings.Contains(rendered, "\x1b]") || strings.Contains(rendered, "\x1b[2J") || strings.Contains(rendered, "\a") {
		t.Fatalf("markdown structure or terminal safety lost: %q", rendered)
	}
	for line := range strings.Lines(rendered) {
		if ansi.StringWidth(strings.TrimSuffix(line, "\n")) > 45 {
			t.Fatalf("wide rendered line: %q", line)
		}
	}
}

func TestNativeRenderToolExpansionImportedIdentityAndStructuredOutput(t *testing.T) {
	code := strings.Repeat("print('line')\n", 20)
	args, err := json.Marshal(map[string]string{"code": code})
	if err != nil {
		t.Fatal(err)
	}
	message := nativeMessage(20, "assistant", "")
	message.Source = &protocol.MessageSource{SessionID: "source", MessageID: "original", Sequence: 10}
	message.Parts = []protocol.Part{{Type: "tool_call", Call: &protocol.ToolCall{ID: "call", Name: "execute", Arguments: args}}}
	collapsed := strings.Join(renderNativeMessage(message, 70, false), "\n")
	expanded := strings.Join(renderNativeMessage(message, 70, true), "\n")
	if !strings.Contains(collapsed, "assistant · #20 · imported") || !strings.Contains(collapsed, "/tools expand") || strings.Count(collapsed, "print('line')") != 8 || strings.Count(expanded, "print('line')") != 20 || strings.Contains(expanded, `{"code"`) || message.TurnID != nil || message.InputID != nil {
		t.Fatal(collapsed, expanded)
	}
	output := `{"result":{"format_version":1,"output":"hello\n","has_value":true,"value":{"answer":42}},"error":"language error"}`
	if got := nativeResultText(output); got != "hello\n\nValue: {\"answer\":42}\nError: language error" {
		t.Fatal(got)
	}
	for _, raw := range []string{`{"result":{"output":"untyped"}}`, "plain output", `{"error":"transport"}`} {
		if got := nativeResultText(raw); got != raw {
			t.Fatalf("unrecognized result changed: %q", got)
		}
	}
}

func TestNativeRenderBoundsDoNotChangeCanonicalMessage(t *testing.T) {
	message := nativeMessage(1, "assistant", strings.Repeat("🐎", nativeRenderInput))
	before := message.Parts[0].Text
	rows := renderNativeMessage(message, 80, true)
	if nativeRowBytes(rows) > nativeRenderBody+100 || len(rows) > 4097 || !utf8.ValidString(strings.Join(rows, "\n")) || !strings.Contains(strings.Join(rows, "\n"), "complete") || message.Parts[0].Text != before {
		t.Fatal("display bound changed canonical content", len(rows), nativeRowBytes(rows))
	}
	if got, cut := nativeTextPrefix("a🐎b", 3); got != "a" || !cut {
		t.Fatalf("split rune: %q %v", got, cut)
	}
	cache := nativeRenderCache{}
	cache.prepare([]protocol.Message{message}, 80, false)
	first := cache.message(message)
	if again := cache.message(message); &again[0] != &first[0] {
		t.Fatal("immutable message was rendered again")
	}
	cache.prepare([]protocol.Message{message}, 40, true)
	if len(cache.blocks) != 0 || cache.width != 40 || !cache.expanded {
		t.Fatal("layout change kept stale rendered blocks")
	}
	cache.message(message)
	cache.prepare(nil, 40, true)
	if cache.bytes != 0 || len(cache.blocks) != 0 {
		t.Fatal("evicted history retained its render cache")
	}
	cache.theme--
	cache.prepare(nil, 40, true)
	if cache.theme != themeGeneration() {
		t.Fatal("theme generation was not refreshed")
	}
}

func TestNativeReasoningDisplayIsProvisionalAndAbsentFromHistoricalPages(t *testing.T) {
	m := &nativeModel{history: nativeTranscript{preview: &protocol.MessagePreview{Text: "answer", Reasoning: "live thought"}}, input: newInput(), width: 80, height: 24}
	m.command("/reasoning on")
	if text := ansi.Strip(strings.Join(m.rows, "\n")); !strings.Contains(text, "live thought") || !strings.Contains(text, "live preview only") {
		t.Fatal(text)
	}
	m.browse = &nativeBrowse{transcript: nativeTranscript{messages: []protocol.Message{nativeMessage(1, "user", "older")}}}
	m.refresh()
	if text := ansi.Strip(strings.Join(m.rows, "\n")); strings.Contains(text, "live thought") || strings.Contains(text, "provisional") {
		t.Fatal("live preview was attached to old history", text)
	}
}
