package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/session"
)

// liveChipModel is a daemon-backed model with one registered pasted image, so
// the send-site tests can compare the recorded daemon payload with the echo.
func liveChipModel(t *testing.T) (*model, *fakeDaemonConnection, pastedImage) {
	t.Helper()
	connection := newFakeDaemonConnection(session.RootSnapshot{RootID: "root"})
	client, err := NewClient(ClientOptions{
		ClientID: "tui", RootID: "root",
		Connector: func(context.Context, map[string]int64) (daemonConnection, error) { return connection, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	client.Start()
	t.Cleanup(func() { _ = client.Close() })
	waitClientState(t, client, ClientLive)
	m := &model{client: client, clientState: ClientLive, input: newInput(), now: time.Now}
	img := pastedImage{n: 1, path: "/home/u/.whip/pastes/shot.png", display: "shot.png"}
	m.images, m.imageSeq = []pastedImage{img}, 1
	return m, connection, img
}

// lastPayload returns the payload of the most recent daemon command.
func lastPayload(t *testing.T, connection *fakeDaemonConnection) string {
	t.Helper()
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if len(connection.commands) == 0 {
		t.Fatal("no daemon command recorded")
	}
	return string(connection.commands[len(connection.commands)-1].Payload)
}

// A clipboard paste has no filename: the chip is the bare numbered form.
func TestClipboardPasteInsertsAnonymousChip(t *testing.T) {
	m := compactCmdModel()
	tm, _ := m.Update(imageMsg{path: "/home/u/.whip/pastes/a.png"})
	m = tm.(*model)
	if got, want := m.input.Value(), "[Image 1"+chipSentinel+"] "; got != want {
		t.Fatalf("input = %q, want %q", got, want)
	}
	tm, _ = m.Update(imageMsg{path: "/home/u/.whip/pastes/b.png"})
	m = tm.(*model)
	if !strings.HasSuffix(m.input.Value(), "[Image 2"+chipSentinel+"] ") || len(m.images) != 2 {
		t.Fatalf("second paste should register n=2, input=%q images=%d", m.input.Value(), len(m.images))
	}
}

// A pasted file whose basename contains ] must not break chip resolution:
// chipText turns brackets into parentheses so imageChipRe still matches.
func TestChipDisplayNameSanitizesBrackets(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	img := filepath.Join(t.TempDir(), "a]b.png")
	if err := os.WriteFile(img, []byte("\x89PNG\r\n\x1a\nfake"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := compactCmdModel()
	tm, cmd := m.Update(tea.PasteMsg{Content: img + "\n"})
	m = tm.(*model)
	tm, _ = m.Update(cmd())
	m = tm.(*model)
	chip := m.input.Value()
	if !strings.Contains(chip, "[Image 1: a)b.png"+chipSentinel+"]") {
		t.Errorf("chip should sanitize ] to ), got %q", chip)
	}
	if !strings.Contains(m.expandImageChips(chip), "@"+m.images[0].path) {
		t.Errorf("sanitized chip did not resolve: %q", m.expandImageChips(chip))
	}
}

// Submit sends the daemon the real @path while the transcript echo keeps the chip.
func TestSubmitExpandsImageChipsForDaemonOnly(t *testing.T) {
	m, connection, img := liveChipModel(t)
	m.input.SetValue("look at " + img.chipText() + " please")
	_, command := m.thinKey(keyMsg(tea.KeyEnter))
	if command == nil {
		t.Fatal("enter returned no command")
	}
	message := command().(clientCommandMsg)
	if message.action.Operation != "submit" {
		t.Fatalf("operation = %q, want submit", message.action.Operation)
	}
	payload := lastPayload(t, connection)
	if !strings.Contains(payload, "@"+img.path) || strings.Contains(payload, chipSentinel) {
		t.Errorf("daemon payload should carry the @path, not the chip: %s", payload)
	}
	if echo := m.transcriptText(); !strings.Contains(echo, img.chipText()) || strings.Contains(echo, "@"+img.path) {
		t.Errorf("transcript echo should keep the chip: %q", echo)
	}
}

// Text typed while a turn runs steers it; the chip expands there too.
func TestSteerExpandsImageChips(t *testing.T) {
	m, connection, img := liveChipModel(t)
	m.busy = true
	m.input.SetValue("also " + img.chipText())
	_, command := m.thinKey(keyMsg(tea.KeyEnter))
	if command == nil {
		t.Fatal("enter returned no command")
	}
	if op := command().(clientCommandMsg).action.Operation; op != "steer" {
		t.Fatalf("operation = %q, want steer", op)
	}
	if payload := lastPayload(t, connection); !strings.Contains(payload, "@"+img.path) {
		t.Errorf("steer payload should carry the @path: %s", payload)
	}
}

// The explicit /steer command expands chips like the typed steer path.
func TestSteerCommandExpandsImageChips(t *testing.T) {
	m, connection, img := liveChipModel(t)
	_, command := m.thinCommand("/steer see " + img.chipText())
	if command == nil {
		t.Fatal("/steer returned no command")
	}
	command()
	if payload := lastPayload(t, connection); !strings.Contains(payload, "@"+img.path) {
		t.Errorf("/steer payload should carry the @path: %s", payload)
	}
}

// Hand-typed "[Image 1]" lacks the sentinel and never attaches; a chip whose
// number is not registered (or lost its closing bracket) stays literal too,
// minus the invisible sentinel so the model gets clean text.
func TestUnrecognizedChipsStayLiteral(t *testing.T) {
	m := compactCmdModel()
	m.images = []pastedImage{{n: 1, path: "/p/a.png"}}
	for _, text := range []string{"[Image 1]", "[Image 2" + chipSentinel + "]", "[Image 0" + chipSentinel + "]", "[Image 1: shot.png" + chipSentinel} {
		if got, want := m.expandImageChips(text), strings.ReplaceAll(text, chipSentinel, ""); got != want {
			t.Errorf("expandImageChips(%q) = %q, want %q", text, got, want)
		}
	}
}

// A chip pasted right after a word gets a leading space: "look at@/path"
// would never be seen as a mention by the daemon's whitespace split.
func TestPasteAfterWordSeparatesChip(t *testing.T) {
	m := compactCmdModel()
	m.input.SetValue("look at")
	tm, _ := m.Update(imageMsg{path: "/home/u/.whip/pastes/a.png"})
	m = tm.(*model)
	if got, want := m.input.Value(), "look at [Image 1"+chipSentinel+"] "; got != want {
		t.Fatalf("input = %q, want %q", got, want)
	}
	m.input.SetValue("look at ")
	tm, _ = m.Update(imageMsg{path: "/home/u/.whip/pastes/b.png"})
	m = tm.(*model)
	if got, want := m.input.Value(), "look at [Image 2"+chipSentinel+"] "; got != want {
		t.Fatalf("after a space, input = %q, want %q", got, want)
	}
}

// /computer wraps the task in a submit; the chip expands there like any prose.
func TestComputerCommandExpandsImageChips(t *testing.T) {
	m, connection, img := liveChipModel(t)
	_, command := m.thinCommand("/computer click " + img.chipText())
	if command == nil {
		t.Fatal("/computer returned no command")
	}
	command()
	if payload := lastPayload(t, connection); !strings.Contains(payload, "@"+img.path) || strings.Contains(payload, chipSentinel) {
		t.Errorf("/computer payload should carry the @path: %s", payload)
	}
}

// /goal persists its text and re-sends it on every goal check, so the chip
// must expand to the stable @path before it is stored.
func TestGoalCommandExpandsImageChips(t *testing.T) {
	m, connection, img := liveChipModel(t)
	_, command := m.thinCommand("/goal match " + img.chipText())
	if command == nil {
		t.Fatal("/goal returned no command")
	}
	command()
	if payload := lastPayload(t, connection); !strings.Contains(payload, "@"+img.path) {
		t.Errorf("/goal payload should carry the @path: %s", payload)
	}
}

// history.clear succeeding empties the registry so a recalled chip stays literal.
func TestClearResetsImageRegistry(t *testing.T) {
	m := compactCmdModel()
	m.images, m.imageSeq = []pastedImage{{n: 1, path: "/p/a.png"}}, 1
	chip := m.images[0].chipText()
	tm, _ := m.Update(clientCommandMsg{action: Action{Operation: "history.clear"}, result: daemon.CommandResult{Status: "succeeded"}})
	m = tm.(*model)
	if len(m.images) != 0 || m.imageSeq != 0 {
		t.Fatalf("clear left registry: images=%d imageSeq=%d", len(m.images), m.imageSeq)
	}
	if got := m.expandImageChips(chip); got != "[Image 1]" {
		t.Errorf("recalled chip after clear should stay literal, got %q", got)
	}
}

// A failed clear keeps the registry: the images are still in the conversation.
func TestFailedClearKeepsImageRegistry(t *testing.T) {
	m := compactCmdModel()
	m.images, m.imageSeq = []pastedImage{{n: 1, path: "/p/a.png"}}, 1
	tm, _ := m.Update(clientCommandMsg{action: Action{Operation: "history.clear"}, result: daemon.CommandResult{Error: "busy"}})
	m = tm.(*model)
	if len(m.images) != 1 || m.imageSeq != 1 {
		t.Fatalf("failed clear touched registry: images=%d imageSeq=%d", len(m.images), m.imageSeq)
	}
}

// Switching to another root session drops the chips; a snapshot of the same
// root keeps them.
func TestRootSwitchResetsImageRegistry(t *testing.T) {
	m := compactCmdModel()
	m.applyClientSnapshot(session.RootSnapshot{RootID: "a"})
	m.images, m.imageSeq = []pastedImage{{n: 1, path: "/p/a.png"}}, 1
	m.applyClientSnapshot(session.RootSnapshot{RootID: "a", Cursor: 1})
	if len(m.images) != 1 {
		t.Fatal("a snapshot of the same root must keep the registry")
	}
	m.applyClientSnapshot(session.RootSnapshot{RootID: "b"})
	if len(m.images) != 0 || m.imageSeq != 0 {
		t.Fatalf("root switch left registry: images=%d imageSeq=%d", len(m.images), m.imageSeq)
	}
}

func TestTruncateImageName(t *testing.T) {
	cases := []struct{ name, want string }{
		{"shot.png", "shot.png"},
		{"Screenshot 2026-09-04 at 3.21.45 PM.png", "Screenshot 2026-09-….png"},
		{"日本語の長いスクリーンショット名.png", "日本語の長いスクリ….png"},               // wide runes count two columns
		{"ab.averylongextension1234", ".averylongextension1234"}, // no room for stem+…: the extension alone
		{".averyveryverylongextensionname", "…"},                 // the extension itself does not fit
	}
	for _, tc := range cases {
		got := truncateImageName(tc.name, maxImageNameRunes)
		if got != tc.want {
			t.Errorf("truncateImageName(%q) = %q, want %q", tc.name, got, tc.want)
		}
		if w := ansi.StringWidth(got); w > maxImageNameRunes {
			t.Errorf("truncateImageName(%q) is %d columns wide, limit %d", tc.name, w, maxImageNameRunes)
		}
	}
}
