// tasks.go: the persistent background-subagent area and the per-task detail
// view.
//
// The dock is a strip rendered below the input box (above the status line)
// whenever background tasks exist — running or recently settled — so the user
// always knows how many subagents are in flight without running /subagents.
// ctrl+t focuses it, and so does ↓ on an empty input (the strip sits right
// under the cursor); ↑/↓ (or the mouse wheel over the strip) moves the
// selection, ↑ off the top row hands focus back to the input, enter opens the
// selected task's detail view, and esc backs out: detail → dock → main
// thread. The detail view is a scrollback pane filled from the
// task's live event stream (registry.Subscribe) while it runs, and from the
// stored Report once it settles.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/llm"
)

// taskEventMsg is one live event from an opened background task (OnText /
// OnToolStart / OnToolEnd fire from the subagent's worker goroutine; prog.Send
// funnels them onto the UI thread like every other stream).
type taskEventMsg struct {
	id   string
	kind int // 0 = text, 1 = tool start, 2 = tool end, 3 = steer injected, 4 = follow-up turn settled
	s    string
	s2   string // tool args (start) or result (end)
}

// sendTaskMsg hands a task event to the UI without ever blocking the subagent
// worker goroutine: prog.Send parks while the UI event queue is backed up, so
// the send is detached into its own goroutine. Program.Send is safe for
// concurrent use (it just selects on the program's msg channel), and if the
// program exits first, bubbletea unblocks every pending Send — no leak. The
// pane resyncs from the task's Report on the next paint, so a reordered or
// lost interim frame is cosmetic; the worker must never stall on the UI.
func sendTaskMsg(p *tea.Program, msg taskEventMsg) {
	if p == nil {
		return // headless tests
	}
	go p.Send(msg)
}

// renderTaskEvent writes one task event to the transcript buffer. Shared by
// the live stream (taskEventMsg in tui.go) and the journal replay in
// openTask, so replayed history and live events can never drift in format.
// Kind 4 (follow-up settled) only ever arrives live, never from the journal.
func renderTaskEvent(buf *strings.Builder, kind int, s, s2 string) {
	switch kind {
	case 0: // text delta
		buf.WriteString(s)
	case 1: // tool start
		fmt.Fprintf(buf, "\n%s %s %s\n", toolStyle.Render("⚒"), s, dimStyle.Render(s2))
	case 2: // tool end
		preview := strings.Split(strings.TrimRight(s2, "\n"), "\n")
		if len(preview) > 4 {
			preview = append(preview[:4], fmt.Sprintf("… +%d lines", len(s2)-4))
		}
		fmt.Fprintf(buf, "%s\n", dimStyle.Render("  "+strings.Join(preview, "\n  ")))
	case 3: // a steered message reached the running subagent (task_steer /
		// chat). Render it as a user message — the steering main agent (or the
		// human in the task chat) is the subagent's orchestrator, acting as its
		// user, so the transcript reads like a normal user turn.
		fmt.Fprintf(buf, "\n%s %s\n", youStyle.Render("you:"), s)
	case 4: // follow-up turn settled
		if s != "" {
			fmt.Fprintf(buf, "\n%s\n", errStyle.Render(s))
		} else {
			buf.WriteString("\n")
		}
	}
}

// taskView is the open per-task pane: the live transcript of one background
// subagent (or its stored report once settled), plus a chat input — a task IS
// a session: while it runs, typing steers it; once it settles, typing runs
// follow-up turns on its retained context. Restored tasks are read-only.
type taskView struct {
	id    string
	vp    viewport.Model
	buf   strings.Builder // full transcript text; vp shows a window into it
	live  bool            // subscribed to the task's event stream
	input textinput.Model
	busy  bool // a follow-up turn is in flight on the settled subagent
	// followCancel cancels the in-flight follow-up turn (ctrl+x). Set with
	// busy on the UI goroutine; the worker only reads its copy.
	followCancel context.CancelFunc
}

