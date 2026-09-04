package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func pressKey(m *model, kt rune) *model { return pressMsg(m, keyMsg(kt)) }

func pressMsg(m *model, msg tea.KeyPressMsg) *model {
	tm, _ := m.key(msg)
	return tm.(*model)
}

// The reported bug: typing "$go" filtered the skill menu, but tab only moved
// the selection — the rest of the name never got typed. Tab now completes.
func TestTabCompletesSkillName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".whip/skills/go-style"), 0o755)
	os.WriteFile(filepath.Join(home, ".whip/skills/go-style/SKILL.md"),
		[]byte("---\nname: go-style\ndescription: d\n---\n"), 0o644)
	m := modelCmdModel()
	m = typeStr(t, m, "$go-sty")
	if m.menu == nil || len(m.menu.cands) != 1 || m.menu.cands[0].Text != "$go-style" {
		t.Fatalf("menu should hold exactly $go-style: %+v", m.menu)
	}
	m = pressKey(m, tea.KeyTab)
	if v := m.input.Value(); v != "$go-style" {
		t.Fatalf("tab should complete the skill name, got %q", v)
	}
	// a second tab on a single match is a no-op, and enter submits the line
	m = pressKey(m, tea.KeyTab)
	if v := m.input.Value(); v != "$go-style" {
		t.Fatalf("second tab should not corrupt a single match, got %q", v)
	}
}

// Tab cycles through candidates with preview: each press inserts the next
// candidate, wrapping at both ends. ("/e" matches /effort only, so use /mode:
// /model, /model-for-session.)
func TestTabCyclesWithPreview(t *testing.T) {
	m := modelCmdModel()
	m = typeStr(t, m, "/mode") // /model, /model-for-session — two candidates, deterministic order
	m = pressKey(m, tea.KeyTab)
	first := m.input.Value()
	if first != "/model" && first != "/model-for-session" {
		t.Fatalf("tab should preview a candidate, got %q", first)
	}
	m = pressKey(m, tea.KeyTab)
	second := m.input.Value()
	if first == second {
		t.Fatalf("second tab should preview the next candidate, still %q", first)
	}
	m = pressKey(m, tea.KeyTab) // wraps around
	if m.input.Value() != first {
		t.Fatalf("wrap should return to %q, got %q", first, m.input.Value())
	}
	m = pressMsg(m, shiftTab()) // and back
	if m.input.Value() != second {
		t.Fatalf("shift+tab should return to %q, got %q", second, m.input.Value())
	}
}

// Esc while tab-cycling dismisses the menu and reverts to the completed
// prefix the cycle started from (readline-style; it does not un-complete).
func TestEscRevertsTabCycle(t *testing.T) {
	m := modelCmdModel()
	m = typeStr(t, m, "/mo")
	m = pressKey(m, tea.KeyTab) // previews /model
	m = pressKey(m, tea.KeyTab) // previews /mouse
	m = pressKey(m, tea.KeyEsc)
	if m.menu != nil {
		t.Fatal("esc should close the menu")
	}
	if m.input.Value() != "/model" {
		t.Fatalf("esc should revert to the cycle base, got %q", m.input.Value())
	}
}

// Enter on a tab-cycled execNow command runs it (same as arrows + enter);
// enter on a non-execNow candidate commits the preview with a trailing space.
func TestEnterCommitsTabCycle(t *testing.T) {
	m := modelCmdModel()
	m = typeStr(t, m, "/mo") // candidates in order: /model, /mouse
	m = pressKey(m, tea.KeyTab)
	if m.input.Value() != "/model" {
		t.Fatalf("tab should preview /model first, got %q", m.input.Value())
	}
	m = pressKey(m, tea.KeyEnter)
	if m.mpicker == nil {
		t.Fatal("enter on tab-cycled /model should open the model picker")
	}
	if m.input.Value() != "" {
		t.Fatalf("execNow clears the input, got %q", m.input.Value())
	}

	// non-execNow command: /effort with args commits as text
	t.Setenv("HOME", t.TempDir())
	m = modelCmdModel()
	m = typeStr(t, m, "/effort ")
	m = pressKey(m, tea.KeyTab) // previews the first effort level
	want := m.input.Value() + " "
	m = pressKey(m, tea.KeyEnter)
	if m.input.Value() != want {
		t.Fatalf("enter should commit the preview as %q, got %q", want, m.input.Value())
	}
}

// Arrows still move the menu selection without touching the input.
func TestArrowsNavigateMenuOnly(t *testing.T) {
	m := modelCmdModel()
	m = typeStr(t, m, "/m")
	idx := m.menu.idx
	m = pressKey(m, tea.KeyDown)
	if m.menu == nil || m.menu.idx == idx {
		t.Fatal("down should move the selection")
	}
	if m.input.Value() != "/m" {
		t.Fatalf("arrows must not edit the input, got %q", m.input.Value())
	}
}

// execNow commands still run immediately on enter, even mid-cycle.
func TestExecNowRunsOnEnterWhileCycling(t *testing.T) {
	m := modelCmdModel()
	m = typeStr(t, m, "/m")
	m = pressKey(m, tea.KeyTab) // cycles /model ↔ /mouse with preview
	for m.input.Value() != "/mouse" {
		m = pressKey(m, tea.KeyTab)
	}
	m = pressKey(m, tea.KeyEnter)
	if m.input.Value() != "" {
		t.Fatalf("/mouse should run and clear the input, got %q", m.input.Value())
	}
	if !m.mouseOn {
		t.Fatal("/mouse should have toggled mouse capture on")
	}
}
