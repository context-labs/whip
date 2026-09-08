package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

// sseTextServer serves every streaming chat request with a fixed text
// response — enough for a background subagent's Turn to complete.
func sseTextServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := json.Marshal(body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, `data: {"choices":[{"delta":{"content":%s},"finish_reason":"stop"}]}`+"\n\n", b)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
}

// tasksModel builds a headless model whose agent can start background tasks
// against a stub server (no tea.Program: prog.Send paths are nil-guarded).
func tasksModel(url string) *model {
	m := &model{
		input:    newInput(),
		agent:    agent.New(llm.New(url, "k"), "m", 100, "sys"),
		queueSel: -1, // not navigating the queue (the zero value would arm esc's queue branch)
	}
	m.width, m.height = 80, 30
	m.input.SetWidth(78)
	return m
}

// tasksModelStore adds a real session store so task persistence is exercised.
func tasksModelStore(t *testing.T, url string) *model {
	t.Helper()
	st, err := session.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	m := tasksModel(url)
	m.store = st
	m.cfg = &config.Config{
		DefaultModel: "m",
		Providers:    map[string]config.Provider{"p": {BaseURL: url, APIKey: "k"}},
		Models:       map[string]config.Model{"m": {Providers: []string{"p"}}},
	}
	m.modelName, m.provName = "m", "p"
	return m
}

// Resuming a session restores its background subagents into the dock — and a
// task persisted mid-flight comes back as an explicit error, not "running":
// the subagent died with the process.
func TestResumeRestoresTasks(t *testing.T) {
	srv := sseTextServer(t, "ok")
	defer srv.Close()
	m := tasksModelStore(t, srv.URL)

	// a session with messages and two tasks: one settled, one "running" (the
	// state a crashed whip leaves behind)
	id, err := m.store.Create("/tmp", "m", "p")
	if err != nil {
		t.Fatal(err)
	}
	msgs := []llm.Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "q", Authored: true}, {Role: "assistant", Content: "a"}}
	if err := m.store.Save(id, 1, msgs, "m", "p"); err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(-time.Hour)
	m.store.SaveTask(id, session.Task{ID: "task-1", Description: "finished probe", Prompt: "p", Status: "done", Report: "the report", StartedAt: start, EndedAt: start.Add(time.Minute)})
	m.store.SaveTask(id, session.Task{ID: "task-2", Description: "died mid-flight", Prompt: "p", Status: "running", StartedAt: start})

	// fresh agent, like a new process
	m.agent = agent.New(llm.New(srv.URL, "k"), "m", 100, "sys")
	if err := m.resume(id); err != nil {
		t.Fatal(err)
	}

	tasks := m.agent.Tasks().List()
	if len(tasks) != 2 {
		t.Fatalf("resume should restore 2 tasks, got %d", len(tasks))
	}
	done, ok := m.agent.Tasks().Get("task-1")
	if !ok || done.Status != agent.TaskDone || done.Report != "the report" {
		t.Fatalf("settled task should restore verbatim, got %+v", done)
	}
	stale, ok := m.agent.Tasks().Get("task-2")
	if !ok || stale.Status != agent.TaskError || !strings.Contains(stale.Report, "interrupted") {
		t.Fatalf("a persisted running task must restore as interrupted-error, got %+v", stale)
	}
	// restored tasks are history: /tasks lists them (marked), the dock does NOT
	// — their subagents died with the previous process
	dock := stripAll(m.tasksDock())
	if strings.Contains(dock, "finished probe") || strings.Contains(dock, "died mid-flight") {
		t.Fatalf("restored subagents must not clutter the dock, got %q", dock)
	}
	view := stripAll(m.tasksView())
	if !strings.Contains(view, "finished probe") || !strings.Contains(view, "(restored)") {
		t.Fatalf("/tasks should list restored subagents with a marker, got %q", view)
	}
	// opening a restored settled task renders its stored report — no live stream
	m.openTask("task-1")
	if m.taskVP.live {
		t.Fatal("a restored settled task must not subscribe to events")
	}
	if !strings.Contains(stripAll(m.taskViewView()), "the report") {
		t.Fatalf("restored task view should show the stored report, got %q", stripAll(m.taskViewView()))
	}
}

// Resumed sessions seed ↑ history with only messages the user actually typed:
// steered subagent reports and goal prompts are stored as role "user" with
// Authored=false and must not be recallable.
func TestResumeHistorySkipsUnauthoredMessages(t *testing.T) {
	srv := sseTextServer(t, "ok")
	defer srv.Close()
	m := tasksModelStore(t, srv.URL)

	id, err := m.store.Create("/tmp", "m", "p")
	if err != nil {
		t.Fatal(err)
	}
	msgs := []llm.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "typed by the human", Authored: true},
		{Role: "assistant", Content: "a"},
		{Role: "user", Content: "[background task task-1 done] PONG", Authored: false}, // steered report
		{Role: "user", Content: "continue until the goal is met", Authored: false},     // goal prompt
		{Role: "user", Content: "another typed one", Authored: true},
	}
	if err := m.store.Save(id, 1, msgs, "m", "p"); err != nil {
		t.Fatal(err)
	}

	m.agent = agent.New(llm.New(srv.URL, "k"), "m", 100, "sys")
	if err := m.resume(id); err != nil {
		t.Fatal(err)
	}
	if len(m.hist) != 2 || m.hist[0] != "typed by the human" || m.hist[1] != "another typed one" {
		t.Fatalf("↑ history should hold only authored messages, got %v", m.hist)
	}
}

