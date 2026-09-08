package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// macOS screenshot filenames always contain spaces. @-mentioning one must
// attach the image — the tokenizer must not split the path at its spaces.
func TestImagePartsPathWithSpaces(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "Screenshot 2026-09-04 at 3.21.45 PM.png")
	if err := os.WriteFile(img, []byte("\x89PNG\r\n\x1a\nfake"), 0o644); err != nil {
		t.Fatal(err)
	}
	parts, _ := imageParts("look at @" + img + " please")
	if len(parts) != 1 {
		t.Fatalf("imageParts with a space-containing path = %d parts, want 1", len(parts))
	}
}

// A Finder drag pastes a backslash-escaped path; the paste handler must
// unescape it before statting.
func TestPastedImagePathBackslashEscaped(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "Screenshot 2026-09-04 at 3.21.45 PM.png")
	if err := os.WriteFile(img, []byte("\x89PNG\r\n\x1a\nfake"), 0o644); err != nil {
		t.Fatal(err)
	}
	escaped := strings.ReplaceAll(img, " ", `\ `)
	if got, ok := pastedImagePath(escaped); !ok || got != img {
		t.Errorf("pastedImagePath(%q) = (%q, %v), want (%q, true)", escaped, got, ok, img)
	}
}
