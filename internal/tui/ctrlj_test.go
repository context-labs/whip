package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Regression: typing a first line then hitting ctrl+j must keep the first
// line visible. Under the live program the textarea's internal viewport kept
// the scroll offset it computed at height 1 (YOffset=1), and the deferred
// growInput rebuild inherited it — the first line scrolled out of view. The
// handler now resets the scroll (SetValue) after inserting the newline.
//
// The bug only reproduces under a REAL tea.Program (its cursor-blink command
// delivery is what leaves the stale scroll offset; synchronous Update replays
// never blanked the view), so this drives one. Model state is read inside
// the program's event loop via viewProbe — never directly from the test
// goroutine, which would race the program.
func TestCtrlJFirstLineStaysVisible(t *testing.T) {
	m := compactCmdModel()
	p := tea.NewProgram(m, tea.WithOutput(nopWriter{}), tea.WithInput(strings.NewReader("")), tea.WithoutSignalHandler())
	done := make(chan struct{})
	go func() { p.Run(); close(done) }()
	defer func() { p.Kill(); <-done }()
	time.Sleep(100 * time.Millisecond) // program started, first frame rendered

	for _, r := range "hello first line" {
		p.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		time.Sleep(30 * time.Millisecond)
	}
	p.Send(tea.KeyMsg{Type: tea.KeyCtrlJ})

	// read the input view inside the event loop (race-safe)
	ch := make(chan string, 1)
	p.Send(viewProbe{fn: func(m *model) { ch <- m.input.View() }})
	v := <-ch
	if !strings.Contains(v, "hello first line") {
		t.Fatalf("after ctrl+j the first line vanished from the input view: %q", strings.Split(v, "\n"))
	}
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }
