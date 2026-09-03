package rlm

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrWorkerCapacity = errors.New("worker_capacity_exhausted")
	ErrWorkerLimit    = ErrWorkerCapacity
	ErrKernelClosed   = errors.New("RLM kernel closed")
	ErrKernelRunning  = errors.New("RLM kernel is running")
	ErrManagerClosed  = errors.New("RLM kernel manager closed")
	ErrMemoryLimit    = errors.New("RLM worker memory limit exceeded")
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

type KernelState string

const (
	KernelCold     KernelState = "cold"
	KernelResident KernelState = "resident"
	KernelRunning  KernelState = "running"
)

type kernelResident struct {
	running  bool
	lastUsed uint64
}

type kernelGrant struct {
	victim *Kernel
	err    error
}

type kernelWaiter struct {
	kernel *Kernel
	grant  chan kernelGrant
}

// Manager schedules logical kernels onto a bounded pool of resident worker
// processes. Acquisitions are FIFO; an idle least-recently-used worker is
// suspended when a new session needs a full pool.
type Manager struct {
	mu       sync.Mutex
	max      int
	clock    uint64
	resident map[*Kernel]kernelResident
	evicting map[*Kernel]bool
	waiters  map[*Kernel]*kernelWaiter
	queue    []*kernelWaiter
	closed   bool
}

func NewManager(maxWorkers int) *Manager {
	if maxWorkers < 1 {
		maxWorkers = DefaultLimits().MaxWorkers
	}
	return &Manager{
		max: maxWorkers, resident: make(map[*Kernel]kernelResident),
		evicting: make(map[*Kernel]bool), waiters: make(map[*Kernel]*kernelWaiter),
	}
}

func (manager *Manager) acquire(ctx context.Context, kernel *Kernel) (kernelGrant, error) {
	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		return kernelGrant{}, ErrManagerClosed
	}
	if _, exists := manager.waiters[kernel]; exists {
		manager.mu.Unlock()
		return kernelGrant{}, errors.New("RLM kernel already queued")
	}
	waiter := &kernelWaiter{kernel: kernel, grant: make(chan kernelGrant, 1)}
	manager.waiters[kernel] = waiter
	manager.queue = append(manager.queue, waiter)
	manager.scheduleLocked()
	manager.mu.Unlock()

	select {
	case grant := <-waiter.grant:
		return grant, grant.err
	case <-ctx.Done():
		manager.mu.Lock()
		if manager.waiters[kernel] == waiter {
			manager.removeWaiterLocked(waiter)
			manager.scheduleLocked()
			manager.mu.Unlock()
			return kernelGrant{}, ctx.Err()
		}
		manager.mu.Unlock()
		// A grant won the race with cancellation. Honor the grant so its LRU
		// replacement is completed, then the caller can release it cleanly.
		grant := <-waiter.grant
		return grant, grant.err
	}
}

func (manager *Manager) scheduleLocked() {
	for len(manager.queue) > 0 {
		waiter := manager.queue[0]
		if manager.evicting[waiter.kernel] {
			return
		}
		state, resident := manager.resident[waiter.kernel]
		if resident && state.running {
			return
		}
		var victim *Kernel
		if !resident && len(manager.resident) >= manager.max {
			var oldest uint64
			for candidate, candidateState := range manager.resident {
				if candidateState.running || manager.evicting[candidate] {
					continue
				}
				if victim == nil || candidateState.lastUsed < oldest {
					victim, oldest = candidate, candidateState.lastUsed
				}
			}
			if victim == nil {
				return
			}
			delete(manager.resident, victim)
			manager.evicting[victim] = true
		}
		manager.clock++
		manager.resident[waiter.kernel] = kernelResident{running: true, lastUsed: manager.clock}
		delete(manager.waiters, waiter.kernel)
		manager.queue = manager.queue[1:]
		waiter.grant <- kernelGrant{victim: victim}
	}
}

func (manager *Manager) removeWaiterLocked(target *kernelWaiter) {
	delete(manager.waiters, target.kernel)
	for index, waiter := range manager.queue {
		if waiter == target {
			manager.queue = append(manager.queue[:index], manager.queue[index+1:]...)
			return
		}
	}
}

