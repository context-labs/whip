package rlm

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

var (
	ErrWorkerLimit  = errors.New("RLM worker limit reached")
	ErrKernelClosed = errors.New("RLM kernel closed")
	ErrMemoryLimit  = errors.New("RLM worker memory limit exceeded")
)

type Limits struct {
	Steps        uint64
	HostRequests int
	Wall         time.Duration
	MemoryBytes  uint64
	OutputBytes  int
	FrameBytes   int
	MaxWorkers   int
}

func DefaultLimits() Limits {
	return Limits{
		Steps: defaultSteps, HostRequests: defaultHostRequests, Wall: 30 * time.Second,
		MemoryBytes: defaultMemoryBytes, OutputBytes: defaultOutputBytes,
		FrameBytes: defaultFrameBytes, MaxWorkers: 4,
	}
}

func (limits Limits) normalized() Limits {
	defaults := DefaultLimits()
	if limits.Steps == 0 {
		limits.Steps = defaults.Steps
	}
	if limits.HostRequests == 0 {
		limits.HostRequests = defaults.HostRequests
	}
	if limits.Wall == 0 {
		limits.Wall = defaults.Wall
	}
	if limits.MemoryBytes == 0 {
		limits.MemoryBytes = defaults.MemoryBytes
	}
	if limits.OutputBytes == 0 {
		limits.OutputBytes = defaults.OutputBytes
	}
	if limits.FrameBytes == 0 {
		limits.FrameBytes = defaults.FrameBytes
	}
	if limits.MaxWorkers == 0 {
		limits.MaxWorkers = defaults.MaxWorkers
	}
	return limits
}

type Manager struct {
	slots chan struct{}
}

func NewManager(maxWorkers int) *Manager {
	if maxWorkers < 1 {
		maxWorkers = DefaultLimits().MaxWorkers
	}
	return &Manager{slots: make(chan struct{}, maxWorkers)}
}

func (manager *Manager) reserve() error {
	select {
	case manager.slots <- struct{}{}:
		return nil
	default:
		return ErrWorkerLimit
	}
}

func (manager *Manager) release() { <-manager.slots }

func (manager *Manager) Active() int { return len(manager.slots) }

type KernelOptions struct {
	// Command is the executable plus any hidden-mode prefix. Production uses
	// [current executable, "_kernel"]; tests may use a test helper prefix.
	Command []string
	Limits  Limits
	Manager *Manager
	Host    Host
}

type Result struct {
	Value  any    `json:"value"`
	Output string `json:"output,omitempty"`
	Steps  uint64 `json:"steps"`
}

type Kernel struct {
	mu      sync.Mutex
	command []string
	limits  Limits
	manager *Manager
	host    Host
	worker  *workerProcess
	nextID  uint64
	closed  bool
}

type workerProcess struct {
	command *exec.Cmd
	input   io.WriteCloser
	output  *bufio.Reader
	done    chan struct{}
	dir     string
	release func()
	stderr  *limitedBuffer
}

func NewKernel(options KernelOptions) (*Kernel, error) {
	limits := options.Limits.normalized()
	if limits.HostRequests < 1 || limits.Wall < time.Millisecond || limits.MemoryBytes > math.MaxInt64/4 || limits.OutputBytes < 1 || limits.FrameBytes < 1 || limits.MaxWorkers < 1 {
		return nil, errors.New("invalid RLM kernel limits")
	}
	command := append([]string(nil), options.Command...)
	if len(command) == 0 {
		executable, err := os.Executable()
		if err != nil {
			return nil, err
		}
		command = []string{executable, "_kernel"}
	}
	if options.Manager == nil {
		options.Manager = NewManager(limits.MaxWorkers)
	}
	return &Kernel{command: command, limits: limits, manager: options.Manager, host: options.Host}, nil
}

func (kernel *Kernel) Exec(ctx context.Context, code string) (Result, error) {
	kernel.mu.Lock()
	defer kernel.mu.Unlock()
	if kernel.closed {
		return Result{}, ErrKernelClosed
	}
	if err := kernel.start(); err != nil {
		return Result{}, err
	}
	kernel.nextID++
	id := kernel.nextID
	if err := writeFrame(kernel.worker.input, kernel.limits.FrameBytes, frame{Type: "eval", ID: id, Code: code}); err != nil {
		kernel.stop()
		return Result{}, err
	}
	evalCtx, cancel := context.WithTimeout(ctx, kernel.limits.Wall)
	defer cancel()
	for {
		response, err := kernel.read(evalCtx)
		if err != nil {
			kernel.stop()
			return Result{}, err
		}
		switch response.Type {
		case "host_request":
			if err := validateModuleOperation(response.Module, response.Operation); err != nil {
				kernel.stop()
				return Result{}, err
			}
			var value any
			var callErr error
			if kernel.host == nil {
				callErr = errors.New("RLM host is not bound")
			} else {
				value, callErr = kernel.host.Call(evalCtx, response.Module, response.Operation, response.Arguments)
			}
			reply := frame{Type: "host_response", ID: response.ID, Value: value}
			if callErr != nil {
				reply.Value = nil
				reply.Error = callErr.Error()
			}
			if err := writeFrame(kernel.worker.input, kernel.limits.FrameBytes, reply); err != nil {
				kernel.stop()
				return Result{}, err
			}
		case "result":
			if response.ID != id {
				kernel.stop()
				return Result{}, errors.New("mismatched RLM evaluation result")
			}
			result := Result{Value: response.Value, Output: response.Output, Steps: response.Steps}
			if response.Error != "" {
				return result, errors.New(response.Error)
			}
			return result, nil
		default:
			kernel.stop()
			return Result{}, fmt.Errorf("unexpected RLM worker frame %q", response.Type)
		}
	}
}

