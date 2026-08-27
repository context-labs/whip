package tui

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
)

// busyQueueModel builds a model that is busy with a populated queue.
func busyQueueModel(queue ...string) *model {
	m := &model{
		input:    newInput(),
		agent:    &agent.Agent{},
		busy:     true,
		queue:    queue,
		queueSel: -1,
	}
	m.width = 80
	m.input.SetWidth(m.width - 2)
	// a real busy model has a cancel func (set by submitTurn); the test
	// fixture needs one too so the empty-enter steer path can call it
	_, m.cancel = context.WithCancel(context.Background())
	return m
}

func press(t *testing.T, m *model, msg tea.KeyMsg) *model {
	t.Helper()
	tm, _ := m.key(msg)
	return tm.(*model)
}

func TestQueueNavigateAndSelect(t *testing.T) {
	m := busyQueueModel("first", "second", "third")

	// ↑ from the input selects the newest queued message
	m = press(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if m.queueSel != 2 {
		t.Fatalf("↑ should select newest (index 2), got %d", m.queueSel)
	}
	// ↑ again moves older
	m = press(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if m.queueSel != 1 {
		t.Fatalf("second ↑ should move to index 1, got %d", m.queueSel)
	}
	// ↓ moves back newer
	m = press(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.queueSel != 2 {
		t.Fatalf("↓ should move to index 2, got %d", m.queueSel)
	}
	// ↓ off the end deselects
	m = press(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.queueSel != -1 {
		t.Fatalf("↓ past the end should deselect, got %d", m.queueSel)
	}
}

func TestQueueDeleteSelected(t *testing.T) {
	m := busyQueueModel("first", "second", "third")

	m = press(t, m, tea.KeyMsg{Type: tea.KeyUp}) // select "third" (idx 2)
	m = press(t, m, tea.KeyMsg{Type: tea.KeyUp}) // select "second" (idx 1)
	m = press(t, m, tea.KeyMsg{Type: tea.KeyDelete})
	if len(m.queue) != 2 || m.queue[0] != "first" || m.queue[1] != "third" {
		t.Fatalf("after deleting idx1: queue=%v", m.queue)
	}
	// selection clamps to the new last element
	if m.queueSel != 1 {
		t.Fatalf("selection should clamp to last index 1, got %d", m.queueSel)
	}
	// delete again removes "third", leaving "first"
	m = press(t, m, tea.KeyMsg{Type: tea.KeyDelete})
	if len(m.queue) != 1 || m.queue[0] != "first" {
		t.Fatalf("after second delete: queue=%v", m.queue)
	}
	if m.queueSel != 0 {
		t.Fatalf("selection should be 0, got %d", m.queueSel)
	}
	// delete the last one clears the queue and deselects
	m = press(t, m, tea.KeyMsg{Type: tea.KeyDelete})
	if len(m.queue) != 0 || m.queueSel != -1 {
		t.Fatalf("queue should be empty and deselected: %v sel=%d", m.queue, m.queueSel)
	}
}

func TestQueueBackspaceAlsoDeletes(t *testing.T) {
	m := busyQueueModel("only")
	m = press(t, m, tea.KeyMsg{Type: tea.KeyUp})
	m = press(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	if len(m.queue) != 0 {
		t.Fatalf("backspace should remove the selected queued message: %v", m.queue)
	}
}

func TestQueueNavOnlyWhenInputEmpty(t *testing.T) {
	m := busyQueueModel("queued")
	m.input.SetValue("typing something")
	m.input.CursorEnd()
	sel := m.queueSel
	m = press(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if m.queueSel != sel {
		t.Fatalf("with text in the input, ↑ should edit history not the queue (sel %d→%d)", sel, m.queueSel)
	}
}

func TestQueueSelResetsOnSteer(t *testing.T) {
	m := busyQueueModel("a", "b")
	m = press(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if m.queueSel < 0 {
		t.Fatal("expected a selection")
	}
	// empty enter cancels the turn; the queue drains in turnDoneMsg
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.cancel == nil {
		t.Fatal("expected the turn to be canceled for immediate steering")
	}
	if len(m.queue) == 0 {
		t.Fatal("queue should persist until turnDoneMsg drains it")
	}
}

// TestBusyCmdAllowList pins which commands run mid-turn (and which /goal
// forms do) — anything else must queue as a message.
func TestBusyCmdAllowList(t *testing.T) {
	runs := []string{
		"/help", "/theme", "/theme dark", "/mouse", "/effort", "/effort high",
		"/tasks", "/tasks abc123", "/goal", "/goal clear", "/goal rounds 5",
		"/cd", "/cd /tmp", "/pwd",
	}
	for _, c := range runs {
		if !busyCmd(c) {
			t.Errorf("%q should run mid-turn", c)
		}
	}
	queues := []string{
		"/goal resume", "/goal ship the release", "/model", "/model x",
		"/compact", "/clear", "/fork", "/rename", "/resume", "/quit",
		"/bogus", "hello",
	}
	for _, c := range queues {
		if busyCmd(c) {
			t.Errorf("%q should queue, not run mid-turn", c)
		}
	}
}

func TestEnterWhileBusyRunsSettingsCommand(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // keep cfg.Save() away from the real config
	m := busyQueueModel()
	m.cfg = &config.Config{}
	m.input.SetValue("/effort high")
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if len(m.queue) != 0 {
		t.Fatalf("/effort should run now, not queue: %v", m.queue)
	}
	if m.agent.Effort != "high" {
		t.Fatalf("effort should have changed to high, got %q", m.agent.Effort)
	}
	if len(m.blocks) == 0 {
		t.Fatal("the confirmation note should land in the transcript")
	}
	if m.hist[len(m.hist)-1] != "/effort high" {
		t.Fatalf("the command should be in history: %v", m.hist)
	}
}

func TestEnterWhileBusyQueuesOtherCommands(t *testing.T) {
	m := busyQueueModel()
	m.input.SetValue("/model gpt-5")
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.queue) != 1 || m.queue[0] != "/model gpt-5" {
		t.Fatalf("/model should still queue while busy: %v", m.queue)
	}
}

func TestEnterWhileBusyQueuesGoalSubmits(t *testing.T) {
	m := busyQueueModel()
	m.input.SetValue("/goal ship it")
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.queue) != 1 || m.queue[0] != "/goal ship it" {
		t.Fatalf("/goal <text> submits a turn and must queue: %v", m.queue)
	}

	m = busyQueueModel()
	m.input.SetValue("/goal resume")
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.queue) != 1 || m.queue[0] != "/goal resume" {
		t.Fatalf("/goal resume submits a turn and must queue: %v", m.queue)
	}
}

func TestEnterWhileBusyRunsGoalSettings(t *testing.T) {
	m := busyQueueModel()
	m.goal = "old goal"
	m.input.SetValue("/goal clear")
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.goal != "" {
		t.Fatalf("/goal clear should run now, goal=%q", m.goal)
	}
	if len(m.queue) != 0 {
		t.Fatalf("/goal clear should not queue: %v", m.queue)
	}
}

func TestEscInterruptsMidResponse(t *testing.T) {
	m := &model{input: newInput(), agent: &agent.Agent{}, busy: true}
	m.width = 80
	m.input.SetWidth(78)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.cancel = cancel

	m.key(tea.KeyMsg{Type: tea.KeyEsc})
	if ctx.Err() != context.Canceled {
		t.Fatalf("esc should cancel the in-flight turn, ctx.Err=%v", ctx.Err())
	}
}

func TestEscDoesNotInterruptWhenIdle(t *testing.T) {
	m := &model{input: newInput(), agent: &agent.Agent{}, busy: false}
	m.width = 80
	m.input.SetWidth(78)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.cancel = cancel // set but not busy

	m.key(tea.KeyMsg{Type: tea.KeyEsc})
	if ctx.Err() == context.Canceled {
		t.Fatal("esc while idle should not cancel")
	}
}

// stubLLM answers chat completions with an immediate empty SSE stream so a
// drained queue can submit without touching the network.
func stubLLM() *llm.OpenAI {
	return &llm.OpenAI{
		HTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader("data: [DONE]\n\n")),
			}, nil
		})},
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestEmptyEnterSteerDrainsQueue proves the reported regression end to end:
// queue a message while busy, empty-enter to steer (cancels the turn), then
// the turn ends with the wrapped cancellation error an http client actually
// returns ("Post ...: context canceled"). The queue must still drain — the
// queued message submits as the next turn.
func TestEmptyEnterSteerDrainsQueue(t *testing.T) {
	m := busyQueueModel()
	m.agent.Client = stubLLM()
	m.agent.Messages = []llm.Message{{Role: "system", Content: "sys"}}

	// first enter while busy: the typed message queues
	m.input.SetValue("thanks we're all done")
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.queue) != 1 || m.queue[0] != "thanks we're all done" {
		t.Fatalf("typed text should queue while busy: %v", m.queue)
	}

	// second enter on the empty input: force-steer cancels the in-flight turn
	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	// the canceled http request reports a *url.Error wrapping context.Canceled,
	// not the sentinel itself
	wrapped := fmt.Errorf("Post %q: %w", "https://api.example/v1/chat/completions", context.Canceled)
	tm, _ := m.Update(turnDoneMsg{err: wrapped})
	m = tm.(*model)

	if len(m.queue) != 0 {
		t.Fatalf("the canceled turn should drain the queue, still queued: %v", m.queue)
	}
	if !m.busy {
		t.Fatal("the queued message should have submitted as the next turn (busy)")
	}
	if !hasUserMsg(t, m, "thanks we're all done") {
		t.Fatalf("the queued message should be submitted to the model, got %+v", m.agent.Messages)
	}
}