func (manager *Manager) release(kernel *Kernel) {
	manager.mu.Lock()
	if state, ok := manager.resident[kernel]; ok {
		manager.clock++
		state.running, state.lastUsed = false, manager.clock
		manager.resident[kernel] = state
	}
	manager.scheduleLocked()
	manager.mu.Unlock()
}

func (manager *Manager) depart(kernel *Kernel) {
	manager.mu.Lock()
	delete(manager.resident, kernel)
	manager.scheduleLocked()
	manager.mu.Unlock()
}

func (manager *Manager) finishEviction(kernel *Kernel) {
	manager.mu.Lock()
	delete(manager.evicting, kernel)
	manager.scheduleLocked()
	manager.mu.Unlock()
}

func (manager *Manager) abandon(kernel *Kernel) {
	manager.mu.Lock()
	delete(manager.resident, kernel)
	manager.scheduleLocked()
	manager.mu.Unlock()
}

func (manager *Manager) suspend(kernel *Kernel) error {
	manager.mu.Lock()
	state, resident := manager.resident[kernel]
	if resident && state.running {
		manager.mu.Unlock()
		return ErrKernelRunning
	}
	if !resident {
		manager.mu.Unlock()
		return nil
	}
	delete(manager.resident, kernel)
	manager.evicting[kernel] = true
	manager.mu.Unlock()

	kernel.suspendEvicted()
	manager.finishEviction(kernel)
	return nil
}

func (manager *Manager) running(kernel *Kernel) bool {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return manager.resident[kernel].running
}

func (manager *Manager) cancel(kernel *Kernel) {
	manager.mu.Lock()
	var cancelled *kernelWaiter
	if waiter := manager.waiters[kernel]; waiter != nil {
		cancelled = waiter
		manager.removeWaiterLocked(waiter)
	}
	delete(manager.resident, kernel)
	delete(manager.evicting, kernel)
	manager.scheduleLocked()
	manager.mu.Unlock()
	if cancelled != nil {
		cancelled.grant <- kernelGrant{err: ErrKernelClosed}
	}
}

func (manager *Manager) Active() int {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return len(manager.resident)
}

func (manager *Manager) State(kernel *Kernel) KernelState {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if state, ok := manager.resident[kernel]; ok {
		if state.running {
			return KernelRunning
		}
		return KernelResident
	}
	return KernelCold
}

// Close rejects future work and wakes queued acquisitions. Kernel owners are
// still responsible for closing their resident processes.
func (manager *Manager) Close() {
	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		return
	}
	manager.closed = true
	waiters := append([]*kernelWaiter(nil), manager.queue...)
	manager.queue = nil
	clear(manager.waiters)
	manager.mu.Unlock()
	for _, waiter := range waiters {
		waiter.grant <- kernelGrant{err: ErrManagerClosed}
	}
}

type KernelOptions struct {
	// Command is the executable plus any hidden-mode prefix. Production uses
	// [current executable, "_kernel"]; tests may use a test helper prefix.
	Command []string
	Limits  Limits
	Manager *Manager
	Host    Host
	// Scratch, when set, persists the worker's globals after every cell and
	// restores them into each fresh worker process.
	Scratch ScratchStore
	// OnRestore, when set, observes every completed restore (turn start or
	// mid-turn). It runs while the kernel is locked, so it must return fast.
	OnRestore func(context.Context, RestoreReport)
}

type Result struct {
	Value  any    `json:"value"`
	Output string `json:"output,omitempty"`
	Steps  uint64 `json:"steps"`
	// Restored is set when this cell ran on a worker that was restarted
	// mid-turn and had its scratch revived first.
	Restored *RestoreReport `json:"restored,omitempty"`
}

// TurnStart describes the worker a turn lease found: whether a previously
// started worker had to be replaced, and what scratch the replacement
// revived (nil when nothing was stored).
type TurnStart struct {
	Restarted bool
	Restore   *RestoreReport
}

type Kernel struct {
	execMu      sync.Mutex
	mu          sync.Mutex
	command     []string
	limits      Limits
	manager     *Manager
	host        Host
	worker      *workerProcess
	nextID      uint64
	closed      bool
	everStarted bool

	scratch      ScratchStore
	onRestore    func(context.Context, RestoreReport)
	snapshotHash [32]byte
	needsRestore bool // a fresh process has not loaded the stored scratch yet
}

