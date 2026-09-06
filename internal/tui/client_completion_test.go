package tui

import (
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/protocol"
	"testing"
)

func TestHostCompletionRejectsStaleInputAndRoot(t *testing.T) {
	m := modelCmdModel()
	m.input.SetValue("@main")
	old := &clientCompletion{value: "@ma", rootID: m.sessionID}
	current := &clientCompletion{value: "@main", rootID: m.sessionID}
	m.hostCompletion = current
	result := daemon.CompletionResult{Candidates: []protocol.CompletionCandidate{{Text: "@main.go"}}}
	m.applyHostCompletion(clientCompletionMsg{request: old, result: result})
	if m.menu != nil {
		t.Fatal("stale request installed menu")
	}
	m.sessionID = "different-root"
	m.applyHostCompletion(clientCompletionMsg{request: current, result: result})
	if m.menu != nil {
		t.Fatal("old root installed menu")
	}
	current.rootID = m.sessionID
	m.applyHostCompletion(clientCompletionMsg{request: current, result: result})
	if m.menu == nil || m.menu.cands[0].Text != "@main.go" {
		t.Fatal("current host result missing")
	}
}

func TestCompletionKindsKeepStaticMenusLocal(t *testing.T) {
	for _, tc := range []struct {
		value              string
		tab                bool
		kind, prefix, head string
	}{
		{"show @src/m", false, "mention", "src/m", "show "},
		{"$go", false, "skill", "go", ""},
		{"src/", true, "path", "src/", ""},
		{"src/", false, "", "", ""},
		{"/model", true, "", "", ""},
	} {
		kind, prefix, head := completionKind(tc.value, tc.tab)
		if kind != tc.kind || prefix != tc.prefix || head != tc.head {
			t.Errorf("%q: %q %q %q", tc.value, kind, prefix, head)
		}
	}
}