// Starting a background task with a store attached persists it; the settle
// overwrites the running row with the final report (end-to-end through the
// OnRecord hook, no tea.Program).
func TestTaskPersistsOnStartAndSettle(t *testing.T) {
	srv := sseTextServer(t, "the final report")
	defer srv.Close()
	m := tasksModelStore(t, srv.URL)
	m.wireTasks()

	id, err := m.store.Create("/tmp", "m", "p")
	if err != nil {
		t.Fatal(err)
	}
	m.sessionID = id
	m.agent.Tasks().SetSessionID(id) // what persist() publishes

	task := m.agent.StartBackground("probe", "p", agent.SubModel{})
	defer m.agent.Tasks().Cancel(task.ID)

	// the start lands a running row (OnRecord fires synchronously)
	rows, err := m.store.LoadTasks(id)
	if err != nil || len(rows) != 1 || rows[0].Status != "running" {
		t.Fatalf("start should persist a running row: %v %+v", err, rows)
	}

	waitSettled(t, task)
	rows, err = m.store.LoadTasks(id)
	if err != nil || len(rows) != 1 {
		t.Fatalf("settle must not add a row: %v %d", err, len(rows))
	}
	if rows[0].Status != "done" || rows[0].Report != "the final report" {
		t.Fatalf("settle should overwrite with the final state, got %+v", rows[0])
	}
}

// A task started in a brand-new session (no session row yet when it starts)
// is still persisted: the registry's published session id is read at record
// time, so the settle — which lands after the turn's persist() publishes the
// id — records the task even though the start was skipped.
func TestTaskPersistsWhenSessionIDAssignedMidFlight(t *testing.T) {
	// Hold the stream open until the session id is published: without this the
	// subagent can settle before SetSessionID lands, and skipping the record
	// is then correct behavior (the settle genuinely raced the publish).
	stream := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		select {
		case <-stream:
		case <-r.Context().Done():
			return
		}
		b, _ := json.Marshal("late report")
		fmt.Fprintf(w, `data: {"choices":[{"delta":{"content":%s},"finish_reason":"stop"}]}`+"\n\n", b)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	m := tasksModelStore(t, srv.URL)
	m.wireTasks()
	// no session id published: the start's OnRecord must no-op, not fail

	task := m.agent.StartBackground("probe", "p", agent.SubModel{})
	defer m.agent.Tasks().Cancel(task.ID)

	id, err := m.store.Create("/tmp", "m", "p")
	if err != nil {
		t.Fatal(err)
	}
	m.agent.Tasks().SetSessionID(id) // what persist() publishes when the turn lands
	close(stream)                    // let the subagent's stream complete now

	waitSettled(t, task)
	rows, err := m.store.LoadTasks(id)
	if err != nil || len(rows) != 1 {
		t.Fatalf("the settle should still persist the task: %v %d", err, len(rows))
	}
	if rows[0].Status != "done" || rows[0].Report != "late report" {
		t.Fatalf("got %+v", rows[0])
	}
}

