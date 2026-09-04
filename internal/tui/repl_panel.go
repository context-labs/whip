package tui

import (
	"encoding/json"
	"fmt"
	"github.com/context-labs/whip/internal/tui/ui"
	"image/color"
	"strings"
	"time"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/session"
)

// The REPL panel is a mode of the opencode right sidebar (ctrl+x r or /repl)
// that shows the visible agent's Starlark cells as they happen, notebook
// style: the code as the model writes it (stream.tool.call snapshots), live
// print output (stream.tool.output), each host call with its duration
// (stream.cell.host), the result, and worker restarts. Cells for every agent
// are kept so the panel follows the agent tree, and a strip lists other
// running agents.

const replOutputTail = 6 // output lines shown per cell

type replHost struct {
	name, summary, duration, err string
}

type replCell struct {
	id      string
	n       int
	code    string
	started time.Time
	ended   time.Time
	output  string
	hosts   []replHost
	value   string
	errText string
	steps   uint64
	// restart is a marker row ("restarted · restored 9") instead of a cell.
	restart  string
	finished bool // stream.tool.completed seen (replayed cells have no ended time)
}

type replAgent struct {
	cells  []replCell
	count  int
	seq    int64     // newest event seq folded in; snapshots replay older ones, which are skipped
	tool   string    // the tool the agent is running now ("" between tools); rlm_exec shows as a cell instead
	toolAt time.Time // when that tool started (zero on replay)
}

// panelWidth is the REPL panel's width: replMinWidth alone (or when it has
// displaced the left column); beside both columns it takes what a
// chatMinWidth chat leaves, up to replMaxWidth.
func (m *model) panelWidth() int {
	if !m.leftVisible() {
		return replMinWidth
	}
	return min(max(m.termWidth-m.mainX()-2-chatMinWidth, replMinWidth), replMaxWidth)
}

func (m *model) replAgentFor(agentID string) *replAgent {
	if m.repl == nil {
		m.repl = map[string]*replAgent{}
	}
	value := m.repl[agentID]
	if value == nil {
		value = &replAgent{}
		m.repl[agentID] = value
	}
	return value
}

func (m *model) replNow() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

// replApplySeq folds an event with a known sequence number, skipping ones
// already seen: live events and later snapshot replays carry the same seq.
func (m *model) replApplySeq(agentID, kind string, event daemon.StreamEvent, seq int64) {
	if seq > 0 {
		agent := m.replAgentFor(agentID)
		if seq <= agent.seq {
			return
		}
		agent.seq = seq
	}
	m.replApply(agentID, kind, event)
}

// replApply folds one presentation event into an agent's cell history.
func (m *model) replApply(agentID, kind string, event daemon.StreamEvent) {
	if event.ID == "" {
		return
	}
	agent := m.replAgentFor(agentID)
	find := func() *replCell {
		for index := range agent.cells {
			if agent.cells[index].id == event.ID && agent.cells[index].restart == "" {
				return &agent.cells[index]
			}
		}
		return nil
	}
	ensure := func() *replCell {
		if cell := find(); cell != nil {
			return cell
		}
		agent.cells = append(agent.cells, replCell{id: event.ID})
		return &agent.cells[len(agent.cells)-1]
	}
	switch kind {
	case "stream.tool.call":
		if event.Name != "rlm_exec" {
			return
		}
		ensure().code = codeFromPartialArgs(event.Args)
	case "stream.tool.started":
		if event.Name != "rlm_exec" {
			agent.tool, agent.toolAt = event.Name, m.replNow() // activity for the agent rows
			return
		}
		agent.tool = ""
		cell := ensure()
		if code := codeFromPartialArgs(event.Args); code != "" {
			cell.code = code
		}
		if cell.n == 0 {
			agent.count++
			cell.n = agent.count
		}
		if !m.replReplaying {
			cell.started = m.replNow()
		}
	case "stream.tool.output":
		if cell := find(); cell != nil {
			cell.output = event.Text
		}
	case "stream.cell.host":
		if cell := find(); cell != nil {
			cell.hosts = append(cell.hosts, replHost{name: event.Name, summary: event.Args, duration: event.Text, err: event.Result})
		}
	case "stream.tool.completed":
		agent.tool = ""
		if event.Name != "rlm_exec" {
			return
		}
		cell := ensure()
		cell.finished = true
		if cell.n == 0 {
			agent.count++
			cell.n = agent.count
		}
		if !m.replReplaying {
			cell.ended = m.replNow()
		}
		if rest, failed := strings.CutPrefix(event.Result, "Error:"); failed {
			cell.errText = strings.TrimSpace(rest)
			return
		}
		var result struct {
			Value  any    `json:"value"`
			Output string `json:"output"`
			Steps  uint64 `json:"steps"`
		}
		if json.Unmarshal([]byte(event.Result), &result) != nil {
			return
		}
		cell.output, cell.steps = result.Output, result.Steps
		if result.Value != nil {
			encoded, _ := json.Marshal(result.Value)
			cell.value = string(encoded)
		}
	}
}

