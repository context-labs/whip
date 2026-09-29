package instruction

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func standingEditFixture(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "standing.md")
	if err := os.WriteFile(path, []byte(text), 0o640); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStandingFileEditPreservesRawSourceAndCAS(t *testing.T) {
	raw := "# comment\r\n\r\n Prefer café. \r\n"
	path := standingEditFixture(t, raw)
	before, err := ReadStandingFile(t.Context(), path)
	if err != nil || before.Text != raw || len(before.Revision) != 64 {
		t.Fatal(before, err)
	}
	if unchanged, err := WriteStandingFile(t.Context(), path, before.Revision, raw); err != nil || unchanged != before {
		t.Fatal(unchanged, err)
	}
	after, err := WriteStandingFile(t.Context(), path, before.Revision, "")
	if err != nil || after.Text != "" || after.Revision == before.Revision {
		t.Fatal(after, err)
	}
	if _, err := WriteStandingFile(t.Context(), path, before.Revision, "old edit"); !errors.Is(err, ErrStandingChanged) {
		t.Fatal(err)
	}
	if actual, err := ReadStandingFile(t.Context(), path); err != nil || actual != after {
		t.Fatal(actual, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatal(info, err)
	}
	files, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
}

func TestStandingFileRevisionBindsReplacementAndParentIdentity(t *testing.T) {
	for _, change := range []string{"file", "directory"} {
		t.Run(change, func(t *testing.T) {
			path := standingEditFixture(t, "same bytes")
			before, err := ReadStandingFile(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			if change == "file" {
				if err := os.Rename(path, path+".old"); err != nil {
					t.Fatal(err)
				}
			} else {
				directory := filepath.Dir(path)
				if err := os.Rename(directory, directory+".old"); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.RemoveAll(directory + ".old") })
				if err := os.Mkdir(directory, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(path, []byte(before.Text), 0o640); err != nil {
				t.Fatal(err)
			}
			replaced, err := ReadStandingFile(t.Context(), path)
			if err != nil || replaced.Revision == before.Revision {
				t.Fatal(replaced, err)
			}
			if _, err := WriteStandingFile(t.Context(), path, before.Revision, "wrong target"); !errors.Is(err, ErrStandingChanged) {
				t.Fatal(err)
			}
		})
	}
}

func TestStandingFileAnchoredEditRejectsDirectoryRetarget(t *testing.T) {
	path := standingEditFixture(t, "original")
	before, err := ReadStandingFile(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := openStandingDirectory(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	parent := filepath.Dir(path)
	moved := parent + ".old"
	if err := os.Rename(parent, moved); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(moved) })
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = writeStandingFile(t.Context(), directory, path, before.Revision, "lost edit", (*os.File).Sync)
	if !errors.Is(err, ErrStandingChanged) {
		t.Fatal(err)
	}
	for _, check := range []struct{ path, text string }{{path, "replacement"}, {filepath.Join(moved, "standing.md"), "original"}} {
		raw, err := os.ReadFile(check.path)
		if err != nil || string(raw) != check.text {
			t.Fatal(check, string(raw), err)
		}
	}
}

func TestStandingFileReadAndEditBoundsAndUnsafeTargets(t *testing.T) {
	for _, text := range []string{strings.Repeat("x", session.MaxInstructionSourceBytes+1), strings.Repeat("🙂", session.MaxInstructionSourceBytes/4+1), "nul\x00", string([]byte{0xff})} {
		path := standingEditFixture(t, "valid")
		before, err := ReadStandingFile(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := WriteStandingFile(t.Context(), path, before.Revision, text); !errors.Is(err, session.ErrInvalid) {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadStandingFile(t.Context(), path); err == nil {
			t.Fatal("invalid raw file accepted")
		}
	}
	for _, kind := range []string{"missing", "symlink", "fifo", "directory", "unsafe mode"} {
		t.Run(kind, func(t *testing.T) {
			path := standingEditFixture(t, "original")
			before, err := ReadStandingFile(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "symlink":
				target := standingEditFixture(t, "private")
				err = os.Symlink(target, path)
			case "fifo":
				err = syscall.Mkfifo(path, 0o600)
			case "directory":
				err = os.Mkdir(path, 0o700)
			case "unsafe mode":
				err = os.WriteFile(path, []byte("unsafe"), 0o666)
				if err == nil {
					err = os.Chmod(path, 0o666)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, err := range []error{func() error { _, e := ReadStandingFile(t.Context(), path); return e }(), func() error { _, e := WriteStandingFile(t.Context(), path, before.Revision, "edit"); return e }()} {
				if err == nil || strings.Contains(err.Error(), filepath.Dir(path)) {
					t.Fatal("unsafe target or private path", err)
				}
			}
		})
	}
}

func TestStandingFilePostPublicationFailureRequiresReread(t *testing.T) {
	path := standingEditFixture(t, "before")
	before, err := ReadStandingFile(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := openStandingDirectory(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	_, err = writeStandingFile(t.Context(), directory, path, before.Revision, "after", func(*os.File) error { return errors.New("sync failed") })
	if err == nil || !strings.Contains(err.Error(), "may be visible") {
		t.Fatal(err)
	}
	after, err := ReadStandingFile(t.Context(), path)
	if err != nil || after.Text != "after" || after.Revision == before.Revision {
		t.Fatal(after, err)
	}
	if _, err := WriteStandingFile(t.Context(), path, before.Revision, "after"); !errors.Is(err, ErrStandingChanged) {
		t.Fatal(err)
	}
}

func TestStandingFileExactLimitAndSafeParent(t *testing.T) {
	path := standingEditFixture(t, strings.Repeat("x", session.MaxInstructionSourceBytes))
	before, err := ReadStandingFile(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	after, err := WriteStandingFile(t.Context(), path, before.Revision, strings.Repeat("y", session.MaxInstructionSourceBytes))
	if err != nil || len(after.Text) != session.MaxInstructionSourceBytes {
		t.Fatal(len(after.Text), err)
	}
	alias := filepath.Join(t.TempDir(), "linked-parent")
	if err := os.Symlink(filepath.Dir(path), alias); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadStandingFile(t.Context(), filepath.Join(alias, "standing.md")); err == nil {
		t.Fatal("linked parent accepted")
	}
	if err := os.Chmod(filepath.Dir(path), 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteStandingFile(t.Context(), path, after.Revision, "unsafe parent"); !errors.Is(err, session.ErrInvalid) {
		t.Fatal(err)
	}
}