// mkKey builds a KeyMsg from a name ("enter", "esc", "ctrl+t", "up", "down").
func mkKey(name string) tea.KeyMsg {
	switch name {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "ctrl+t":
		return tea.KeyMsg{Type: tea.KeyCtrlT}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
}

// waitSettled blocks until the task's Done channel closes.
func waitSettled(t *testing.T, task *agent.BackgroundTask) {
	t.Helper()
	select {
	case <-task.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("task never settled")
	}
}

func TestTasksDockHiddenWithoutTasks(t *testing.T) {
	srv := sseTextServer(t, "ok")
	defer srv.Close()
	m := tasksModel(srv.URL)
	if got := m.tasksDock(); got != "" {
		t.Fatalf("dock should be empty without tasks, got %q", got)
	}
}

func TestTasksDockListsTasks(t *testing.T) {
	srv := sseTextServer(t, "ok")
	defer srv.Close()
	m := tasksModel(srv.URL)
	task := m.agent.StartBackground("probe grafana", "look around", agent.SubModel{})
	defer m.agent.Tasks().Cancel(task.ID)

	dock := stripAll(m.tasksDock())
	if !strings.Contains(dock, task.ID) || !strings.Contains(dock, "probe grafana") {
		t.Fatalf("dock should list the running task, got %q", dock)
	}
	if !strings.Contains(dock, "⏳") {
		t.Fatalf("running task should show the spinner icon, got %q", dock)
	}
}

func TestCtrlTFocusesDockAndArrowsSelect(t *testing.T) {
	srv := sseTextServer(t, "ok")
	defer srv.Close()
	m := tasksModel(srv.URL)
	t1 := m.agent.StartBackground("first", "p", agent.SubModel{})
	defer m.agent.Tasks().Cancel(t1.ID)
	t2 := m.agent.StartBackground("second", "p", agent.SubModel{})
	defer m.agent.Tasks().Cancel(t2.ID)

	m.key(mkKey("ctrl+t"))
	if !m.tasksFocus {
		t.Fatal("ctrl+t should focus the dock")
	}
	if m.taskSel != 0 {
		t.Fatalf("selection should start on the newest task, got %d", m.taskSel)
	}
	m.key(mkKey("down"))
	if m.taskSel != 1 {
		t.Fatalf("↓ should move the selection down, got %d", m.taskSel)
	}
	m.key(mkKey("up"))
	if m.taskSel != 0 {
		t.Fatalf("↑ should move the selection back up, got %d", m.taskSel)
	}
	m.key(mkKey("esc")) // esc is not a dock key: it stays the interrupt/rewind shortcut
	if !m.tasksFocus {
		t.Fatal("esc must not consume dock focus")
	}
	m.key(mkKey("up")) // the dock sits below the input: ↑ past the top hands focus back
	if m.tasksFocus {
		t.Fatal("↑ past the top row should return focus to the input")
	}
}

func TestEnterOpensTaskViewAndEscBacksOut(t *testing.T) {
	srv := sseTextServer(t, "report-body")
	defer srv.Close()
	m := tasksModel(srv.URL)
	task := m.agent.StartBackground("probe", "find things", agent.SubModel{})
	defer m.agent.Tasks().Cancel(task.ID)

	m.key(mkKey("ctrl+t"))
	m.key(mkKey("enter"))
	if m.taskVP == nil || m.taskVP.id != task.ID {
		t.Fatalf("enter should open the selected task, got %+v", m.taskVP)
	}
	body := stripAll(m.taskViewView())
	if !strings.Contains(body, "probe") || !strings.Contains(body, "find things") {
		t.Fatalf("task view should show description and prompt, got %q", body)
	}
	if !strings.Contains(m.View(), "esc back") {
		t.Fatal("the open task view should render the back hint")
	}
	m.key(mkKey("esc"))
	if m.taskVP != nil {
		t.Fatal("esc should close the task view")
	}
	if !m.tasksFocus {
		t.Fatal("esc from a task view should land on the focused dock")
	}
	m.key(mkKey("up")) // ↑ past the dock's top row returns to the input (esc no longer does)
	if m.tasksFocus {
		t.Fatal("↑ past the top row should return to the main thread")
	}
}

// settled tasks linger in the dock until the user sends a new message, so the
// focused dock's list is stable between the last paint and the keypress
// (submitTurn sweeps it on the next authored turn). Enter must not index the
// empty list, and a stale selection beyond the list clamps instead of
// panicking.
func TestEnterOnEmptyFocusedDockDoesNotPanic(t *testing.T) {
	srv := sseTextServer(t, "ok")
	defer srv.Close()
	m := tasksModel(srv.URL)

	m.tasksFocus = true
	m.key(mkKey("enter")) // dock empty: was an index-out-of-range panic
	if m.taskVP != nil {
		t.Fatal("enter on an empty dock should open nothing")
	}

	// stale selection beyond the shrunk list clamps instead of panicking
	task := m.agent.StartBackground("probe", "p", agent.SubModel{})
	defer m.agent.Tasks().Cancel(task.ID)
	m.tasksFocus = true
	m.taskSel = 5 // beyond the single dock row
	m.key(mkKey("enter"))
	if m.taskVP == nil || m.taskVP.id != task.ID {
		t.Fatalf("enter should clamp to the only task, got %+v", m.taskVP)
	}
}

// Clicking a dock row selects THAT row and expands it inline: when the dock
// is focused its hint row sits above the task rows and must not be clickable
// itself — the click hitbox used to start one row too high, acting on the
// task above the one clicked. Enter opens the full detail view for the
// selected row.
func TestDockClickSelectsClickedRow(t *testing.T) {
	srv := sseTextServer(t, "ok")
	defer srv.Close()
	m := tasksModel(srv.URL)
	t1 := m.agent.StartBackground("first", "p", agent.SubModel{})
	defer m.agent.Tasks().Cancel(t1.ID)
	t2 := m.agent.StartBackground("second", "p", agent.SubModel{})
	defer m.agent.Tasks().Cancel(t2.ID)

	click := func(y int) tea.Model {
		tm, _ := m.Update(tea.MouseMsg{
			Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Y: y,
		})
		return tm
	}

	// unfocused: a click on the dock focuses it (newest row selected) without
	// opening anything
	m.layout()
	m2 := click(m.dockTop()).(*model)
	if !m2.tasksFocus || m2.taskVP != nil {
		t.Fatalf("first click should focus, not open: focus=%v vp=%+v", m2.tasksFocus, m2.taskVP)
	}
	m = m2

	// focused: a hint row sits above the task rows — clicking the SECOND task
	// row must select the second task, not the first (the old off-by-one). The
	// assertion is screen-position-based, not dockTop-based: the task rows
	// render at stripTop+1 (past the hint) and stripTop+2.
	m.layout()
	stripTop := m.height - 2 - m.dockRows // the strip renders below the input, above blank+status
	m2 = click(stripTop + 2).(*model)
	if m2.taskSel != 1 || !m2.taskExpanded {
		t.Fatalf("clicking the second task row should select+expand it: sel=%d expanded=%v", m2.taskSel, m2.taskExpanded)
	}
	m = m2

	// enter opens the selected task's detail view
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(*model)
	if m.taskVP == nil || m.taskVP.id != t1.ID {
		t.Fatalf("enter should open %s, got %+v", t1.ID, m.taskVP)
	}
	m.taskVP = nil
	m.tasksFocus = true

	// the hint row itself is not clickable
	m2 = click(stripTop).(*model)
	if m2.taskVP != nil {
		t.Fatal("clicking the hint row should not open a task")
	}
	if !m2.tasksFocus {
		t.Fatal("clicking near the dock keeps it focused")
	}
}

// While the palette is open it owns the screen; a click near the bottom must
// not hit the dock hidden behind it.
func TestDockClickIgnoredWhilePaletteOpen(t *testing.T) {
	srv := sseTextServer(t, "ok")
	defer srv.Close()
	m := tasksModel(srv.URL)
	task := m.agent.StartBackground("probe", "p", agent.SubModel{})
	defer m.agent.Tasks().Cancel(task.ID)

	m.layout()
	top := m.dockTop()
	m.openPalette()
	m2, _ := m.Update(tea.MouseMsg{
		Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Y: top,
	})
	if m2.(*model).taskVP != nil {
		t.Fatal("a click while the palette is open must not open a dock task")
	}
}

func TestSettledTaskViewShowsReport(t *testing.T) {
	srv := sseTextServer(t, "the final report")
	defer srv.Close()
	m := tasksModel(srv.URL)
	task := m.agent.StartBackground("probe", "p", agent.SubModel{})
	waitSettled(t, task)

	m.openTask(task.ID)
	if m.taskVP.live {
		t.Fatal("a settled task's view should not subscribe to events")
	}
	if !strings.Contains(stripAll(m.taskViewView()), "the final report") {
		t.Fatalf("settled task view should render the report, got %q", stripAll(m.taskViewView()))
	}
	if _, _, ok := m.agent.Tasks().SubscribeWithJournal(task.ID, agent.Events{}); ok {
		t.Fatal("subscribing a settled task should not report live")
	}
}

// The detail view replays the journaled transcript, so opening a task AFTER
// it emitted (or even settled) shows the full history — tool calls included —
// not just the final report.
func TestTaskViewReplaysJournal(t *testing.T) {
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		call++
		switch call {
		case 1:
			fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"t1","type":"function","function":{"name":"read","arguments":"{\"path\":\"/tmp/x\"}"}}]}}]}`+"\n\n")
			fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
		default:
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"the final report"},"finish_reason":"stop"}]}`+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	m := tasksModel(srv.URL)
	task := m.agent.StartBackground("probe", "p", agent.SubModel{})
	waitSettled(t, task)

	m.openTask(task.ID)
	if m.taskVP.live {
		t.Fatal("a settled task's view should not subscribe to events")
	}
	view := stripAll(m.taskViewView())
	for _, want := range []string{"⚒ read", "the final report", "done:"} {
		if !strings.Contains(view, want) {
			t.Fatalf("replayed view missing %q:\n%s", want, view)
		}
	}
}

