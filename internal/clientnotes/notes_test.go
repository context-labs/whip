package clientnotes

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func fixture(t *testing.T) (string, *Store) {
	t.Helper()
	home := t.TempDir()
	store, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	return home, store
}

func TestLocalNotesAreFreshAndScopedByBothRemoteIdentities(t *testing.T) {
	home := t.TempDir()
	old := filepath.Join(home, "memory.md")
	if err := os.WriteFile(old, []byte("- [ ] retained old note\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	snapshot, err := store.Installation().Read(t.Context())
	if err != nil || len(snapshot.Entries) != 0 || !strings.Contains(snapshot.Path, "client-v4/memory/installation.md") {
		t.Fatal(snapshot, err)
	}
	first, err := store.Session("host-one", "same-session")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Session("host-two", "same-session")
	if err != nil {
		t.Fatal(err)
	}
	crafted, err := store.Session("../../runtime-v4", "../../host.json")
	if err != nil {
		t.Fatal(err)
	}
	if first.file == second.file || strings.Contains(crafted.file, "..") || filepath.Dir(crafted.file) != "." {
		t.Fatal(first, second, crafted)
	}
	scoped, err := first.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scoped.Path, []byte("- [ ] local only\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	other, err := second.Read(t.Context())
	if err != nil || len(other.Entries) != 0 {
		t.Fatal(other, err)
	}
	raw, err := os.ReadFile(old)
	if err != nil || string(raw) != "- [ ] retained old note\n" {
		t.Fatal(string(raw), err)
	}
}

func TestForgetPreservesMarkdownAndRequiresTheListedRevision(t *testing.T) {
	_, store := fixture(t)
	scope := store.Installation()
	snapshot, err := scope.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	original := "# User notes\r\n\r\n- [ ] 日本語\r\n- [x] completed\nprose without a final newline"
	if err := os.WriteFile(snapshot.Path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := scope.Read(t.Context())
	if err != nil || len(before.Entries) != 2 || before.Entries[0].Text != "日本語" || !before.Entries[1].Done {
		t.Fatal(before, err)
	}
	after, err := scope.Forget(t.Context(), 1, before.Revision)
	if err != nil || !after.Entries[0].Done {
		t.Fatal(after, err)
	}
	raw, err := os.ReadFile(snapshot.Path)
	if err != nil || string(raw) != strings.Replace(original, "- [ ]", "- [x]", 1) {
		t.Fatal(string(raw), err)
	}
	info, err := os.Stat(snapshot.Path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal(info, err)
	}
	if _, err := scope.Forget(t.Context(), 1, before.Revision); !errors.Is(err, ErrChanged) {
		t.Fatal("stale selection changed notes", err)
	}
	if _, err := scope.Forget(t.Context(), 2, after.Revision); err == nil {
		t.Fatal("done entry accepted")
	}
	if _, err := scope.Forget(t.Context(), 3, after.Revision); err == nil {
		t.Fatal("missing entry accepted")
	}
	if err := os.WriteFile(snapshot.Path, []byte("external editor\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := scope.Forget(t.Context(), 1, after.Revision); !errors.Is(err, ErrChanged) {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(snapshot.Path)
	if err != nil || string(raw) != "external editor\n" {
		t.Fatal("external note overwritten", string(raw), err)
	}
}

func TestConcurrentStoresCannotOverwriteTheSameListedRevision(t *testing.T) {
	for range 16 {
		t.Run("first_use", concurrentStores)
	}
}

func concurrentStores(t *testing.T) {
	home, first := fixture(t)
	second, err := Open(home)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	scope := first.Installation()
	before, err := scope.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(before.Path, []byte("- [ ] first\n- [ ] second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err = scope.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var joined sync.WaitGroup
	for i, s := range []*Store{first, second} {
		joined.Go(func() { <-start; _, err := s.Installation().Forget(t.Context(), i+1, before.Revision); results <- err })
	}
	close(start)
	joined.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrChanged) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatal(successes, conflicts)
	}
}

func TestNotesBoundsSymlinksAndCancelledLockWait(t *testing.T) {
	_, store := fixture(t)
	scope := store.Installation()
	snapshot, err := scope.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{bytes.Repeat([]byte("x"), MaxBytes+1), {0xff}, []byte("nul\x00")} {
		if err := os.WriteFile(snapshot.Path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := scope.Read(t.Context()); err == nil {
			t.Fatal("invalid note file accepted", len(raw))
		}
	}
	if err := os.Remove(snapshot.Path); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(outside, []byte("- [ ] private\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, snapshot.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := scope.Read(t.Context()); err == nil {
		t.Fatal("symbolic link accepted")
	}
	if err := os.Remove(snapshot.Path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snapshot.Path, []byte("- [ ] first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err = scope.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	lock, err := store.root.OpenFile(".write.lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := scope.Forget(ctx, 1, snapshot.Revision); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := scope.Read(t.Context()); !errors.Is(err, os.ErrClosed) {
		t.Fatal(err)
	}
}

func TestClientNamespaceDoesNotFollowARetiredDirectoryLink(t *testing.T) {
	home := t.TempDir()
	retired := filepath.Join(home, "runtime-v4")
	if err := os.Mkdir(retired, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(retired, filepath.Join(home, "client-v4")); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(home); err == nil {
		t.Fatal("retired directory link accepted")
	}
	files, err := os.ReadDir(retired)
	if err != nil || len(files) != 0 {
		t.Fatal("retired runtime changed", files, err)
	}
}

func TestExistingLockMustBeARegularFile(t *testing.T) {
	for _, kind := range []string{"symlink", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			_, store := fixture(t)
			scope := store.Installation()
			before, err := scope.Read(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			body := "- [ ] preserve\n"
			if err := os.WriteFile(before.Path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			before, err = scope.Read(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			lockPath := filepath.Join(store.directory, ".write.lock")
			if kind == "symlink" {
				err = os.Symlink("installation.md", lockPath)
			} else {
				err = syscall.Mkfifo(lockPath, 0o600)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := scope.Forget(t.Context(), 1, before.Revision); err == nil {
				t.Fatal("unsafe lock accepted")
			}
			raw, err := os.ReadFile(before.Path)
			if err != nil || string(raw) != body {
				t.Fatal("notes changed", string(raw), err)
			}
		})
	}
}
