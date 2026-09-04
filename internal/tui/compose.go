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
// rectangles mean the region is absent this frame.
type frameRects struct {
	area, main, gap, side uv.Rectangle // gap/side are empty when the sidebar is hidden
}

// rects lays out a w×h frame: a left margin, the main column at m.width, and
// (when visible) the gap and the sidebar/REPL panel on the right.
func (m *model) rects(w, h int) frameRects {
	r := frameRects{area: rect(0, 0, w, h), main: rect(opencodeLeftMargin, 0, m.width, h)}
	if m.sidebarVisible() {
		pw := m.panelWidth()
		r.gap = rect(w-pw-opencodeRightGap, 0, opencodeRightGap, h)
		r.side = rect(w-pw, 0, pw, h)
	}
	return r
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
	r := m.rects(w, h)
	scr := m.screen(w, h)
	// Draw order matters: a StyledString clears its rectangle before painting,
	// so later layers must be the ones on top.
	uv.NewStyledString(body).Draw(scr, r.main)
	if m.sidebarVisible() {
		if m.replPanel { // the REPL sits on the native background like the chat: a hairline tells the columns apart
			rule := &uv.Cell{Content: "│", Width: 1, Style: uv.Style{Fg: currentTheme().Muted}}
			for y := 0; y < h; y++ {
				scr.SetCell(r.gap.Min.X+1, y, rule)
			}
		}
		uv.NewStyledString(m.sidebarView(h)).Draw(scr, r.side)
	}
	var rows []string
	switch { // one floating dialog over the dimmed session
	case m.palette != nil:
		rows = m.ocDialogRows()
	case m.msgActions != nil:
		rows = m.ocMsgActionRows()
	case m.mpicker != nil:
		rows = m.ocModelDialogRows()
	case m.picker != nil:
		rows = m.ocSessionDialogRows()
	}
	switch {
	case len(rows) > 0:
		dimArea(scr, r.area)
		dw := lipgloss.Width(rows[0])
		drawRows(scr, rows, max((max(w, dw)-dw)/2, 0), max((h-len(rows))/3, 0)) // centered, upper third
	case m.menu != nil: // the completion popup floats above the input; the frame beneath never reflows
		menu := strings.Split(m.menuView(), "\n")
		drawRows(scr, menu, opencodeLeftMargin, m.inputBodyOff-len(menu)) // rows above the frame clip
	}
	if m.toast != "" {
		toast := m.toastRows()
		drawRows(scr, toast, max(w-lipgloss.Width(toast[0])-2, 0), 2) // top-right, over everything
	}
	if m.height > 0 {
		m.viewH = h
	}
	m.recordInputRows()

	view := tea.NewView(scr.Render())
	view.AltScreen = true
	if m.mouseOn {
		view.MouseMode = tea.MouseModeAllMotion // clicks, wheel, drag
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
