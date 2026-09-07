package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
)

// A macOS screenshot preview pastes its temporary file path into the
// terminal. Treat that path as an attachment instead of a slash command.
func TestPastedScreenshotPathAttachesImage(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())

	source := filepath.Join(t.TempDir(), "Screenshot") // preview paths need not have an extension
	image := []byte("\x89PNG\r\n\x1a\nimage-data")
	if err := os.WriteFile(source, image, 0o600); err != nil {
		t.Fatal(err)
	}

	m := compactCmdModel()
	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(source + "\n"), Paste: true})
	m = tm.(*model)
	if cmd == nil {
		t.Fatal("pasted screenshot path should schedule an attachment")
	}

	tm, _ = m.Update(cmd())
	m = tm.(*model)
	// The input shows a compact chip, not the raw path or file contents.
	if !strings.Contains(m.input.Value(), "[Image 1: Screenshot"+chipSentinel+"]") {
		t.Fatalf("input should show a chip, got %q", m.input.Value())
	}
	// The on-disk copy (auto-numbered as this session's image 1) holds the
	// pasted bytes verbatim.
	if len(m.images) != 1 || m.images[0].n != 1 {
		t.Fatalf("expected one session image, got %+v", m.images)
	}
	data, err := os.ReadFile(m.images[0].path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(image) {
		t.Fatalf("saved image = %q, want %q", data, image)
	}
}

func TestSaveClipboardImageStaysWithinPasteDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)

	if _, err := saveClipboardImage("../../escaped", []byte("image")); err == nil {
		t.Fatal("path-traversing extension should be rejected")
	}
	if _, err := os.Stat(filepath.Join(home, "escaped")); !os.IsNotExist(err) {
		t.Fatalf("write escaped the paste directory: %v", err)
	}
}

// Paste collapse is opt-in (config collapsePaste): off by default a paste
// lands verbatim; on, a ≥3-line paste becomes a placeholder whose real text
// swaps back in at submit.
func TestPasteCollapseOptIn(t *testing.T) {
	paste := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("line1\nline2\nline3"), Paste: true}

	// default (nil) — off: the textarea takes the raw paste
	m := compactCmdModel()
	m.Update(paste)
	if !strings.Contains(m.input.Value(), "line1") {
		t.Fatalf("paste should land verbatim by default, got %q", m.input.Value())
	}
	if m.pasteBuf != "" {
		t.Fatal("no buffer held when collapse is off")
	}

	// on — collapse to a placeholder, real text held
	on := true
	m2 := compactCmdModel()
	m2.cfg.CollapsePaste = &on
	m2.Update(paste)
	if !strings.Contains(m2.input.Value(), "[Pasted ~3 lines]") {
		t.Fatalf("collapsed input should show the placeholder, got %q", m2.input.Value())
	}
	if m2.pasteBuf == "" {
		t.Fatal("the real paste text should be held")
	}
	// submit swaps it back
	m2.input.SetValue(m2.input.Value()) // settle
	m2.permDialog = nil
	// drive the submit path's swap directly (the placeholder → real text)
	text := strings.TrimSpace(m2.input.Value())
	text = strings.Replace(text, "[Pasted ~3 lines]", strings.TrimSpace(m2.pasteBuf), 1)
	if !strings.Contains(text, "line1\nline2\nline3") {
		t.Fatalf("submit should restore the real text, got %q", text)
	}
}

// A short paste (1-2 lines) never collapses, even when the option is on.
func TestPasteCollapseShortPasteIgnored(t *testing.T) {
	on := true
	m := compactCmdModel()
	m.cfg.CollapsePaste = &on
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("just one line"), Paste: true})
	if strings.Contains(m.input.Value(), "[Pasted") {
		t.Fatal("a one-line paste should not collapse")
	}
}

// Chip rendering: the input and transcript show a compact [Image N] chip, with
// the source filename snippet when one exists and a bare number for anonymous
// (clipboard) pastes. Truncation keeps long screenshot names readable.
func TestImageChipRendering(t *testing.T) {
	// Anonymous clipboard paste → bare number, no filename.
	anon := pastedImage{n: 2, path: "/tmp/copy.png", display: ""}
	if got := anon.chipText(); got != "[Image 2"+chipSentinel+"]" {
		t.Fatalf("anonymous chip = %q, want %q", got, "[Image 2"+chipSentinel+"]")
	}

	// Short source filename fits whole.
	short := pastedImage{n: 1, path: "/tmp/copy.png", display: "shot.png"}
	if got := short.chipText(); got != "[Image 1: shot.png"+chipSentinel+"]" {
		t.Fatalf("short chip = %q, want %q", got, "[Image 1: shot.png"+chipSentinel+"]")
	}

	// Long screenshot name truncates, keeping the extension.
	long := pastedImage{n: 3, path: "/tmp/copy.png", display: "Screenshot 2026-09-04 at 10.21.33 AM.png"}
	got := long.chipText()
	if strings.Contains(got, "10.21.33") || !strings.HasSuffix(got, ".png"+chipSentinel+"]") || !strings.Contains(got, "…") {
		t.Fatalf("long chip = %q, want a truncated stem + ellipsis + .png", got)
	}
	// The snippet between "colon " and the closing bracket respects the budget.
	inner := strings.TrimSuffix(strings.TrimPrefix(got, "[Image 3: "), chipSentinel+"]")
	if runewidth.StringWidth(inner) > maxImageNameRunes {
		t.Fatalf("chip snippet %q exceeds the %d-rune budget", inner, maxImageNameRunes)
	}
}

