package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// inputFilter runs on Bubble Tea's event loop before Update (tea.WithFilter)
// and thins mouse motion to one event per 16 ms: a drag reports motion on
// every cell the pointer crosses, and each report is a full Update + View.
// Everything else passes through untouched — returning nil DROPS a message,
// so only MouseMotionMsg is ever gated.
type inputFilter struct {
	now        func() time.Time
	lastMotion time.Time
}

func newInputFilter() *inputFilter { return &inputFilter{now: time.Now} }

func (f *inputFilter) Filter(_ tea.Model, msg tea.Msg) tea.Msg {
	if _, ok := msg.(tea.MouseMotionMsg); ok {
		t := f.now()
		if t.Sub(f.lastMotion) < 16*time.Millisecond {
			return nil
		}
		f.lastMotion = t
	}
	return msg
}
