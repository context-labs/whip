package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

// unusableHome points WHIP_HOME at a path nested inside a regular file, so
// every config.Dir/config.Load call fails the way a broken install does.
func unusableHome(t *testing.T) {
	t.Helper()
	f := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WHIP_HOME", filepath.Join(f, "whip"))
}

// whip sessions lists stored sessions newest-first with id, title, model, age.
func TestSessionsCLI(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WHIP_HOME", dir)

	st, _ := session.Open(filepath.Join(dir, "sessions.db"))
	id, _ := st.Create("/tmp", "kimi-k3-fast", "inference")
	st.Save(id, 0, []llm.Message{
		{Role: "user", Content: "how do I unstage a file", Authored: true},
		{Role: "assistant", Content: "git restore --staged"},
	}, "kimi-k3-fast", "inference")
	st.Close()
	useTestDaemon(t)

	out := captureStdout(t, func() {
		if err := sessionsCLI(); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "how do I unstage a file") || !strings.Contains(out, "kimi-k3-fast") {
		t.Fatalf("sessions should list id/title/model, got:\n%s", out)
	}
	if !strings.Contains(out, "just now") && !strings.Contains(out, time.Now().Format("2006-01-02")) {
		t.Fatalf("age column should render, got:\n%s", out)
	}
}

// An empty store says so instead of printing an empty table, and a session
// with no first user message yet renders as "(untitled)".
func TestSessionsCLIEmptyAndUntitled(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WHIP_HOME", dir)
	useTestDaemon(t)

	out := captureStdout(t, func() {
		if err := sessionsCLI(); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "no sessions yet") {
		t.Fatalf("an empty store should say so, got %q", out)
	}

	st, err := session.Open(filepath.Join(dir, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.Create("/tmp", "m", "p")
	if err != nil {
		t.Fatal(err)
	}
	// no authored user message yet: the session has no title to show
	if err := st.Save(id, 0, []llm.Message{{Role: "assistant", Content: "hi"}}, "m", "p"); err != nil {
		t.Fatal(err)
	}
	st.Close()

	out = captureStdout(t, func() {
		if err := sessionsCLI(); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "(untitled)") {
		t.Fatalf("a titleless session should render as (untitled), got %q", out)
	}
}

// A long title is truncated to the column width with an ellipsis.
func TestSessionsCLITruncatesTitle(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WHIP_HOME", dir)

	st, err := session.Open(filepath.Join(dir, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := st.Create("/tmp", "m", "p")
	long := strings.Repeat("q", 80)
	if err := st.Save(id, 0, []llm.Message{{Role: "user", Content: long, Authored: true}}, "m", "p"); err != nil {
		t.Fatal(err)
	}
	st.Close()
	useTestDaemon(t)

	out := captureStdout(t, func() {
		if err := sessionsCLI(); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "…") || strings.Contains(out, long) {
		t.Fatalf("a long title should be truncated with an ellipsis, got %q", out)
	}
}

// A broken config dir and an unopenable store both surface as errors rather
// than a panic or a silently empty listing.
func TestSessionsCLIStoreErrors(t *testing.T) {
	unusableHome(t)
	if err := sessionsCLI(); err == nil {
		t.Error("an unusable WHIP_HOME should error")
	}

	dir := t.TempDir()
	t.Setenv("WHIP_HOME", dir)
	if err := os.Mkdir(filepath.Join(dir, "sessions.db"), 0o700); err != nil {
		t.Fatal(err)
	}
	useTestDaemon(t)
	if err := sessionsCLI(); err == nil {
		t.Error("a sessions.db that is a directory should error")
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
