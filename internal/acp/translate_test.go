package acp

import (
	"encoding/base64"
	"strings"
	"testing"

	acp "github.com/coder/acp-go-sdk"

	"github.com/context-labs/whip/internal/llm"
)

func TestToolKindMap(t *testing.T) {
	want := map[string]acp.ToolKind{
		"read": acp.ToolKindRead, "write": acp.ToolKindEdit, "edit": acp.ToolKindEdit,
		"bash":         acp.ToolKindExecute,
		"browser_exec": acp.ToolKindOther, "mcp__docs__greet": acp.ToolKindOther,
	}
	for name, k := range want {
		if got := toolKind(name); got != k {
			t.Errorf("toolKind(%q) = %v, want %v", name, got, k)
		}
	}
}

// pathArg only extracts a path for the file tools; other tools and bad JSON
// yield "".
func TestPathArg(t *testing.T) {
	cases := []struct{ name, args, want string }{
		{"read", `{"path":"/a/b.go"}`, "/a/b.go"},
		{"write", `{"path":"/a/b.go","content":"x"}`, "/a/b.go"},
		{"edit", `{"path":"/a/b.go"}`, "/a/b.go"},
		{"bash", `{"command":"ls"}`, ""}, // not a file tool
		{"read", `{bad json`, ""},        // unparseable args
		{"read", `{"notpath":"x"}`, ""},  // missing path field
	}
	for _, c := range cases {
		if got := pathArg(c.name, c.args); got != c.want {
			t.Errorf("pathArg(%q, %q) = %q, want %q", c.name, c.args, got, c.want)
		}
	}
}

func TestToolTitle(t *testing.T) {
	cases := []struct{ name, args, want string }{
		{"read", `{"path":"/a/b.go"}`, "Read /a/b.go"},
		{"edit", `{"path":"/a/b.go","old_string":"x","new_string":"y"}`, "Edit /a/b.go"},
		{"bash", `{"command":"go test ./..."}`, "$ go test ./..."},
		{"bash", `{bad json`, "Run command"},
		{"mcp__x__y", `{}`, "mcp__x__y"},
	}
	for _, c := range cases {
		if got := toolTitle(c.name, c.args); got != c.want {
			t.Errorf("toolTitle(%q, %q) = %q, want %q", c.name, c.args, got, c.want)
		}
	}
}

func TestStartToolCall(t *testing.T) {
	u := startToolCall("call_1", "read", `{"path":"/a/b.go","offset":3}`)
	if u.ToolCall == nil {
		t.Fatal("expected tool_call variant")
	}
	tc := u.ToolCall
	if tc.ToolCallId != "call_1" || tc.Title != "Read /a/b.go" {
		t.Errorf("id/title: %v %q", tc.ToolCallId, tc.Title)
	}
	if tc.Kind != acp.ToolKindRead {
		t.Errorf("kind = %v", tc.Kind)
	}
	if tc.Status != acp.ToolCallStatusInProgress {
		t.Errorf("status = %v", tc.Status)
	}
	if len(tc.Locations) != 1 || tc.Locations[0].Path != "/a/b.go" {
		t.Errorf("locations = %+v", tc.Locations)
	}
}

func TestEndToolCallContent(t *testing.T) {
	// Successful edit: text + diff with old/new.
	u := endToolCall("c1", "edit", `{"path":"/f.go","old_string":"a","new_string":"b"}`, "Replaced 1 occurrence(s) in /f.go")
	tu := u.ToolCallUpdate
	if tu == nil {
		t.Fatal("expected tool_call_update")
	}
	if tu.Status == nil || *tu.Status != acp.ToolCallStatusCompleted {
		t.Errorf("status = %v", tu.Status)
	}
	if len(tu.Content) != 2 {
		t.Fatalf("content len = %d, want 2 (text + diff)", len(tu.Content))
	}
	d := tu.Content[1].Diff
	if d == nil || d.Path != "/f.go" || d.NewText != "b" || d.OldText == nil || *d.OldText != "a" {
		t.Errorf("diff = %+v", d)
	}

	// New-file write: diff with nil oldText.
	u = endToolCall("c2", "write", `{"path":"/n.go","content":"package n"}`, "Wrote 9 bytes to /n.go")
	d = u.ToolCallUpdate.Content[1].Diff
	if d == nil || d.OldText != nil || d.NewText != "package n" {
		t.Errorf("write diff = %+v", d)
	}

	// Failed call: failed status, text only.
	u = endToolCall("c3", "bash", `{"command":"rm -rf /"}`, "Error: Permission denied: nope")
	if *u.ToolCallUpdate.Status != acp.ToolCallStatusFailed {
		t.Errorf("status = %v, want failed", *u.ToolCallUpdate.Status)
	}
	if len(u.ToolCallUpdate.Content) != 1 {
		t.Errorf("failed call content len = %d, want 1", len(u.ToolCallUpdate.Content))
	}
}

