// Package terminal owns the login shells behind workspace terminal tabs: one
// PTY per terminal, a bounded replay ring, and a single live attachment that
// receives ordered output. It knows nothing about JSON-RPC; the daemon adapts
// a connection to Sink. The shell runs where the daemon runs, so a terminal on
// an SSH or URL host is a shell on that machine, beside the agent's files.
//
// Ownership: Manager owns every Terminal; a Terminal owns its reader and
// waiter goroutines and closes done exactly once after both return; each
// attachment owns one drain goroutine that stops on detach or exit.
package terminal

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

const (
	// MaxTerminals bounds live plus retained exited terminals per daemon.
	MaxTerminals = 16
	// RingBytes is the replay ring each terminal retains for reattachment.
	RingBytes = 1 << 20
	// ChunkBytes is the largest single output delivery.
	ChunkBytes = 32 << 10
	// MaxWriteBytes bounds one keystroke batch.
	MaxWriteBytes = 16 << 10
	// MaxDimension bounds cols and rows.
	MaxDimension = 1000
	// queueChunks holds a full ring replay twice over, so attach never blocks
	// while it seeds the queue under the terminal lock. Capacity is the contract.
	queueChunks = 2 * RingBytes / ChunkBytes
	killGrace   = 2 * time.Second
	closeGrace  = 500 * time.Millisecond
)

var (
	ErrNotFound = errors.New("terminal not found")
	ErrLimit    = errors.New("terminal limit reached")
	ErrExited   = errors.New("terminal has exited")
	ErrClosed   = errors.New("terminal manager is closed")
)

// Sink receives one attachment's ordered output. Output returns false when the
// receiver is gone; the terminal then detaches and keeps buffering. Output may
// block to apply backpressure: the drain goroutine waits, the queue fills, and
// the PTY reader stalls the shell as a slow physical terminal would. Exited
// follows the last Output of an exited terminal. Detached tells a sink that a
// later attachment replaced it.
type Sink interface {
	Output(id string, cursor int64, data []byte) bool
	Exited(id string, code int, signal string)
	Detached(id string)
}

// Options describes the shell to start. Shell and Env are resolved by the
// daemon, never taken from client input.
type Options struct {
	Shell string
	Args  []string
	Cwd   string
	Env   []string
	Cols  uint16
	Rows  uint16
}

func (o Options) validate() error {
	switch {
	case o.Shell == "":
		return errors.New("terminal requires a shell")
	case !filepath.IsAbs(o.Cwd):
		return errors.New("terminal requires an absolute working directory")
	case o.Cols == 0 || o.Rows == 0 || o.Cols > MaxDimension || o.Rows > MaxDimension:
		return fmt.Errorf("terminal size must be within 1..%d columns and rows", MaxDimension)
	}
	info, err := os.Stat(o.Cwd)
	if err != nil {
		return fmt.Errorf("terminal working directory: %w", err)
	}
	if !info.IsDir() {
		return errors.New("terminal working directory is not a directory")
	}
	return nil
}

// Status is a point-in-time view of one terminal.
type Status struct {
	ID       string
	Cwd      string
	Shell    string
	Cols     uint16
	Rows     uint16
	Exited   bool
	ExitCode int
	Signal   string
}

// Manager owns the daemon's terminals within one limit.
type Manager struct {
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	terminals map[string]*Terminal
	limit     int
	ringBytes int
	closed    bool
	wg        sync.WaitGroup
}

// NewManager creates a manager whose terminals end when ctx ends or Shutdown runs.
func NewManager(ctx context.Context) *Manager { return newManager(ctx, MaxTerminals, RingBytes) }

func newManager(ctx context.Context, limit, ringBytes int) *Manager {
	ctx, cancel := context.WithCancel(ctx)
	return &Manager{ctx: ctx, cancel: cancel, terminals: make(map[string]*Terminal), limit: limit, ringBytes: ringBytes}
}

// Terminal is one shell on one PTY.
type Terminal struct {
	ID      string
	manager *Manager
	cancel  context.CancelFunc
	cmd     *exec.Cmd
	ptmx    *os.File
	ring    *ring
	options Options

	readDone chan struct{} // closed by the reader when the master is unreadable
	done     chan struct{} // closed by the waiter after exit status and reader are settled

	mu       sync.Mutex
	cols     uint16
	rows     uint16
	attached *attachment
	exit     *exitStatus
	exitedAt time.Time
}

type exitStatus struct {
	code   int
	signal string
}

type chunk struct {
	cursor int64
	data   []byte
}

type attachment struct {
	sink     Sink
	queue    chan chunk
	stop     chan struct{}
	drained  chan struct{}
	stopOnce sync.Once
}

func (a *attachment) halt() { a.stopOnce.Do(func() { close(a.stop) }) }

