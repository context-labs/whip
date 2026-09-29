package hostview

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestCreateHumanDirectory(t *testing.T) {
	parent := t.TempDir()
	for _, name := range []string{"project", ".hidden", " spaced name", "café", strings.Repeat("a", 255)} {
		path, err := CreateDirectory(t.Context(), parent, name)
		if err != nil || path != filepath.Join(parent, name) {
			t.Fatalf("create %q = %q, %v", name, path, err)
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Fatalf("created directory = %v, %v", info, err)
		}
		if _, err := CreateDirectory(t.Context(), parent, name); !errors.Is(err, os.ErrExist) {
			t.Fatalf("existing directory was adopted: %v", err)
		}
	}
	outside := t.TempDir()
	alias := filepath.Join(parent, "alias")
	if err := os.Symlink(outside, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateDirectory(t.Context(), alias, "child"); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(outside, "child")); err != nil || !info.IsDir() {
		t.Fatalf("human alias = %v, %v", info, err)
	}
	if _, err := CreateDirectory(t.Context(), parent, "alias"); !errors.Is(err, os.ErrExist) {
		t.Fatalf("existing symlink was adopted: %v", err)
	}
}

func TestCreateHumanDirectoryRejectsPathsAndCancelledWork(t *testing.T) {
	parent := t.TempDir()
	for _, name := range []string{"", " ", "\t\n", ".", "..", "../escape", "nested/child", "/absolute", `nested\child`, "bad\x00name", "\xff", strings.Repeat("a", 256), strings.Repeat("é", 128)} {
		if _, err := CreateDirectory(t.Context(), parent, name); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("accepted name %q: %v", name, err)
		}
	}
	for _, path := range []string{"", "relative", "~/folder", parent + "\x00", "/\xff", "/" + strings.Repeat("a", 4096)} {
		if _, err := CreateDirectory(t.Context(), path, "child"); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("accepted parent %q: %v", path, err)
		}
	}
	if _, err := CreateDirectory(t.Context(), filepath.Join(parent, "missing"), "child"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created missing parents: %v", err)
	}
	file := filepath.Join(parent, "file")
	if err := os.WriteFile(file, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateDirectory(t.Context(), parent, "file"); !errors.Is(err, os.ErrExist) {
		t.Fatalf("replaced file: %v", err)
	}
	if _, err := CreateDirectory(t.Context(), file, "child"); err == nil {
		t.Fatal("accepted file as parent")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := CreateDirectory(ctx, parent, "cancelled"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(parent, "cancelled")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled request created directory")
	}
}
