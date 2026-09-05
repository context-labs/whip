package tui

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"

	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/session"
)

func questionPending(multiple bool) session.LifecycleEvent {
	return session.LifecycleEvent{
		AgentID: "root-agent", QuestionID: "q1", Question: "Which package manager should the scaffold use?", Multiple: multiple,
		Options: []session.QuestionOption{
			{Label: "pnpm", Description: "fast, strict"},
			{Label: "npm", Description: "ships with node"},
			{Label: "bun", Description: "the newest runtime; the repo has no lockfile for it yet so CI would need a new cache key"},
		},
	}
}

// sixOptions is the widest question the daemon allows: six described options
// under a question that wraps, taller than a 24-row terminal's dialog budget.
func sixOptions() session.LifecycleEvent {
	event := session.LifecycleEvent{AgentID: "root-agent", QuestionID: "q6", Question: strings.Repeat("Which build file should the scaffold generate for the new service? ", 3)}
	for _, label := range []string{"Makefile", "Taskfile", "justfile", "mage", "bazel", "none"} {
		event.Options = append(event.Options, session.QuestionOption{Label: label, Description: "a long description of the " + label + " option that does not fit beside its label and so wraps under it"})
	}
	return event
}

func openQuestion(t *testing.T, m *model, event session.LifecycleEvent) *questionDialog {
	t.Helper()
	payload, _ := json.Marshal(event)
	if handled, _ := m.applyClientLifecycle("question.pending", payload); !handled || m.question == nil {
		t.Fatalf("question.pending handled=%v dialog=%v", handled, m.question != nil)
	}
	return m.question
}

// sentAnswer runs the key's command and decodes the question.answer payload it sent.
func sentAnswer(t *testing.T, command tea.Cmd) questionAnswer {
	t.Helper()
	message := clientCommandFrom(t, command)
	if message.action.Operation != "question.answer" {
		t.Fatalf("operation = %q, want question.answer", message.action.Operation)
	}
	var sent questionAnswer
	if err := json.Unmarshal(message.action.Payload, &sent); err != nil {
		t.Fatal(err)
	}
	return sent
}

// answerReply is the daemon's reply to our own question.answer for payload.
func answerReply(payload string, result daemon.CommandResult, err error) clientCommandMsg {
	return clientCommandMsg{action: Action{Operation: "question.answer", Payload: json.RawMessage(payload)}, result: result, err: err}
}

// Regenerate deliberately with: go test ./internal/tui -run TestQuestionDialogGolden -update
func TestQuestionDialogGolden(t *testing.T) {
	pinDarkTheme(t)
	t.Run("single", func(t *testing.T) {
		m := goldenModel(140, 40)
		m.question = &questionDialog{LifecycleEvent: questionPending(false), sel: 1, chosen: map[int]bool{}}
		golden.RequireEqual(t, []byte(ansi.Strip(viewStr(m))))
	})
	t.Run("multiple", func(t *testing.T) {
		m := goldenModel(140, 40)
		m.question = &questionDialog{LifecycleEvent: questionPending(true), sel: 2, chosen: map[int]bool{0: true}}
		golden.RequireEqual(t, []byte(ansi.Strip(viewStr(m))))
	})
	t.Run("sending", func(t *testing.T) {
		m := goldenModel(140, 40)
		m.question = &questionDialog{LifecycleEvent: questionPending(false), chosen: map[int]bool{}, inFlight: true}
		golden.RequireEqual(t, []byte(ansi.Strip(viewStr(m))))
	})
	t.Run("80x24-window", func(t *testing.T) { // the options window around the cursor; the hints stay
		m := goldenModel(80, 24)
		m.question = &questionDialog{LifecycleEvent: sixOptions(), sel: 5, chosen: map[int]bool{}}
		frame := ansi.Strip(viewStr(m))
		if !strings.Contains(frame, "6 none") || !strings.Contains(frame, "esc dismiss") || strings.Contains(frame, "1 Makefile") {
			t.Fatalf("cursor row or hints are off the panel:\n%s", frame)
		}
		golden.RequireEqual(t, []byte(frame))
	})
}

