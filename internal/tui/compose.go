package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// The frame is composed by drawing regions into one cell buffer at explicit
// rectangles and handing the buffer's rendering to Bubble Tea. Regions still
// render themselves as strings (they migrate to native draws one at a time);
// only WHERE their output lands changed, which is what deleted every
// string-splicing overlay helper.

// frameRects are the screen rectangles of the frame's regions. Empty
// rectangles mean the region is absent this frame. layout() computes them
// after every Update (they are the one source of truth for drawing AND hit
// testing), so the mouse math never inverts string layout again.
type frameRects struct {
	area, main, gap, side uv.Rectangle // gap/side are empty when the sidebar is hidden
	details               uv.Rectangle // open agent's details banner above the transcript
	transcript            uv.Rectangle // the viewport
	input, inputText      uv.Rectangle // the prompt box, and the textarea rows inside it
	status                uv.Rectangle // the status line
}

// measure is every main-column region except the transcript, in viewBody's
// row order. layout() gives the transcript what these leave over, and
// layoutFrame stacks the rectangles from the same numbers, so the budget and
// the geometry cannot drift apart.
type measure struct {
	details  int   // agent details banner rows (0 = none); followed by a blank row
	optional []int // each present transient region: a blank row, then its rows
	rewind   int   // rewind picker rows (0 = none); followed by a blank row
	input    int   // prompt rows: the box (textarea + 4) or the bare name-prompt row; 0 while a command owns the terminal
	inputTxt int   // textarea rows inside the prompt
	hints    int   // quit / esc hint rows
	dock     int   // agents dock rows (narrow terminals)
}

func (m *model) measure() measure {
	var mm measure
	if details := m.agentDetails(); details != "" {
		mm.details = lipgloss.Height(details)
	}
	for _, opt := range []struct {
		on   bool
		rows func() int
	}{
		{m.curThink != "", func() int { return lipgloss.Height(m.thinkView()) }},
		{m.busy && !m.thinkStart.IsZero(), func() int { return 1 }},
		{m.current != "", func() int { return lipgloss.Height(m.currentView()) }},
		{m.iactive != nil, func() int { return lipgloss.Height(m.interactiveView()) }},
		{m.permDialog != nil, func() int { return lipgloss.Height(m.permView()) }},
		{len(m.plan) > 0, func() int { return lipgloss.Height(m.planView()) }},
	} {
		if opt.on {
			mm.optional = append(mm.optional, opt.rows())
		}
	}
	if m.rew != nil {
		mm.rewind = lipgloss.Height(m.rewindView())
	}
	if m.iactive == nil {
		mm.inputTxt = m.input.Height()
		mm.input = mm.inputTxt + 4 // padding row, textarea, padding row, meta row, tail
		if m.namePrompt != nil {
			mm.input = mm.inputTxt // a bare "label ▏value" row, no box chrome
		}
	}
	if m.quit1 {
		mm.hints++
	}
	if m.escClr || (m.esc1 && m.rew == nil && m.namePrompt == nil) {
		mm.hints++
	}
	if dock := m.agentsDock(); dock != "" {
		mm.dock = lipgloss.Height(dock)
	}
	return mm
}

// fixed is the number of rows everything but the transcript needs.
func (mm measure) fixed() int {
	n := 0
	if mm.details > 0 {
		n += mm.details + 1
	}
	for _, rows := range mm.optional {
		n += 1 + rows
	}
	n++ // the blank row above the rewind picker / the input
	if mm.rewind > 0 {
		n += mm.rewind + 1
	}
	return n + mm.input + mm.hints + mm.dock + 2 // + a blank row and the status line
}

// layoutFrame lays out a w×h frame: a left margin, the main column at
// m.width with its regions stacked in viewBody's order around the transcript
// (whose height the viewport already holds), and (when visible) the gap and
// the sidebar/REPL panel on the right.
func (m *model) layoutFrame(w, h int) frameRects {
	r := frameRects{area: rect(0, 0, w, h), main: rect(opencodeLeftMargin, 0, m.width, h)}
	if m.sidebarVisible() {
		pw := m.panelWidth()
		r.gap = rect(w-pw-opencodeRightGap, 0, opencodeRightGap, h)
		r.side = rect(w-pw, 0, pw, h)
	}
	mm := m.measure()
	x, y := r.main.Min.X, 0
	if mm.details > 0 {
		r.details = rect(x, y, m.width, mm.details)
		y += mm.details + 1
	}
	r.transcript = rect(x, y, m.width, m.vp.Height())
	y += m.vp.Height()
	for _, rows := range mm.optional {
		y += 1 + rows
	}
	y++
	if mm.rewind > 0 {
		y += mm.rewind + 1
	}
	if mm.input > 0 {
		r.input = rect(x, y, m.width, mm.input)
		if m.namePrompt != nil {
			tx := x + lipgloss.Width(m.namePrompt.label) + 1
			if m.namePrompt.mask {
				tx += 2 // the "┃ " the mask view prepends
			}
			r.inputText = rect(tx, y, x+m.width-tx, mm.inputTxt)
		} else {
			r.inputText = rect(x+3, y+1, m.width-3, mm.inputTxt) // "┃  " gutter
		}
		y += mm.input
	}
	y += mm.hints + mm.dock
	r.status = rect(x, y+1, m.width, 1) // a blank row, then the status line
	return r
}

