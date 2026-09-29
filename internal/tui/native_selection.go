package tui

import (
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/protocol"
)

type nativeSelectionPane uint8

const (
	nativeSelectTranscript nativeSelectionPane = iota
	nativeSelectInput
	nativeSelectREPL
)

type nativeSelectionArea struct {
	x, y, width, height int
	rows                []string
	offset              int
}

type nativeSelection struct {
	historyRevision protocol.Counter
	selection
	pane         nativeSelectionPane
	owner        protocol.ID
	generation   uint64
	area         nativeSelectionArea
	dragX, dragY int
	scroll       bool
}

type nativeSelectionTick struct{ selection *nativeSelection }

type nativeSelectionClick struct {
	clickMark
	owner      protocol.ID
	generation uint64
	pane       nativeSelectionPane
}

type nativeMessageRows struct {
	id         protocol.ID
	start, end int
	tool       bool
}

func (m *nativeModel) selectionArea(pane nativeSelectionPane) nativeSelectionArea {
	x := 0
	if m.sidebarVisible() {
		x = 44
	}
	switch pane {
	case nativeSelectInput:
		if m.input.Value() == "" {
			return nativeSelectionArea{}
		}
		return nativeSelectionArea{x: x, y: m.vp.Height() + 1 + m.completionHeight(), width: m.transcriptWidth(), height: m.input.Height(), rows: strings.Split(m.input.View(), "\n")}
	case nativeSelectREPL:
		if !m.replVisible() {
			return nativeSelectionArea{}
		}
		space := currentTheme().Space
		return nativeSelectionArea{x: m.width - m.replWidth() + 1 + space.PadX, y: space.PadY + 2, width: m.replVP.Width(), height: min(m.replVP.Height(), m.height-space.PadY-2), rows: m.replDisplay, offset: m.replVP.YOffset()}
	default:
		return nativeSelectionArea{x: x, width: m.transcriptWidth(), height: m.vp.Height(), rows: m.rows, offset: m.vp.YOffset()}
	}
}

func (a nativeSelectionArea) point(x, y int, clamp bool) (selPos, bool) {
	if a.width <= 0 || a.height <= 0 || len(a.rows) == 0 {
		return selPos{}, false
	}
	if !clamp && (x < a.x || x >= a.x+a.width || y < a.y || y >= a.y+a.height) {
		return selPos{}, false
	}
	row := y - a.y + a.offset
	if !clamp && (row < 0 || row >= len(a.rows)) {
		return selPos{}, false
	}
	row = max(min(row, min(a.offset+a.height-1, len(a.rows)-1)), a.offset)
	width := min(ansi.StringWidth(a.rows[row]), a.width)
	return selPos{row: row, col: max(min(x-a.x, width), 0)}, true
}

func (m *nativeModel) selectionRevision() protocol.Counter {
	if m.browse != nil {
		return m.browse.transcript.snapshot.Revision
	}
	return m.history.snapshot.Revision
}

func (m *nativeModel) selectionValid() bool {
	s := m.selection
	if s == nil || s.owner != m.owner.ID || s.generation != m.generation || s.historyRevision != m.selectionRevision() {
		return false
	}
	a := m.selectionArea(s.pane)
	return s.area.x == a.x && s.area.y == a.y && s.area.width == a.width && s.area.height == a.height && slices.Equal(s.area.rows, a.rows)
}

func (m *nativeModel) validateSelection() {
	if m.selection != nil && !m.selectionValid() {
		m.selection = nil
	}
}

func (m *nativeModel) selectionMouse(message tea.MouseMsg) (tea.Cmd, bool) {
	mouse := message.Mouse()
	if m.menu != nil || m.picker != nil || m.decision != nil || m.palette != nil || m.completion != nil || nativePreferenceLabel(m.preferences.Mouse, true) == "off" || mouse.Mod&tea.ModShift != 0 {
		m.selection = nil
		return nil, true
	}
	m.validateSelection()
	switch message.(type) {
	case tea.MouseWheelMsg:
		m.selection = nil
		return nil, false
	case tea.MouseClickMsg:
		m.selection = nil
		if mouse.Button != tea.MouseLeft {
			return nil, false
		}
		for _, pane := range []nativeSelectionPane{nativeSelectTranscript, nativeSelectInput, nativeSelectREPL} {
			area := m.selectionArea(pane)
			point, ok := area.point(mouse.X, mouse.Y, false)
			if !ok {
				continue
			}
			s := &nativeSelection{historyRevision: m.selectionRevision(), anchor: point, cur: point, pane: pane, owner: m.owner.ID, generation: m.generation, area: area, dragX: mouse.X, dragY: mouse.Y}
			m.selection = s
			now := time.Now()
			last := m.selectionClick
			if last.owner == m.owner.ID && last.generation == m.generation && last.pane == pane && last.x == mouse.X && last.y == mouse.Y && last.n > 0 && last.n < 3 && now.Sub(last.at) <= multiClickWindow {
				last.n++
			} else {
				last.n = 1
			}
			last.x, last.y, last.at = mouse.X, mouse.Y, now
			last.owner, last.generation, last.pane = m.owner.ID, m.generation, pane
			m.selectionClick = last
			if last.n > 1 {
				line := ansi.Strip(area.rows[point.row])
				start, end := 0, min(ansi.StringWidth(line), area.width)
				if last.n == 2 {
					start, end = wordBounds(line, point.col)
				}
				s.anchor.col, s.cur.col, s.done = start, min(end, area.width), true
				return m.copySelection(), true
			}
			return nil, true
		}
	case tea.MouseMotionMsg:
		if s := m.selection; s != nil && !s.done {
			s.dragX, s.dragY = mouse.X, mouse.Y
			s.cur, _ = m.selectionArea(s.pane).point(mouse.X, mouse.Y, true)
			if !s.scroll && s.pane != nativeSelectInput && (mouse.Y < s.area.y || mouse.Y >= s.area.y+s.area.height) {
				s.scroll = true
				return nativeSelectionDelay(s), true
			}
			return nil, true
		}
	case tea.MouseReleaseMsg:
		if s := m.selection; s != nil && !s.done {
			s.cur, _ = m.selectionArea(s.pane).point(mouse.X, mouse.Y, true)
			s.done = true
			if s.anchor == s.cur {
				m.selection = nil
				if s.pane == nativeSelectTranscript {
					m.toggleToolAt(s.cur.row)
				}
				return nil, true
			}
			return m.copySelection(), true
		}
	}
	return nil, false
}

