package tui

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// In-app drag selection. whip enables mouse reporting (?1000 clicks/wheel +
// ?1002 button-motion), and enabling ANY mouse mode makes most terminals
// (Ghostty, kitty, …) hand the drag to the app instead of starting a native
// selection — so with capture on there is no drag-to-copy unless the app
// implements it. We track the dragged range over the transcript viewport,
// highlight it (reverse video), and copy on release.
//
// The copy goes to the system clipboard via OSC 52 (works locally and over
// SSH/tmux, subject to the terminal's clipboard-osc52 setting); pbcopy /
// wl-copy / xclip is the fallback for terminals that ignore OSC 52
// (Terminal.app). Inside tmux the drag reaches whip too (tmux forwards it
// because mouse_any_flag is set), so this same selection works there — no
// copy-mode override.
//
// ponytail: selection is only tracked over the transcript viewport (the common
// case: copying agent output). The header/dock/input rows aren't selectable,
// and copy reconstruction mimics the word-aware wrap rather than keeping a
// parallel plain-text render of every block.

// selPos is one endpoint of the drag. For the transcript region, row is a
// viewport content row (see selPoint); for the input region, row is an index
// into inputLines. input marks which region the endpoint is in — a selection
// is always confined to one region (anchor's), so the two never mix.
type selPos struct {
	row, col int  // col is a display cell (wide runes count 2)
	input    bool // true = endpoint is in the input box, not the transcript
}

// selection is the in-flight (dragging) or last-completed selection. It
// survives release so the highlight stays on screen until any keypress or a
// new press clears it.
type selection struct {
	anchor, cur selPos
	done        bool // button released; highlight stays until cleared
}

// selPoint converts ABSOLUTE screen coords to a selection endpoint. Content
// row r renders at screen row viewTop + 3 (header + hint + blank) +
// (r + contentPad - YOffset) - vpLead, where vpLead is the top blank rows
// viewportView dropped. Invert that:
// content row = y - viewTop - 3 - contentPad + YOffset + vpLead.
//
// clamp=false (a press): rows outside the block range return ok=false — a
// press on the header, input box, or dock must NOT start a selection (it
// would eat those rows' own click handling). clamp=true (drag motion): rows
// outside clamp to the nearest content row so an overshooting drag selects to
// the start/end.
func (m *model) selPoint(x, y int, clamp bool) (selPos, bool) {
	if len(m.blocks) == 0 || m.viewH == 0 { // viewH 0: nothing rendered yet
		return selPos{}, false
	}
	row := y - m.viewTop - 3 - m.contentPad() + m.vp.YOffset + m.vpLead
	first, last := m.blocks[0].y0, m.blocks[len(m.blocks)-1].y1
	if !clamp && (row < first || row > last) {
		return selPos{}, false
	}
	row = max(min(row, last), first)
	w := ansi.StringWidth(m.contentLine(row))
	return selPos{row: row, col: max(min(x, w), 0)}, true
}

// inInputRow reports whether an absolute screen row is inside the input box.
func (m *model) inInputRow(y int) bool {
	return m.inputTop >= 0 && y >= m.inputTop && y < m.inputTop+len(m.inputLines)
}

// inputPoint converts absolute screen coords in the input box to a selection
// endpoint (region=input). clamp=true (drag motion) clamps the row into the
// box; clamp=false (a press) returns ok=false outside it.
func (m *model) inputPoint(x, y int, clamp bool) (selPos, bool) {
	if m.inputTop < 0 || len(m.inputLines) == 0 {
		return selPos{}, false
	}
	row := y - m.inputTop
	if !clamp && (row < 0 || row >= len(m.inputLines)) {
		return selPos{}, false
	}
	row = max(min(row, len(m.inputLines)-1), 0)
	w := ansi.StringWidth(m.inputLines[row])
	return selPos{row: row, col: max(min(x, w), 0), input: true}, true
}

// contentLine returns the unstyled text on content row r, read from the
// block's CACHED RENDER (b.rendered — the exact bytes on screen, marker and
// indent included) with ANSI stripped. Reading the render, not the raw
// b.text, keeps columns aligned: assistant blocks render "● "+indent ahead of
// their text, and that prefix is on screen, so a screen column maps into the
// rendered line directly. Blank separator rows between blocks return "".
func (m *model) contentLine(r int) string {
	for i := range m.blocks {
		b := &m.blocks[i]
		if r < b.y0 || r > b.y1 {
			continue
		}
		rows := strings.Split(ansi.Strip(b.rendered), "\n")
		if r-b.y0 < len(rows) {
			return strings.TrimRight(rows[r-b.y0], " \t")
		}
		return ""
	}
	return ""
}

// cellSlice returns the cells [off, off+n) of s (already ANSI-stripped).
func cellSlice(s string, off, n int) string {
	var b strings.Builder
	col := 0
	for _, r := range s {
		w := runewidth.RuneWidth(r)
		if col+w > off && col < off+n {
			b.WriteRune(r)
		}
		col += w
		if col >= off+n {
			break
		}
	}
	return b.String()
}