func (kernel *Kernel) read(ctx context.Context) (frame, error) {
	process := kernel.worker
	type outcome struct {
		frame frame
		err   error
	}
	result := make(chan outcome, 1)
	go func() {
		value, err := readFrame(process.output, kernel.limits.FrameBytes)
		result <- outcome{frame: value, err: err}
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case value := <-result:
			if value.err != nil {
				if errors.Is(value.err, io.EOF) {
					<-process.done
				} else {
					select {
					case <-process.done:
					default:
						return value.frame, value.err
					}
				}
				if detail := process.stderr.String(); detail != "" {
					return frame{}, fmt.Errorf("RLM worker exited: %s", detail)
				}
			}
			return value.frame, value.err
		case <-ticker.C:
			resident, err := residentBytes(process.command.Process.Pid)
			if err == nil && resident > kernel.limits.MemoryBytes {
				return frame{}, ErrMemoryLimit
			}
		case <-ctx.Done():
			return frame{}, fmt.Errorf("RLM cell deadline: %w", ctx.Err())
		}
	}
}

func (kernel *Kernel) start() (err error) {
	if kernel.worker != nil {
		select {
		case <-kernel.worker.done:
			kernel.stop()
		default:
			return nil
		}
	}
	if err := kernel.manager.reserve(); err != nil {
		return err
	}
	release := sync.OnceFunc(kernel.manager.release)
	defer func() {
		if err != nil {
			release()
		}
	}()
	dir, err := os.MkdirTemp("", "whip-rlm-worker-")
	if err != nil {
		return err
	}
	failed := true
	defer func() {
		if failed {
			_ = os.RemoveAll(dir)
		}
	}()
	args := append(append([]string(nil), kernel.command[1:]...),
		"-steps", strconv.FormatUint(kernel.limits.Steps, 10),
		"-host-requests", strconv.Itoa(kernel.limits.HostRequests),
		"-memory-bytes", strconv.FormatUint(kernel.limits.MemoryBytes, 10),
		"-output-bytes", strconv.Itoa(kernel.limits.OutputBytes),
		"-frame-bytes", strconv.Itoa(kernel.limits.FrameBytes),
	)
	// The kernel owns cancellation through process-group termination; the
	// background command context prevents exec from installing a competing
	// single-process kill path.
	command := exec.CommandContext(context.Background(), kernel.command[0], args...)
	command.Dir = dir
	command.Env = []string{}
	configureCommand(command)
	input, err := command.StdinPipe()
	if err != nil {
		return err
	}
	output, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	stderr := &limitedBuffer{limit: kernel.limits.OutputBytes}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		return err
	}
	process := &workerProcess{
		command: command, input: input, output: bufio.NewReaderSize(output, min(kernel.limits.FrameBytes, 64<<10)),
		done: make(chan struct{}), dir: dir, release: release, stderr: stderr,
	}
	kernel.worker = process
	go func() {
		_ = command.Wait()
		// A crashed worker may have descendants in its dedicated group. Reap the
		// group before publishing completion so no caller can observe done while
		// an orphan remains alive.
		_ = killProcessGroup(command.Process.Pid)
		close(process.done)
		kernel.mu.Lock()
		if kernel.worker == process {
			kernel.worker = nil
			_ = os.RemoveAll(process.dir)
			process.release()
		}
		kernel.mu.Unlock()
	}()
	failed = false
	return nil
}

func (kernel *Kernel) stop() {
	process := kernel.worker
	if process == nil {
		return
	}
	kernel.worker = nil
	_ = process.input.Close()
	select {
	case <-process.done:
	default:
		_ = killProcessGroup(process.command.Process.Pid)
		<-process.done
	}
	_ = os.RemoveAll(process.dir)
	process.release()
}

func (kernel *Kernel) Close() {
	kernel.mu.Lock()
	defer kernel.mu.Unlock()
	if kernel.closed {
		return
	}
	kernel.closed = true
	kernel.stop()
}

func (kernel *Kernel) Started() bool {
	kernel.mu.Lock()
	defer kernel.mu.Unlock()
	return kernel.worker != nil
}

type limitedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
	limit  int
}

func (buffer *limitedBuffer) Write(value []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	written := len(value)
	remaining := buffer.limit - buffer.buffer.Len()
	if remaining > 0 {
		_, _ = buffer.buffer.Write(value[:min(len(value), remaining)])
	}
	return written, nil
}

func (buffer *limitedBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.String()
}