// hasUserMsg reports whether the conversation holds the given user message.
// submitTurn appends it from a goroutine, so read via the agent's published
// snapshot (never the live slice) and poll briefly rather than assert on the
// first read.
func hasUserMsg(t *testing.T, m *model, content string) bool {
	t.Helper()
	for range 100 {
		for _, msg := range m.agent.MessagesSnapshot() {
			if msg.Role == "user" && msg.Content == content {
				return true
			}
		}
		time.Sleep(time.Millisecond)
	}
	return false
}

// TestEmptyEnterSteerDrainsQueueOnSentinel covers the unwrapped sentinel too
// (agent.go's post-tool `return ctx.Err()` path).
func TestEmptyEnterSteerDrainsQueueOnSentinel(t *testing.T) {
	m := busyQueueModel()
	m.agent.Client = stubLLM()
	m.agent.Messages = []llm.Message{{Role: "system", Content: "sys"}}
	m.queue = []string{"follow up"}

	tm, _ := m.Update(turnDoneMsg{err: context.Canceled})
	m = tm.(*model)

	if len(m.queue) != 0 {
		t.Fatalf("the canceled turn should drain the queue, still queued: %v", m.queue)
	}
	if !m.busy {
		t.Fatal("the queued message should have submitted as the next turn (busy)")
	}
}