// Numbered per session: a second paste increments the chip number, and each
// chip resolves back to its own on-disk copy at prepare time.
func TestImageChipsNumberAndResolve(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())

	src1 := filepath.Join(t.TempDir(), "first.png")
	src2 := filepath.Join(t.TempDir(), "second.png")
	if err := os.WriteFile(src1, []byte("\x89PNG\r\n\x1a\none"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src2, []byte("\x89PNG\r\n\x1a\ntwo"), 0o600); err != nil {
		t.Fatal(err)
	}

	m := compactCmdModel()
	paste := func(src string) {
		tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(src + "\n"), Paste: true})
		m = tm.(*model)
		tm, _ = m.Update(cmd())
		m = tm.(*model)
	}
	paste(src1)
	paste(src2)

	if !strings.Contains(m.input.Value(), "[Image 1: first.png"+chipSentinel+"]") || !strings.Contains(m.input.Value(), "[Image 2: second.png"+chipSentinel+"]") {
		t.Fatalf("chips should number 1 and 2, got %q", m.input.Value())
	}
	if m.imageSeq != 2 || len(m.images) != 2 {
		t.Fatalf("session image bookkeeping: seq=%d len=%d", m.imageSeq, len(m.images))
	}

	// prepareTurn rewrites each chip back to its own @path so imageParts reads
	// the right bytes; the input value itself is untouched (still chips).
	expanded := m.expandImageChips(m.input.Value())
	if !strings.Contains(expanded, "@"+m.images[0].path) || !strings.Contains(expanded, "@"+m.images[1].path) {
		t.Fatalf("chips should expand to their @paths, got %q", expanded)
	}
	if !strings.Contains(m.input.Value(), "[Image 1:") || !strings.Contains(m.input.Value(), "[Image 2:") {
		t.Fatal("expandImageChips mutating the input value")
	}
	// A chip with an unknown number stays literal rather than guessing.
	if got := m.expandImageChips("[Image 99: nope.png" + chipSentinel + "]"); got != "[Image 99: nope.png"+chipSentinel+"]" {
		t.Fatalf("unknown chip should stay literal, got %q", got)
	}
	// Hand-typed "[Image 1]" (no sentinel) must NOT expand — it would silently
	// attach images[0] the user never pasted.
	if got := m.expandImageChips("[Image 1]"); got != "[Image 1]" {
		t.Fatalf("hand-typed [Image 1] without sentinel should stay literal, got %q", got)
	}
	// Same for a hand-typed chip with a display name.
	if got := m.expandImageChips("[Image 1: fake.png]"); got != "[Image 1: fake.png]" {
		t.Fatalf("hand-typed chip without sentinel should stay literal, got %q", got)
	}
}

// macOS temp screenshot hover files live in a transient staging dir that can
// be swept away before the model call uses them. The paste copies the bytes
// off immediately, so deleting the source after the paste keeps the attached
// image intact.
func TestTempImageCopiedBeforeSourceVanishes(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	// Simulate the transient staging area via t.TempDir().
	source := filepath.Join(t.TempDir(), "Screenshot temp") // no extension, like hover thumbnails
	image := []byte("\x89PNG\r\n\x1a\nhover-image")
	if err := os.WriteFile(source, image, 0o600); err != nil {
		t.Fatal(err)
	}

	m := compactCmdModel()
	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(source + "\n"), Paste: true})
	m = tm.(*model)
	tm, _ = m.Update(cmd())
	m = tm.(*model)

	if len(m.images) != 1 {
		t.Fatalf("expected one pasted image, got %d", len(m.images))
	}
	copied := m.images[0].path
	if copied == source {
		t.Fatal("the paste must copy out of the transient staging dir, not reference it")
	}
	// The chip still shows the original staging filename.
	if !strings.Contains(m.input.Value(), "[Image 1: Screenshot temp"+chipSentinel+"]") {
		t.Fatalf("chip should carry the staging display name, got %q", m.input.Value())
	}

	// Now the staging file vanished — exactly what happens before the model call.
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(copied)
	if err != nil {
		t.Fatalf("copied image should survive the source vanishing: %v", err)
	}
	if string(got) != string(image) {
		t.Fatalf("copied image = %q, want %q", got, image)
	}

	// And prepareTurn can still resolve the chip to the surviving copy.
	if !strings.Contains(m.expandImageChips(m.input.Value()), "@"+copied) {
		t.Fatalf("chip should resolve to the surviving copy, got %q", m.expandImageChips(m.input.Value()))
	}
}
