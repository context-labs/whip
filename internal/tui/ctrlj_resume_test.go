package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/context-labs/whip/internal/llm"
)

// Same ctrl+j regression but with a populated transcript (the resume case):
// the viewport has content, so layout computes a non-trivial height and the
// input sits below it.
func TestCtrlJFirstLineVisibleWithTranscript(t *testing.T) {
	m := compactCmdModel()
	messages := []llm.Message{
		{Role: "user", Content: "earlier question", Authored: true},
		{Role: "assistant", Content: "earlier answer with several lines\nof content\nright here"},
	}
	m.clientView.messages = append([]llm.Message{{Role: "system", Content: "system"}}, messages...)
	m.seedTranscript(messages, 1)
	m.height = 24
	m.layout()
	t.Logf("layout: vp.Height=%d inputHeight=%d", m.vp.Height(), m.input.Height())
	_ = lipgloss.Height // keep import

	p := tea.NewProgram(m, tea.WithOutput(nopWriter{}), tea.WithInput(strings.NewReader("")), tea.WithoutSignalHandler(), tea.WithWindowSize(80, 24))
	done := make(chan struct{})
	go func() { p.Run(); close(done) }()
	defer func() { p.Kill(); <-done }()
	time.Sleep(100 * time.Millisecond)

	for _, r := range "hello first line" {
		p.Send(keyRunes(string(r)))
		time.Sleep(30 * time.Millisecond)
	}
	p.Send(ctrlKey('j'))

	ch := make(chan string, 1)
	p.Send(viewProbe{fn: func(m *model) { ch <- m.input.View() }})
	v := <-ch
	if !strings.Contains(v, "hello first line") {
		t.Fatalf("REPRODUCED with transcript: %q", strings.Split(v, "\n"))
	}
	t.Logf("view ok: %q", strings.Split(v, "\n"))
}
