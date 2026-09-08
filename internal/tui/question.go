package tui

import (
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tui/ui"
)

// user.ask: the root agent's Starlark cell blocks on a question; the daemon
// publishes it as question.pending, this dialog floats over the dimmed
// session, and the pick goes back as the client operation question.answer.
// question.answered / question.closed (turn cancelled, agent stopped) close
// the dialog wherever the answer came from — another client may have picked.

// questionDialog is the open question (the question.pending event: agent,
// id, text, options, Multiple), the cursor, the toggled set (Multiple) and
// whether the answer is on its way (keys are ignored until the reply or the
// daemon's event closes the dialog, or the command fails and hands it back).
// A batched ask pages through Questions; drafts keeps each page's picks and
// written response until the batch is submitted from the last page.
type questionDialog struct {
	session.LifecycleEvent
	sel      int
	chosen   map[int]bool
	inFlight bool
	index    int
	drafts   []questionDraft
	editing  bool
}

// questionDraft is the buffered answer for one page of a batch: picked
// options by index, free text, or a skip.
type questionDraft struct {
	chosen  map[int]bool
	text    string
	skipped bool
}

// questionAnswer is the question.answer client-op payload.
type questionAnswer struct {
	ID        string                         `json:"id"`
	Answer    []string                       `json:"answer,omitempty"`
	Dismissed bool                           `json:"dismissed,omitempty"`
	Answers   []protocol.QuestionAnswerEntry `json:"answers,omitempty"`
}

// sets returns the batch's questions, synthesizing one from the legacy flat
// fields when the daemon (or an older snapshot) carried a single ask.
func (q *questionDialog) sets() []session.QuestionSet {
	if len(q.Questions) > 0 {
		return q.Questions
	}
	return []session.QuestionSet{{Question: q.Question, Options: q.Options, Multiple: q.Multiple}}
}

// current returns the page on display.
func (q *questionDialog) current() session.QuestionSet { return q.sets()[q.index] }

// applyQuestionEvent opens the dialog for the root agent's question and
// settles it when the daemon records an answer or drops the question.
func (m *model) applyQuestionEvent(kind string, event session.LifecycleEvent) {
	switch kind {
	case "question.pending":
		if root := m.rootAgentID(); root != "" && event.AgentID != "" && event.AgentID != root {
			return // only the root may ask (the daemon refuses children); never open a dialog for one
		}
		sets := event.Questions
		if len(sets) == 0 && event.Question != "" {
			sets = []session.QuestionSet{{Question: event.Question, Options: event.Options, Multiple: event.Multiple}}
		}
		if len(sets) == 0 || len(sets[0].Options) == 0 {
			return // malformed: nothing to pick, and the key handler indexes the options
		}
		if m.question != nil && m.question.QuestionID == event.QuestionID {
			return // already open (listed by a snapshot, then replayed): keep the cursor
		}
		dialog := &questionDialog{LifecycleEvent: event, chosen: map[int]bool{}}
		if len(sets) > 1 {
			dialog.drafts = make([]questionDraft, len(sets))
			for i := range dialog.drafts {
				dialog.drafts[i].chosen = map[int]bool{}
			}
		}
		m.question = dialog
	case "question.answered":
		m.settleQuestion(event.QuestionID, questionOutcome(event))
	case "question.closed":
		m.settleQuestion(event.QuestionID, "closed: "+event.Error)
	}
}

// applyClientQuestions is the snapshot path: a client that connects while the
// root is blocked on user.ask has no question.pending to replay (it predates
// the cursor), so the daemon lists open questions in the snapshot; a dialog
// the snapshot no longer lists was settled while this client was away.
func (m *model) applyClientQuestions(questions []session.LifecycleEvent) {
	if m.question != nil && !slices.ContainsFunc(questions, func(question session.LifecycleEvent) bool { return question.QuestionID == m.question.QuestionID }) {
		m.question = nil
	}
	if len(questions) > 0 {
		m.applyQuestionEvent("question.pending", questions[0])
	}
}

// settleQuestion closes the dialog for id — whichever of the daemon's event or
// our own reply lands first — and notes the outcome, with the question it
// answers, under the root's transcript (a child's view is not the place).
func (m *model) settleQuestion(id, outcome string) {
	q := m.question
	if q == nil || q.QuestionID != id {
		return
	}
	m.question = nil
	if m.agentOpen == "" {
		m.append(dimStyle.Render(`(question "` + ansiTruncate(strings.Join(strings.Fields(q.Question), " "), 40) + `" ` + outcome + ")"))
	}
}

