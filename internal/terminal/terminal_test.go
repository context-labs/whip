package terminal

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const shell = "/bin/sh"

// recordingSink collects output in order and reports lifecycle callbacks.
type recordingSink struct {
	mu       sync.Mutex
	out      bytes.Buffer
	cursors  []int64
	sizes    []int
	exited   chan exitStatus
	detached chan string
	block    chan struct{} // when set, Output blocks until it closes
	refuse   bool
}

func newSink() *recordingSink {
	return &recordingSink{exited: make(chan exitStatus, 1), detached: make(chan string, 4)}
}

func (s *recordingSink) Output(_ string, cursor int64, data []byte) bool {
	if s.block != nil {
		<-s.block
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refuse {
		return false
	}
	s.cursors = append(s.cursors, cursor)
	s.sizes = append(s.sizes, len(data))
	s.out.Write(data)
	return true
}

func (s *recordingSink) Exited(_ string, code int, signal string) {
	s.exited <- exitStatus{code: code, signal: signal}
}

func (s *recordingSink) Detached(id string) { s.detached <- id }

func (s *recordingSink) text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.out.String()
}

func (s *recordingSink) wait(t *testing.T, substr string) string {
	t.Helper()
	return s.waitMatch(t, regexp.MustCompile(regexp.QuoteMeta(substr)))
}