// tasksDockHeight is the maximum number of screen rows the dock strip
// occupies (hint row + task rows); the strip scrolls if there are more tasks.
const tasksDockHeight = 6

// dockTasks returns the dock's tasks — running ones plus settled ones,
// never restored ones — newest first. Settled tasks stay in the dock until
// the user sends a new message (submitTurn sweeps them then), so the user
// can review a finished subagent's transcript before moving on. Bare test
// models have no agent; the dock is simply empty.
func (m *model) dockTasks() []agent.BackgroundTask {
	if m.agent == nil {
		return nil
	}
	var out []agent.BackgroundTask
	for _, t := range m.agent.Tasks().List() {
		if t.Restored {
			continue // resume history belongs in /tasks, not the dock
		}
		out = append(out, t)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// clampTaskSel keeps the dock selection inside the current task list.
func (m *model) clampTaskSel() {
	if n := len(m.dockTasks()); m.taskSel >= n {
		m.taskSel = max(n-1, 0)
	}
}

// dockExpandRows is how many lines of recent activity an expanded dock row
// shows below its summary (the most recent N lines of the task's journal).
const dockExpandRows = 4

// dockTaskExpand renders the selected task's recent activity for the dock's
// inline expansion: the tail of its event journal (tool starts/ends, text
// deltas, steers) replayed through renderTaskEvent, or its report once
// settled. The journal is a byte-bounded ring, so the tail may start with
// "[earlier output dropped]" when old events aged out. Returns nil when
// there's nothing to show (no journal yet, no report, unknown id).
func (m *model) dockTaskExpand(id string) []string {
	if m.agent == nil {
		return nil
	}
	t, ok := m.agent.Tasks().Get(id)
	if !ok {
		return nil
	}
	events, truncated, _ := m.agent.Tasks().SubscribeWithJournal(id, agent.Events{})
	var buf strings.Builder
	switch {
	case len(events) > 0:
		if truncated {
			buf.WriteString(dimStyle.Render("  [earlier output dropped]") + "\n")
		}
		for _, e := range events {
			renderTaskEvent(&buf, e.Kind, e.S, e.S2)
		}
	case t.Report != "":
		buf.WriteString(t.Report)
	case t.Prompt != "":
		// a fresh task has journaled nothing yet: show what it's working on
		buf.WriteString(t.Prompt)
	default:
		return nil
	}
	lines := strings.Split(strings.TrimRight(ansi.Strip(buf.String()), "\n"), "\n")
	if len(lines) > dockExpandRows {
		lines = lines[len(lines)-dockExpandRows:]
	}
	for i, l := range lines {
		lines[i] = "   " + dimStyle.Render("│ ") + truncLine(l, max(m.width-10, 8))
	}
	return lines
}

// tasksDock renders the persistent strip: one row per task with a live
// status icon, plus a hint row when the dock is focused. The selected row
// expands inline (space/click) to show the task's recent activity without
// leaving the main view; dockOffsets records each task row's screen offset
// so click hit-testing lands on the right task even below an expansion.
func (m *model) tasksDock() string {
	tasks := m.dockTasks()
	if len(tasks) == 0 {
		m.dockOffsets = nil
		return ""
	}
	m.clampTaskSel()

	rows := make([]string, 0, len(tasks)+2)
	if m.tasksFocus {
		hint := " ⚙ subagents — ↑/↓ select (↑ past top: back to input) · space expand · enter open"
		if m.taskExpanded {
			hint = " ⚙ subagents — ↑/↓ select · space collapse · enter open"
		}
		rows = append(rows, dimStyle.Render(hint))
	}

	budget := tasksDockHeight - len(rows)
	if len(tasks) > budget { // reserve a row for the "+N more" counter
		budget--
	}
	// An expanded row spends extra rows from the budget on its activity lines.
	extra := 0
	if m.tasksFocus && m.taskExpanded && m.taskSel < len(tasks) {
		extra = len(m.dockTaskExpand(tasks[m.taskSel].ID))
	}
	lo := 0
	if m.tasksFocus && m.taskSel >= budget-extra {
		lo = m.taskSel - (budget - extra) + 1 // keep the selection visible
	}
	hi := min(lo+budget-extra, len(tasks))
	hi = max(hi, min(lo+1, len(tasks))) // always show at least the selected task

	m.dockOffsets = m.dockOffsets[:0]
	m.dockLo = lo
	offset := 0
	for i := lo; i < hi; i++ {
		t := tasks[i]
		icon := toolStyle.Render("⏳")
		switch t.Status {
		case agent.TaskDone:
			icon = "✓"
		case agent.TaskError, agent.TaskCancelled:
			icon = errStyle.Render("✗")
		}
		line := fmt.Sprintf("%s %s  %s", icon, t.ID, truncLine(t.Description, max(m.width-24, 8)))
		var meta string
		if t.Status == agent.TaskRunning {
			meta = fmt.Sprintf("  %ds", int(time.Since(t.StartedAt).Seconds()))
		} else {
			meta = "  " + string(t.Status)
		}
		selected := m.tasksFocus && i == m.taskSel
		switch {
		case selected:
			line = botStyle.Render(" → "+line) + toolStyle.Render(meta)
		case t.Status == agent.TaskRunning:
			line = "   " + toolStyle.Render(line) + dimStyle.Render(meta)
		default:
			line = "   " + line + dimStyle.Render(meta)
		}
		m.dockOffsets = append(m.dockOffsets, offset)
		rows = append(rows, line)
		offset++
		if selected && m.taskExpanded {
			for _, el := range m.dockTaskExpand(t.ID) {
				rows = append(rows, el)
				offset++
			}
		}
	}
	m.dockTaskRows = offset
	if more := len(tasks) - hi; more > 0 {
		rows = append(rows, dimStyle.Render(fmt.Sprintf("   … +%d more (ctrl+t to browse)", more)))
	}
	return strings.Join(rows, "\n")
}

// openTask opens the detail view for one task: a scrollback pane seeded with
// the prompt, then the journaled transcript replayed up to now, then the
// live event stream while the task runs (or the stored report once it has
// settled). The replay+subscribe is one atomic registry call, so no event
// landing mid-open is missed or shown twice.
func (m *model) openTask(id string) {
	t, ok := m.agent.Tasks().Get(id)
	if !ok {
		return
	}
	tv := &taskView{id: id}
	tv.input = textinput.New()
	tv.input.Prompt = youStyle.Render("› ")
	tv.input.Placeholder = "message this subagent (enter to send)"
	tv.input.Focus()
	fmt.Fprintf(&tv.buf, "%s %s  %s\n\n%s %s\n",
		toolStyle.Render("⚙"), t.ID, t.Description,
		youStyle.Render("prompt:"), t.Prompt)
	p := m.prog
	events, truncated, live := m.agent.Tasks().SubscribeWithJournal(id, agent.Events{
		OnText: func(s string) {
			sendTaskMsg(p, taskEventMsg{id: id, kind: 0, s: s})
		},
		OnToolStart: func(_, n, a string) {
			sendTaskMsg(p, taskEventMsg{id: id, kind: 1, s: n, s2: a})
		},
		OnToolEnd: func(_, n, r string) {
			sendTaskMsg(p, taskEventMsg{id: id, kind: 2, s: n, s2: r})
		},
		OnSteer: func(s string) {
			sendTaskMsg(p, taskEventMsg{id: id, kind: 3, s: s})
		},
	})
	tv.live = live
	if truncated {
		fmt.Fprintf(&tv.buf, "\n%s\n", dimStyle.Render("  [earlier output dropped]"))
	}
	for _, e := range events {
		renderTaskEvent(&tv.buf, e.Kind, e.S, e.S2)
	}
	switch {
	case live:
		if len(events) > 0 {
			tv.buf.WriteString("\n")
		}
		fmt.Fprintf(&tv.buf, "\n%s\n", dimStyle.Render("  running…"))
	case len(events) > 0:
		// Settled with a journal: the replay above already shows the work;
		// close it out with the final status line.
		fmt.Fprintf(&tv.buf, "\n%s %s\n", toolStyle.Render(string(t.Status)+":"), t.Report)
	case t.Restored && m.store != nil:
		// A restored task's subagent died with the last process, so there's no
		// live stream and no journal — but its transcript persisted. Replay it
		// read-only so the view shows the completed work, not just the report.
		if msgs, err := m.store.SubagentTranscript(m.sessionID, t.ID); err == nil && len(msgs) > 0 {
			renderTranscript(&tv.buf, msgs)
		} else {
			fmt.Fprintf(&tv.buf, "\n%s %s\n", toolStyle.Render(string(t.Status)+":"), t.Report)
		}
	default:
		fmt.Fprintf(&tv.buf, "\n%s %s\n", toolStyle.Render(string(t.Status)+":"), t.Report)
	}
	m.taskVP = tv
	m.refreshTaskVP()
}

// renderTranscript writes a persisted conversation as role-labeled blocks for
// the restored-task detail view (read-only history). Tool calls show as their
// name; the system prompt is skipped.
func renderTranscript(buf *strings.Builder, msgs []llm.Message) {
	for _, msg := range msgs {
		switch msg.Role {
		case "system":
			continue
		case "user":
			fmt.Fprintf(buf, "\n%s %s\n", youStyle.Render("you:"), msg.Content)
		case "assistant":
			if msg.Content != "" {
				fmt.Fprintf(buf, "\n%s\n", msg.Content)
			}
			for _, tc := range msg.ToolCalls {
				fmt.Fprintf(buf, "\n%s %s %s\n", toolStyle.Render("⚒"), tc.Function.Name, dimStyle.Render(tc.Function.Arguments))
			}
		case "tool":
			preview := strings.Split(strings.TrimRight(msg.Content, "\n"), "\n")
			if len(preview) > 4 {
				preview = append(preview[:4], fmt.Sprintf("… +%d lines", len(msg.Content)-4))
			}
			fmt.Fprintf(buf, "%s\n", dimStyle.Render("  "+strings.Join(preview, "\n  ")))
		}
	}
}

// taskChatable reports whether the open task can receive messages: restored
// tasks have no live subagent (it died with the previous process).
func (m *model) taskChatable(tv *taskView) bool {
	t, ok := m.agent.Tasks().Get(tv.id)
	return ok && !t.Restored
}

// taskSend routes one typed message to the open task's subagent: steering
// while it runs, a follow-up turn on its retained context once it settled.
func (m *model) taskSend(tv *taskView, text string) {
	t, ok := m.agent.Tasks().Get(tv.id)
	if !ok {
		return
	}
	tv.input.SetValue("")
	switch {
	case t.Status == agent.TaskRunning:
		if err := m.agent.SteerTask(tv.id, text); err != nil {
			fmt.Fprintf(&tv.buf, "\n%s\n", errStyle.Render(err.Error()))
			break
		}
		fmt.Fprintf(&tv.buf, "\n%s %s\n", youStyle.Render("you:"), text)
	case tv.busy:
		fmt.Fprintf(&tv.buf, "\n%s\n", dimStyle.Render("(still replying — wait for the current reply)"))
	default:
		fmt.Fprintf(&tv.buf, "\n%s %s\n\n", youStyle.Render("you:"), text)
		ctx, cancel := context.WithCancel(context.Background())
		tv.busy, tv.followCancel = true, cancel
		p, id := m.prog, tv.id
		ev := agent.Events{
			OnText: func(s string) {
				sendTaskMsg(p, taskEventMsg{id: id, kind: 0, s: s})
			},
			OnToolStart: func(_, n, a string) {
				sendTaskMsg(p, taskEventMsg{id: id, kind: 1, s: n, s2: a})
			},
			OnToolEnd: func(_, n, r string) {
				sendTaskMsg(p, taskEventMsg{id: id, kind: 2, s: n, s2: r})
			},
		}
		ag := m.agent
		go func() {
			defer cancel()
			_, err := ag.FollowupTask(ctx, id, text, ev)
			e := ""
			if err != nil {
				e = err.Error()
			}
			sendTaskMsg(p, taskEventMsg{id: id, kind: 4, s: e})
		}()
	}
	m.refreshTaskVP()
}

// refreshTaskVP resizes the open task pane to the free screen area and
// reloads its content, following the tail while the task streams.
func (m *model) refreshTaskVP() {
	tv := m.taskVP
	if tv == nil {
		return
	}
	// 3 rows of chrome: the header, the chat input, and the footer hint
	tv.vp.Width, tv.vp.Height = m.width, max(m.height-3, 1)
	tv.input.Width = max(m.width-4, 8)
	atBottom := tv.vp.AtBottom()
	tv.vp.SetContent(tv.buf.String())
	if atBottom {
		tv.vp.GotoBottom()
	}
}

// taskViewKey handles input while a task detail view is open: typing goes to
// the chat input (enter sends — steer while running, follow-up turn once
// settled), ↑/↓/PgUp/PgDn scroll the pane, ctrl+x cancels the running task or
// the in-flight follow-up, esc backs out to the dock.
func (m *model) taskViewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	tv := m.taskVP
	switch msg.Type {
	case tea.KeyEsc:
		m.taskVP = nil
		m.tasksFocus = true // land on the dock so ↑/↓ keep working; ↑ past the top returns to the input
		return m, nil
	case tea.KeyCtrlT:
		m.taskVP = nil
		m.tasksFocus = true
		return m, nil
	case tea.KeyCtrlX:
		if tv.busy && tv.followCancel != nil {
			tv.followCancel()
		} else {
			m.agent.Tasks().Cancel(tv.id)
		}
		return m, nil
	case tea.KeyEnter:
		if text := strings.TrimSpace(tv.input.Value()); text != "" && m.taskChatable(tv) {
			m.taskSend(tv, text)
		}
		return m, nil
	case tea.KeyUp, tea.KeyDown, tea.KeyPgUp, tea.KeyPgDown:
		var cmd tea.Cmd
		tv.vp, cmd = tv.vp.Update(msg)
		return m, cmd
	}
	if !m.taskChatable(tv) { // restored: read-only, keys keep scrolling
		var cmd tea.Cmd
		tv.vp, cmd = tv.vp.Update(msg)
		return m, cmd
	}
	var cmd tea.Cmd
	tv.input, cmd = tv.input.Update(msg)
	return m, cmd
}

// taskViewView renders the open task pane full-screen: header, transcript,
// the chat input, and a footer hint (View's layout mirrors View's structure).
func (m *model) taskViewView() string {
	tv := m.taskVP
	t, ok := m.agent.Tasks().Get(tv.id)
	status := "running"
	if ok {
		status = string(t.Status)
	}
	if ok && t.Restored {
		status += ", restored"
	}
	if tv.busy {
		status += ", replying"
	}
	head := toolStyle.Render(fmt.Sprintf(" ⚙ %s — %s", tv.id, truncLine(t.Description, max(m.width-30, 8)))) +
		dimStyle.Render("  ("+status+")")
	in := " " + tv.input.View()
	hint := " esc back · ↑/↓ scroll · enter send (steers while running, chats after) · ctrl+x cancel"
	if ok && t.Restored {
		in = dimStyle.Render(" (restored from a previous session — read-only)")
		hint = " esc back · ↑/↓ scroll"
	}
	foot := dimStyle.Render(hint)
	return head + "\n" + sanitizeView(tv.vp.View()) + "\n" + in + "\n" + foot
}
