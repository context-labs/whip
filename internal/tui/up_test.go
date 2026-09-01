package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/llm"
)

// `whip up <words>` carries its prompt on the model until Init kicks it off;
// the msg through Update must take the exact typed-submission path (busy,
// authored user message, up-arrow history).
func TestInitialPromptSubmitsFirstTurn(t *testing.T) {
	m := busyQueueModel()
	m.busy = false
	m.cancel = nil
	m.agent = &agent.Agent{Client: stubLLM()}
	// prepareTurn rewrites Messages[0] with the fresh system prompt, so the
	// fixture needs the system slot a real session always has.
	m.agent.Messages = []llm.Message{{Role: "system", Content: "sys"}}
	m.initialPrompt = "fix the flaky test"

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init with an initial prompt must return the kickoff cmd")
	}
	// Init batches blink with the kickoff; find the initialPromptMsg inside.
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("Init should batch blink + kickoff, got %T", cmd())
	}
	var msg tea.Msg
	for _, c := range batch {
		if got := c(); got != nil {
			if _, is := got.(initialPromptMsg); is {
				msg = got
			}
		}
	}
	if msg == nil {
		t.Fatal("the batched cmds should include the initialPromptMsg kickoff")
	}

	tm, _ := m.Update(msg)
	m = tm.(*model)

	if !m.busy {
		t.Fatal("the initial prompt should start a turn (busy)")
	}
	if m.initialPrompt != "" {
		t.Fatal("the kickoff is one-shot; initialPrompt should be consumed")
	}
	if len(m.hist) != 1 || m.hist[0] != "fix the flaky test" {
		t.Fatalf("the prompt belongs in up-arrow history, got %v", m.hist)
	}
	if !hasUserMsg(t, m, "fix the flaky test") {
		t.Fatalf("the prompt should reach the model as a user message, got %+v", m.agent.MessagesSnapshot())
	}
}

// Without an initial prompt Init never emits the kickoff — whether it returns
// the bare blink cmd or a batch (tmux theme polling also batches) — and a
// stray initialPromptMsg against an empty/busy model is a no-op.
func TestNoInitialPromptNoKickoff(t *testing.T) {
	m := busyQueueModel()
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init always returns at least textarea.Blink")
	}
	if msg := cmd(); msg != nil {
		if _, is := msg.(initialPromptMsg); is {
			t.Fatal("without an initial prompt Init must not emit the kickoff")
		}
		if batch, is := msg.(tea.BatchMsg); is {
			for _, c := range batch {
				if _, is := c().(initialPromptMsg); is {
					t.Fatal("without an initial prompt the batch must not carry the kickoff")
				}
			}
		}
	}

	m.initialPrompt = ""
	tm, _ := m.Update(initialPromptMsg{})
	m = tm.(*model)
	if len(m.agent.MessagesSnapshot()) != 0 {
		t.Fatal("an empty initialPrompt must not submit")
	}
}

// A replayed msg (or a busy session) never double-submits.
func TestInitialPromptMsgIgnoredWhileBusy(t *testing.T) {
	m := busyQueueModel()
	m.initialPrompt = "queued behind a turn"
	tm, _ := m.Update(initialPromptMsg{})
	m = tm.(*model)
	if len(m.agent.MessagesSnapshot()) != 0 {
		t.Fatal("a busy model must not submit the initial prompt")
	}
	if m.initialPrompt != "queued behind a turn" {
		t.Fatal("a swallowed kickoff should leave the prompt untouched")
	}
}
