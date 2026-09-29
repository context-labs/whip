package bashrun

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestCompletionJoinsDescendants(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	for _, kind := range []string{"pipe", "pty", "job"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			opts := Options{Command: "echo $$ > group; sleep 30 & echo done", Cwd: dir, Timeout: 5 * time.Second, Interactive: kind == "pty"}
			if kind == "job" {
				job, err := Start(t.Context(), opts)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = job.Kill() })
				waitJob(t, job)
			} else {
				result := Run(t.Context(), opts)
				if result.Exit != "" || !strings.Contains(result.Output, "done") {
					t.Fatal(result)
				}
			}
			data, err := os.ReadFile(filepath.Join(dir, "group"))
			if err != nil {
				t.Fatal(err)
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatal(err)
			}
			if err := syscall.Kill(-pid, 0); !errors.Is(err, syscall.ESRCH) {
				t.Fatalf("completion retained group %d: %v", pid, err)
			}
		})
	}
}

func TestRunJoinsOutputCallback(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	t.Cleanup(releaseOnce)
	signal := sync.OnceFunc(func() { close(entered) })
	done := make(chan Result, 1)
	go func() {
		done <- Run(t.Context(), Options{
			Command: "echo ready; sleep .2", Timeout: 5 * time.Second,
			OnUpdate: func(string) { signal(); <-release },
		})
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("no output callback")
	}
	select {
	case <-done:
		t.Fatal("Run returned before its callback joined")
	case <-time.After(300 * time.Millisecond):
	}
	releaseOnce()
	select {
	case result := <-done:
		if result.Exit != "" {
			t.Fatal(result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not join callback")
	}
}

func TestPTYJoinsBlockedKeyWriter(t *testing.T) {
	keys := make(chan []byte, 1)
	keys <- []byte(strings.Repeat("x", 1<<20))
	done := make(chan Result, 1)
	go func() {
		done <- Run(t.Context(), Options{Command: "stty -echo -icanon; sleep .1", Interactive: true, Keys: keys, Timeout: 2 * time.Second})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("PTY key writer prevented joined completion")
	}
	// The caller owns the input channel; completion must never close it.
	select {
	case keys <- []byte("x"):
	default:
	}
	close(keys)
}

func TestPrivateShellManagerDoesNotInheritSecrets(t *testing.T) {
	t.Setenv("WHIP_PRIVATE_SHELL_TEST_SECRET", "must-not-inherit")
	result := Run(t.Context(), Options{Command: "env", Env: map[string]string{"EXPLICIT_SHELL_VALUE": "permitted"}})
	if result.Exit != "" || strings.Contains(result.Output, "must-not-inherit") || !strings.Contains(result.Output, "EXPLICIT_SHELL_VALUE=permitted") {
		t.Fatal(result)
	}
}
