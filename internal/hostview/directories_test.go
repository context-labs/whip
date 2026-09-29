package hostview

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
)

func TestHostDirectoriesBoundedPaginationAndHumanSymlinks(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	for _, name := range []string{"alpha", "beta", ".hidden", "space "} {
		if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("not listed"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	first, err := Directories(t.Context(), DirectoryParams{Path: "~", Limit: 2})
	if err != nil || len(first.Entries) != 2 || first.Entries[0].Name != "alpha" || !first.HasMore || first.NextAfter == nil {
		t.Fatal(first, err)
	}
	second, err := Directories(t.Context(), DirectoryParams{Path: root, After: *first.NextAfter, Limit: 2})
	if err != nil || len(second.Entries) != 2 || second.HasMore || second.Entries[1].Name != "space " {
		t.Fatal(second, err)
	}
	hidden, err := Directories(t.Context(), DirectoryParams{Path: root, Prefix: ".", ShowHidden: true, Limit: 1})
	if err != nil || len(hidden.Entries) != 1 || hidden.Entries[0].Name != ".hidden" {
		t.Fatal(hidden, err)
	}
	selected, err := Directories(t.Context(), DirectoryParams{Path: filepath.Join(root, "link"), Limit: 1})
	if err != nil || len(selected.Entries) != 0 {
		t.Fatal(selected, err)
	}
	for _, p := range []DirectoryParams{{Path: "relative", Limit: 1}, {Path: root, After: "../", Limit: 1}, {Path: root, Limit: 0}, {Path: root, Prefix: "\xff", Limit: 1}, {Path: filepath.Join(root, "file"), Limit: 1}} {
		if _, err := Directories(t.Context(), p); err == nil {
			t.Fatal("accepted", p)
		}
	}
	fifo := filepath.Join(root, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Directories(t.Context(), DirectoryParams{Path: fifo, Limit: 1}); err == nil {
		t.Fatal("accepted fifo")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Directories(ctx, DirectoryParams{Path: root, Limit: 1}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(root, "file"))
	if !reflect.DeepEqual(before, []byte("not listed")) {
		t.Fatal("file changed")
	}
}