func (s *recordingSink) waitMatch(t *testing.T, pattern *regexp.Regexp) string {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if text := s.text(); pattern.MatchString(text) {
			return text
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("output never matched %q; got %q", pattern, s.text())
	return ""
}

func testOptions(t *testing.T) Options {
	t.Helper()
	dir := t.TempDir()
	return Options{
		Shell: shell, Cwd: dir, Cols: 80, Rows: 24,
		Env: []string{"PATH=/usr/bin:/bin", "TERM=dumb", "HOME=" + dir, "PS1=$ ", "ENV=", "BASH_ENV="},
	}
}

func startManager(t *testing.T, limit, ringBytes int) *Manager {
	t.Helper()
	m := newManager(t.Context(), limit, ringBytes)
	t.Cleanup(m.Shutdown)
	return m
}

func open(t *testing.T, m *Manager) *Terminal {
	t.Helper()
	term, err := m.Open(testOptions(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Reap the shell before its temporary working directory is removed.
	t.Cleanup(func() { _ = m.Close(term.ID) })
	return term
}

func write(t *testing.T, term *Terminal, text string) {
	t.Helper()
	if err := term.Write([]byte(text)); err != nil {
		t.Fatalf("write %q: %v", text, err)
	}
}

func TestOpenValidatesOptions(t *testing.T) {
	m := startManager(t, 2, RingBytes)
	good := testOptions(t)
	for name, mutate := range map[string]func(*Options){
		"shell":    func(o *Options) { o.Shell = "" },
		"relative": func(o *Options) { o.Cwd = "relative" },
		"missing":  func(o *Options) { o.Cwd = o.Cwd + "/missing" },
		"cols":     func(o *Options) { o.Cols = 0 },
		"rows":     func(o *Options) { o.Rows = MaxDimension + 1 },
	} {
		options := good
		mutate(&options)
		if _, err := m.Open(options); err == nil {
			t.Errorf("%s: open accepted invalid options", name)
		}
	}
}

func TestShellEchoesInputAndReportsExit(t *testing.T) {
	m := startManager(t, 2, RingBytes)
	term := open(t, m)
	sink := newSink()
	status, from, err := m.Attach(term.ID, 0, sink)
	if err != nil || from != 0 || status.Exited || status.Cols != 80 || status.Rows != 24 {
		t.Fatalf("attach = %+v, %d, %v", status, from, err)
	}
	write(t, term, "echo whip-$((20+22))\n")
	sink.wait(t, "whip-42")
	write(t, term, "exit 7\n")
	select {
	case exit := <-sink.exited:
		if exit.code != 7 || exit.signal != "" {
			t.Fatalf("exit = %+v", exit)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("exit was never reported")
	}
	if !term.Status().Exited || term.Status().ExitCode != 7 {
		t.Fatalf("status after exit = %+v", term.Status())
	}
	if err := term.Write([]byte("x")); !errors.Is(err, ErrExited) {
		t.Fatalf("write after exit = %v", err)
	}
	// Cursors are contiguous: each chunk starts where the previous ended.
	sink.mu.Lock()
	defer sink.mu.Unlock()
	var expected int64
	for i, cursor := range sink.cursors {
		if cursor != expected {
			t.Fatalf("chunk %d cursor %d, want %d", i, cursor, expected)
		}
		expected = cursor + int64(sink.sizes[i])
	}
	if expected == 0 {
		t.Fatal("no output chunks were recorded")
	}
}

func TestResizeIsVisibleToTheShell(t *testing.T) {
	m := startManager(t, 2, RingBytes)
	term := open(t, m)
	sink := newSink()
	if _, _, err := m.Attach(term.ID, 0, sink); err != nil {
		t.Fatal(err)
	}
	if err := term.Resize(100, 40); err != nil {
		t.Fatalf("resize: %v", err)
	}
	write(t, term, "stty size\n")
	sink.wait(t, "40 100")
	if status := term.Status(); status.Cols != 100 || status.Rows != 40 {
		t.Fatalf("status size = %dx%d", status.Cols, status.Rows)
	}
	if err := term.Resize(0, 10); err == nil {
		t.Fatal("resize accepted zero columns")
	}
}

func TestReattachReplaysFromCursorAndClampsToRing(t *testing.T) {
	m := startManager(t, 2, 256)
	term := open(t, m)
	first := newSink()
	if _, _, err := m.Attach(term.ID, 0, first); err != nil {
		t.Fatal(err)
	}
	// Match executed output, not PTY input echo; suppress later prompts so the
	// completion markers also identify the end of the retained output.
	write(t, term, "PS1=''; stty -echo; echo first-$((1+1))-marker\n")
	first.wait(t, "first-2-marker\r\n")
	m.Detach(first)
	seen := int64(len(first.text()))
	// Output while detached is retained, not lost.
	write(t, term, "echo second-marker\n")
	second := newSink()
	deadline := time.Now().Add(15 * time.Second)
	var text string
	for time.Now().Before(deadline) {
		status, from, err := m.Attach(term.ID, seen, second)
		if err != nil || status.Exited {
			t.Fatalf("attach = %+v, %v", status, err)
		}
		term.mu.Lock()
		ringStart := term.ring.start
		term.mu.Unlock()
		if from != seen && from != ringStart {
			t.Fatalf("replay started at %d, want %d or the ring start", from, seen)
		}
		if text = second.text(); strings.Contains(text, "second-marker") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(text, "second-marker") {
		t.Fatalf("replay missed output written while detached: %q", text)
	}
	// Overflow the 256-byte ring, then a stale cursor is clamped to its start.
	write(t, term, "printf 'x%.0s' $(seq 1 600); echo end-marker\n")
	second.wait(t, "end-marker\r\n")
	third := newSink()
	_, from, err := m.Attach(term.ID, 0, third)
	if err != nil {
		t.Fatal(err)
	}
	term.mu.Lock()
	ringStart := term.ring.start
	term.mu.Unlock()
	if from == 0 || from != ringStart {
		t.Fatalf("clamped replay started at %d, ring start %d", from, ringStart)
	}
	third.wait(t, "end-marker\r\n")
	if len(third.text()) > 256 {
		t.Fatalf("replayed %d bytes from a 256-byte ring", len(third.text()))
	}
	// The replaced attachment learns it was replaced; the replacement does not.
	select {
	case id := <-second.detached:
		if id != term.ID {
			t.Fatalf("detached id = %q", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("previous attachment was never told it was detached")
	}
	select {
	case <-third.detached:
		t.Fatal("live attachment reported detached")
	default:
	}
}

func TestReattachingTheSameSinkDoesNotReportDetached(t *testing.T) {
	m := startManager(t, 2, RingBytes)
	term := open(t, m)
	sink := newSink()
	if _, _, err := m.Attach(term.ID, 0, sink); err != nil {
		t.Fatal(err)
	}
	write(t, term, "echo again-marker\n")
	sink.wait(t, "again-marker")
	if _, _, err := m.Attach(term.ID, -1, sink); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sink.detached:
		t.Fatal("same sink reattaching was reported as detached")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestAttachingAnExitedTerminalReplaysThenReportsExit(t *testing.T) {
	m := startManager(t, 2, RingBytes)
	term := open(t, m)
	write(t, term, "echo before-exit; exit 3\n")
	select {
	case <-term.Done():
	case <-time.After(15 * time.Second):
		t.Fatal("shell never exited")
	}
	sink := newSink()
	status, _, err := m.Attach(term.ID, 0, sink)
	if err != nil || !status.Exited || status.ExitCode != 3 {
		t.Fatalf("attach exited terminal = %+v, %v", status, err)
	}
	select {
	case exit := <-sink.exited:
		if exit.code != 3 {
			t.Fatalf("exit = %+v", exit)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("exit was never reported")
	}
	if !strings.Contains(sink.text(), "before-exit") {
		t.Fatalf("replay before exit was lost: %q", sink.text())
	}
}

func TestCloseHangsUpTheForegroundJob(t *testing.T) {
	m := startManager(t, 2, RingBytes)
	term := open(t, m)
	sink := newSink()
	if _, _, err := m.Attach(term.ID, 0, sink); err != nil {
		t.Fatal(err)
	}
	// The tty echoes the typed command too, so match the child's own line.
	pattern := regexp.MustCompile(`(?m)^child-pid (\d+)\r?$`)
	write(t, term, "sh -c 'echo; echo child-pid $$; exec sleep 300'\n")
	text := sink.waitMatch(t, pattern)
	pid, _ := strconv.Atoi(pattern.FindStringSubmatch(text)[1])
	start := time.Now()
	if err := m.Close(term.ID); err != nil {
		t.Fatalf("close: %v", err)
	}
	if time.Since(start) > killGrace+5*time.Second {
		t.Fatalf("close took %s", time.Since(start))
	}
	if _, ok := m.Get(term.ID); ok {
		t.Fatal("closed terminal is still listed")
	}
	if err := m.Close(term.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second close = %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Fatalf("foreground job %d survived the hangup", pid)
}

func TestLimitRefusesThenEvictsExitedTerminals(t *testing.T) {
	m := startManager(t, 1, RingBytes)
	first := open(t, m)
	if _, err := m.Open(testOptions(t)); !errors.Is(err, ErrLimit) {
		t.Fatalf("second open = %v, want limit", err)
	}
	write(t, first, "exit\n")
	select {
	case <-first.Done():
	case <-time.After(15 * time.Second):
		t.Fatal("shell never exited")
	}
	second := open(t, m)
	if _, ok := m.Get(first.ID); ok {
		t.Fatal("exited terminal was not evicted for the new one")
	}
	if _, ok := m.Get(second.ID); !ok {
		t.Fatal("new terminal is missing")
	}
}

func TestDetachReleasesAReaderBlockedOnASlowSink(t *testing.T) {
	m := startManager(t, 2, RingBytes)
	term := open(t, m)
	slow := newSink()
	slow.block = make(chan struct{})
	if _, _, err := m.Attach(term.ID, 0, slow); err != nil {
		t.Fatal(err)
	}
	// Far more than the queue holds: the reader must block on push. Use a byte
	// stream so millions of newline translations do not dominate the test.
	write(t, term, fmt.Sprintf("head -c %d /dev/zero; echo flood-$((1+1))-done\n", 4*queueChunks*ChunkBytes))
	time.Sleep(300 * time.Millisecond)
	m.Detach(slow)
	close(slow.block)
	fresh := newSink()
	// Include retained output if the shell finished between detach and attach.
	if _, _, err := m.Attach(term.ID, 0, fresh); err != nil {
		t.Fatal(err)
	}
	// The shell was stalled by the slow receiver, not killed; it finishes now.
	fresh.wait(t, "flood-2-done")
	// Only the delivery that was in flight when the sink unblocked completes;
	// a halted attachment never drains the chunks queued behind it.
	slow.mu.Lock()
	defer slow.mu.Unlock()
	if len(slow.cursors) > 1 {
		t.Fatalf("halted attachment delivered %d chunks", len(slow.cursors))
	}
}

func TestRefusingSinkDetachesItself(t *testing.T) {
	m := startManager(t, 2, RingBytes)
	term := open(t, m)
	sink := newSink()
	sink.refuse = true
	if _, _, err := m.Attach(term.ID, 0, sink); err != nil {
		t.Fatal(err)
	}
	write(t, term, "echo refused-marker\n")
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		term.mu.Lock()
		attached := term.attached
		term.mu.Unlock()
		if attached == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("a sink that refused output stayed attached")
}

func TestShutdownEndsEveryShell(t *testing.T) {
	m := newManager(t.Context(), 4, RingBytes)
	first, second := open(t, m), open(t, m)
	m.Shutdown()
	for _, term := range []*Terminal{first, second} {
		select {
		case <-term.Done():
		default:
			t.Fatalf("terminal %s survived shutdown", term.ID)
		}
		if !term.Status().Exited {
			t.Fatalf("terminal %s has no exit status", term.ID)
		}
	}
	if _, err := m.Open(testOptions(t)); !errors.Is(err, ErrClosed) {
		t.Fatalf("open after shutdown = %v", err)
	}
	if _, _, err := m.Attach(first.ID, 0, newSink()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("attach after shutdown = %v", err)
	}
}

func TestMain(m *testing.M) {
	if _, err := os.Stat(shell); err != nil {
		fmt.Fprintln(os.Stderr, "skipping terminal tests:", err)
		os.Exit(0)
	}
	os.Exit(m.Run())
}
