//go:build darwin || linux

package capability

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	ErrProcessManagerClosed = errors.New("process manager is closed")
	ErrRootStopped          = errors.New("root is stopped")
)

// ProcessOptions contains the explicit authority passed to one child.
type ProcessOptions struct {
	Cwd            string
	Env            map[string]string
	Stdin          io.Reader
	Stdout         io.Writer
	Stderr         io.Writer
	ControllingTTY bool
}

// Process is started and reaped by its manager; Wait may be called repeatedly.
type Process struct {
	cmd         *exec.Cmd
	done        chan struct{}
	groupDone   chan struct{}
	mu          sync.Mutex
	err         error
	groupMu     sync.Mutex
	groupClosed bool
}

func (p *Process) PID() int { return p.cmd.Process.Pid }

func (p *Process) Wait() error {
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

func (p *Process) finish(err error) {
	p.mu.Lock()
	p.err = err
	p.mu.Unlock()
	close(p.done)
}

func (p *Process) Kill() error {
	p.groupMu.Lock()
	defer p.groupMu.Unlock()
	if p.groupClosed {
		return nil
	}
	err := syscall.Kill(-p.PID(), syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func (p *Process) groupGone() bool {
	p.groupMu.Lock()
	defer p.groupMu.Unlock()
	if p.groupClosed {
		return true
	}
	if err := syscall.Kill(-p.PID(), 0); !errors.Is(err, syscall.ESRCH) {
		return false
	}
	p.groupClosed = true
	close(p.groupDone)
	return true
}

type processRoot struct {
	stopped bool
	active  map[*Process]struct{}
	stops   map[uint64]func() error
}

// ProcessManager tracks process groups by root. A descendant that deliberately
// calls setsid can escape its group; full shell authority retains that limit.
type ProcessManager struct {
	mu     sync.Mutex
	env    map[string]string
	roots  map[string]*processRoot
	nextID uint64
	closed bool
}

func NewProcessManager() *ProcessManager {
	return &ProcessManager{env: snapshotEnvironment(), roots: make(map[string]*processRoot)}
}

// Start launches a child in a new session and atomically registers it to rootID.
func (m *ProcessManager) Start(ctx context.Context, rootID, name string, args []string, opts ProcessOptions) (*Process, error) {
	if rootID == "" {
		return nil, errors.New("root ID is required")
	}
	if name == "" {
		return nil, errors.New("command is required")
	}
	if opts.Cwd == "" {
		return nil, errors.New("cwd is required")
	}
	cwd, err := filepath.Abs(opts.Cwd)
	if err != nil {
		return nil, err
	}
	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil {
		return nil, fmt.Errorf("canonicalize cwd: %w", err)
	}
	info, err := os.Stat(cwd)
	if err != nil {
		return nil, fmt.Errorf("stat cwd: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("cwd is not a directory: %s", cwd)
	}
	if opts.Stdin == nil || opts.Stdout == nil || opts.Stderr == nil {
		return nil, errors.New("stdin, stdout, and stderr are required")
	}
	env, err := m.environment(opts.Env)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(context.WithoutCancel(ctx), name, args...) //nolint:gosec // callers authorize the executable through the capability ledger
	cmd.Dir = cwd
	cmd.Env = env
	cmd.Stdin = opts.Stdin
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr
	cmd.ExtraFiles = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: opts.ControllingTTY}
	p := &Process{cmd: cmd, done: make(chan struct{}), groupDone: make(chan struct{})}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, ErrProcessManagerClosed
	}
	root := m.roots[rootID]
	if root == nil {
		root = &processRoot{active: make(map[*Process]struct{}), stops: make(map[uint64]func() error)}
		m.roots[rootID] = root
	}
	if root.stopped {
		m.mu.Unlock()
		return nil, ErrRootStopped
	}
	if err := ctx.Err(); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	if err := markOpenDescriptorsCloseOnExec(); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	root.active[p] = struct{}{}
	m.mu.Unlock()

	go m.wait(ctx, root, p)
	return p, nil
}

// StartPiped starts a managed child and returns its stdin and stdout pipes.
func (m *ProcessManager) StartPiped(ctx context.Context, rootID, name string, args []string, opts ProcessOptions) (*Process, io.WriteCloser, io.ReadCloser, error) {
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		return nil, nil, nil, err
	}
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		_ = stdinR.Close()
		_ = stdinW.Close()
		return nil, nil, nil, err
	}
	opts.Stdin, opts.Stdout = stdinR, stdoutW
	process, err := m.Start(ctx, rootID, name, args, opts)
	_ = stdinR.Close()
	_ = stdoutW.Close()
	if err != nil {
		_ = stdinW.Close()
		_ = stdoutR.Close()
		return nil, nil, nil, err
	}
	return process, stdinW, stdoutR, nil
}

func (m *ProcessManager) wait(ctx context.Context, root *processRoot, p *Process) {
	go func() {
		select {
		case <-ctx.Done():
			_ = p.Kill()
		case <-p.groupDone:
		}
	}()
	err := p.cmd.Wait()
	p.finish(err)
	for !p.groupGone() {
		time.Sleep(10 * time.Millisecond)
	}
	m.mu.Lock()
	delete(root.active, p)
	m.mu.Unlock()
}