func TestQuestionRowsNeverExceedTheDialogHeight(t *testing.T) {
	pinDarkTheme(t)
	for _, height := range []int{12, 16, 24} {
		m := goldenModel(80, height)
		m.question = &questionDialog{LifecycleEvent: sixOptions(), sel: 5, chosen: map[int]bool{}}
		rows := m.questionRows()
		if len(rows) > m.dialogHeight() {
			t.Fatalf("height %d: %d rows over a budget of %d", height, len(rows), m.dialogHeight())
		}
		if text := ansi.Strip(strings.Join(rows, "\n")); !strings.Contains(text, "6 none") || !strings.Contains(text, "esc dismiss") {
			t.Fatalf("height %d: cursor row or hints missing:\n%s", height, text)
		}
	}
}

func TestQuestionDialogSitsUnderThePalette(t *testing.T) {
	m := compactCmdModel()
	m.question = &questionDialog{LifecycleEvent: questionPending(false), chosen: map[int]bool{}}
	if m.topDialog() != m.question || !m.floatingOpen() {
		t.Fatal("the question does not own the keyboard")
	}
	m.openThinPalette()
	if m.topDialog() != m.palette {
		t.Fatal("the palette does not stay on top of the question")
	}
}

func TestQuestionSingleKeysMoveJumpAndAnswer(t *testing.T) {
	m, _ := liveQueueModel(t)
	q := openQuestion(t, m, questionPending(false))
	for _, step := range []struct {
		key  tea.KeyPressMsg
		want int
	}{
		{keyMsg(tea.KeyDown), 1},
		{keyRunes("j"), 2},
		{ctrlKey('n'), 0}, // wraps
		{keyMsg(tea.KeyUp), 2},
		{keyRunes("k"), 1},
		{ctrlKey('p'), 0},
		{keyRunes("3"), 2},
		{keyRunes("9"), 2}, // out of range: ignored
		{keyRunes(" "), 2}, // space only toggles in multiple mode
	} {
		if _, command := m.thinKey(step.key); command != nil || q.sel != step.want {
			t.Fatalf("after %s: sel=%d want %d command=%v", step.key, q.sel, step.want, command != nil)
		}
	}
	_, command := m.thinKey(keyMsg(tea.KeyEnter))
	if sent := sentAnswer(t, command); sent.ID != "q1" || strings.Join(sent.Answer, ",") != "bun" || sent.Dismissed {
		t.Fatalf("sent %+v", sent)
	}
	if !q.inFlight {
		t.Fatal("answer not marked in flight")
	}
	if _, command := m.thinKey(keyMsg(tea.KeyUp)); command != nil || q.sel != 2 {
		t.Fatal("keys were not ignored while the answer is in flight")
	}
	if !strings.Contains(ansi.Strip(strings.Join(m.questionRows(), "\n")), "sending…") {
		t.Fatal("hints do not say sending…")
	}
	// the reply closes the dialog and writes the line; the event that follows adds nothing
	m.Update(answerReply(`{"id":"q1","answer":["bun"]}`, daemon.CommandResult{Status: "succeeded", Output: "answered"}, nil))
	if m.question != nil {
		t.Fatal("the succeeded reply did not close the dialog")
	}
	payload, _ := json.Marshal(session.LifecycleEvent{QuestionID: "q1", Answer: []string{"bun"}})
	if handled, _ := m.applyClientLifecycle("question.answered", payload); !handled {
		t.Fatal("question.answered was not handled")
	}
	if text := m.transcriptText(); strings.Count(text, "answered: bun)") != 1 || !strings.Contains(text, `(question "Which package manager`) {
		t.Fatalf("transcript = %q", text)
	}
}

