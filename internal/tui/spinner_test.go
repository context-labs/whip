package tui

import (
	"testing"

	"github.com/context-labs/whip/internal/session"
)

// The busy spinner animates: Update arms a tick loop when a turn is running,
// each tick advances the frame and schedules the next, the loop lapses when
// the turn ends and re-arms when the next one starts, and a theme change
// mid-turn keeps it running.
func TestBusySpinnerTicks(t *testing.T) {
	m := compactCmdModel()
	m.applyOpencodeStyles() // installs the spinner frames, as Run does
	m.Update(mkWinSize(100, 30))
	if _, cmd := m.Update(mkWinSize(100, 30)); cmd != nil || m.spinning {
		t.Fatal("idle: no spinner loop")
	}
	m.busy = true
	if _, cmd := m.Update(mkWinSize(100, 30)); cmd == nil || !m.spinning {
		t.Fatal("busy: Update must arm the spinner loop")
	}
	before := m.spin.View()
	if _, cmd := m.Update(m.spin.Tick()); cmd == nil || m.spin.View() == before {
		t.Fatalf("a tick must advance the frame and schedule the next (frame %q)", m.spin.View())
	}
	m.applyOpencodeStyles() // theme change mid-turn
	before = m.spin.View()
	if _, cmd := m.Update(m.spin.Tick()); cmd == nil || m.spin.View() == before {
		t.Fatal("a theme change must not orphan the running loop")
	}
	m.busy = false
	if _, cmd := m.Update(m.spin.Tick()); cmd != nil || m.spinning {
		t.Fatal("idle again: the loop must lapse")
	}
	m.busy = true
	if _, cmd := m.Update(mkWinSize(100, 30)); cmd == nil || !m.spinning {
		t.Fatal("the next turn must re-arm the loop")
	}
	// A running child with an idle root animates too (its row shows elapsed
	// time). That arm happens in Update, the one site whose command is always
	// returned; the handler that used to arm here dropped its own tick.
	m.busy = false
	if _, cmd := m.Update(m.spin.Tick()); cmd != nil || m.spinning {
		t.Fatal("idle root, no children: the loop must lapse")
	}
	m.clientView.agents = []session.RuntimeAgent{{ID: "root"}, {ID: "child", ParentID: "root", LifecyclePhase: "running"}}
	if _, cmd := m.Update(mkWinSize(100, 30)); cmd == nil || !m.spinning {
		t.Fatal("a running child must arm the loop through Update")
	}
	m.clientView.agents = nil
	if _, cmd := m.Update(m.spin.Tick()); cmd != nil || m.spinning {
		t.Fatal("no running agents: the loop must lapse")
	}
}
