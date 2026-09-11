package agent

import (
	"testing"

	"github.com/context-labs/whip/internal/llm"
)

// Notices raised mid-turn join the fixed ephemeral text on every request and
// never enter the message history.
func TestEventsEphemeralJoinsNotices(t *testing.T) {
	notices := ""
	events := Events{EphemeralSystem: "Runtime notice: restarted.", EphemeralNotices: func() string { return notices }}
	if got := events.ephemeral(); got != "Runtime notice: restarted." {
		t.Fatalf("ephemeral without notices = %q", got)
	}
	notices = "Hook before_tool rewrote files.read arguments."
	if got := events.ephemeral(); got != "Runtime notice: restarted.\nHook before_tool rewrote files.read arguments." {
		t.Fatalf("ephemeral with notices = %q", got)
	}
	if got := (Events{EphemeralNotices: func() string { return notices }}).ephemeral(); got != notices {
		t.Fatalf("notices alone = %q", got)
	}
	history := []llm.Message{{Role: "system", Content: "prompt"}, {Role: "user", Content: "hi"}}
	messages := withEphemeralSystem(history, events.ephemeral())
	if len(messages) != 3 || messages[1].Role != "system" || messages[1].Content != events.ephemeral() || len(history) != 2 {
		t.Fatalf("ephemeral system placement = %+v", messages)
	}
}