type workerProcess struct {
	command *exec.Cmd
	input   io.WriteCloser
	output  *bufio.Reader
	done    chan struct{}
	dir     string
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
	return &Kernel{command: command, limits: limits, manager: options.Manager, host: options.Host, scratch: options.Scratch, onRestore: options.OnRestore}, nil
}

func (kernel *Kernel) Exec(ctx context.Context, code string) (Result, error) {
	kernel.execMu.Lock()
	defer kernel.execMu.Unlock()
	release := func() {}
	var restored *RestoreReport
	if pinned, _ := ctx.Value(kernelLeaseKey{}).(*Kernel); pinned != kernel || !kernel.manager.running(kernel) {
		start, acquired, err := kernel.acquire(ctx)
		if err != nil {
			return Result{}, err
		}
		release, restored = acquired, start.Restore
	}
	defer release()

	kernel.mu.Lock()
	defer kernel.mu.Unlock()
	if kernel.closed {
		return Result{}, ErrKernelClosed
	}
	if err := kernel.startProcess(); err != nil {
		return Result{}, err
	}
	if report, err := kernel.restoreLocked(ctx); err != nil {
		return Result{}, err
	} else if report != nil {
		restored = report
	}
	result, err := kernel.evalLocked(ctx, code)
	result.Restored = restored
	kernel.snapshotLocked(ctx)
	return result, err
}