// Opening a RUNNING task replays what streamed before the open, then keeps
// streaming live events after it.
func TestRunningTaskViewReplaysThenStreams(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"early-text"}}]}`+"\n\n")
		w.(http.Flusher).Flush() // delivered before the view opens
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"late-text"},"finish_reason":"stop"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(func() { close(release) })
	defer srv.Close()
	m := tasksModel(srv.URL)
	task := m.agent.StartBackground("probe", "p", agent.SubModel{})
	defer m.agent.Tasks().Cancel(task.ID)

	// Wait for the journal to hold the early delta, then open mid-run.
	for range 100 {
		if events, _, _ := m.agent.Tasks().SubscribeWithJournal(task.ID, agent.Events{}); len(events) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	m.openTask(task.ID)
	if !m.taskVP.live {
		t.Fatal("a running task's view should subscribe to live events")
	}
	if view := stripAll(m.taskViewView()); !strings.Contains(view, "early-text") {
		t.Fatalf("view opened mid-run should replay pre-open output:\n%s", view)
	}
	// Live events keep arriving after the open (headless model: Update direct).
	m.Update(taskEventMsg{id: task.ID, kind: 0, s: "late-text"})
	if view := stripAll(m.taskViewView()); !strings.Contains(view, "late-text") {
		t.Fatalf("live event after open missing:\n%s", view)
	}
}

func TestSlashTasksFocusesDockAndOpensByID(t *testing.T) {
	srv := sseTextServer(t, "ok")
	defer srv.Close()
	m := tasksModel(srv.URL)
	task := m.agent.StartBackground("probe", "p", agent.SubModel{})
	defer m.agent.Tasks().Cancel(task.ID)

	m.command("/tasks")
	if !m.tasksFocus {
		t.Fatal("bare /tasks should focus the dock")
	}
	m.command("/tasks " + task.ID)
	if m.taskVP == nil || m.taskVP.id != task.ID {
		t.Fatalf("/tasks <id> should open that task's view, got %+v", m.taskVP)
	}
}

// A settled-but-unseen task still occupies a dock row: the strip is the
// record of every background subagent, not just the in-flight ones.
func TestTasksDockShowsSettledTasks(t *testing.T) {
	srv := sseTextServer(t, "ok")
	defer srv.Close()
	m := tasksModel(srv.URL)
	task := m.agent.StartBackground("finished probe", "p", agent.SubModel{})
	waitSettled(t, task)

	dock := stripAll(m.tasksDock())
	if !strings.Contains(dock, "✓") || !strings.Contains(dock, "finished probe") {
		t.Fatalf("dock should show the settled task with a ✓, got %q", dock)
	}
	if !strings.Contains(dock, "done") {
		t.Fatalf("settled row should name its status, got %q", dock)
	}
}

// The dock eats into the transcript's height exactly by its rendered rows
// (the blank above the input is part of the base chrome), so it never
// overlaps or underflows the layout.
// Go through Update: its deferred layout() always runs, whereas a direct
// layout() call skips the resize when the dims coincidentally match.
func TestLayoutReservesDockHeight(t *testing.T) {
	srv := sseTextServer(t, "ok")
	defer srv.Close()
	m := tasksModel(srv.URL)
	m.Update(mkWinSize(80, 30))
	base := m.vp.Height

	task := m.agent.StartBackground("probe", "p", agent.SubModel{})
	defer m.agent.Tasks().Cancel(task.ID)
	tm, _ := m.Update(taskUpdateMsg{}) // force a layout pass with the task visible
	m = tm.(*model)
	dockRows := lipgloss.Height(m.tasksDock())
	if dockRows != 1 {
		t.Fatalf("one unfocused task should be one dock row, got %d", dockRows)
	}
	if m.vp.Height != base-dockRows {
		t.Fatalf("viewport should shrink by exactly the dock rows: base=%d now=%d dock=%d", base, m.vp.Height, dockRows)
	}
	// and the dock renders on its own row below the input (above the status
	// line), so ↓ from an empty input lands on it naturally
	v := stripAll(m.View())
	di := strings.Index(v, "probe")
	ii := strings.Index(v, "Ask whip")
	if di < 0 || ii < 0 || di < ii {
		t.Fatalf("dock must render below the input: dock@%d input@%d\n%s", di, ii, v)
	}
	if m.dockTop() < 0 || m.dockTop() >= m.height {
		t.Fatalf("dockTop out of screen: %d (height %d)", m.dockTop(), m.height)
	}

	m.tasksFocus = true // the focused hint row costs one more
	tm, _ = m.Update(taskUpdateMsg{})
	m = tm.(*model)
	if m.vp.Height != base-dockRows-1 {
		t.Fatalf("focused dock should cost the hint row too: %d vs %d", m.vp.Height, base-dockRows-1)
	}
}

// ctrl+t with no tasks is a no-op (nothing to focus).
func TestCtrlTNoopWithoutTasks(t *testing.T) {
	srv := sseTextServer(t, "ok")
	defer srv.Close()
	m := tasksModel(srv.URL)
	m.key(mkKey("ctrl+t"))
	if m.tasksFocus {
		t.Fatal("ctrl+t should not focus an empty dock")
	}
}

// With more tasks than the strip fits, the dock scrolls to keep the
// selection visible and advertises the hidden remainder.
func TestDockScrollsWithSelection(t *testing.T) {
	srv := sseTextServer(t, "ok")
	defer srv.Close()
	m := tasksModel(srv.URL)
	// task IDs come from a global counter, so tests can't rely on a fresh
	// numbering — the probe-N descriptions are what the dock shows
	for i := range 8 {
		tk := m.agent.StartBackground(fmt.Sprintf("probe-%d", i), "p", agent.SubModel{})
		defer m.agent.Tasks().Cancel(tk.ID)
	}

	m.tasksFocus = true
	m.taskSel = 6 // beyond the visible window
	if got := lipgloss.Height(m.tasksDock()); got > tasksDockHeight {
		t.Fatalf("dock must stay within %d rows, rendered %d", tasksDockHeight, got)
	}
	dock := stripAll(m.tasksDock())
	if !strings.Contains(dock, "probe-1") { // newest-first: sel 6 = probe-1
		t.Fatalf("scrolled dock should keep the selection visible, got %q", dock)
	}
	if !strings.Contains(dock, "more") {
		t.Fatalf("dock should advertise hidden rows, got %q", dock)
	}
	// the newest task scrolled out of view. Task IDs come from a global
	// counter shared across tests in the process, so count the rendered task
	// rows instead of pinning a specific ID at the window's edge.
	rendered := 0
	for line := range strings.Lines(dock) {
		if strings.Contains(line, "probe-") && !strings.Contains(line, "more") {
			rendered++
		}
	}
	if rendered != tasksDockHeight-2 { // hint row + "+N more" row are the budget
		t.Fatalf("scrolled dock should render exactly %d task rows, got %d rows in %q", tasksDockHeight-2, rendered, dock)
	}
}

// A click on a dock row opens that task's view; the wheel moves the
// selection through the strip.
func TestDockMouseClickExpandsTask(t *testing.T) {
	srv := sseTextServer(t, "ok")
	defer srv.Close()
	m := tasksModel(srv.URL)
	t1 := m.agent.StartBackground("first", "p", agent.SubModel{})
	defer m.agent.Tasks().Cancel(t1.ID)
	t2 := m.agent.StartBackground("second", "p", agent.SubModel{})
	defer m.agent.Tasks().Cancel(t2.ID)

	m.layout()
	top := m.dockTop()
	if n := len(m.dockTasks()); n != 2 {
		t.Fatalf("want 2 dock tasks, got %d", n)
	}
	// unfocused: a click on a dock row just focuses the dock (selects the
	// newest task); it no longer jumps straight into the detail view
	tm, _ := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 5, Y: top})
	m = tm.(*model)
	if !m.tasksFocus || m.taskSel != 0 {
		t.Fatalf("first click should focus the dock on row 0: focus=%v sel=%d", m.tasksFocus, m.taskSel)
	}
	if m.taskVP != nil {
		t.Fatalf("first click must not open the detail view, got %+v", m.taskVP)
	}

	// focused: clicking a row selects it and expands it inline (row 1 = t1)
	tm, _ = m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 5, Y: m.dockTop() + 1})
	m = tm.(*model)
	if m.taskSel != 1 || !m.taskExpanded {
		t.Fatalf("second click should select+expand row 1: sel=%d expanded=%v", m.taskSel, m.taskExpanded)
	}
	if m.taskVP != nil {
		t.Fatalf("click should expand inline, not open the detail view: %+v", m.taskVP)
	}
	// the expansion shows the task's activity in the dock itself
	if dock := stripAll(m.tasksDock()); !strings.Contains(dock, "first") {
		t.Fatalf("expanded dock should render the selected task's detail, got %q", dock)
	}

	// clicking the same row again collapses it
	tm, _ = m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 5, Y: m.dockTop() + 1})
	m = tm.(*model)
	if m.taskExpanded {
		t.Fatal("clicking the expanded row should collapse it")
	}

	// enter still opens the full detail view for the selected task
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(*model)
	if m.taskVP == nil || m.taskVP.id != t1.ID {
		t.Fatalf("enter should open the selected task's detail view, got %+v", m.taskVP)
	}

	// back out, then wheel down: scrolls the selection and collapses
	m.taskVP = nil
	m.tasksFocus = true
	m.taskExpanded = true
	tm, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, X: 5, Y: m.dockTop()})
	m = tm.(*model)
	if m.taskExpanded {
		t.Fatal("wheel-scrolling the dock should collapse the expansion")
	}
}

// Live events from the subagent append into the open view's transcript.
func TestTaskEventAppendsToOpenView(t *testing.T) {
	srv := sseTextServer(t, "ok")
	defer srv.Close()
	m := tasksModel(srv.URL)
	task := m.agent.StartBackground("probe", "p", agent.SubModel{})
	defer m.agent.Tasks().Cancel(task.ID)

	m.openTask(task.ID)
	tm, _ := m.Update(taskEventMsg{id: task.ID, kind: 0, s: "streamed text"})
	m = tm.(*model)
	tm, _ = m.Update(taskEventMsg{id: task.ID, kind: 1, s: "bash", s2: `{"command":"ls"}`})
	m = tm.(*model)
	tm, _ = m.Update(taskEventMsg{id: task.ID, kind: 2, s: "bash", s2: "file1\nfile2"})
	m = tm.(*model)

	buf := m.taskVP.buf.String()
	for _, want := range []string{"streamed text", "⚒ bash", "file1"} {
		if !strings.Contains(stripAll(buf), want) {
			t.Fatalf("open view transcript missing %q: %q", want, stripAll(buf))
		}
	}
	// events for a different task are ignored
	tm, _ = m.Update(taskEventMsg{id: "task-999", kind: 0, s: "stray"})
	m = tm.(*model)
	if strings.Contains(m.taskVP.buf.String(), "stray") {
		t.Fatal("events for other tasks must not leak into the open view")
	}
}

// When the open task settles, the view swaps the live stream for the stored
// final report (taskUpdateMsg reseeds it).
func TestOpenTaskViewRefreshesOnSettle(t *testing.T) {
	srv := sseTextServer(t, "the streamed final report")
	defer srv.Close()
	m := tasksModel(srv.URL)
	task := m.agent.StartBackground("probe", "p", agent.SubModel{})

	m.openTask(task.ID)
	if !m.taskVP.live {
		t.Fatal("view of a running task should be live")
	}
	waitSettled(t, task)
	tm, _ := m.Update(taskUpdateMsg{})
	m = tm.(*model)

	if m.taskVP == nil || m.taskVP.live {
		t.Fatal("settled task's view should no longer be live")
	}
	if !strings.Contains(stripAll(m.taskVP.buf.String()), "the streamed final report") {
		t.Fatalf("refreshed view should show the report, got %q", stripAll(m.taskVP.buf.String()))
	}
	head := stripAll(m.taskViewView())
	if !strings.Contains(head, "(done)") {
		t.Fatalf("header should show the settled status, got %q", head)
	}
}

// ctrl+x in an open view cancels a running task; plain runes go to the chat
// input instead (typing must not trigger actions).
func TestTaskViewCtrlXCancels(t *testing.T) {
	srv := sseTextServer(t, "ok")
	defer srv.Close()
	m := tasksModel(srv.URL)
	task := m.agent.StartBackground("probe", "p", agent.SubModel{})

	m.openTask(task.ID)
	m.taskViewKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if got := m.taskVP.input.Value(); got != "x" {
		t.Fatalf("plain runes should type into the chat input, got %q", got)
	}
	m.taskViewKey(tea.KeyMsg{Type: tea.KeyCtrlX})
	waitSettled(t, task)
	snap, _ := m.agent.Tasks().Get(task.ID)
	if snap.Status != agent.TaskCancelled {
		t.Fatalf("ctrl+x should cancel the running task, got %s", snap.Status)
	}
}

// ctrl+t inside an open view returns to the focused dock (not the input).
func TestCtrlTFromTaskViewLandsOnDock(t *testing.T) {
	srv := sseTextServer(t, "ok")
	defer srv.Close()
	m := tasksModel(srv.URL)
	task := m.agent.StartBackground("probe", "p", agent.SubModel{})
	defer m.agent.Tasks().Cancel(task.ID)

	m.openTask(task.ID)
	m.key(mkKey("ctrl+t"))
	if m.taskVP != nil {
		t.Fatal("ctrl+t should close the task view")
	}
	if !m.tasksFocus {
		t.Fatal("ctrl+t from a task view should land on the focused dock")
	}
}

// sendTaskMsg must never block the subagent worker goroutine, even when the
// UI isn't draining its queue: prog.Send parks on the program's msg channel,
// so the helper detaches the send. Nil program (headless) must be a no-op.
func TestSendTaskMsgNeverBlocksWorker(t *testing.T) {
	sendTaskMsg(nil, taskEventMsg{id: "task-1"}) // headless no-op must not panic

	// A real program whose event loop never runs simulates a wedged UI: Send
	// would block forever on the undrained queue.
	p := tea.NewProgram(&model{})
	done := make(chan struct{})
	go func() {
		sendTaskMsg(p, taskEventMsg{id: "task-1", kind: 0, s: "chunk"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("sendTaskMsg blocked on an undrained program — it must detach the Send")
	}
}

// A settled subagent's transcript persists as an attributed session; after a
// fresh process resumes the parent, opening the restored task replays the full
// transcript (read-only) instead of just the bare report.
func TestRestoredTaskReplaysPersistedTranscript(t *testing.T) {
	srv := sseTextServer(t, "exploration findings here")
	defer srv.Close()
	m := tasksModelStore(t, srv.URL)
	m.wireTasks()

	id, err := m.store.Create("/tmp", "m", "p")
	if err != nil {
		t.Fatal(err)
	}
	m.sessionID = id
	m.agent.Tasks().SetSessionID(id)

	task := m.agent.StartBackground("probe the tree", "find the files", agent.SubModel{})
	waitSettled(t, task)

	// The settle persisted the transcript as an attributed session row.
	if _, err := m.store.SubagentTranscript(id, task.ID); err != nil {
		t.Fatalf("transcript should persist on settle: %v", err)
	}

	// Fresh process: new agent, resume the parent session.
	m.agent = agent.New(llm.New(srv.URL, "k"), "m", 100, "sys")
	if err := m.resume(id); err != nil {
		t.Fatal(err)
	}
	m.openTask(task.ID)
	if m.taskVP.live {
		t.Fatal("a restored task has no live stream")
	}
	view := stripAll(m.taskViewView())
	for _, want := range []string{"find the files", "exploration findings here"} {
		if !strings.Contains(view, want) {
			t.Fatalf("restored task view should replay the persisted transcript, missing %q:\n%s", want, view)
		}
	}
}

// A steered message in the subagent view renders as a user message — the
// steering main agent (or the human in the task chat) is the subagent's
// orchestrator, acting as its user. The transcript should read like a normal
// user turn, not a system note.
func TestSteeredMessageRendersAsUser(t *testing.T) {
	srv := sseTextServer(t, "ok")
	defer srv.Close()
	m := tasksModel(srv.URL)
	task := m.agent.StartBackground("probe", "p", agent.SubModel{})
	defer m.agent.Tasks().Cancel(task.ID)
	m.openTask(task.ID)

	// live path: a steer event renders with the "you:" label
	m.Update(taskEventMsg{id: task.ID, kind: 3, s: "check the other file too"})
	view := stripAll(m.taskViewView())
	if !strings.Contains(view, "you: check the other file too") {
		t.Fatalf("steered message should render as a user turn, got:\n%s", view)
	}
	if strings.Contains(view, "steered:") || strings.Contains(view, "you (steer)") {
		t.Fatalf("steered message must not carry a 'steered' label, got:\n%s", view)
	}
}

// Settled tasks stay in the dock until the user sends a new message: a
// machine turn (steer/wake, authored=false) must NOT sweep them, a user-typed
// turn (authored=true) does.
func TestSettledTasksLingerUntilUserMessage(t *testing.T) {
	srv := sseTextServer(t, "ok")
	defer srv.Close()
	m := tasksModel(srv.URL)
	task := m.agent.StartBackground("finished probe", "p", agent.SubModel{})
	waitSettled(t, task)

	// still in the dock after settling (no age-out)
	if len(m.dockTasks()) != 1 {
		t.Fatalf("settled task should stay in the dock, got %d", len(m.dockTasks()))
	}

	// a machine turn (authored=false, e.g. a steered report or wake) must not sweep it
	m.submitTurn("[subagent done] report", false)
	if len(m.dockTasks()) != 1 {
		t.Fatal("a machine turn must not clear the settled task from the dock")
	}

	// the user sending a new message sweeps it
	m2 := tasksModel(srv.URL)
	task2 := m2.agent.StartBackground("another probe", "p", agent.SubModel{})
	waitSettled(t, task2)
	m2.submitTurn("what's next?", true)
	if len(m2.dockTasks()) != 0 {
		t.Fatalf("a user message should sweep settled tasks, got %d", len(m2.dockTasks()))
	}
}

// Space on a focused dock expands the selected task inline: its recent
// journal activity renders under the row without opening the detail view
// (Ruslan: "couldn't expand tasks to see what tasks were in progress/done").
// Space types normally when the dock isn't focused.
func TestDockSpaceExpandsTask(t *testing.T) {
	srv := sseTextServer(t, "ok")
	defer srv.Close()
	m := tasksModel(srv.URL)
	task := m.agent.StartBackground("research", "p", agent.SubModel{})
	defer m.agent.Tasks().Cancel(task.ID)
	m.Update(mkWinSize(80, 24))

	// focus the dock and expand with space
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	m = tm.(*model)
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = tm.(*model)
	if !m.taskExpanded {
		t.Fatal("space on a focused dock should expand the selected task")
	}
	if m.taskVP != nil {
		t.Fatal("space must expand inline, not open the detail view")
	}

	// the expansion shows what the task is working on under its row
	dock := stripAll(m.tasksDock())
	if !strings.Contains(dock, "│ p") {
		t.Fatalf("expanded dock should show the task's prompt, got %q", dock)
	}

	// space again collapses; ↑/↓ moving the selection also collapses
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = tm.(*model)
	if m.taskExpanded {
		t.Fatal("space on the expanded row should collapse it")
	}

	// unfocused: space is a typed character in the input
	m.tasksFocus = false
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	m = tm.(*model)
	if m.input.Value() != " " {
		t.Fatalf("space without dock focus should type into the input, got %q", m.input.Value())
	}
}

// dockTaskExpand tails the journal (never the whole log) and renders nothing
// for a task with no events and no report yet.
func TestDockTaskExpandContent(t *testing.T) {
	srv := sseTextServer(t, "ok")
	defer srv.Close()
	m := tasksModel(srv.URL)
	task := m.agent.StartBackground("probe", "p", agent.SubModel{})
	defer m.agent.Tasks().Cancel(task.ID)

	if got := m.dockTaskExpand(task.ID); len(got) != 1 || !strings.Contains(stripAll(got[0]), "p") {
		t.Fatalf("fresh task expands to its prompt, got %q", got)
	}
	if got := m.dockTaskExpand("no-such-task"); got != nil {
		t.Fatalf("unknown id expands to nothing, got %q", got)
	}
}

// With the dock scrolled (selection past the visible window), dockOffsets are
// window-relative: a click must map through the window base to the displayed
// task, not to the newest one. And the trailing "+N more" counter row is not
// a task — clicking it must not move the selection.
func TestDockClickMapsThroughScrollWindow(t *testing.T) {
	srv := sseTextServer(t, "ok")
	defer srv.Close()
	m := tasksModel(srv.URL)
	for i := range 8 {
		tk := m.agent.StartBackground(fmt.Sprintf("probe-%d", i), "p", agent.SubModel{})
		defer m.agent.Tasks().Cancel(tk.ID)
	}
	m.tasksFocus = true
	m.taskSel = 6 // scrolls the window so lo > 0
	m.layout()
	if m.dockLo == 0 {
		t.Fatalf("test setup: selection 6 should scroll the window, dockLo=%d", m.dockLo)
	}
	lo := m.dockLo
	top := m.dockTop()

	click := func(y int) {
		tm, _ := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 5, Y: y})
		m = tm.(*model)
	}
	// the top visible row is task lo, not task 0
	click(top)
	if m.taskSel != lo {
		t.Fatalf("clicking the top visible row should select task %d (window base), got %d", lo, m.taskSel)
	}
	// the "+N more" row sits right after the task rows: a click there is inert
	m.layout()
	before := m.taskSel
	dock := stripAll(m.tasksDock())
	if !strings.Contains(dock, "more") {
		t.Fatalf("test setup: dock should show the +N more counter, got %q", dock)
	}
	click(m.dockTop() + m.dockTaskRows)
	if m.taskSel != before {
		t.Fatalf("clicking the +N more row must not change the selection: %d → %d", before, m.taskSel)
	}
}

// A settled task's session row carries the sub's own bill (usage_*), stamped
// alongside its transcript, while the parent's own usage stays untouched.
func TestTaskRowCarriesSubUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"report"},"finish_reason":"stop"}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[],"usage":{"prompt_tokens":123,"completion_tokens":45}}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()
	m := tasksModelStore(t, srv.URL)
	m.wireTasks()
	id, err := m.store.Create("/tmp", "m", "p")
	if err != nil {
		t.Fatal(err)
	}
	m.sessionID = id
	m.agent.Tasks().SetSessionID(id)

	task := m.agent.StartBackground("probe", "p", agent.SubModel{})
	defer m.agent.Tasks().Cancel(task.ID)
	waitSettled(t, task)

	meta, _, err := m.store.Load("task-" + id + "-" + task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if meta.UsageIn != 123 || meta.UsageOut != 45 {
		t.Fatalf("task row should carry the sub's own bill, got in=%d out=%d", meta.UsageIn, meta.UsageOut)
	}
	if u := m.agent.Usage(); u.PromptTokens != 0 {
		t.Fatalf("parent's own usage must not include the sub: %+v", u)
	}
	if u := m.agent.SubUsage()["m @ "]; u.PromptTokens != 123 || u.CompletionTokens != 45 {
		t.Fatalf("parent ledger should hold the sub's spend: %+v", m.agent.SubUsage())
	}
}