func TestQuestionMultipleTogglesAndAnswersTheChosenSet(t *testing.T) {
	m, _ := liveQueueModel(t)
	q := openQuestion(t, m, questionPending(true))
	if _, command := m.thinKey(keyMsg(tea.KeyEnter)); command != nil || m.question == nil {
		t.Fatal("enter with nothing toggled must be a no-op")
	}
	m.thinKey(keyRunes(" "))
	m.thinKey(keyRunes("2"))
	m.thinKey(keyRunes(" "))
	m.thinKey(keyRunes(" ")) // toggles back off
	m.thinKey(keyRunes("3"))
	m.thinKey(keyRunes(" "))
	if !q.chosen[0] || q.chosen[1] || !q.chosen[2] {
		t.Fatalf("chosen = %v", q.chosen)
	}
	_, command := m.thinKey(keyMsg(tea.KeyEnter))
	if sent := sentAnswer(t, command); strings.Join(sent.Answer, ",") != "pnpm,bun" || sent.Dismissed {
		t.Fatalf("sent %+v", sent)
	}
}

func TestQuestionEscDismisses(t *testing.T) {
	m, _ := liveQueueModel(t)
	openQuestion(t, m, questionPending(false))
	_, command := m.thinKey(keyMsg(tea.KeyEscape))
	if sent := sentAnswer(t, command); sent.ID != "q1" || len(sent.Answer) != 0 || !sent.Dismissed {
		t.Fatalf("sent %+v", sent)
	}
	// the event lands first here; the reply that follows adds nothing
	payload, _ := json.Marshal(session.LifecycleEvent{QuestionID: "q1", Dismissed: true})
	m.applyClientLifecycle("question.answered", payload)
	m.Update(answerReply(`{"id":"q1","dismissed":true}`, daemon.CommandResult{Status: "succeeded", Output: "dismissed"}, nil))
	if m.question != nil || strings.Count(m.transcriptText(), `" dismissed)`) != 1 {
		t.Fatalf("dialog=%v transcript=%q", m.question != nil, m.transcriptText())
	}
}

func TestQuestionClosedByTheDaemonDropsTheDialog(t *testing.T) {
	m, _ := liveQueueModel(t)
	openQuestion(t, m, questionPending(false))
	other, _ := json.Marshal(session.LifecycleEvent{QuestionID: "q-other", Error: "turn cancelled"})
	m.applyClientLifecycle("question.closed", other)
	if m.question == nil {
		t.Fatal("another question's close event closed this dialog")
	}
	payload, _ := json.Marshal(session.LifecycleEvent{QuestionID: "q1", Error: "turn cancelled"})
	if handled, _ := m.applyClientLifecycle("question.closed", payload); !handled || m.question != nil {
		t.Fatal("question.closed did not close the dialog")
	}
	if !strings.Contains(m.transcriptText(), `" closed: turn cancelled)`) {
		t.Fatalf("transcript = %q", m.transcriptText())
	}
	// with a child's transcript open the root's outcome line is not written into it
	openQuestion(t, m, questionPending(false))
	m.agentOpen = "root-agent:child"
	before := m.transcriptText()
	m.applyClientLifecycle("question.closed", payload)
	if m.question != nil || m.transcriptText() != before {
		t.Fatalf("dialog=%v transcript=%q", m.question != nil, m.transcriptText())
	}
}

func TestQuestionClosesWhenTheRootTurnEnds(t *testing.T) {
	m, _ := liveQueueModel(t)
	openQuestion(t, m, questionPending(false))
	payload, _ := json.Marshal(session.LifecycleEvent{AgentID: "root-agent"})
	if handled, _ := m.applyClientLifecycle("turn.interrupted", payload); !handled || m.question != nil {
		t.Fatal("the root turn ending did not close the dialog")
	}
	if !strings.Contains(m.transcriptText(), `" closed: the turn ended)`) {
		t.Fatalf("transcript = %q", m.transcriptText())
	}
}

