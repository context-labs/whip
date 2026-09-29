package theme

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestCatalogContextCancellationAndNonblockingSources(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := CatalogContext(ctx, t.TempDir()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := ResolveContext(ctx, "dark", t.TempDir()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(directory, "fifo.json"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := readThemeFile(root, "fifo.json"); err == nil {
		t.Fatal("FIFO source accepted")
	}
	if err := os.Symlink("fifo.json", filepath.Join(directory, "link.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := readThemeFile(root, "link.json"); err == nil {
		t.Fatal("symlink source accepted")
	}
}
