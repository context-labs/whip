// Package terminal owns the login shells behind workspace terminal tabs: one
// PTY per terminal, a bounded replay ring, and a single live attachment that
// receives ordered output. It knows nothing about JSON-RPC; a transport adapts
// a connection to Sink. The shell runs where the host command runs, so a terminal on
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
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/context-labs/whip/internal/capability"
)

const (
	// MaxTerminals bounds live plus retained exited terminals per host command.
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
	Output(ctx context.Context, id string, cursor int64, data []byte) bool
	Exited(id string, code int, signal string)
	Detached(id string)
}

// Options describes the shell to start. Shell and Env are resolved by the
// host, never taken from client input.
type Options struct {
	Shell string
	Args  []string
	Cwd   string
	Env   map[string]string
	Cols  uint16
	Rows  uint16
}

func (o Options) validate() error {
	switch {
	case !filepath.IsAbs(o.Shell):
		return errors.New("terminal requires an absolute host-resolved shell")
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
	Closing    bool
	Start, End int64
	CreatedAt  time.Time
	ID         string
	Cwd        string
	Shell      string
	Cols       uint16
	Rows       uint16
	Exited     bool
	ExitCode   int
	Signal     string
}

// Manager owns the host command's terminals within one limit.
type Manager struct {
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	terminals map[string]*Terminal
	limit     int
	ringBytes int
	closed    bool
	processes *capability.ProcessManager
	shutdown  sync.Once
	wg        sync.WaitGroup
}

// NewManager creates a manager whose terminals end when ctx ends or Shutdown runs.
func NewManager(ctx context.Context) *Manager { return newManager(ctx, MaxTerminals, RingBytes) }

func newManager(ctx context.Context, limit, ringBytes int) *Manager {
	ctx, cancel := context.WithCancel(ctx)
	return &Manager{ctx: ctx, cancel: cancel, terminals: make(map[string]*Terminal), limit: limit, ringBytes: ringBytes, processes: capability.NewProcessManager()}
}

// Terminal is one shell on one PTY.
type Terminal struct {
	ID          string
	manager     *Manager
	cancel      context.CancelFunc
	process     *capability.Process
	writeSlot   chan struct{}
	writes      sync.WaitGroup
	stopContext func() bool
	contextDone chan struct{}
	hangupOnce  sync.Once
	foreground  int
	closing     bool
	ptmx        *os.File
	ring        *ring
	options     Options

	readDone chan struct{} // closed by the reader when the master is unreadable
	done     chan struct{} // closed by the waiter after exit status and reader are settled

	mu        sync.Mutex
	attachMu  sync.Mutex
	retired   bool
	cols      uint16
	rows      uint16
	attached  *attachment
	retiring  *attachment
	exit      *exitStatus
	exitedAt  time.Time
	createdAt time.Time
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
	ctx      context.Context
	cancel   context.CancelFunc
}

func (a *attachment) halt() { a.stopOnce.Do(func() { a.cancel(); close(a.stop) }) }

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
	if m.closed || m.ctx.Err() != nil {
		return nil, ErrClosed
	}
	if len(m.terminals) >= m.limit && !m.evictExitedLocked() {
		return nil, ErrLimit
	}
	ctx, cancel := context.WithCancel(context.WithoutCancel(m.ctx))
	t := &Terminal{
		ID: "term-" + strings.ToLower(rand.Text()), createdAt: time.Now().UTC(), manager: m, cancel: cancel, ring: newRing(m.ringBytes), options: options,
		cols: options.Cols, rows: options.Rows, readDone: make(chan struct{}), done: make(chan struct{}), writeSlot: make(chan struct{}, 1), contextDone: make(chan struct{}),
	}
	options.Args, options.Env = slices.Clone(options.Args), maps.Clone(options.Env)
	ptmx, tty, err := openPTY(options.Cols, options.Rows)
	if err != nil {
		cancel()
		return nil, err
	}
	process, err := m.processes.Start(ctx, t.ID, options.Shell, options.Args, capability.ProcessOptions{Cwd: options.Cwd, Env: options.Env, Stdin: tty, Stdout: tty, Stderr: tty, ControllingTTY: true})
	_ = tty.Close()
	if err != nil {
		cancel()
		_ = ptmx.Close()
		_ = m.processes.StopRoot(t.ID)
		return nil, fmt.Errorf("start terminal shell: %w", err)
	}
	t.process, t.ptmx = process, ptmx
	t.stopContext = context.AfterFunc(m.ctx, func() { defer close(t.contextDone); t.hangup() })
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
		exited, at := t.evictableLocked(), t.exitedAt
		t.mu.Unlock()
		if exited && (oldest == nil || at.Before(oldest.exitedAt)) {
			oldest = t
		}
	}
	if oldest == nil {
		return false
	}
	oldest.mu.Lock()
	defer oldest.mu.Unlock()
	// An attachment may have captured this handle before Open took m.mu.
	// Recheck and retire under its lock so it cannot attach after eviction.
	if !oldest.evictableLocked() {
		return false
	}
	oldest.retired = true
	delete(m.terminals, oldest.ID)
	oldest.cancel()
	return true
}

