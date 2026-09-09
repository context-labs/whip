package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// The transcript must render inline (no alt-screen) so native drag-to-copy
// works everywhere: no alt-screen escape in the view, and the view still
// assembles header + transcript + input. Mouse capture is ON by default for
// wheel scroll + clicks (via click/wheel-only ?1000 reporting, no motion, so
// drag-selection stays native); the model field reflects that default.
func TestInlineRendering(t *testing.T) {
	m := compactCmdModel()
	m.Update(mkWinSize(80, 30))
	m.appendAssistant("hello **world**")
	v := m.View()
	if strings.Contains(v, "\x1b[?1049h") || strings.Contains(v, "\x1b[?47h") {
		t.Fatal("view must not enter the alternate screen")
	}
	for _, want := range []string{"whip ·", "hello", "world"} {
		if !strings.Contains(stripAll(v), want) {
			t.Errorf("inline view missing %q", want)
		}
	}
	// mouse capture on by default (wheel scroll); drag-copy stays native via
	// click/wheel-only reporting (no motion), asserted by TestMouseDefaultsOn
	if !m.mouseOn {
		t.Fatal("mouse capture must default on for wheel scroll")
	}
}

func TestInlineViewReturnsToBottomAfterTemporaryGrowth(t *testing.T) {
	m := compactCmdModel()
	m.Update(mkWinSize(80, 30))
	m.View()
	bottom := m.viewTop

	// Busy mode temporarily adds the spinner and its separator. Bubble Tea's
	// inline renderer scrolls a growing frame upward but cannot move its top back
	// down when the frame shrinks again.
	m.busy = true
	m.layout()
	grown := m.View()
	if m.viewTop >= bottom {
		t.Fatalf("test setup: growing view must move up: %d -> %d", bottom, m.viewTop)
	}

	m.busy = false
	m.layout()
	shrunk := m.View()
	if m.viewTop != bottom {
		t.Fatalf("shrunk view must return to bottom: got top %d, want %d", m.viewTop, bottom)
	}
	if got, want := lipgloss.Height(shrunk), lipgloss.Height(grown); got != want {
		t.Fatalf("shrunk render must retain the physical frame height: got %d, want %d", got, want)
	}
}

func TestInlineFrameHeightIsCappedByTerminal(t *testing.T) {
	m := compactCmdModel()
	m.Update(mkWinSize(80, 9))
	m.View()

	m.busy = true
	m.layout()
	m.View() // The busy frame is 10 rows tall, one more than the terminal.

	m.busy = false
	m.layout()
	shrunk := m.View()
	if got, want := lipgloss.Height(shrunk), m.height; got != want {
		t.Fatalf("shrunk render must not retain an oversized frame: got %d rows, want %d", got, want)
	}
	if got, want := m.viewTop+m.viewH, m.height; got != want {
		t.Fatalf("shrunk content must remain bottom-anchored: got bottom %d, want %d", got, want)
	}
}

func stripAll(s string) string {
	out := strings.Builder{}
	i := 0
	for i < len(s) {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' && s[i] != 'h' && s[i] != 'l' {
				i++
			}
			i++
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}
