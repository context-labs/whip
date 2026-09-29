package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// transcriptView holds scroll geometry and reads visible rendered rows from
// its client. It owns no messages, session state, or transcript projection.
type transcriptView struct {
	width, height int
	yoff, total   int
	rows          func(y int) string // styled content row y (0 <= y < total)
}

func (v transcriptView) Width() int          { return v.width }
func (v transcriptView) Height() int         { return v.height }
func (v transcriptView) YOffset() int        { return v.yoff }
func (v transcriptView) TotalLineCount() int { return v.total }

func (v *transcriptView) SetWidth(w int)   { v.width = w }
func (v *transcriptView) SetHeight(h int)  { v.height = h; v.clamp() }
func (v *transcriptView) SetYOffset(y int) { v.yoff = y; v.clamp() }
func (v *transcriptView) setTotal(n int)   { v.total = n; v.clamp() }

func (v transcriptView) maxOffset() int { return max(v.total-v.height, 0) }
func (v *transcriptView) clamp()        { v.yoff = max(min(v.yoff, v.maxOffset()), 0) }

// AtBottom reports whether the newest rows are in view (follow mode).
func (v transcriptView) AtBottom() bool { return v.yoff >= v.maxOffset() }

func (v *transcriptView) GotoBottom()      { v.yoff = v.maxOffset() }
func (v *transcriptView) ScrollUp(n int)   { v.SetYOffset(v.yoff - n) }
func (v *transcriptView) ScrollDown(n int) { v.SetYOffset(v.yoff + n) }

// Update scrolls on the wheel (three rows a tick) and on page keys.
func (v transcriptView) Update(msg tea.Msg) (transcriptView, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			v.ScrollUp(3)
		case tea.MouseWheelDown:
			v.ScrollDown(3)
		}
	case tea.KeyPressMsg:
		switch msg.String() {
		case "pgup":
			v.ScrollUp(v.height)
		case "pgdown":
			v.ScrollDown(v.height)
		}
	}
	return v, nil
}

// View renders the visible window: exactly height rows, each clipped to
// width; rows past the content are blank.
func (v transcriptView) View() string {
	if v.height <= 0 {
		return ""
	}
	lines := make([]string, v.height)
	for i := range lines {
		if y := v.yoff + i; v.rows != nil && y >= 0 && y < v.total {
			line := v.rows(y)
			if v.width > 0 {
				line = ansi.Truncate(line, v.width, "")
			}
			lines[i] = line
		}
	}
	return strings.Join(lines, "\n")
}
