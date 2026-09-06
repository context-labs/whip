package tui

import (
	"strings"
	"testing"

	bubbletea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/context-labs/whip/internal/session"
)

func TestMCPPermissionShowsArgumentsBeyondFirstLine(t *testing.T) {
	m := &model{width: 72}
	detail := "MCP server \"billing\", tool \"delete.invoice\", source \"ACP attachment\"\nArguments: {\"memo\":\"" + strings.Repeat("x", 120) + "\",\"invoice\":\"TARGET42\"}"
	m.applyClientPermissions([]session.PermissionSnapshot{{
		ID: "permission", AgentID: "child", Operation: "mcp.call", Command: detail, Rule: "opaque-definition-rule",
	}})
	view := ansi.Strip(m.permView())
	for _, want := range []string{"billing", "delete.invoice", "ACP attachment", "Arguments:", "TARGET42", "server definition in this tree"} {
		if !strings.Contains(view, want) {
			t.Fatalf("permission omits %q: %s", want, view)
		}
	}
	if strings.Contains(view, "opaque-definition-rule") {
		t.Fatal("permission shows an opaque digest as the decision label")
	}
	m.applyClientPermissions(nil)
	if m.permView() != "" {
		t.Fatal("retired permission is still shown")
	}
}

func TestMCPPermissionPagesLargeArguments(t *testing.T) {
	m := &model{width: 72, height: 24}
	m.applyClientPermissions([]session.PermissionSnapshot{{
		ID: "permission", AgentID: "child", Operation: "mcp.call",
		Command: "MCP server billing, tool delete.invoice\nArguments: " + strings.Repeat("argument ", 500) + "TARGET42",
	}})
	for range 100 {
		view := ansi.Strip(m.permView())
		if strings.Count(view, "\n") > m.height/3+4 || !strings.Contains(view, "allow once") {
			t.Fatalf("permission controls do not fit with paged details: %s", view)
		}
		if strings.Contains(view, "TARGET42") {
			break
		}
		m.thinPermissionKey(bubbletea.KeyPressMsg{Code: bubbletea.KeyPgDown})
	}
	if !strings.Contains(ansi.Strip(m.permView()), "TARGET42") {
		t.Fatal("last argument is not reachable by paging")
	}
	for range 100 {
		m.thinPermissionKey(bubbletea.KeyPressMsg{Code: bubbletea.KeyPgUp})
	}
	if !strings.Contains(ansi.Strip(m.permView()), "MCP server billing") {
		t.Fatal("server identity is not reachable by paging back")
	}
}
