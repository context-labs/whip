package computer

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestBundledHelperExplicitPublicationPreservesExactBytes(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	payload := []byte("signed fixture bytes\x00\xff")
	path, err := publishBundle(t.Context(), dir, payload)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "bin", "whip-computer") {
		t.Fatal(path)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatal(got, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatal(info, err)
	}
	if _, err := publishBundle(t.Context(), dir, payload); err != nil {
		t.Fatal(err)
	}
	items, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
}

func TestBundledHelperRejectsUnsafeDirectoriesAndUnavailablePayload(t *testing.T) {
	for _, kind := range []string{"empty", "cancelled", "shared-root", "shared-bin", "root-link", "bin-link", "bin-fifo"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Chmod(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			payload := []byte("fixture")
			switch kind {
			case "empty":
				payload = nil
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "shared-root":
				if err := os.Chmod(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			case "shared-bin":
				if err := os.Mkdir(filepath.Join(dir, "bin"), 0o755); err != nil {
					t.Fatal(err)
				}
			case "root-link":
				link := filepath.Join(t.TempDir(), "link")
				if err := os.Symlink(dir, link); err != nil {
					t.Fatal(err)
				}
				dir = link
			case "bin-link":
				if err := os.Symlink(t.TempDir(), filepath.Join(dir, "bin")); err != nil {
					t.Fatal(err)
				}
			case "bin-fifo":
				if err := unix.Mkfifo(filepath.Join(dir, "bin"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := publishBundle(ctx, dir, payload); err == nil {
				t.Fatal("unsafe publication accepted")
			}
			if _, err := os.Stat(filepath.Join(dir, "bin", "whip-computer")); err == nil {
				t.Fatal("unexpected publication")
			}
		})
	}
}

func TestBundledHelperDoesNotFollowExistingDestination(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "untouched")
	if err := os.WriteFile(out, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(out, filepath.Join(dir, "bin", "whip-computer")); err != nil {
		t.Fatal(err)
	}
	if _, err := publishBundle(t.Context(), dir, []byte("fixture")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil || string(got) != "original" {
		t.Fatal(string(got), err)
	}
}

func TestBundledHelperAvailabilityHasNoFilesystemEffects(t *testing.T) {
	if BundledAvailable() != (len(helperBinary) > 0 && len(helperBinary) <= maxBundleBytes) {
		t.Fatal("availability differs from embedded payload")
	}
	if len(helperBinary) == 0 {
		dir := t.TempDir()
		if _, err := PublishBundled(t.Context(), dir); !errors.Is(err, ErrBundledUnavailable) {
			t.Fatal(err)
		}
		items, err := os.ReadDir(dir)
		if err != nil || len(items) != 0 {
			t.Fatal(items, err)
		}
	}
}