// frameSize is the terminal size, or the body's own size before the first
// WindowSizeMsg (headless tests).
func (m *model) frameSize() (int, int) {
	w := m.termWidth
	if w <= 0 {
		w = opencodeLeftMargin + m.width
	}
	return w, m.height
}

// frameNow lays out the current state at the current size. It is cheap (a
// few height measurements) and pure, so hit testing always agrees with what
// View draws, even when state moved without an Update in between.
func (m *model) frameNow() frameRects {
	w, h := m.frameSize()
	return m.layoutFrame(w, h)
}

// region names what is under a screen point.
type region uint8

const (
	regNone       region = iota
	regInput             // the textarea rows inside the prompt box
	regTranscript        // the viewport
	regSide              // the sidebar / REPL panel
)

// hit maps an absolute screen point to the topmost region under it and the
// point's coordinates local to that region's rectangle.
func (m *model) hit(x, y int) (reg region, lx, ly int) {
	r := m.frameNow()
	// the left margin belongs to the transcript: a drag that starts at the
	// screen edge selects from column 0 (lx clamps negative to 0)
	margin := r.transcript
	margin.Min.X = 0
	for _, c := range []struct {
		reg region
		rc  uv.Rectangle
	}{{regInput, r.inputText}, {regTranscript, margin}, {regSide, r.side}} {
		if inRect(c.rc, x, y) {
			return c.reg, x - r.transcript.Min.X*boolToInt(c.reg == regTranscript) - c.rc.Min.X*boolToInt(c.reg != regTranscript), y - c.rc.Min.Y
		}
	}
	return regNone, 0, 0
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func inRect(rc uv.Rectangle, x, y int) bool {
	return x >= rc.Min.X && x < rc.Max.X && y >= rc.Min.Y && y < rc.Max.Y
}

// rect is uv.Rect with non-negative dimensions (uv does not normalize, and a
// degenerate terminal size can make the main column narrower than zero).
func rect(x, y, w, h int) uv.Rectangle { return uv.Rect(x, y, max(w, 0), max(h, 0)) }

// screen returns the frame buffer, reallocated only when the size changes
// (a cell is 112 bytes; a 140×40 frame is ~600 KB, too much to allocate per
// frame) and cleared otherwise.
func (m *model) screen(w, h int) *uv.ScreenBuffer {
	if m.scr == nil || m.scr.Width() != w || m.scr.Height() != h {
		scr := uv.NewScreenBuffer(w, h)
		scr.Method = ansi.GraphemeWidth // every whip measurement is grapheme-based (lipgloss.Width)
		m.scr = &scr
		return m.scr
	}
	m.scr.Clear()
	return m.scr
}

// View draws the frame.
func (m *model) View() tea.View {
	m.syncInputPlaceholder()
	body := m.viewBody()
	w, h := m.termWidth, m.height
	if w <= 0 { // no size yet (tests): the frame is exactly the body
		w = opencodeLeftMargin + max(m.width, lipgloss.Width(body))
	}
	if h <= 0 {
		h = lipgloss.Height(body)
	}
	r := m.layoutFrame(w, h)
	scr := m.screen(w, h)
	// Draw order matters: a StyledString clears its rectangle before painting,
	// so later layers must be the ones on top.
	uv.NewStyledString(body).Draw(scr, r.main)
	m.paintSelection(scr, r)
	if m.sidebarVisible() {
		if m.replPanel { // the REPL sits on the native background like the chat: a hairline tells the columns apart
			rule := &uv.Cell{Content: "│", Width: 1, Style: uv.Style{Fg: currentTheme().Muted}}
			for y := 0; y < h; y++ {
				scr.SetCell(r.gap.Min.X+1, y, rule)
			}
		}
		uv.NewStyledString(m.sidebarView(h)).Draw(scr, r.side)
	}
	if ds := m.dialogs(); len(ds) > 0 { // floating dialogs over the dimmed session, bottom→top
		dimArea(scr, r.area)
		for _, d := range ds {
			rows := d.rows(m)
			if len(rows) == 0 {
				continue
			}
			dw := lipgloss.Width(rows[0])
			drawRows(scr, rows, max((max(w, dw)-dw)/2, 0), max((h-len(rows))/3, 0)) // centered, upper third
		}
	} else if m.menu != nil { // the completion popup floats above the input; the frame beneath never reflows
		menu := strings.Split(m.menuView(), "\n")
		drawRows(scr, menu, opencodeLeftMargin, r.input.Min.Y-len(menu)) // rows above the frame clip
	}
	if m.toast != "" {
		toast := m.toastRows()
		drawRows(scr, toast, max(w-lipgloss.Width(toast[0])-2, 0), 2) // top-right, over everything
	}
	m.recordInputRows()

	view := tea.NewView(scr.Render())
	view.Cursor = m.cursor(r)
	view.AltScreen = true
	if m.mouseOn {
		// Button-motion (?1002) reports drags without the hover flood of
		// all-motion. tmux forwards drags to whip only under all-motion
		// (mouse_any_flag), so it keeps ?1003 there.
		view.MouseMode = tea.MouseModeCellMotion
		if inTmuxEnv() {
			view.MouseMode = tea.MouseModeAllMotion
		}
	}
	return view
}

// drawRows paints pre-rendered rows at (x, y); rows outside the screen clip.
func drawRows(scr uv.Screen, rows []string, x, y int) {
	bounds := scr.Bounds()
	for i, row := range rows {
		area := rect(x, y+i, lipgloss.Width(row), 1).Intersect(bounds)
		if area.Empty() {
			continue
		}
		uv.NewStyledString(row).Draw(scr, area)
	}
}

// dimArea renders the backdrop faint so a dialog reads as the foreground.
func dimArea(scr uv.Screen, area uv.Rectangle) {
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			c := scr.CellAt(x, y)
			if c == nil || c.Width == 0 {
				continue
			}
			n := *c
			n.Style.Attrs |= uv.AttrFaint
			scr.SetCell(x, y, &n)
		}
	}
}

