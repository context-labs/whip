package acp

import (
	"testing"

	acp "github.com/coder/acp-go-sdk"
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
	u := endToolCall("c1", "edit", `{"path":"/f.go","old_string":"a","new_string":"b"}`, "Replaced 1 occurrence(s) in /f.go", false)
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
	u = endToolCall("c2", "write", `{"path":"/n.go","content":"package n"}`, "Wrote 9 bytes to /n.go", false)
	d = u.ToolCallUpdate.Content[1].Diff
	if d == nil || d.OldText != nil || d.NewText != "package n" {
		t.Errorf("write diff = %+v", d)
	}

	// Failed call: failed status, text only.
	u = endToolCall("c3", "bash", `{"command":"rm -rf /"}`, "Error: Permission denied: nope", true)
	if *u.ToolCallUpdate.Status != acp.ToolCallStatusFailed {
		t.Errorf("status = %v, want failed", *u.ToolCallUpdate.Status)
	}
	if len(u.ToolCallUpdate.Content) != 1 {
		t.Errorf("failed call content len = %d, want 1", len(u.ToolCallUpdate.Content))
	}
}
