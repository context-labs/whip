package terminal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestCloseJoinsIgnoringForegroundGroupAndLeaderExitDescendants(t *testing.T) {
	for _, leaderExits := range []bool{false, true} {
		t.Run(strconv.FormatBool(leaderExits), func(t *testing.T) {
			m := startManager(t, 2, RingBytes)
			options := testOptions(t)
			if leaderExits {
				options.Args = []string{"-c", `trap '' HUP; (trap '' HUP; echo descendant-$$; sleep 300) & echo background-$!; exit 0`}
			}
			term, err := m.Open(options)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = m.Close(term.ID) })
			sink := newSink()
			if _, _, err := m.Attach(term.ID, 0, sink); err != nil {
				t.Fatal(err)
			}
			var pid int
			if leaderExits {
				pattern := regexp.MustCompile(`background-(\d+)`)
				text := sink.waitMatch(t, pattern)
				pid, _ = strconv.Atoi(pattern.FindStringSubmatch(text)[1])
			} else {
				write(t, term, "sh -c 'trap \"\" HUP; echo; echo foreground-$$; exec sleep 300'\n")
				pattern := regexp.MustCompile(`(?m)^foreground-(\d+)\r?$`)
				text := sink.waitMatch(t, pattern)
				pid, _ = strconv.Atoi(pattern.FindStringSubmatch(text)[1])
				term.mu.Lock()
				group := foregroundGroup(term.ptmx, term.process.PID())
				term.mu.Unlock()
				if group <= 1 {
					t.Fatal("did not exercise separate interactive foreground process group")
				}
			}
			start := time.Now()
			if err := m.Close(term.ID); err != nil {
				t.Fatal(err)
			}
			if time.Since(start) > 3*time.Second {
				t.Fatal("terminal close did not join promptly")
			}
			if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
				t.Fatalf("child %d survived joined close: %v", pid, err)
			}
		})
	}
}

func TestSlowSinkCloseAndTakeoverAreJoined(t *testing.T) {
	m := startManager(t, 1, RingBytes)
	term := open(t, m)
	slow := newSink()
	slow.block = make(chan struct{})
	if _, _, err := m.Attach(term.ID, 0, slow); err != nil {
		t.Fatal(err)
	}
	write(t, term, "echo before-$((2+2))-takeover\n")
	fresh := newSink()
	attached := make(chan error, 1)
	go func() { _, _, err := m.Attach(term.ID, 0, fresh); attached <- err }()
	select {
	case err := <-attached:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("takeover did not cancel blocked sink")
	}
	fresh.wait(t, "before-4-takeover")
	blocked := newSink()
	blocked.block = make(chan struct{})
	if _, _, err := m.Attach(term.ID, 0, blocked); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- m.Close(term.ID) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("close did not join blocked sink")
	}
	term.mu.Lock()
	attachedNow := term.attached
	term.mu.Unlock()
	if attachedNow != nil {
		t.Fatal("attachment survived close")
	}
	if _, _, err := m.Attach(term.ID, 0, newSink()); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	next := open(t, m)
	if len(m.List()) != 1 || next.ID == term.ID {
		t.Fatal("reservation was not released once joined")
	}
}

func TestCancellationClosesMasterAndBoundedWrites(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	m := NewManager(ctx)
	defer m.Shutdown()
	term := open(t, m)
	// A program which never reads the PTY eventually fills its input queue.
	write(t, term, "stty -echo -icanon; sleep 300\n")
	time.Sleep(30 * time.Millisecond)
	deadline, stop := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer stop()
	var err error
	for range 100 {
		err = term.WriteContext(deadline, []byte(strings.Repeat("x", MaxWriteBytes)))
		if err != nil {
			break
		}
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stalled write=%v", err)
	}
	cancel()
	select {
	case <-term.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("manager context did not stop terminal")
	}
}

func TestTerminalEnvironmentDoesNotInheritAmbientCredentials(t *testing.T) {
	t.Setenv("WHIP_FAKE_CREDENTIAL", "do-not-inherit")
	m := startManager(t, 2, RingBytes)
	term := open(t, m)
	sink := newSink()
	if _, _, err := m.Attach(term.ID, 0, sink); err != nil {
		t.Fatal(err)
	}
	write(t, term, "printf 'secret-is-%s-end\\n' \"${WHIP_FAKE_CREDENTIAL-unset}\"\n")
	sink.wait(t, "secret-is-unset-end")
	options := testOptions(t)
	options.Env["BASH_ENV"] = filepath.Join(t.TempDir(), "startup")
	if _, err := m.Open(options); err == nil {
		t.Fatal("unsafe startup override accepted")
	}
}

func TestConcurrentCloseRetainsSlotsUntilJoined(t *testing.T) {
	m := startManager(t, 2, RingBytes)
	var workers sync.WaitGroup
	for range 6 {
		workers.Go(func() {
			for range 10 {
				options := Options{Shell: "/bin/sh", Args: []string{"-c", "sleep 0.01"}, Cwd: os.TempDir(), Cols: 80, Rows: 24}
				term, err := m.Open(options)
				if errors.Is(err, ErrLimit) {
					continue
				}
				if err != nil {
					t.Error(err)
					return
				}
				if len(m.List()) > 2 {
					t.Error("capacity exceeded")
				}
				if err := m.Close(term.ID); err != nil && !errors.Is(err, ErrNotFound) {
					t.Error(err)
					return
				}
			}
		})
	}
	workers.Wait()
	if len(m.List()) != 0 {
		t.Fatal("closed terminal retained")
	}
}

func TestBoundedCursorReadsAndFinalExit(t *testing.T) {
	term := &Terminal{ring: newRing(64)}
	term.mu.Lock()
	term.ring.start = 1 << 53
	term.ring.end = 1 << 53
	term.ring.append([]byte(strings.Repeat("x", 100)))
	term.mu.Unlock()
	first, err := term.Read(1<<53, 7)
	if err != nil || !first.Truncated || len(first.Data) != 7 || first.From != (1<<53)+36 || first.Next != first.From+7 {
		t.Fatalf("page=%+v %v", first, err)
	}
	first.Data[0] = 'z'
	second, err := term.Read(first.From, 7)
	if err != nil || string(second.Data) != "xxxxxxx" {
		t.Fatal("page aliased ring")
	}
	if _, err := term.Read(first.End+1, 1); !errors.Is(err, ErrCursor) {
		t.Fatal("future cursor was silently clamped")
	}
	if _, err := term.Read(0, ChunkBytes+1); !errors.Is(err, ErrCursor) {
		t.Fatal("oversized page accepted")
	}
	m := startManager(t, 1, 64)
	term = open(t, m)
	write(t, term, "exit\n")
	<-term.Done()
	last, err := term.Read(0, ChunkBytes)
	if err != nil || !last.Status.Exited {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	again, err := term.Read(last.End, ChunkBytes)
	if err != nil || again.End != last.End || len(again.Data) != 0 {
		t.Fatal("output advanced after final exit")
	}
}

func TestEvictionRetiresCapturedAttachmentHandle(t *testing.T) {
	m := startManager(t, 1, RingBytes)
	options := testOptions(t)
	options.Args = []string{"-c", "exit 0"}
	previous, err := m.Open(options)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-previous.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("terminal did not exit")
	}
	_ = open(t, m)
	// This is the handle Attach can capture before the replacement Open takes
	// the manager lock. It must not start a new drain after its slot is released.
	m.wg.Add(1)
	_, _, err = previous.attach(0, newSink())
	if err != nil {
		m.wg.Done()
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale attachment: %v", err)
	}
}
