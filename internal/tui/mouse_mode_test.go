package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// enableClickWheelMouse must emit button-motion tracking (?1002) with SGR
// coords (?1006) so a held left-drag reports motion events — whip turns those
// into its own selection (select.go), because terminals suppress native
// drag-to-copy once any mouse mode is on. ?1002 is a superset of ?1000
// (press/release/wheel still report). ?1002, not ?1003: motion bytes only
// flow while a button is held.
func TestClickWheelMouseEscapes(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	enableClickWheelMouse(w)
	w.Close()
	buf := make([]byte, 256)
	_ = r.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _ := r.Read(buf)
	got := string(buf[:n])

	if !strings.Contains(got, "\x1b[?1006h") {
		t.Errorf("must enable SGR coords ?1006h, got %q", got)
	}
	if !strings.Contains(got, "\x1b[?1002h") {
		t.Errorf("must enable button-motion ?1002h so a drag reports motion (in-app selection), got %q", got)
	}
	// THE mac drag-to-copy regression: terminals keep a single mouse-tracking
	// mode, so writing ?1000h anywhere after ?1002h downgrades tracking to
	// click-only — drags stop reporting motion, and selection never starts.
	if strings.Contains(got, "\x1b[?1000h") {
		t.Errorf("must NOT enable ?1000h (it downgrades ?1002 button-motion tracking), got %q", got)
	}
	if strings.Contains(got, "1003") {
		t.Errorf("must NOT enable any-motion ?1003 (passive moves stay silent), got %q", got)
	}
}

// disableClickWheelMouse must release exactly the modes enableClickWheelMouse set.
func TestDisableClickWheelMouse(t *testing.T) {
	r, w, _ := os.Pipe()
	disableClickWheelMouse(w)
	w.Close()
	buf := make([]byte, 256)
	_ = r.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _ := r.Read(buf)
	got := string(buf[:n])
	if !strings.Contains(got, "\x1b[?1000l") || !strings.Contains(got, "\x1b[?1006l") || !strings.Contains(got, "\x1b[?1002l") {
		t.Errorf("must release ?1000, ?1002 and ?1006, got %q", got)
	}
}

// The startup mouse enable must run AFTER bubbletea enters the alt screen:
// entering ?1049 clears the terminal's mouse-tracking modes, so writing the
// enable before p.Run() (the old code) silently dropped it — mouse only
// worked after re-toggling /mouse. Init() schedules the post-altscreen
// enable (mouseInitMsg) only when opencode mode starts with the mouse on.
func TestMouseInitMsgScheduledOnlyForOpencode(t *testing.T) {
	m := &model{input: newInput(), mouseOn: true, uiMode: opencodeMode}
	if !initSendsMouseMsg(m) {
		t.Error("opencode + mouseOn: Init must schedule the post-altscreen mouse enable")
	}

	m = &model{input: newInput(), mouseOn: true} // inline mode: Run() enables directly
	if initSendsMouseMsg(m) {
		t.Error("inline mode: Init must NOT schedule the mouse enable (Run writes it pre-start)")
	}

	m = &model{input: newInput(), mouseOn: false, uiMode: opencodeMode}
	if initSendsMouseMsg(m) {
		t.Error("mouse off: Init must not schedule the enable")
	}
}

// initSendsMouseMsg runs m.Init()'s commands and reports whether any
// produces a mouseInitMsg. Commands are run concurrently: tea.Batch kicks
// every command off in its own goroutine (Init also carries a 10s theme-poll
// tick under TMUX — executing it inline would block the test).
func initSendsMouseMsg(m *model) bool {
	cmd := m.Init()
	if cmd == nil {
		return false
	}
	res := make(chan tea.Msg, 16)
	collect := func(c tea.Cmd) {
		res <- c()
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		_, ok = msg.(mouseInitMsg)
		return ok
	}
	for _, c := range batch {
		if c == nil {
			continue
		}
		go collect(c)
	}
	for range batch {
		select {
		case msg := <-res:
			if _, ok := msg.(mouseInitMsg); ok {
				return true
			}
		case <-time.After(2 * time.Second):
			return false // a tick never fired inside the window: not scheduled
		}
	}
	return false
}

// The mouseInitMsg handler writes the enable escapes to the TTY — with SGR
// coords + button-motion, then all-motion for opencode's hover. Verified
// here against a pipe by substituting stdout.
func TestMouseInitMsgEnablesMouse(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	m := &model{input: newInput(), mouseOn: true, uiMode: opencodeMode}
	tm, _ := m.Update(mouseInitMsg{})
	_ = tm
	w.Close()

	buf := make([]byte, 256)
	_ = r.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _ := r.Read(buf)
	got := string(buf[:n])
	if !strings.Contains(got, "\x1b[?1002h") || !strings.Contains(got, "\x1b[?1006h") {
		t.Errorf("mouseInitMsg must enable click+wheel+drag mouse, got %q", got)
	}
	if !strings.Contains(got, "\x1b[?1003h") {
		t.Errorf("opencode mode must also enable all-motion ?1003h for hover, got %q", got)
	}
}

// Regression pin: Run()'s pre-start mouse write is inline-mode only. The
// opencode branch must live behind the Init/msg path above — re-adding a
// pre-start enable in Run() would resurrect the alt-screen clobber this test
// guards (escapes written before p.Run() never survive ?1049h).
func TestRunSourceKeepsMouseEnableInlineOnly(t *testing.T) {
	src, err := os.ReadFile("tui.go")
	if err != nil {
		t.Fatal(err)
	}
	// the pre-start block: bottom-anchor write followed by the mouse enable
	// gated to inline mode
	if !strings.Contains(string(src), "if m.mouseOn && cfg.UIMode != opencodeMode {") {
		t.Error("Run()'s pre-start mouse enable must be gated to inline mode (opencode enables from Init, post-altscreen)")
	}
}