// RegisterStop attaches a dependency-owned child process to a root. The stop
// callback must synchronously stop and reap that child.
func (m *ProcessManager) RegisterStop(rootID string, stop func() error) (func(), error) {
	if rootID == "" || stop == nil {
		return nil, errors.New("root ID and stop callback are required")
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, ErrProcessManagerClosed
	}
	root := m.roots[rootID]
	if root == nil {
		root = &processRoot{active: make(map[*Process]struct{}), stops: make(map[uint64]func() error)}
		m.roots[rootID] = root
	}
	if root.stopped {
		m.mu.Unlock()
		return nil, ErrRootStopped
	}
	m.nextID++
	id := m.nextID
	root.stops[id] = stop
	m.mu.Unlock()
	return sync.OnceFunc(func() {
		m.mu.Lock()
		delete(root.stops, id)
		m.mu.Unlock()
	}), nil
}

// StopRoot blocks new starts while it stops and waits for rootID's processes.
// The root may be activated again after this call returns.
func (m *ProcessManager) StopRoot(rootID string) error {
	if rootID == "" {
		return errors.New("root ID is required")
	}
	m.mu.Lock()
	root := m.roots[rootID]
	if root == nil {
		root = &processRoot{active: make(map[*Process]struct{}), stops: make(map[uint64]func() error)}
		m.roots[rootID] = root
	}
	root.stopped = true
	processes := activeProcesses(root)
	stops := rootStops(root)
	m.mu.Unlock()
	err := errors.Join(runStops(stops), stopProcesses(processes))
	m.mu.Lock()
	if m.roots[rootID] == root {
		delete(m.roots, rootID)
	}
	m.mu.Unlock()
	return err
}

// Close prevents every later start and stops all currently tracked roots.
func (m *ProcessManager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	var processes []*Process
	var stops []func() error
	for _, root := range m.roots {
		root.stopped = true
		processes = append(processes, activeProcesses(root)...)
		stops = append(stops, rootStops(root)...)
	}
	m.mu.Unlock()
	return errors.Join(runStops(stops), stopProcesses(processes))
}

func rootStops(root *processRoot) []func() error {
	stops := make([]func() error, 0, len(root.stops))
	for id, stop := range root.stops {
		stops = append(stops, stop)
		delete(root.stops, id)
	}
	return stops
}

func runStops(stops []func() error) error {
	var err error
	for _, stop := range stops {
		err = errors.Join(err, runStop(stop))
	}
	return err
}

func runStop(stop func() error) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("dependency stop panic: %v", value)
		}
	}()
	return stop()
}

func activeProcesses(root *processRoot) []*Process {
	processes := make([]*Process, 0, len(root.active))
	for process := range root.active {
		processes = append(processes, process)
	}
	return processes
}

func stopProcesses(processes []*Process) error {
	var errs []error
	for _, process := range processes {
		if err := process.Kill(); err != nil {
			errs = append(errs, err)
		}
	}
	for _, process := range processes {
		<-process.groupDone
	}
	return errors.Join(errs...)
}

func snapshotEnvironment() map[string]string {
	env := make(map[string]string)
	for _, entry := range os.Environ() {
		name, value, ok := strings.Cut(entry, "=")
		if ok && allowedBaseEnvironment(name) {
			env[name] = value
		}
	}
	return env
}

func allowedBaseEnvironment(name string) bool {
	switch name {
	case "HOME", "PATH", "SHELL", "USER", "LOGNAME",
		"TMPDIR", "TMP", "TEMP", "LANG", "LANGUAGE",
		"TERM", "COLORTERM", "NO_COLOR":
		return true
	}
	return strings.HasPrefix(name, "LC_")
}

func (m *ProcessManager) environment(overrides map[string]string) ([]string, error) {
	values := make(map[string]string, len(m.env)+len(overrides))
	maps.Copy(values, m.env)
	for name, value := range overrides {
		if !validEnvironmentName(name) {
			return nil, fmt.Errorf("invalid environment name %q", name)
		}
		if strings.IndexByte(value, 0) >= 0 {
			return nil, fmt.Errorf("environment value for %q contains NUL", name)
		}
		values[name] = value
	}
	env := make([]string, 0, len(values))
	for name, value := range values {
		env = append(env, name+"="+value)
	}
	sort.Strings(env)
	return env, nil
}

// ChildEnvironment returns the same allowlisted environment used by managed
// children for launchers that own their exec.Cmd internally.
func (m *ProcessManager) ChildEnvironment(overrides map[string]string) ([]string, error) {
	if err := markOpenDescriptorsCloseOnExec(); err != nil {
		return nil, err
	}
	return m.environment(overrides)
}

func validEnvironmentName(name string) bool {
	if name == "" || !environmentLetter(name[0]) {
		return false
	}
	for i := 1; i < len(name); i++ {
		if !environmentLetter(name[i]) && (name[i] < '0' || name[i] > '9') {
			return false
		}
	}
	return true
}

func environmentLetter(char byte) bool {
	return char == '_' || char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z'
}

func markOpenDescriptorsCloseOnExec() error {
	dir := "/proc/self/fd"
	if runtime.GOOS == "darwin" {
		dir = "/dev/fd"
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("list open descriptors: %w", err)
	}
	for _, entry := range entries {
		fd, err := strconv.Atoi(entry.Name())
		if err == nil && fd > 2 {
			syscall.CloseOnExec(fd)
		}
	}
	return nil
}
