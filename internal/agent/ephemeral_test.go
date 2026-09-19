package agent

import (
	"testing"

	"github.com/context-labs/whip/internal/llm"
)

// Notices raised mid-turn join the fixed ephemeral text on every request and
// never enter the message history.
func TestEventsEphemeralJoinsNotices(t *testing.T) {
	notices := ""
	var sent []string
	events := Events{EphemeralSystem: "Runtime notice: restarted.", EphemeralNotices: func() string { return notices }, OnEphemeral: func(text string) { sent = append(sent, text) }}
	if got := events.ephemeral(); got != "Runtime notice: restarted." {
		t.Fatalf("ephemeral without notices = %q", got)
	}
	notices = "Hook before_tool rewrote files.read arguments."
	if got := events.ephemeral(); got != "Runtime notice: restarted.\nHook before_tool rewrote files.read arguments." {
		t.Fatalf("ephemeral with notices = %q", got)
	}
	// The observer sees exactly what each request will carry.
	if len(sent) != 2 || sent[0] != "Runtime notice: restarted." || sent[1] != "Runtime notice: restarted.\nHook before_tool rewrote files.read arguments." {
		t.Fatalf("OnEphemeral saw %q", sent)
	}
	if got := (Events{EphemeralNotices: func() string { return notices }}).ephemeral(); got != notices {
		t.Fatalf("notices alone = %q", got)
	}
	// The ephemeral text is the last message, so the cached prefix (system
	// prompt and history) survives when it changes.
	history := []llm.Message{{Role: "system", Content: "prompt"}, {Role: "user", Content: "hi"}}
	messages := withEphemeralSystem(history, events.ephemeral())
	if len(messages) != 3 || messages[0].Content != "prompt" || messages[1].Role != "user" || messages[2].Role != "system" || messages[2].Content != events.ephemeral() || len(history) != 2 {
		t.Fatalf("ephemeral system placement = %+v", messages)
	}
	if got := withEphemeralSystem(history, ""); len(got) != 2 {
		t.Fatalf("empty ephemeral must leave the request alone: %+v", got)
	}
}