// cursor places the terminal cursor on the textarea's caret, or hides it
// while something else owns the keyboard (a dialog, the rewind picker, the
// permission prompt, an interactive command, a masked secret prompt).
func (m *model) cursor(r frameRects) *tea.Cursor {
	if m.iactive != nil || m.dialogOpen() || r.inputText.Empty() || (m.namePrompt != nil && m.namePrompt.mask) {
		return nil
	}
	c := m.input.Cursor() // relative to the textarea's own view; whip's textarea is frameless with no prompt
	if c == nil {
		return nil
	}
	c.X += r.inputText.Min.X
	c.Y += r.inputText.Min.Y
	if !inRect(r.inputText, c.X, c.Y) {
		return nil
	}
	return c
}

// paintSelection marks the dragged range in reverse video at the cell level,
// after the body is drawn: the selection lives in content coordinates (rows
// of the transcript or of the input text) and maps onto the screen through
// the region rectangles, clipped to them.
func (m *model) paintSelection(scr uv.Screen, r frameRects) {
	if m.sel == nil {
		return
	}
	lo, hi := selOrder(*m.sel)
	for row := lo.row; row <= hi.row; row++ {
		var line string
		var region uv.Rectangle
		var y int
		if lo.input {
			if row >= len(m.inputLines) {
				break
			}
			line, region, y = m.inputLines[row], r.inputText, r.inputText.Min.Y+row
		} else {
			line, region, y = m.contentLine(row), r.transcript, r.transcript.Min.Y+row+m.contentPad()-m.vp.YOffset()
		}
		start, end := selCols(lo, hi, row, ansi.StringWidth(line))
		if start >= end {
			continue
		}
		reverseCells(scr, rect(region.Min.X+start, y, end-start, 1).Intersect(region))
	}
}

// reverseCells sets reverse video on every cell of area.
func reverseCells(scr uv.Screen, area uv.Rectangle) {
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			c := scr.CellAt(x, y)
			if c == nil {
				continue
			}
			n := *c
			if n.Width == 0 {
				n = uv.Cell{Content: " ", Width: 1}
			}
			n.Style.Attrs |= uv.AttrReverse
			scr.SetCell(x, y, &n)
		}
	}
}