// replRestart inserts a worker-restart marker into an agent's history.
func (m *model) replRestart(agentID string, restored, notRestored int) {
	agent := m.replAgentFor(agentID)
	marker := fmt.Sprintf("restarted · restored %d", restored)
	if notRestored > 0 {
		marker += fmt.Sprintf(" · %d skipped", notRestored)
	}
	agent.cells = append(agent.cells, replCell{restart: marker})
}

// replRebuild folds the stored presentation events (a fresh snapshot, a newly
// opened child) into the REPL history. The history is never rebuilt from
// scratch: snapshots only keep the current turn's events and drop idle
// children entirely, so cells seen earlier in this TUI session would vanish.
// Events already folded in are skipped by seq; stored events carry no clock,
// so cells first seen here have no times. The scroll position is left alone.
func (m *model) replRebuild() {
	m.replReplaying = true
	defer func() { m.replReplaying = false }()
	root := m.rootAgentID()
	replay := func(agentID string, events []session.SnapshotEvent) {
		for _, event := range events {
			if !strings.HasPrefix(event.Kind, "stream.") {
				continue
			}
			var payload daemon.StreamEvent
			if json.Unmarshal(event.Payload, &payload) != nil {
				continue
			}
			m.replApplySeq(agentID, event.Kind, payload, event.Seq)
		}
	}
	if root != "" {
		replay(root, m.clientView.presentation)
	}
	for agentID, events := range m.clientView.agentPresentations {
		replay(agentID, events)
	}
}