// push hands a chunk to the drain goroutine. A full queue blocks the caller,
// which is the PTY reader, so a slow receiver stalls the shell the way a slow
// physical terminal would. Halting the attachment releases the caller.
func (a *attachment) push(c chunk) {
	select {
	case a.queue <- c:
	case <-a.stop:
	}
}

// Open starts a shell. At the limit it evicts the oldest exited terminal first.
func (m *Manager) Open(options Options) (*Terminal, error) {
	if err := options.validate(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrClosed
	}
	if len(m.terminals) >= m.limit && !m.evictExitedLocked() {
		return nil, ErrLimit
	}
	ctx, cancel := context.WithCancel(m.ctx)
	t := &Terminal{ID: "term-" + strings.ToLower(rand.Text()), manager: m, cancel: cancel, ring: newRing(m.ringBytes), options: options,
		cols: options.Cols, rows: options.Rows, readDone: make(chan struct{}), done: make(chan struct{})}
	cmd := exec.CommandContext(ctx, options.Shell, options.Args...) //nolint:gosec // G204: the shell is the operator's login shell resolved by the daemon, never client input.
	cmd.Dir = options.Cwd
	cmd.Env = options.Env
	// Cancel hangs up the whole session like a closing terminal window; WaitDelay
	// escalates to SIGKILL for a shell that ignores SIGHUP.
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGHUP) }
	cmd.WaitDelay = killGrace
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: options.Rows, Cols: options.Cols})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("start %s: %w", options.Shell, err)
	}
	t.cmd, t.ptmx = cmd, ptmx
	m.terminals[t.ID] = t
	m.wg.Add(2)
	go t.read()
	go t.wait()
	return t, nil
}

func (m *Manager) evictExitedLocked() bool {
	var oldest *Terminal
	for _, t := range m.terminals {
		t.mu.Lock()
		exited, at := t.exit != nil, t.exitedAt
		t.mu.Unlock()
		if exited && (oldest == nil || at.Before(oldest.exitedAt)) {
			oldest = t
		}
	}
	if oldest == nil {
		return false
	}
	delete(m.terminals, oldest.ID)
	oldest.cancel()
	return true
}

// Get returns the terminal with id.
func (m *Manager) Get(id string) (*Terminal, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.terminals[id]
	return t, ok
}

// Attach makes sink the terminal's live receiver, replays retained output
// from cursor first, and returns the cursor of the first replayed byte.
func (m *Manager) Attach(id string, cursor int64, sink Sink) (Status, int64, error) {
	t, ok := m.Get(id)
	if !ok {
		return Status{}, 0, ErrNotFound
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return Status{}, 0, ErrClosed
	}
	m.wg.Add(1)
	m.mu.Unlock()
	status, from, previous := t.attach(cursor, sink)
	if previous != nil {
		previous.halt()
		<-previous.drained
		if previous.sink != sink {
			previous.sink.Detached(t.ID)
		}
	}
	return status, from, nil
}

func (t *Terminal) attach(cursor int64, sink Sink) (Status, int64, *attachment) {
	a := &attachment{sink: sink, queue: make(chan chunk, queueChunks), stop: make(chan struct{}), drained: make(chan struct{})}
	t.mu.Lock()
	previous := t.attached
	t.attached = a
	data, from := t.ring.read(cursor)
	for offset := 0; offset < len(data); offset += ChunkBytes {
		end := min(offset+ChunkBytes, len(data))
		a.queue <- chunk{cursor: from + int64(offset), data: data[offset:end]}
	}
	status := t.statusLocked()
	t.mu.Unlock()
	go t.drain(a)
	return status, from, previous
}

// Detach drops every attachment held by sink, for a connection that went away.
// The shells keep running and buffering.
func (m *Manager) Detach(sink Sink) {
	for _, t := range m.snapshot() {
		t.mu.Lock()
		a := t.attached
		if a != nil && a.sink == sink {
			t.attached = nil
		} else {
			a = nil
		}
		t.mu.Unlock()
		if a != nil {
			a.halt()
		}
	}
}

func (m *Manager) snapshot() []*Terminal {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*Terminal, 0, len(m.terminals))
	for _, t := range m.terminals {
		result = append(result, t)
	}
	return result
}

// Close hangs up the shell and forgets the terminal once it has exited.
func (m *Manager) Close(id string) error {
	m.mu.Lock()
	t, ok := m.terminals[id]
	if ok {
		delete(m.terminals, id)
	}
	m.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	t.hangup()
	<-t.done
	return nil
}

// Shutdown hangs up every shell and waits for their goroutines.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	m.closed = true
	terminals := make([]*Terminal, 0, len(m.terminals))
	for _, t := range m.terminals {
		terminals = append(terminals, t)
	}
	m.terminals = map[string]*Terminal{}
	m.mu.Unlock()
	for _, t := range terminals {
		t.hangup()
	}
	m.cancel()
	m.wg.Wait()
}