// evalLocked runs one cell on the resident worker, serving host requests
// until the result frame arrives. The caller holds kernel.mu.
func (kernel *Kernel) evalLocked(ctx context.Context, code string) (Result, error) {
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

type kernelLeaseKey struct{}

// AcquireTurn pins one worker for an entire model turn. Every rlm_exec call
// made with the returned context shares the same Starlark globals.
func (kernel *Kernel) AcquireTurn(ctx context.Context) (context.Context, TurnStart, func(), error) {
	start, release, err := kernel.acquire(ctx)
	if err != nil {
		return ctx, TurnStart{}, func() {}, err
	}
	return context.WithValue(ctx, kernelLeaseKey{}, kernel), start, release, nil
}

func (kernel *Kernel) acquire(ctx context.Context) (TurnStart, func(), error) {
	kernel.mu.Lock()
	if kernel.closed {
		kernel.mu.Unlock()
		return TurnStart{}, func() {}, ErrKernelClosed
	}
	kernel.mu.Unlock()

	grant, err := kernel.manager.acquire(ctx, kernel)
	if err != nil {
		return TurnStart{}, func() {}, err
	}
	if grant.victim != nil {
		grant.victim.suspendEvicted()
		kernel.manager.finishEviction(grant.victim)
	}
	if err := ctx.Err(); err != nil {
		kernel.manager.abandon(kernel)
		return TurnStart{}, func() {}, err
	}
	kernel.mu.Lock()
	if kernel.closed {
		kernel.mu.Unlock()
		kernel.manager.abandon(kernel)
		return TurnStart{}, func() {}, ErrKernelClosed
	}
	start := TurnStart{Restarted: kernel.everStarted && kernel.worker == nil}
	err = kernel.startProcess()
	if err == nil {
		kernel.everStarted = true
		start.Restore, err = kernel.restoreLocked(ctx)
	}
	kernel.mu.Unlock()
	if err != nil {
		kernel.manager.abandon(kernel)
		return TurnStart{}, func() {}, err
	}
	return start, sync.OnceFunc(func() { kernel.manager.release(kernel) }), nil
}

// restoreLocked revives stored scratch into a worker process that has not
// loaded it yet. It returns nil when nothing is stored. The caller holds
// kernel.mu with a live worker.
func (kernel *Kernel) restoreLocked(ctx context.Context) (*RestoreReport, error) {
	if !kernel.needsRestore {
		return nil, nil //nolint:nilnil // nil report means nothing to report
	}
	kernel.needsRestore = false
	if kernel.scratch == nil {
		return nil, nil //nolint:nilnil // no store configured
	}
	program, manifest, err := kernel.scratch.Load(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(program) == "" {
		return nil, nil //nolint:nilnil // nothing stored yet
	}
	response, err := kernel.roundTripLocked(ctx, frame{Type: "restore", Code: program})
	if err != nil {
		return nil, err
	}
	var report RestoreReport
	if err := decodeFrameValue(response.Value, &report); err != nil {
		return nil, err
	}
	report.Failed = append(report.Failed, manifest.Skipped...)
	kernel.snapshotHash = sha256.Sum256([]byte(program))
	if kernel.onRestore != nil {
		kernel.onRestore(ctx, report)
	}
	return &report, nil
}

// snapshotLocked captures the worker's globals after a cell and persists
// them when they changed. Failures never fail the cell: the worker is either
// gone (nothing to capture) or the store is unavailable (retried next cell).
func (kernel *Kernel) snapshotLocked(ctx context.Context) {
	if kernel.scratch == nil || kernel.worker == nil {
		return
	}
	// A cancelled turn still gets its last cell captured; the round trip and
	// the store call carry their own deadlines.
	ctx = context.WithoutCancel(ctx)
	response, err := kernel.roundTripLocked(ctx, frame{Type: "snapshot"})
	if err != nil {
		return
	}
	var manifest SnapshotManifest
	if err := decodeFrameValue(response.Value, &manifest); err != nil {
		return
	}
	hash := sha256.Sum256([]byte(response.Code))
	if hash == kernel.snapshotHash {
		return
	}
	saveCtx, cancel := context.WithTimeout(ctx, kernel.limits.Wall)
	defer cancel()
	if err := kernel.scratch.Save(saveCtx, response.Code, manifest); err == nil {
		kernel.snapshotHash = hash
	}
}

// roundTripLocked exchanges one non-eval frame with the worker under the
// cell wall clock. A protocol failure stops the worker like a failed cell.
func (kernel *Kernel) roundTripLocked(ctx context.Context, request frame) (frame, error) {
	kernel.nextID++
	request.ID = kernel.nextID
	if err := writeFrame(kernel.worker.input, kernel.limits.FrameBytes, request); err != nil {
		kernel.stop()
		return frame{}, err
	}
	callCtx, cancel := context.WithTimeout(ctx, kernel.limits.Wall)
	defer cancel()
	response, err := kernel.read(callCtx)
	if err != nil {
		kernel.stop()
		return frame{}, err
	}
	if response.Type != "result" || response.ID != request.ID {
		kernel.stop()
		return frame{}, errors.New("mismatched RLM scratch response")
	}
	if response.Error != "" {
		return frame{}, errors.New(response.Error)
	}
	return response, nil
}

func decodeFrameValue(value any, target any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

// Start makes a worker resident but idle. Production uses AcquireTurn;
// Start remains useful for lifecycle diagnostics.
func (kernel *Kernel) Start() error {
	_, release, err := kernel.acquire(context.Background())
	release()
	return err
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

func (kernel *Kernel) startProcess() (err error) {
	if kernel.worker != nil {
		select {
		case <-kernel.worker.done:
			kernel.stop()
		default:
			return nil
		}
	}
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
		done: make(chan struct{}), dir: dir, stderr: stderr,
	}
	kernel.worker = process
	kernel.needsRestore = kernel.scratch != nil
	go func() {
		_ = command.Wait()
		// A crashed worker may have descendants in its dedicated group. Reap the
		// group before publishing completion so no caller can observe done while
		// an orphan remains alive.
		_ = killProcessGroup(command.Process.Pid)
		close(process.done)
		departed := false
		kernel.mu.Lock()
		if kernel.worker == process {
			kernel.worker = nil
			_ = os.RemoveAll(process.dir)
			departed = true
		}
		kernel.mu.Unlock()
		if departed {
			kernel.manager.depart(kernel)
		}
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
	kernel.manager.depart(kernel)
}

// Suspend discards an idle subprocess without closing the logical kernel.
// Its next turn starts cold and receives a scratch-reset notice.
func (kernel *Kernel) Suspend() error {
	kernel.mu.Lock()
	closed := kernel.closed
	kernel.mu.Unlock()
	if closed {
		return ErrKernelClosed
	}
	return kernel.manager.suspend(kernel)
}

func (kernel *Kernel) suspendEvicted() {
	kernel.mu.Lock()
	kernel.stop()
	kernel.mu.Unlock()
}

func (kernel *Kernel) Close() {
	kernel.mu.Lock()
	if kernel.closed {
		kernel.mu.Unlock()
		return
	}
	kernel.closed = true
	kernel.stop()
	kernel.mu.Unlock()
	kernel.manager.cancel(kernel)
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
