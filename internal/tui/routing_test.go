package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/session"
)

// transcriptModel is a small rendered session with a user and an assistant
// block, laid out and viewed so the mouse math has a frame to work against.
func transcriptModel(t *testing.T) *model {
	t.Helper()
	m := compactCmdModel()
	m.Update(mkWinSize(100, 30))
	m.append(" ❯ hi")
	m.appendAssistant("answer")
	m.layout()
	viewStr(m)
	return m
}

// The Message Actions dialog is keyboard-driven once open: esc closes it,
// arrows move the selection, enter runs the selected action and closes it.
// (Its key handler existed but was never wired into the key chain.)
func TestMsgActionsKeyboard(t *testing.T) {
	m := transcriptModel(t)
	m.clickAt(5, m.blocks[1].y0+m.contentPad())
	if m.msgActions == nil {
		t.Fatal("clicking the assistant block should open Message Actions")
	}
	m.key(keyMsg(tea.KeyEscape))
	if m.msgActions != nil {
		t.Fatal("esc should close Message Actions")
	}
	m.clickAt(5, m.blocks[1].y0+m.contentPad())
	m.key(keyMsg(tea.KeyDown))
	if m.msgActions == nil || m.msgActions.sel != 1 {
		t.Fatalf("down should move the selection: %+v", m.msgActions)
	}
	m.key(keyMsg(tea.KeyEnter))
	if m.msgActions != nil {
		t.Fatal("enter should run the action and close the dialog")
	}
}

// Presses and the wheel never reach the transcript under a dialog, an
// input-slot mode, or the completion menu.
func TestClickUnderDialogIsSwallowed(t *testing.T) {
	m := transcriptModel(t)
	row := m.blocks[1].y0 + m.contentPad()
	for name, open := range map[string]func(){
		"model picker": func() { m.mpicker = &modelPicker{} },
		"session picker": func() {
			m.picker = &picker{metas: []session.Meta{{ID: "a"}}, previews: map[string][2]string{"a": {}}}
		},
		"rewind":          func() { m.rew = &rewindState{entries: []rewindEntry{{cut: 0}}} },
		"completion menu": func() { m.menu = &menu{} },
		"palette":         func() { m.openThinThemePalette() },
	} {
		m.mpicker, m.picker, m.rew, m.menu, m.palette = nil, nil, nil, nil, nil
		open()
		m.clickAt(5, row)
		if m.msgActions != nil {
			t.Fatalf("%s: a click underneath opened Message Actions", name)
		}
		if handled, _ := m.handleMouseSelect(clickMsg(5, row)); !handled || m.sel != nil {
			t.Fatalf("%s: press should be swallowed (handled=%v sel=%v)", name, handled, m.sel)
		}
	}
	m.menu = nil
	before := m.vp.YOffset()
	if handled, _ := m.handleMouseSelect(wheelMsg(5, row, true)); !handled || m.vp.YOffset() != before {
		t.Fatal("the wheel must not scroll the transcript under a dialog")
	}
}

// Arrow keys in the session picker follow the rendered order: idx 0 is the
// top row, so down moves down.
func TestSessionPickerArrowsFollowRenderOrder(t *testing.T) {
	m := compactCmdModel()
	m.picker = &picker{
		metas:    []session.Meta{{ID: "a"}, {ID: "b"}, {ID: "c"}},
		previews: map[string][2]string{"a": {}, "b": {}, "c": {}},
	}
	for _, step := range []struct {
		key  rune
		want int
	}{{tea.KeyDown, 1}, {tea.KeyDown, 2}, {tea.KeyDown, 2}, {tea.KeyUp, 1}, {tea.KeyUp, 0}, {tea.KeyUp, 0}} {
		m.key(keyMsg(step.key))
		if m.picker.idx != step.want {
			t.Fatalf("after %q idx = %d, want %d", keyMsg(step.key).String(), m.picker.idx, step.want)
		}
	}
}

// ctrl+c closes the rewind picker like esc instead of being swallowed.
func TestRewindCtrlC(t *testing.T) {
	m := compactCmdModel()
	m.rew = &rewindState{entries: []rewindEntry{{cut: 0, text: "x"}}}
	m.key(ctrlKey('c'))
	if m.rew != nil {
		t.Fatal("ctrl+c should close the rewind picker")
	}
}

// A block's render cache follows the theme generation: a runtime theme
// change repaints every block instead of waiting for a resize.
func TestBlockCacheFollowsTheme(t *testing.T) {
	b := block{kind: blockUser, text: "hi"}
	dark := b.renderAt(40)
	SetLightTheme(true)
	defer SetLightTheme(false)
	if light := b.renderAt(40); light == dark {
		t.Fatal("block render cache served the previous theme's colors")
	}
}

// The runtime sidebar toggle and the resize path share one width formula.
func TestRecalcWidthMatchesResize(t *testing.T) {
	m := compactCmdModel()
	m.Update(mkWinSize(140, 40))
	withSidebar := m.width
	m.sidebarHide = true
	m.recalcWidth()
	if m.width != 140-opencodeLeftMargin {
		t.Fatalf("hidden sidebar width = %d", m.width)
	}
	m.sidebarHide = false
	m.recalcWidth()
	if m.width != withSidebar {
		t.Fatalf("restored width = %d, want %d", m.width, withSidebar)
	}
}