func TestQuestionSnapshotOpensAndSettlesTheDialog(t *testing.T) {
	m, _ := liveQueueModel(t)
	snapshot := session.RootSnapshot{RootID: "root", Cursor: 1, Meta: session.Meta{ID: "root"}, Agents: m.clientView.agents, Questions: []session.LifecycleEvent{questionPending(false)}}
	m.applyClientSnapshot(snapshot)
	if m.question == nil || m.question.QuestionID != "q1" {
		t.Fatal("a snapshot listing the open question did not open the dialog")
	}
	m.question.sel = 2
	payload, _ := json.Marshal(questionPending(false))
	m.applyClientLifecycle("question.pending", payload)
	if m.question.sel != 2 {
		t.Fatal("the replayed question.pending reset the open dialog")
	}
	snapshot.Cursor, snapshot.Questions = 2, nil
	m.applyClientSnapshot(snapshot)
	if m.question != nil {
		t.Fatal("a snapshot without the question kept the dialog")
	}
}

func TestQuestionCtrlCWhileSendingReachesTheGlobalHandler(t *testing.T) {
	m, _ := liveQueueModel(t)
	q := openQuestion(t, m, questionPending(false))
	q.inFlight = true
	if _, command := m.thinKey(keyMsg(tea.KeyUp)); command != nil || q.sel != 0 {
		t.Fatal("keys other than ctrl+c must stay ignored while sending")
	}
	m.thinKey(ctrlKey('c'))
	if m.question != nil || !m.interrupt1 {
		t.Fatalf("dialog=%v interrupt armed=%v", m.question != nil, m.interrupt1)
	}
}

func TestQuestionFromAChildNeverOpens(t *testing.T) {
	m, _ := liveQueueModel(t)
	event := questionPending(false)
	event.AgentID = "root-agent:child"
	payload, _ := json.Marshal(event)
	if handled, _ := m.applyClientLifecycle("question.pending", payload); !handled || m.question != nil {
		t.Fatalf("child question handled=%v dialog=%v", handled, m.question != nil)
	}
}

func TestQuestionAnswerFailures(t *testing.T) {
	m, _ := liveQueueModel(t)
	q := openQuestion(t, m, questionPending(false))
	q.inFlight = true
	// replies for a question that closed meanwhile leave the dialog now open alone
	m.Update(answerReply(`{"id":"q-old","answer":["x"]}`, daemon.CommandResult{Status: "failed", Error: `question "q-old" is not open`}, nil))
	m.Update(answerReply(`{"id":"q-old","answer":["x"]}`, daemon.CommandResult{}, errors.New("socket closed")))
	if m.question != q || !q.inFlight {
		t.Fatalf("another question's reply touched the dialog: dialog=%v inFlight=%v", m.question == q, q.inFlight)
	}
	m.Update(answerReply(`{"id":"q1","answer":["bun"]}`, daemon.CommandResult{}, errors.New("socket closed")))
	if m.question != q || q.inFlight || !strings.Contains(m.transcriptText(), "socket closed") {
		t.Fatalf("transport failure: dialog=%v inFlight=%v transcript=%q", m.question == q, q.inFlight, m.transcriptText())
	}
	q.inFlight = true
	m.Update(answerReply(`{"id":"q1","answer":["bun"]}`, daemon.CommandResult{Status: "failed", Error: "unknown question q1"}, nil))
	if m.question != nil || !strings.Contains(m.transcriptText(), "unknown question q1") {
		t.Fatalf("daemon rejection: dialog=%v transcript=%q", m.question != nil, m.transcriptText())
	}
}

func TestPasteIsSwallowedWhileADialogIsOpen(t *testing.T) {
	m := compactCmdModel()
	m.question = &questionDialog{LifecycleEvent: questionPending(false), chosen: map[int]bool{}}
	m.thinPaste(tea.PasteMsg{Content: "stray text"})
	if m.input.Value() != "" {
		t.Fatalf("paste reached the covered prompt: %q", m.input.Value())
	}
	m.question = nil
	m.thinPaste(tea.PasteMsg{Content: "stray text"})
	if m.input.Value() != "stray text" {
		t.Fatalf("paste with no dialog = %q", m.input.Value())
	}
}