// TestEmptyEnterIdleDrainsStuckQueue is the recovery path for the stuck state
// the bug left sessions in: idle with a stranded queue, empty enter does
// nothing. It must submit the head of the queue.
func TestEmptyEnterIdleDrainsStuckQueue(t *testing.T) {
	m := busyQueueModel("stranded")
	m.busy = false // idle — the turn already ended without draining
	m.agent.Client = stubLLM()
	m.agent.Messages = []llm.Message{{Role: "system", Content: "sys"}}

	m = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if len(m.queue) != 0 {
		t.Fatalf("empty enter while idle should drain the stuck queue: %v", m.queue)
	}
	if !m.busy {
		t.Fatal("the stranded message should have submitted (busy)")
	}
	if !hasUserMsg(t, m, "stranded") {
		t.Fatalf("the stranded message should be submitted, got %+v", m.agent.Messages)
	}
}

func TestQueueViewShowsSelection(t *testing.T) {
	m := busyQueueModel("first", "second")
	m.queueSel = 1
	m.agent.Model = "m"
	m.provName = "p"
	view := m.View()
	if !strings.Contains(view, "del to remove") {
		t.Errorf("selected queued message should show a delete hint:\n%s", view)
	}
	if !strings.Contains(view, "↑/↓ select") {
		t.Errorf("queue footer should advertise navigation:\n%s", view)
	}
}

// A long queued message must render as ONE line, not wrap to full height —
// otherwise a couple of long queued messages crowd out the transcript.
func TestQueueRendersOneLineEach(t *testing.T) {
	long := strings.Repeat("a very long queued message that would wrap to many lines ", 6)
	m := busyQueueModel("short", long, "another "+strings.Repeat("word ", 30))
	view := m.View()

	for _, q := range m.queue {
		// count rendered lines containing this message's content
		needle := q
		if len(needle) > 20 {
			needle = needle[:20] // a prefix survives truncation
		}
		count := 0
		for line := range strings.SplitSeq(view, "\n") {
			if strings.Contains(ansi.Strip(line), ansi.Strip(needle)) {
				count++
			}
		}
		if count > 1 {
			t.Errorf("queued message rendered on %d lines (want 1): %.40q…", count, q)
		}
	}
}
