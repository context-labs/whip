package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

// unusableHome points WHIPCODE_HOME at a path nested inside a regular file, so
// every config.Dir/config.Load call fails the way a broken install does.
func unusableHome(t *testing.T) {
	t.Helper()
	f := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WHIPCODE_HOME", filepath.Join(f, "whip"))
}

func TestSessionsCLI(t *testing.T) {
	r := runFixture(t, "listed reply", nil)
	if _, err := runCapture(t, "", "hello"); err != nil {
		t.Fatal(err)
	}
	owner := nativeSession(t)
	tree, err := r.Tree(t.Context(), session.TreeID(owner.TreeID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.UpdateTree(t.Context(), tree.ID, tree.Revision, session.TreeMetadata{Title: new("how do I unstage a file")}); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := sessionsCLI(); err != nil {
			t.Fatal(err)
		}
	})
	for _, part := range []string{string(owner.ID), "how do I unstage a file", "test", "just now"} {
		if !strings.Contains(out, part) {
			t.Fatal(out, part)
		}
	}
}

func TestSessionsCLIEmptyAndUntitled(t *testing.T) {
	r := runFixture(t, "reply", nil)
	out := captureStdout(t, func() {
		if err := sessionsCLI(); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "no sessions yet") {
		t.Fatal(out)
	}
	refs, err := r.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.CreateTree(t.Context(), store.CreateTree{Engine: session.Starlark, Definition: refs[0], WorkingDirectory: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	out = captureStdout(t, func() {
		if err := sessionsCLI(); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "(untitled)") {
		t.Fatal(out)
	}
}

func TestSessionsCLITruncatesTitle(t *testing.T) {
	r := runFixture(t, "reply", nil)
	refs, err := r.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	title := strings.Repeat("界", 80)
	if _, _, err := r.CreateTree(t.Context(), store.CreateTree{Metadata: session.TreeMetadata{Title: &title}, Engine: session.Starlark, Definition: refs[0], WorkingDirectory: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := sessionsCLI(); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, strings.Repeat("界", 39)+"…") || strings.Contains(out, title) {
		t.Fatal(out)
	}
}

func TestSessionsCLIStoreErrors(t *testing.T) {
	r := runFixture(t, "reply", nil)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := sessionsCLI(); err == nil {
		t.Fatal("closed runtime returned a successful listing")
	}
}

func TestTruncAndAgo(t *testing.T) {
	if got := trunc("short", 10); got != "short" {
		t.Errorf("trunc should leave a short string alone, got %q", got)
	}
	if got := trunc("0123456789abc", 10); got != "012345678…" {
		t.Errorf("trunc(…, 10) = %q", got)
	}

	now := time.Now()
	for _, c := range []struct {
		d    time.Duration
		want string
	}{
		{10 * time.Second, "just now"},
		{5 * time.Minute, "5m ago"},
		{3 * time.Hour, "3h ago"},
	} {
		if got := ago(now.Add(-c.d)); got != c.want {
			t.Errorf("ago(-%s) = %q, want %q", c.d, got, c.want)
		}
	}
	old := now.Add(-72 * time.Hour)
	if got := ago(old); got != old.Format("2006-01-02") {
		t.Errorf("anything older than a day should show the date, got %q", got)
	}
}

// freezeHome makes the config directory read-only for the rest of the test,
// so every config write-back fails the way a locked-down install does.
func freezeHome(t *testing.T, home string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root writes through a read-only directory")
	}
	if err := os.Chmod(home, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(home, 0o700) })
}