// hangup cancels the command, which signals the shell's group, and closes the
// master so the foreground job sees a tty hangup exactly as when a terminal
// window closes. The waiter still settles the exit status.
func (t *Terminal) hangup() {
	t.cancel()
	_ = t.ptmx.Close()
}

// Write sends keystrokes to the shell.
func (t *Terminal) Write(data []byte) error {
	if len(data) == 0 || len(data) > MaxWriteBytes {
		return fmt.Errorf("terminal write requires 1..%d bytes", MaxWriteBytes)
	}
	if t.Status().Exited {
		return ErrExited
	}
	_, err := t.ptmx.Write(data)
	return err
}

// Resize changes the PTY window size; the shell receives SIGWINCH.
func (t *Terminal) Resize(cols, rows uint16) error {
	if cols == 0 || rows == 0 || cols > MaxDimension || rows > MaxDimension {
		return fmt.Errorf("terminal size must be within 1..%d columns and rows", MaxDimension)
	}
	if t.Status().Exited {
		return ErrExited
	}
	if err := pty.Setsize(t.ptmx, &pty.Winsize{Rows: rows, Cols: cols}); err != nil {
		return err
	}
	t.mu.Lock()
	t.cols, t.rows = cols, rows
	t.mu.Unlock()
	return nil
}

// Status reports the terminal's current size and exit state.
func (t *Terminal) Status() Status {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.statusLocked()
}

func (t *Terminal) statusLocked() Status {
	status := Status{ID: t.ID, Cwd: t.options.Cwd, Shell: t.options.Shell, Cols: t.cols, Rows: t.rows}
	if t.exit != nil {
		status.Exited, status.ExitCode, status.Signal = true, t.exit.code, t.exit.signal
	}
	return status
}

// Done closes after the shell has exited and its output has been drained.
func (t *Terminal) Done() <-chan struct{} { return t.done }

// read pumps the master into the ring and the live attachment.
func (t *Terminal) read() {
	defer t.manager.wg.Done()
	defer close(t.readDone)
	buf := make([]byte, ChunkBytes)
	for {
		n, err := t.ptmx.Read(buf)
		if n > 0 {
			data := bytes.Clone(buf[:n])
			t.mu.Lock()
			cursor := t.ring.end
			t.ring.append(data)
			attached := t.attached
			t.mu.Unlock()
			if attached != nil {
				attached.push(chunk{cursor: cursor, data: data})
			}
		}
		if err != nil {
			return
		}
	}
}

// wait settles the exit status, gives a grandchild holding the tty a short
// grace to flush, then closes the master so the reader ends.
func (t *Terminal) wait() {
	defer t.manager.wg.Done()
	err := t.cmd.Wait()
	status := exitStatusOf(t.cmd, err)
	t.mu.Lock()
	t.exit, t.exitedAt = &status, time.Now()
	t.mu.Unlock()
	timer := time.NewTimer(closeGrace)
	select {
	case <-t.readDone:
		timer.Stop()
	case <-timer.C:
	}
	_ = t.ptmx.Close()
	<-t.readDone
	close(t.done)
}

func exitStatusOf(cmd *exec.Cmd, err error) exitStatus {
	state := cmd.ProcessState
	if state == nil {
		message := "exited before wait"
		if err != nil {
			message = err.Error()
		}
		return exitStatus{code: -1, signal: message}
	}
	if ws, ok := state.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return exitStatus{code: -1, signal: ws.Signal().String()}
	}
	return exitStatus{code: state.ExitCode()}
}

// drain delivers one attachment's chunks in order, then the exit status once
// the terminal is done and the queue is empty. Queued output always wins over
// the done signal so a replayed exited terminal never reports exit early.
func (t *Terminal) drain(a *attachment) {
	defer t.manager.wg.Done()
	defer close(a.drained)
	deliver := func(c chunk) bool {
		if a.sink.Output(t.ID, c.cursor, c.data) {
			return true
		}
		t.mu.Lock()
		if t.attached == a {
			t.attached = nil
		}
		t.mu.Unlock()
		a.halt()
		return false
	}
	for {
		// A halted attachment stops before the next delivery, even with queued
		// output: its receiver is gone or has been replaced.
		select {
		case <-a.stop:
			return
		default:
		}
		select {
		case c := <-a.queue:
			if !deliver(c) {
				return
			}
			continue
		default:
		}
		select {
		case c := <-a.queue:
			if !deliver(c) {
				return
			}
		case <-a.stop:
			return
		case <-t.done:
			for {
				select {
				case c := <-a.queue:
					if !deliver(c) {
						return
					}
				default:
					status := t.Status()
					a.sink.Exited(t.ID, status.ExitCode, status.Signal)
					return
				}
			}
		}
	}
}