func (t *Terminal) evictableLocked() bool {
	if t.exit == nil {
		return false
	}
	for _, attached := range []*attachment{t.attached, t.retiring} {
		if attached != nil {
			select {
			case <-attached.drained:
			default:
				return false
			}
		}
	}
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
	m.mu.Lock()
	t, ok := m.terminals[id]
	if !ok {
		m.mu.Unlock()
		return Status{}, 0, ErrNotFound
	}
	if m.closed {
		m.mu.Unlock()
		return Status{}, 0, ErrClosed
	}
	m.wg.Add(1)
	m.mu.Unlock()
	status, from, err := t.attach(cursor, sink)
	if err != nil {
		m.wg.Done()
	}
	return status, from, err
}

func (t *Terminal) attach(cursor int64, sink Sink) (Status, int64, error) {
	t.attachMu.Lock()
	defer t.attachMu.Unlock()
	t.mu.Lock()
	if t.retired {
		t.mu.Unlock()
		return Status{}, 0, ErrNotFound
	}
	previous := t.attached
	retiring := t.retiring
	t.mu.Unlock()
	if retiring != nil && retiring != previous {
		retiring.halt()
		<-retiring.drained
	}
	if previous != nil {
		previous.halt()
		<-previous.drained
		if previous.sink != sink {
			previous.sink.Detached(t.ID)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	a := &attachment{ctx: ctx, cancel: cancel, sink: sink, queue: make(chan chunk, queueChunks), stop: make(chan struct{}), drained: make(chan struct{})}
	t.mu.Lock()
	if t.retired {
		t.mu.Unlock()
		cancel()
		return Status{}, 0, ErrNotFound
	}
	t.attached = a
	t.retiring = nil
	data, from := t.ring.read(cursor)
	for offset := 0; offset < len(data); offset += ChunkBytes {
		end := min(offset+ChunkBytes, len(data))
		a.queue <- chunk{cursor: from + int64(offset), data: data[offset:end]}
	}
	status := t.statusLocked()
	t.mu.Unlock()
	go t.drain(a)
	return status, from, nil
}

// Detach drops every attachment held by sink, for a connection that went away.
// The shells keep running and buffering.
func (m *Manager) Detach(sink Sink) {
	for _, t := range m.snapshot() {
		t.mu.Lock()
		a := t.attached
		if a != nil && a.sink == sink {
			t.attached = nil
			t.retiring = a
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
		t.mu.Lock()
		t.retired = true
		t.mu.Unlock()
	}
	m.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	t.hangup()
	<-t.done
	t.detachAndJoin()
	m.mu.Lock()
	if m.terminals[id] == t {
		delete(m.terminals, id)
	}
	m.mu.Unlock()
	return nil
}

// Shutdown hangs up every shell and waits for their goroutines.
func (m *Manager) Shutdown() {
	m.shutdown.Do(func() {
		m.mu.Lock()
		m.closed = true
		terminals := make([]*Terminal, 0, len(m.terminals))
		for _, t := range m.terminals {
			t.mu.Lock()
			t.retired = true
			t.mu.Unlock()
			terminals = append(terminals, t)
		}
		m.mu.Unlock()
		for _, t := range terminals {
			t.hangup()
		}
		m.cancel()
		_ = m.processes.Close()
		for _, t := range terminals {
			<-t.done
			t.detachAndJoin()
		}
		m.wg.Wait()
		m.mu.Lock()
		clear(m.terminals)
		m.mu.Unlock()
	})
}

// hangup freezes human input and captures the foreground group from the owned
// controlling PTY before closing it. It never follows arbitrary process trees.
func (t *Terminal) hangup() {
	t.hangupOnce.Do(func() {
		t.mu.Lock()
		t.closing = true
		t.foreground = foregroundGroup(t.ptmx, t.process.PID())
		attached := t.attached
		foreground := t.foreground
		t.mu.Unlock()
		if attached != nil {
			attached.halt()
		}
		killForeground(foreground)
		t.cancel()
		_ = t.ptmx.Close()
	})
}

func (t *Terminal) detachAndJoin() {
	t.attachMu.Lock()
	defer t.attachMu.Unlock()
	t.mu.Lock()
	attached := t.attached
	retiring := t.retiring
	t.attached = nil
	t.retiring = nil
	t.mu.Unlock()
	if retiring != nil && retiring != attached {
		retiring.halt()
		<-retiring.drained
	}
	if attached != nil {
		attached.halt()
		<-attached.drained
	}
}

// Write sends keystrokes to the shell.
func (t *Terminal) Write(data []byte) error { return t.WriteContext(context.Background(), data) }

// WriteContext serializes bounded keystroke batches and joins cancellation before
// releasing the write slot. A timed-out or partial write must never be replayed.
func (t *Terminal) WriteContext(parent context.Context, data []byte) error {
	if len(data) == 0 || len(data) > MaxWriteBytes {
		return fmt.Errorf("terminal write requires 1..%d bytes", MaxWriteBytes)
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	select {
	case t.writeSlot <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	case <-t.done:
		return ErrExited
	}
	defer func() { <-t.writeSlot }()
	t.mu.Lock()
	if t.closing || t.exit != nil {
		t.mu.Unlock()
		return ErrExited
	}
	t.writes.Add(1)
	t.mu.Unlock()
	defer t.writes.Done()
	deadline, _ := ctx.Deadline()
	if err := t.ptmx.SetWriteDeadline(deadline); err != nil {
		return err
	}
	joined := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { defer close(joined); _ = t.ptmx.SetWriteDeadline(time.Now()) })
	n, err := t.ptmx.Write(data)
	if !stop() {
		<-joined
	}
	_ = t.ptmx.SetWriteDeadline(time.Time{})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if err == nil && n != len(data) {
		return io.ErrShortWrite
	}
	return err
}

// Resize changes the PTY window size; the shell receives SIGWINCH.
func (t *Terminal) Resize(cols, rows uint16) error {
	if cols == 0 || rows == 0 || cols > MaxDimension || rows > MaxDimension {
		return fmt.Errorf("terminal size must be within 1..%d columns and rows", MaxDimension)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closing || t.exit != nil {
		return ErrExited
	}
	if err := setSize(t.ptmx, cols, rows); err != nil {
		return err
	}
	t.cols, t.rows = cols, rows
	return nil
}

// Status reports the terminal's current size and exit state.
func (t *Terminal) Status() Status {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.statusLocked()
}

func (t *Terminal) statusLocked() Status {
	status := Status{Closing: t.closing && t.exit == nil, Start: t.ring.start, End: t.ring.end, CreatedAt: t.createdAt, ID: t.ID, Cwd: t.options.Cwd, Shell: t.options.Shell, Cols: t.cols, Rows: t.rows}
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
	err := t.process.Wait()
	status := exitStatusOf(err)
	// The leader's exit does not release its remaining group. Capture the current
	// foreground job while the PTY still establishes its session ownership.
	t.mu.Lock()
	t.closing = true
	if t.foreground == 0 {
		t.foreground = foregroundGroup(t.ptmx, t.process.PID())
	}
	foreground := t.foreground
	t.mu.Unlock()
	killForeground(foreground)
	t.process.Stop()
	joinForeground(foreground)
	_ = t.manager.processes.StopRoot(t.ID)
	timer := time.NewTimer(closeGrace)
	select {
	case <-t.readDone:
		timer.Stop()
	case <-timer.C:
		t.mu.Lock()
		attached := t.attached
		t.mu.Unlock()
		if attached != nil {
			attached.halt()
		}
	}
	_ = t.ptmx.Close()
	<-t.readDone
	t.writes.Wait()
	t.cancel()
	if !t.stopContext() {
		<-t.contextDone
	}
	t.mu.Lock()
	t.exit, t.exitedAt = &status, time.Now()
	t.mu.Unlock()
	close(t.done)
}

func exitStatusOf(err error) exitStatus {
	if err == nil {
		return exitStatus{}
	}
	if exited, ok := errors.AsType[*exec.ExitError](err); ok {
		if status, ok := exited.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return exitStatus{code: -1, signal: status.Signal().String()}
		}
		return exitStatus{code: exited.ExitCode()}
	}
	return exitStatus{code: -1, signal: "process wait failed"}
}

// drain delivers one attachment's chunks in order, then the exit status once
// the terminal is done and the queue is empty. Queued output always wins over
// the done signal so a replayed exited terminal never reports exit early.
func (t *Terminal) drain(a *attachment) {
	defer t.manager.wg.Done()
	defer close(a.drained)
	defer a.cancel()
	deliver := func(c chunk) bool {
		if a.sink.Output(a.ctx, t.ID, c.cursor, c.data) {
			return true
		}
		t.mu.Lock()
		if t.attached == a {
			t.attached = nil
			t.retiring = a
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