func TestPromptFromBlocks(t *testing.T) {
	blocks := []acp.ContentBlock{
		acp.TextBlock("hello"),
		{ResourceLink: &acp.ContentBlockResourceLink{Type: "resource_link", Uri: "file:///a/b.go", Name: "b.go"}},
		{Resource: &acp.ContentBlockResource{Type: "resource", Resource: acp.EmbeddedResourceResource{
			TextResourceContents: &acp.TextResourceContents{Uri: "file:///c.go", Text: "package c"},
		}}},
	}
	text, parts := promptFromBlocks(blocks, false)
	want := "hello\n\n@file:///a/b.go\n\nFile: file:///c.go\n```\npackage c\n```"
	if text != want {
		t.Errorf("text =\n%q\nwant\n%q", text, want)
	}
	if len(parts) != 0 {
		t.Errorf("parts = %d, want 0", len(parts))
	}
}

// promptFromBlocks: blob resources, audio, and image-without-vision all fall
// back to inline text markers (only vision images become parts).
func TestPromptFromBlocksFallbacks(t *testing.T) {
	blocks := []acp.ContentBlock{
		{Resource: &acp.ContentBlockResource{Type: "resource", Resource: acp.EmbeddedResourceResource{
			BlobResourceContents: &acp.BlobResourceContents{Uri: "file:///bin.dat"},
		}}},
		{Audio: &acp.ContentBlockAudio{Type: "audio", MimeType: "audio/mpeg"}},
		{Image: &acp.ContentBlockImage{Type: "image", Data: "AAAA", MimeType: "image/png"}},
	}
	text, parts := promptFromBlocks(blocks, false) // vision off
	if !strings.Contains(text, "[binary resource: file:///bin.dat]") {
		t.Errorf("blob resource marker missing: %q", text)
	}
	if !strings.Contains(text, "[audio: audio/mpeg — not supported]") {
		t.Errorf("audio marker missing: %q", text)
	}
	if !strings.Contains(text, "[image: image/png]") {
		t.Errorf("non-vision image marker missing: %q", text)
	}
	if len(parts) != 0 {
		t.Errorf("parts = %d, want 0 with vision off", len(parts))
	}
}

func TestPromptImageVision(t *testing.T) {
	img := acp.ContentBlock{Image: &acp.ContentBlockImage{
		Type: "image", Data: base64.StdEncoding.EncodeToString([]byte("PNG")), MimeType: "image/png",
	}}

	text, parts := promptFromBlocks([]acp.ContentBlock{img}, true)
	if len(parts) != 1 || parts[0].ImageURL == nil {
		t.Fatalf("vision: parts = %+v", parts)
	}
	if text != "" {
		t.Errorf("vision: text = %q, want empty", text)
	}

	text, parts = promptFromBlocks([]acp.ContentBlock{img}, false)
	if len(parts) != 0 || text != "[image: image/png]" {
		t.Errorf("no vision: text=%q parts=%v", text, parts)
	}

	// corrupt base64 degrades to placeholder even with vision
	img.Image.Data = "!!!"
	text, parts = promptFromBlocks([]acp.ContentBlock{img}, true)
	if len(parts) != 0 || text != "[image: image/png]" {
		t.Errorf("bad b64: text=%q parts=%v", text, parts)
	}
}

func TestReplayUpdates(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: "fix the bug"},
		{Role: "assistant", Content: "looking", ToolCalls: []llm.ToolCall{toolCall("t1", "read", `{"path":"/f.go"}`)}},
		{Role: "tool", ToolCallID: "t1", Content: "1\tpackage f"},
		{Role: "assistant", Content: "", ToolCalls: []llm.ToolCall{toolCall("t2", "bash", `{"command":"false"}`)}},
		{Role: "tool", ToolCallID: "t2", Content: "Error: exit 1"},
		{Role: "tool", ToolCallID: "orphan", Content: "ignored"}, // no matching call
		{Role: "assistant", Content: "done"},
	}
	up := replayUpdates(msgs)
	if len(up) != 7 {
		t.Fatalf("updates = %d, want 7", len(up))
	}
	if up[0].UserMessageChunk == nil {
		t.Errorf("0: want user chunk, got %+v", up[0])
	}
	if up[1].AgentMessageChunk == nil {
		t.Errorf("1: want agent chunk")
	}
	if up[2].ToolCall == nil || up[2].ToolCall.ToolCallId != "t1" {
		t.Errorf("2: want tool_call t1, got %+v", up[2])
	}
	// t1 result: completed, read result text.
	end1 := up[3].ToolCallUpdate
	if end1 == nil || *end1.Status != acp.ToolCallStatusCompleted {
		t.Errorf("3: want completed tool_call_update, got %+v", up[3])
	}
	// t2 result: failed.
	end2 := up[5].ToolCallUpdate
	if end2 == nil || *end2.Status != acp.ToolCallStatusFailed {
		t.Errorf("5: want failed tool_call_update, got %+v", up[5])
	}
	if up[6].AgentMessageChunk == nil {
		t.Errorf("6: want final agent chunk")
	}
}

func toolCall(id, name, args string) llm.ToolCall {
	var tc llm.ToolCall
	tc.ID = id
	tc.Type = "function"
	tc.Function.Name = name
	tc.Function.Arguments = args
	return tc
}