// questionResults adapts the wire answer entries to the stored type.
func questionResults(entries []protocol.QuestionAnswerEntry) []session.QuestionResult {
	if entries == nil {
		return nil
	}
	results := make([]session.QuestionResult, len(entries))
	for i, entry := range entries {
		results[i] = session.QuestionResult{Answer: entry.Answer, Dismissed: entry.Dismissed}
	}
	return results
}

func questionOutcome(event session.LifecycleEvent) string {
	if len(event.Answers) > 0 {
		var answered int
		for _, result := range event.Answers {
			if !result.Dismissed {
				answered++
			}
		}
		if answered == 0 {
			return "dismissed"
		}
		return "answered " + strconv.Itoa(answered) + "/" + strconv.Itoa(len(event.Answers))
	}
	if event.Dismissed {
		return "dismissed"
	}
	return "answered: " + strings.Join(event.Answer, ", ")
}

// questionKey: ↑/k/ctrl+p and ↓/j/ctrl+n move, 1..n jump, space toggles
// (Multiple), enter answers with the cursor option (or the toggled set, a
// no-op while empty), esc dismisses. A batch pages: enter answers and
// advances, tab/→ next, shift+tab/← back, s skips the page, / edits a
// written response, and the last page's enter submits them all.
func (m *model) questionKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	q := m.question
	if q.inFlight {
		if msg.String() == "ctrl+c" {
			// The reply is overdue; drop the inert panel and let ctrl+c do its
			// usual work (interrupt the turn, arm quit) instead of swallowing it.
			m.question = nil
			return m.thinKey(msg)
		}
		return m, nil
	}
	sets := q.sets()
	batch := len(sets) > 1
	if batch {
		return m.batchQuestionKey(msg, sets)
	}
	n := len(q.Options)
	switch key := msg.String(); key {
	case "up", "k", "ctrl+p":
		q.sel = (q.sel + n - 1) % n
	case "down", "j", "ctrl+n":
		q.sel = (q.sel + 1) % n
	case "space":
		if q.Multiple {
			q.chosen[q.sel] = !q.chosen[q.sel]
		}
	case "enter":
		if !q.Multiple {
			return m.answerQuestion([]string{q.Options[q.sel].Label}, false)
		}
		var answer []string
		for i, opt := range q.Options {
			if q.chosen[i] {
				answer = append(answer, opt.Label)
			}
		}
		if len(answer) == 0 {
			return m, nil
		}
		return m.answerQuestion(answer, false)
	case "esc", "ctrl+c":
		return m.answerQuestion(nil, true)
	default:
		if i, err := strconv.Atoi(key); err == nil && i >= 1 && i <= n {
			q.sel = i - 1
		}
	}
	return m, nil
}

// batchQuestionKey drives the wizard. Free-text editing is a mode: while
// editing, keys go to the page's response text and esc/enter leave the mode.
func (m *model) batchQuestionKey(msg tea.KeyPressMsg, sets []session.QuestionSet) (tea.Model, tea.Cmd) {
	q := m.question
	set := sets[q.index]
	draft := &q.drafts[q.index]
	n := len(set.Options)
	key := msg.String()
	if q.editing {
		switch key {
		case "esc", "enter":
			q.editing = false
		case "backspace":
			if runes := []rune(draft.text); len(runes) > 0 {
				draft.text = string(runes[:len(runes)-1])
			}
		case "space":
			draft.text += " "
		default:
			if msg.Text != "" {
				draft.text += msg.Text
				draft.skipped = false
			}
		}
		return m, nil
	}
	switch key {
	case "up", "k", "ctrl+p":
		q.sel = (q.sel + n - 1) % n
	case "down", "j", "ctrl+n":
		q.sel = (q.sel + 1) % n
	case "space":
		if set.Multiple {
			draft.chosen[q.sel] = !draft.chosen[q.sel]
			draft.skipped = false
		}
	case "/":
		q.editing = true
	case "s":
		draft.chosen, draft.text, draft.skipped = map[int]bool{}, "", true
		return m.batchAdvance(sets)
	case "tab", "right":
		return m.batchAdvance(sets)
	case "shift+tab", "left":
		if q.index > 0 {
			q.index--
			q.sel = 0
		}
	case "enter":
		if !set.Multiple && len(set.Options) > 0 && draft.text == "" {
			draft.chosen = map[int]bool{q.sel: true}
		}
		draft.skipped = false
		return m.batchAdvance(sets)
	case "esc", "ctrl+c":
		return m.dismissQuestionBatch()
	default:
		if i, err := strconv.Atoi(key); err == nil && i >= 1 && i <= n {
			q.sel = i - 1
		}
	}
	return m, nil
}

