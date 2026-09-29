package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/sys/unix"

	"github.com/context-labs/whip/internal/protocol"
)

func nativeFileLinkDirectory(t *testing.T) string {
	t.Helper()
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "src", "main.go"), []byte("package fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	return directory
}

func TestNativeFileLinksRequireKnownLocalOwnerAndCurrentCWD(t *testing.T) {
	first, second := nativeFileLinkDirectory(t), nativeFileLinkDirectory(t)
	m := &nativeModel{owner: protocol.Session{ID: "first", WorkingDirectory: first}, input: newInput(), width: 80, height: 24, history: nativeTranscript{messages: []protocol.Message{nativeMessage(1, "assistant", "See src/main.go:12 for the change.")}}}
	m.refresh()
	if view := m.View().Content; strings.Contains(view, "\x1b]8;;file:") {
		t.Fatal("borrowed/default connection interpreted host path as local", view)
	}
	m.localFilesystem = true
	view := m.View().Content
	if !strings.Contains(view, absFileURIAt(first, "src/main.go", "12")) {
		t.Fatal("known-local existing file was not linked", view)
	}
	m.owner.WorkingDirectory = second
	view = m.View().Content
	if strings.Contains(view, absFileURIAt(first, "src/main.go", "12")) || !strings.Contains(view, absFileURIAt(second, "src/main.go", "12")) {
		t.Fatal("cwd change reused stale link mapping", view)
	}
	m.owner.ID, m.owner.WorkingDirectory = "other", first
	view = m.View().Content
	if !strings.Contains(view, absFileURIAt(first, "src/main.go", "12")) || strings.Contains(view, absFileURIAt(second, "src/main.go", "12")) {
		t.Fatal("owner change reused stale link mapping", view)
	}
	if err := os.Rename(first, first+"-retired"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(first + "-retired") })
	if err := os.Symlink(second, first); err != nil {
		t.Fatal(err)
	}
	if view := m.View().Content; strings.Contains(view, "\x1b]8;;file:") {
		t.Fatal("symlink cwd replacement retained local links", view)
	}
}

func TestNativeFileLinksRefuseSymlinksOutsideFilesAndSpecialFiles(t *testing.T) {
	directory, outside := nativeFileLinkDirectory(t), nativeFileLinkDirectory(t)
	for _, value := range []struct{ from, to string }{{filepath.Join(directory, "alias"), filepath.Join(directory, "src")}, {filepath.Join(directory, "other.go"), filepath.Join(outside, "src", "main.go")}} {
		if err := os.Symlink(value.to, value.from); err != nil {
			t.Fatal(err)
		}
	}
	if err := unix.Mkfifo(filepath.Join(directory, "pipe.go"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := nativeLinkRoot(directory)
	if root == nil {
		t.Fatal("could not open fixture")
	}
	defer func() { _ = root.Close() }()
	for _, path := range []string{"alias/main.go", "other.go", "pipe.go", "src", "../outside.go", "src/../src/main.go", "./../outside.go", filepath.Join(outside, "src", "main.go")} {
		if nativeLinkFile(root, directory, path) {
			t.Fatalf("unsafe or outside path linked: %q", path)
		}
	}
	for _, path := range []string{"src/main.go", "./src/main.go", filepath.Join(directory, "src", "main.go")} {
		if !nativeLinkFile(root, directory, path) {
			t.Fatalf("safe scoped file refused: %q", path)
		}
	}
}

func TestNativeFileLinksPreserveWebTargetsStylesAndBoundedObservation(t *testing.T) {
	directory := nativeFileLinkDirectory(t)
	frame := "\x1b[31msrc/main.go:5\x1b[0m " + hyperlink("https://example.test/src/main.go", "src/main.go") + " file:///private/secret\n"
	linked := nativeFileLinks(frame, directory)
	if strings.Count(linked, "\x1b]8;;file:") != 1 || !strings.Contains(linked, "\x1b]8;;https://example.test/src/main.go") || ansi.Strip(linked) != ansi.Strip(frame) || ansi.StringWidth(linked) != ansi.StringWidth(frame) {
		t.Fatal("linking changed display or nested another link", linked)
	}
	many := nativeFileLinks(strings.Repeat("src/main.go ", 200), directory)
	if strings.Count(many, "\x1b]8;;file:") != 128 {
		t.Fatal("path observation did not respect its frame bound")
	}
	large := strings.Repeat("src/main.go ", (1<<20)/12+1)
	if result := nativeFileLinks(large, directory); result != large {
		t.Fatal("oversized frame was probed")
	}
}