// selText extracts the selected text, one line per covered row — blank rows
// included, so paragraph breaks and block separators paste as the blank lines
// they are on screen (a terminal's native copy does the same). Trailing
// whitespace is trimmed per line, trailing blank lines from the whole copy.
func (m *model) selText(s selection) string {
	lo, hi := selOrder(s)
	var lines []string
	for r := lo.row; r <= hi.row; r++ {
		var ln string
		if lo.input {
			if r < len(m.inputLines) {
				ln = m.inputLines[r]
			}
		} else {
			ln = m.contentLine(r)
		}
		start, end := selCols(lo, hi, r, ansi.StringWidth(ln))
		lines = append(lines, strings.TrimRight(cellSlice(ln, start, end-start), " \t"))
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

// selOrder returns the selection endpoints top-to-bottom.
func selOrder(s selection) (lo, hi selPos) {
	lo, hi = s.anchor, s.cur
	if lo.row > hi.row || (lo.row == hi.row && lo.col > hi.col) {
		lo, hi = hi, lo
	}
	return lo, hi
}

// selCols is the [start, end) cell range selected on row r.
func selCols(lo, hi selPos, r, lineWidth int) (int, int) {
	start, end := 0, lineWidth
	if r == lo.row {
		start = lo.col
	}
	if r == hi.row {
		end = hi.col
	}
	return start, max(end, start)
}

// highlightInput repaints the selected range in reverse video on the input
// box's rendered view. Called from viewBody when the selection lives in the
// input region. Row r maps directly to input view line r (the input box is
// not scrolled/trimmed the way the transcript viewport is).
func (m *model) highlightInput(iv string) string {
	if m.sel == nil || !m.sel.anchor.input {
		return iv
	}
	lines := strings.Split(iv, "\n")
	lo, hi := selOrder(*m.sel)
	for r := lo.row; r <= hi.row && r < len(lines); r++ {
		start, end := selCols(lo, hi, r, ansi.StringWidth(lines[r]))
		lines[r] = reverseRange(lines[r], start, end)
	}
	return strings.Join(lines, "\n")
}

// highlightSelection repaints the selected range in reverse video on the FULL
// (untrimmed) viewport view, before viewportView trims pad rows. Content row r
// renders at view row r + contentPad - YOffset. Painting pre-trim means the
// reversed rows can't change how many blank rows the trim drops, so the
// transcript never shifts when a drag starts or ends.
func (m *model) highlightSelection(view string) string {
	if m.sel == nil {
		return view
	}
	lines := strings.Split(view, "\n")
	lo, hi := selOrder(*m.sel)
	base := m.contentPad() - m.vp.YOffset // view row = content row + base
	for r := lo.row; r <= hi.row; r++ {
		si := r + base
		if si < 0 || si >= len(lines) {
			continue
		}
		start, end := selCols(lo, hi, r, ansi.StringWidth(lines[si]))
		lines[si] = reverseRange(lines[si], start, end)
	}
	return strings.Join(lines, "\n")
}

// reverseRange applies SGR reverse video to the cells [start, end) of a
// possibly ANSI-styled line (escape sequences pass through untouched, and the
// count is in display cells, not bytes or runes).
func reverseRange(line string, start, end int) string {
	if start >= end {
		return line
	}
	var b strings.Builder
	col := 0
	on := false
	for i := 0; i < len(line); {
		if line[i] == 0x1b {
			j := i + 1 // pass the whole escape sequence through
			if j < len(line) && line[j] == '[' {
				for j++; j < len(line) && (line[j] < 0x40 || line[j] > 0x7e); j++ {
				}
				j++ // the final byte
			} else if j < len(line) {
				j++
			}
			b.WriteString(line[i:j])
			if on {
				// styled lines carry SGR resets mid-text (glamour styles text
				// in chunks, each ending with \x1b[0m) and a reset cancels
				// reverse video too — re-assert it or the highlight visibly
				// dies at the first reset inside the range.
				b.WriteString("\x1b[7m")
			}
			i = j
			continue
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		if !on && col >= start && col < end {
			b.WriteString("\x1b[7m")
			on = true
		} else if on && col >= end {
			b.WriteString("\x1b[27m")
			on = false
		}
		b.WriteRune(r)
		col += runewidth.RuneWidth(r)
		i += size
	}
	if on {
		b.WriteString("\x1b[27m")
	}
	return b.String()
}

// copyText puts s on the system clipboard: OSC 52 first (terminal-owned,
// works over SSH; tmux needs set-clipboard on), then a platform tool for
// terminals that swallow OSC 52 (Terminal.app). Both are attempted — a
// terminal that honors OSC 52 just wins; the tool covers the rest.
func copyText(s string) {
	if s == "" {
		return
	}
	seq := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(s)) + "\a"
	if os.Getenv("TMUX") != "" {
		// DCS passthrough so the outer terminal sees it (allow-passthrough).
		seq = "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
	}
	fmt.Fprint(os.Stdout, seq)
	for _, c := range [][]string{
		{"pbcopy"}, {"wl-copy"}, {"xclip", "-selection", "clipboard"},
	} {
		path, err := exec.LookPath(c[0])
		if err != nil {
			continue
		}
		cmd := exec.CommandContext(context.Background(), path, c[1:]...)
		cmd.Stdin = strings.NewReader(s)
		if cmd.Run() == nil {
			return
		}
	}
}

// selScrollTick drives edge auto-scroll: while a drag is parked above/below
// the transcript, the viewport keeps scrolling one line per tick so the
// selection can keep growing past what's on screen.
type selScrollTick struct{}

// handleMouseSelect runs the selection state machine for one mouse event. It
// returns handled=true when the event is consumed by selection: a left press
// inside the transcript block range (the viewport must NOT see it — a click
// scrolls the viewport, shifting contentPad mid-drag), motion while dragging,
// and release. A release with no drag replays the press as a click so
// tool-block expand still works. cmd is the edge-scroll tick when a drag sits
// past the viewport's top/bottom.
func (m *model) handleMouseSelect(msg tea.MouseMsg) (handled bool, cmd tea.Cmd) {
	switch msg.Action {
	case tea.MouseActionPress:
		m.sel = nil // any new press drops the old highlight
		if msg.Button != tea.MouseButtonLeft {
			return false, nil
		}
		// Input box: a press there starts an input-region selection. The textarea
		// doesn't use mouse for editing, so consuming it costs nothing.
		if p, ok := m.inputPoint(msg.X, msg.Y, false); ok {
			m.sel = &selection{anchor: p, cur: p}
			return true, nil
		}
		p, ok := m.selPoint(msg.X, msg.Y, false)
		if !ok {
			return false, nil
		}
		m.sel = &selection{anchor: p, cur: p}
		return true, nil // consumed: the viewport must not scroll on this press
	case tea.MouseActionMotion:
		if m.sel == nil || m.sel.done {
			return false, nil
		}
		// The drag stays in the anchor's region: input-anchored drags clamp into
		// the input box; transcript-anchored drags clamp into the blocks.
		if m.sel.anchor.input {
			if p, ok := m.inputPoint(msg.X, msg.Y, true); ok {
				m.sel.cur = p
			}
			return true, nil // the input box doesn't scroll; no edge tick
		}
		if p, ok := m.selPoint(msg.X, msg.Y, true); ok {
			m.sel.cur = p
		}
		m.selDragX, m.selDragY = msg.X, msg.Y
		return true, m.selEdgeScroll()
	case tea.MouseActionRelease:
		if m.sel == nil || m.sel.done {
			return false, nil
		}
		if m.sel.anchor != m.sel.cur { // a real drag: copy, keep the highlight
			m.sel.done = true
			copyText(m.selText(*m.sel))
			return true, nil
		}
		inputClick := m.sel.anchor.input
		m.sel = nil
		if inputClick {
			return true, nil // a no-drag click in the input box is just focus
		}
		m.clickAt(msg.X, msg.Y) // no drag: the press was a click all along
		return true, nil
	}
	return false, nil
}

// selEdgeScroll scrolls the viewport one line when the in-flight drag sits
// above/below the transcript's screen band, extends the selection to the row
// now under the pointer, and returns a tick so the scroll repeats while the
// pointer stays parked at the edge (motion events only arrive when the mouse
// MOVES). Returns nil when the drag is inside the band or the viewport can't
// scroll further — the next real motion event re-arms it.
func (m *model) selEdgeScroll() tea.Cmd {
	if m.sel == nil || m.sel.done {
		return nil
	}
	top := m.viewTop + 3 // header + tips + blank
	bottom := top + m.vp.Height - 1
	switch {
	case m.selDragY < top && m.vp.YOffset > 0:
		m.vp.SetYOffset(m.vp.YOffset - 1)
	case m.selDragY > bottom && !m.vp.AtBottom():
		m.vp.SetYOffset(m.vp.YOffset + 1)
	default:
		return nil
	}
	m.follow = m.vp.AtBottom()
	if p, ok := m.selPoint(m.selDragX, m.selDragY, true); ok {
		m.sel.cur = p
	}
	return tea.Tick(60*time.Millisecond, func(time.Time) tea.Msg { return selScrollTick{} })
}

// clickAt replays the click actions a press inside the transcript would have
// triggered (tool-block expand/collapse). Dock rows and the ⚡ header control
// are handled before handleMouseSelect sees the event, so only the transcript
// area reaches here. Row math matches selPoint (y is an absolute screen row).
func (m *model) clickAt(x, y int) {
	y -= m.viewTop
	if y <= 1 || m.palette != nil || m.viewH == 0 {
		return
	}
	row := y - 3 - m.contentPad() + m.vp.YOffset + m.vpLead
	for i := range m.blocks {
		if row >= m.blocks[i].y0 && row <= m.blocks[i].y1 && m.blocks[i].toggle() {
			m.refreshVP()
			return
		}
	}
}