// batchAdvance moves to the next page, or submits the batch from the last
// page. The current page must hold something (a pick, text, or a skip).
func (m *model) batchAdvance(sets []session.QuestionSet) (tea.Model, tea.Cmd) {
	q := m.question
	if q.index < len(sets)-1 {
		q.index++
		q.sel = 0
		return m, nil
	}
	return m.submitQuestionBatch()
}

func draftAnswer(set session.QuestionSet, draft questionDraft) []string {
	var answer []string
	for i, opt := range set.Options {
		if draft.chosen[i] {
			answer = append(answer, opt.Label)
		}
	}
	if text := strings.TrimSpace(draft.text); text != "" {
		answer = append(answer, text)
	}
	return answer
}

// submitQuestionBatch sends one question.answer with every page's outcome;
// a page the user never touched is a skip.
func (m *model) submitQuestionBatch() (tea.Model, tea.Cmd) {
	q := m.question
	sets := q.sets()
	answers := make([]protocol.QuestionAnswerEntry, len(sets))
	for i, set := range sets {
		if q.drafts[i].skipped {
			answers[i] = protocol.QuestionAnswerEntry{Dismissed: true}
			continue
		}
		answers[i] = protocol.QuestionAnswerEntry{Answer: draftAnswer(set, q.drafts[i])}
	}
	next, command := m.submitClientAction("question.answer", questionAnswer{ID: q.QuestionID, Answers: answers}, "")
	q.inFlight = command != nil
	return next, command
}

// dismissQuestionBatch marks every page dismissed.
func (m *model) dismissQuestionBatch() (tea.Model, tea.Cmd) {
	q := m.question
	answers := make([]protocol.QuestionAnswerEntry, len(q.sets()))
	for i := range answers {
		answers[i] = protocol.QuestionAnswerEntry{Dismissed: true}
	}
	next, command := m.submitClientAction("question.answer", questionAnswer{ID: q.QuestionID, Answers: answers}, "")
	q.inFlight = command != nil
	return next, command
}

// answerQuestion sends question.answer; the dialog stays up, inert, until the
// reply or question.answered closes it (or the reply fails, see thin_update.go).
func (m *model) answerQuestion(answer []string, dismissed bool) (tea.Model, tea.Cmd) {
	q := m.question
	next, command := m.submitClientAction("question.answer", questionAnswer{ID: q.QuestionID, Answer: answer, Dismissed: dismissed}, "")
	q.inFlight = command != nil // a dead daemon is reported in the transcript and nothing was sent
	return next, command
}

// questionWidth is wider than dialogWidth so option descriptions fit beside
// their labels.
func (m *model) questionWidth() int { return min(72, max(m.width-2, 20)) }