// codeFromPartialArgs extracts the "code" string from rlm_exec arguments that
// may still be streaming: a complete JSON object decodes directly; a
// truncated one yields the decoded prefix of the code so the panel can show
// the cell as the model writes it.
func codeFromPartialArgs(args string) string {
	var complete struct {
		Code string `json:"code"`
	}
	if json.Unmarshal([]byte(args), &complete) == nil {
		return complete.Code
	}
	key := strings.Index(args, `"code"`)
	if key < 0 {
		return ""
	}
	rest := args[key+len(`"code"`):]
	quote := strings.IndexByte(rest, '"')
	if quote < 0 {
		return ""
	}
	rest = rest[quote+1:]
	var b strings.Builder
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		switch c {
		case '"':
			return b.String()
		case '\\':
			if i+1 >= len(rest) {
				return b.String()
			}
			i++
			switch rest[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case 'u':
				if i+4 >= len(rest) {
					return b.String()
				}
				var r rune
				if _, err := fmt.Sscanf(rest[i+1:i+5], "%04x", &r); err != nil {
					return b.String()
				}
				b.WriteRune(r)
				i += 4
			default:
				b.WriteByte(rest[i])
			}
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// replCurrentCell describes an agent's running cell for the agent tree:
// "  In[3] files.list(...)  1.2s", or "" when nothing is executing.
func (m *model) replCurrentCell(agentID string) string {
	history := m.repl[agentID]
	if history == nil {
		return ""
	}
	for index := len(history.cells) - 1; index >= 0; index-- {
		cell := history.cells[index]
		if cell.restart != "" || cell.finished || !cell.ended.IsZero() {
			continue
		}
		cellNo := "·"
		if cell.n > 0 {
			cellNo = fmt.Sprint(cell.n)
		}
		line := fmt.Sprintf("  In[%s] %s", cellNo, firstLine(cell.code))
		if !cell.started.IsZero() {
			line += "  " + replDuration(m.replNow().Sub(cell.started))
		}
		return line
	}
	return ""
}

// replFlat keeps free text on one row: tabs become spaces (lipgloss would
// expand them after truncation), carriage returns vanish, newlines fold.
func replFlat(s string) string {
	return strings.NewReplacer("\t", "    ", "\r", "", "\n", " ⏎ ").Replace(s)
}

func replDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// replStyles are the panel's palette on one background: the REPL column
// itself sits on the native background like the chat, and each cell is a
// card on the panel shade (the same shade as the chat's turn blocks).
type replStyles struct {
	bg                                    color.Color
	head, dim, text, warn, fail, accent   lipgloss.Style
	keyword, str, num, comment, mod, call lipgloss.Style
	gutterRun, gutterDone, gutterFail     lipgloss.Style
}

func newReplStyles(bg color.Color) replStyles {
	th := currentTheme()
	syn := th.Syntax()
	on := func(fg color.Color) lipgloss.Style { return th.On(fg, bg) }
	return replStyles{
		bg:         bg,
		head:       on(th.Text).Bold(true),
		dim:        on(th.Muted),
		text:       on(th.Text),
		warn:       on(th.Warning),
		fail:       on(th.Error),
		accent:     on(th.Info),
		keyword:    on(syn.Keyword),
		str:        on(syn.String),
		num:        on(syn.Number),
		comment:    on(syn.Comment).Italic(true),
		mod:        on(syn.Type),
		call:       on(syn.Func).Bold(true),
		gutterRun:  on(th.Info),
		gutterDone: on(th.Muted),
		gutterFail: on(th.Error),
	}
}

// replPanelView renders the REPL panel at the sidebar's height: a header with
// the agent and its context stats, a strip of other running agents, then the
// visible agent's cells with the newest kept in view.
func (m *model) replPanelView(height int) string {
	width := m.panelWidth()
	st := newReplStyles(nil)
	card := newReplStyles(currentTheme().Surface.Panel)
	inner := width - 3 // two columns of left padding and one of right margin
	cut := func(s string, w int) string { return ansi.Truncate(s, max(w, 1), "…") }

	visible := m.visibleAgentID()
	name := "root"
	if value, ok := m.runtimeAgent(visible); ok && value.Name != "" {
		name = value.Name
	}
	u := m.displayUsage()
	stats := fmtTok(u.PromptTokens+u.CompletionTokens) + " tokens"
	if limit := m.displayContextLimit(); limit > 0 {
		stats += fmt.Sprintf(" (%d%%)", estimateTokens(m.displayMessages())*100/limit)
	}
	if cost, ok := m.sessionCost(); ok {
		stats += fmt.Sprintf(" · $%.2f", cost)
	}
	top := []string{
		st.head.Render(cut("REPL · "+name, inner)),
		st.dim.Render(cut(stats, inner)),
	}

	if agents, _ := m.agentRows(inner, nil, agentsDockHeight); len(agents) > 0 {
		top = append(top, "")
		top = append(top, agents...)
	}
	top = append(top, "")

	var body []string
	history := m.repl[visible]
	if history == nil || len(history.cells) == 0 {
		body = append(body, st.dim.Render("no cells yet"))
	} else {
		for _, cell := range history.cells {
			if cell.restart != "" {
				body = append(body, "", st.warn.Render(cut("── "+cell.restart+" ──", inner)))
				continue
			}
			body = append(body, m.replCellRows(cell, card, inner)...)
		}
	}

	if m.replViewAgent != visible {
		m.replViewAgent, m.replScroll, m.replBodyLen = visible, 0, 0
	}
	if m.replScroll > 0 && len(body) > m.replBodyLen {
		m.replScroll += len(body) - m.replBodyLen // new rows arrive below; what is read stays put
	}
	m.replBodyLen = len(body)

	rows := append([]string(nil), top...)
	if height <= 0 {
		rows = append(rows, body...)
	} else {
		// The newest cell stays in view unless the wheel scrolled the panel
		// up; then a footer row says how far from the bottom it is.
		budget := max(height-len(top), 1)
		m.replScroll = min(m.replScroll, max(len(body)-budget, 0))
		if m.replScroll > 0 {
			budget = max(budget-1, 1)
		}
		end := len(body) - m.replScroll
		body = body[max(end-budget, 0):end]
		if m.replScroll > 0 {
			body = append(body, st.dim.Render(cut(fmt.Sprintf("↓ %d more lines", m.replScroll), inner)))
		}
		rows = append(rows, body...)
		for len(rows) < height {
			rows = append(rows, "")
		}
		rows = rows[:height]
	}
	out := make([]string, len(rows))
	for index, row := range rows {
		out[index] = ui.PadRow("  "+row, width, st.bg)
	}
	return strings.Join(out, "\n")
}

// replCellRows renders one cell notebook-style: a blank separator, then a
// header row, code, host calls, output, result, and error rows behind a
// colored gutter, each padded to inner so the cell reads as one card.
func (m *model) replCellRows(cell replCell, st replStyles, inner int) []string {
	cut := func(s string, w int) string { return ansi.Truncate(s, max(w, 1), "…") }
	gutter := st.gutterDone
	switch {
	case cell.errText != "":
		gutter = st.gutterFail
	case cell.ended.IsZero():
		gutter = st.gutterRun
	}
	bar := gutter.Render("▍ ")
	content := inner - 2
	row := func(styled string) string { return bar + styled }

	header := "In [·]"
	if cell.n > 0 {
		header = fmt.Sprintf("In [%d]", cell.n)
	}
	switch {
	case !cell.ended.IsZero() && !cell.started.IsZero():
		header += "  " + replDuration(cell.ended.Sub(cell.started))
	case !cell.started.IsZero():
		header += "  " + replDuration(m.replNow().Sub(cell.started)) + " …"
	}
	if cell.steps > 0 {
		header += fmt.Sprintf(" · %d steps", cell.steps)
	}
	rows := []string{"", row(st.head.Render(cut(header, content)))}

	var hl starlarkHighlighter
	for _, line := range strings.Split(strings.TrimRight(cell.code, "\n"), "\n") {
		line = strings.NewReplacer("\t", "    ", "\r", "").Replace(line)
		for index, segment := range strings.Split(ansi.Hardwrap(line, max(content-2, 1), true), "\n") {
			styled := hl.line(segment, st)
			if index > 0 {
				styled = st.dim.Render("↪ ") + styled
			}
			rows = append(rows, row(cut(styled, content)))
		}
	}
	for _, host := range cell.hosts {
		line := "→ " + host.name
		if host.summary != "" {
			line += "(" + replFlat(host.summary) + ")"
		}
		if host.duration != "" {
			line += " " + host.duration
		}
		if host.err != "" {
			rows = append(rows, row(st.fail.Render(cut(line+" ✗ "+replFlat(host.err), content))))
		} else {
			rows = append(rows, row(st.dim.Render(cut(line, content))))
		}
	}
	if out := strings.TrimRight(cell.output, "\n"); out != "" {
		lines := strings.Split(out, "\n")
		if len(lines) > replOutputTail {
			hidden := len(lines) - replOutputTail
			if cell.ended.IsZero() {
				lines = append([]string{fmt.Sprintf("… %d earlier lines", hidden)}, lines[len(lines)-replOutputTail:]...)
			} else {
				lines = append(lines[:replOutputTail], fmt.Sprintf("+%d lines", hidden))
			}
		}
		for _, line := range lines {
			rows = append(rows, row(st.text.Render(cut(replFlat(line), content))))
		}
	}
	if cell.value != "" {
		rows = append(rows, row(st.accent.Render("⇒ ")+st.text.Render(cut(replFlat(cell.value), content-2))))
	}
	if cell.errText != "" {
		rows = append(rows, row(st.fail.Render(cut("✗ "+replFlat(cell.errText), content))))
	}
	for index := 1; index < len(rows); index++ { // rows[0] is the separator
		rows[index] = ui.PadRow(rows[index], inner, st.bg)
	}
	return rows
}

// starlarkHighlighter colors one line at a time, carrying an open
// triple-quoted string across lines.
type starlarkHighlighter struct {
	openQuote string // "" or the triple-quote delimiter currently open
}

var starlarkKeywords = map[string]bool{
	"and": true, "break": true, "continue": true, "def": true, "elif": true, "else": true, "for": true,
	"if": true, "in": true, "lambda": true, "load": true, "not": true, "or": true, "pass": true,
	"return": true, "while": true, "True": true, "False": true, "None": true,
}

func (h *starlarkHighlighter) line(text string, st replStyles) string {
	var b strings.Builder
	runes := []rune(text)
	i := 0
	flush := func(style lipgloss.Style, from, to int) {
		if to > from {
			b.WriteString(style.Render(string(runes[from:to])))
		}
	}
	for i < len(runes) {
		if h.openQuote != "" {
			end := strings.Index(string(runes[i:]), h.openQuote)
			if end < 0 {
				flush(st.str, i, len(runes))
				return b.String()
			}
			stop := i + len([]rune(string(runes[i:])[:end])) + 3
			flush(st.str, i, stop)
			h.openQuote = ""
			i = stop
			continue
		}
		c := runes[i]
		switch {
		case c == '#':
			flush(st.comment, i, len(runes))
			return b.String()
		case c == '"' || c == '\'':
			if i+2 < len(runes) && runes[i+1] == c && runes[i+2] == c {
				h.openQuote = string([]rune{c, c, c})
				flush(st.str, i, i+3)
				i += 3
				continue
			}
			start := i
			i++
			for i < len(runes) && runes[i] != c {
				if runes[i] == '\\' {
					i++
				}
				i++
			}
			i = min(i+1, len(runes))
			flush(st.str, start, i)
		case unicode.IsDigit(c):
			start := i
			for i < len(runes) && (unicode.IsDigit(runes[i]) || runes[i] == '.' || runes[i] == '_' || runes[i] == 'x') {
				i++
			}
			flush(st.num, start, i)
		case unicode.IsLetter(c) || c == '_':
			start := i
			for i < len(runes) && (unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i]) || runes[i] == '_') {
				i++
			}
			word := string(runes[start:i])
			switch {
			case starlarkKeywords[word]:
				flush(st.keyword, start, i)
			case i < len(runes) && runes[i] == '.':
				flush(st.mod, start, i)
			case i < len(runes) && runes[i] == '(':
				flush(st.call, start, i)
			default:
				flush(st.text, start, i)
			}
		default:
			start := i
			for i < len(runes) && !unicode.IsLetter(runes[i]) && !unicode.IsDigit(runes[i]) && runes[i] != '_' && runes[i] != '"' && runes[i] != '\'' && runes[i] != '#' {
				i++
			}
			flush(st.text, start, i)
		}
	}
	return b.String()
}

// agentActivity is what an agent is doing right now, for the row's right
// slot: the running REPL cell with its elapsed time, else the running tool.
func (m *model) agentActivity(agentID string) string {
	history := m.repl[agentID]
	if history == nil {
		return ""
	}
	for index := len(history.cells) - 1; index >= 0; index-- {
		cell := history.cells[index]
		if cell.restart != "" || cell.finished || !cell.ended.IsZero() {
			continue
		}
		cellNo := "·"
		if cell.n > 0 {
			cellNo = fmt.Sprint(cell.n)
		}
		line := "In[" + cellNo + "] " + firstLine(cell.code)
		if !cell.started.IsZero() {
			line += "  " + replDuration(m.replNow().Sub(cell.started))
		}
		return strings.TrimSpace(line)
	}
	if history.tool != "" {
		if history.toolAt.IsZero() {
			return history.tool
		}
		return history.tool + " " + replDuration(m.replNow().Sub(history.toolAt))
	}
	return ""
}
