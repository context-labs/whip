package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A pasted [Image N] chip steered mid-turn must expand to its @path (and
// inline the image on vision models) — not reach the model as a literal
// bracket string.
func TestSteerExpandsImageChips(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	m := compactCmdModel()
	m.busy = true // the steer path only fires while a turn is running

	// Paste an image so the chip registry has one entry.
	dir := t.TempDir()
	img := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(img, []byte("\x89PNG\r\n\x1a\nfake"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.imageSeq++
	p := pastedImage{n: 1, path: img, display: "shot.png"}
	m.images = append(m.images, p)

	// The steer path: expandImageChips must run before Steer/SteerImages.
	// The chip text comes from chipText() (with the sentinel), the way a real
	// paste inserts it.
	steerText := m.expandImageChips("check " + p.chipText() + " please")
	if !strings.Contains(steerText, "@"+img) {
		t.Errorf("steer text did not expand the chip: %q", steerText)
	}
}

// /clear resets the conversation AND the pasted-image registry: a recalled
// "[Image 1]" chip from before the clear must not resolve to a different
// image once the session counter restarts.
func TestClearResetsImageRegistry(t *testing.T) {
	m := compactCmdModel()
	m.busy = false // /clear refuses while busy

	// Seed the registry with one image.
	m.imageSeq = 1
	m.images = append(m.images, pastedImage{n: 1, path: "/tmp/shot.png", display: "shot.png"})

	// Run /clear.
	m.command("/clear")

	if len(m.images) != 0 || m.imageSeq != 0 {
		t.Errorf("/clear left image registry: images=%d imageSeq=%d", len(m.images), m.imageSeq)
	}
}

// After /clear, a hand-recalled "[Image 1]" chip must stay literal (the
// registry is empty), not resolve to a new image pasted later at the same N.
func TestRecalledChipAfterClearStaysLiteral(t *testing.T) {
	m := compactCmdModel()
	m.busy = false
	m.command("/clear")
	if got := m.expandImageChips("[Image 1]"); got != "[Image 1]" {
		t.Errorf("recalled chip after /clear resolved: %q", got)
	}
}