// questionRows renders the dialog: a Panel titled Question with the question
// as its heading, one ListRow per option (its number as the badge, plus ○/✓
// in Multiple mode; the description beside the label when it fits, under it
// otherwise) and the key hints. A batch titles itself "Question 2/4" and
// shows the page's written response under the options. On a short terminal
// the options window around the cursor first, then the question loses lines,
// so the cursor row and the hints always show.
func (m *model) questionRows() []string {
	q := m.question
	th := currentTheme()
	bg := th.Surface.Panel
	sets := q.sets()
	set := sets[q.index]
	batch := len(sets) > 1
	title := "Question"
	if batch {
		title = "Question " + strconv.Itoa(q.index+1) + "/" + strconv.Itoa(len(sets))
	}
	p := ui.Panel{Title: title, Count: "esc", Width: m.questionWidth(), Band: true}
	inner := p.Inner(th)
	band := inner + 2
	lead := ui.PadRow("", 1, bg) // band rows overhang the text inset by one cell; plain rows step back in
	heading := strings.Split(wrap(set.Question, inner), "\n")
	hints := th.On(th.Muted, bg).Render("sending…")
	if !q.inFlight {
		var pairs []string
		switch {
		case q.editing:
			pairs = []string{"type", "respond", "enter", "done", "esc", "stop editing"}
		case batch:
			pairs = []string{"↑↓", "select", "enter", "answer", "s", "skip", "/", "write", "tab", "next", "shift+tab", "back", "esc", "dismiss"}
			if set.Multiple {
				pairs = append(pairs, "space", "toggle")
			}
		default:
			pairs = []string{"↑↓", "select", "enter", "answer"}
			if set.Multiple {
				pairs = append(pairs, "space", "toggle")
			}
			pairs = append(pairs, "esc", "dismiss", "1-"+strconv.Itoa(len(set.Options)), "jump")
		}
		hints = ui.Hints(th, bg, pairs...)
	}
	chosen := func(i int) bool {
		if batch {
			return q.drafts[q.index].chosen[i]
		}
		return q.chosen[i]
	}
	option := func(i int) []string {
		opt := set.Options[i]
		badge, badgeColor := strconv.Itoa(i+1), th.Muted
		if set.Multiple {
			mark := "○" // the plan view's pending / done glyphs
			if chosen(i) {
				mark, badgeColor = "✓", th.Success
			}
			badge += " " + mark
		}
		bw := lipgloss.Width(badge)
		label := opt.Label
		if opt.Recommended {
			label += " ★"
		}
		row := ui.ListRow{Badge: badge, BadgeColor: badgeColor, Label: label, Selected: i == q.sel && !q.editing, Primary: true, Width: band}
		// ListRow keeps Right under half the band and truncates it; a longer
		// description gets its own muted lines under the label instead.
		if dw := lipgloss.Width(opt.Description); dw <= band/2-1 && bw+lipgloss.Width(opt.Label)+dw+5 <= band {
			row.Right = opt.Description
		}
		rows := []string{row.Render(th, bg)}
		if row.Right == "" && opt.Description != "" {
			fill, fg := bg, th.Muted // continuation lines read as description, not as more label
			if i == q.sel && !q.editing {
				fill, fg = th.Primary, th.OnPrimary
			}
			for line := range strings.SplitSeq(wrap(opt.Description, band-bw-3), "\n") {
				rows = append(rows, ui.PadRow(th.On(fg, fill).Render(strings.Repeat(" ", bw+2)+line), band, fill))
			}
		}
		return rows
	}
	// body lays out the first qn heading lines and a window of win options
	// around the cursor.
	body := func(qn, win int) []string {
		var body []string
		for i, line := range heading[:qn] {
			if i == qn-1 && qn < len(heading) {
				line = ansi.Truncate(line, inner-1, "") + "…"
			}
			body = append(body, lead+th.On(th.Text, bg).Bold(true).Render(line)) // the heading, on the surface
		}
		body = append(body, "")
		lo, hi := ui.ListWindow(len(set.Options), q.sel, win)
		for i := lo; i < hi; i++ {
			body = append(body, option(i)...)
		}
		if batch {
			draft := q.drafts[q.index]
			response := draft.text
			if q.editing {
				response += "▏"
			}
			if response == "" {
				response = "write a response…"
			}
			fill, fg := bg, th.Muted
			if q.editing {
				fg = th.Text
			}
			body = append(body, "")
			for line := range strings.SplitSeq(wrap("✎ "+response, inner), "\n") {
				body = append(body, lead+th.On(fg, fill).Render(line))
			}
			if draft.skipped {
				body = append(body, lead+th.On(th.Muted, bg).Render("(skipped)"))
			}
		}
		body = append(body, "", lead+hints)
		return strings.Split(p.Render(th, strings.Join(body, "\n")), "\n")
	}
	qn, win := len(heading), len(set.Options)
	rows := body(qn, win)
	for h := m.dialogHeight(); h > 0 && len(rows) > h; rows = body(qn, win) {
		over := len(rows) - h
		switch {
		case win > 1:
			win-- // one option at a time: an option with a wrapped description is several rows
		case qn > 1:
			qn = max(qn-over, 1)
		default:
			// One question line and one option still overflow (a tiny terminal):
			// cut above the hints and the panel's bottom pad, never the hints.
			tail := 1 + th.Space.PadY
			return append(rows[:h-tail], rows[len(rows)-tail:]...)
		}
	}
	return rows
}