func nativeSelectionDelay(s *nativeSelection) tea.Cmd {
	return tea.Tick(60*time.Millisecond, func(time.Time) tea.Msg { return nativeSelectionTick{selection: s} })
}

func (m *nativeModel) selectionScroll(value nativeSelectionTick) tea.Cmd {
	s := m.selection
	if s == nil || s != value.selection || s.done || !m.selectionValid() {
		return nil
	}
	s.scroll = false
	view := &m.vp
	if s.pane == nativeSelectREPL {
		view = &m.replVP
	}
	before := view.YOffset()
	if s.dragY < s.area.y {
		view.ScrollUp(1)
	} else if s.dragY >= s.area.y+s.area.height {
		view.ScrollDown(1)
	}
	if view.YOffset() == before {
		return nil
	}
	m.follow = false
	s.cur, _ = m.selectionArea(s.pane).point(s.dragX, s.dragY, true)
	s.scroll = true
	return nativeSelectionDelay(s)
}

func (m *nativeModel) copySelection() tea.Cmd {
	if !m.selectionValid() {
		m.selection = nil
		return nil
	}
	s := m.selection
	lo, hi := selOrder(s.selection)
	var text strings.Builder
	for row := lo.row; row <= hi.row; row++ {
		line := ansi.Strip(ansi.Truncate(s.area.rows[row], s.area.width, ""))
		start, end := selCols(lo, hi, row, ansi.StringWidth(line))
		line = strings.TrimRight(cellSlice(line, start, end-start), " \t")
		if text.Len()+len(line)+1 > nativeCopyLimit {
			m.status = "Selection exceeds the 1 MiB clipboard limit; nothing was copied."
			return nil
		}
		if row > lo.row {
			text.WriteByte('\n')
		}
		text.WriteString(line)
	}
	value := strings.TrimRight(text.String(), "\n")
	if value == "" {
		return nil
	}
	return m.copyText(value)
}

func (m *nativeModel) selectionRow(pane nativeSelectionPane, row int, line string) string {
	s := m.selection
	if s == nil || s.pane != pane {
		return line
	}
	lo, hi := selOrder(s.selection)
	if row < lo.row || row > hi.row {
		return line
	}
	width := ansi.StringWidth(line)
	start, end := selCols(lo, hi, row, width)
	if start >= end {
		return line
	}
	return ansi.Cut(line, 0, start) + "\x1b[7m" + ansi.Strip(ansi.Cut(line, start, end)) + "\x1b[0m" + ansi.Cut(line, end, width)
}

func (m *nativeModel) selectedInputView() string {
	value := m.input.View()
	if m.selection == nil || m.selection.pane != nativeSelectInput || !m.selectionValid() {
		return value
	}
	rows := strings.Split(value, "\n")
	for i := range rows {
		rows[i] = m.selectionRow(nativeSelectInput, i, rows[i])
	}
	return strings.Join(rows, "\n")
}

func (m *nativeModel) toolExpanded(id protocol.ID) bool {
	if value, ok := m.toolExpansion[id]; ok {
		return value
	}
	return m.expandTools
}

func (m *nativeModel) toggleToolAt(row int) bool {
	for _, block := range m.messageRows {
		if block.tool && row >= block.start && row < block.end {
			if m.toolExpansion == nil {
				m.toolExpansion = make(map[protocol.ID]bool)
			}
			m.toolExpansion[block.id] = !m.toolExpanded(block.id)
			m.refresh()
			return true
		}
	}
	return false
}

func (m *nativeModel) toggleLatestTool() bool {
	for _, block := range slices.Backward(m.messageRows) {
		if block.tool {
			return m.toggleToolAt(block.start)
		}
	}
	return false
}
