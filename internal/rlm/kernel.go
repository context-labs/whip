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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/tools"
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
	Steps                  uint64
	MaxConcurrentHostCalls int
	HostRequests           int
	Wall                   time.Duration
	MemoryBytes            uint64
	OutputBytes            int
	FrameBytes             int
	MaxWorkers             int
}

func DefaultLimits() Limits {
	return Limits{
		Steps: defaultSteps, MaxConcurrentHostCalls: maxOutstandingCalls, HostRequests: defaultHostRequests, Wall: 30 * time.Second,
		MemoryBytes: defaultMemoryBytes, OutputBytes: defaultOutputBytes,
		FrameBytes: defaultFrameBytes, MaxWorkers: 4,
	}
}

func (limits Limits) normalized() Limits {
	defaults := DefaultLimits()
	if limits.MaxConcurrentHostCalls == 0 {
		limits.MaxConcurrentHostCalls = defaults.MaxConcurrentHostCalls
	}
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
	Engine      string
	Checkpoints CheckpointStore
	// Modules names the host modules the worker installs. Nil installs every
	// registered module. The list is fixed for the kernel's lifetime and is
	// re-applied to every replacement worker.
	Modules []string
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
	// OnHostStart observes an invocation immediately before entering the host.
	// Both callbacks run while the kernel is locked and must return fast.
	OnHostStart func(HostCall)
	// OnHostCall observes completion, including host errors and cancellation.
	OnHostCall func(HostCall)
}

// HostCall describes one host module call made from inside a cell: which
// model tool call it belongs to, what was called, a bounded argument summary
// (never raw contents), how long it took, and the error text if it failed.
type HostCall struct {
	CallID string
	// InvocationID distinguishes repeated operations and repeated model call IDs
	// for this kernel's lifetime. Clients also scope it to the agent and turn.
	InvocationID string
	// Status is empty at start, then completed, failed, or cancelled.
	Status    string
	Module    string
	Operation string
	Summary   string
	Duration  time.Duration
	Err       string
}

type ScratchReport struct {
	Warning string        `json:"warning,omitempty"`
	Skipped []SkippedName `json:"skipped"`
}

type Result struct {
	Termination     string            `json:"termination,omitempty"`
	FormatVersion   int               `json:"format_version"`
	ExecutionEngine string            `json:"execution_engine"`
	Language        string            `json:"language"`
	HasValue        bool              `json:"has_value"`
	Metrics         map[string]uint64 `json:"metrics"`
	Scratch         *ScratchReport    `json:"scratch,omitempty"`
	Value           any               `json:"value"`
	Output          string            `json:"output,omitempty"`
	Steps           uint64            `json:"steps"`
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
	engine      EngineDescriptor
	checkpoints CheckpointStore
	execMu      sync.Mutex
	mu          sync.Mutex
	command     []string
	modules     []string
	limits      Limits
	manager     *Manager
	host        Host
	worker      *workerProcess
	nextID      uint64
	closed      bool
	everStarted bool

	scratch      ScratchStore
	onRestore    func(context.Context, RestoreReport)
	onHostStart  func(HostCall)
	onHostCall   func(HostCall)
	snapshotHash [32]byte
	skippedHash  [32]byte
	needsRestore bool // a fresh process has not loaded the stored scratch yet
}

type frameOutcome struct {
	frame frame
	err   error
}

type workerProcess struct {
	frames   chan frameOutcome
	readDone chan struct{}
	command  *exec.Cmd
	input    io.WriteCloser
	output   *bufio.Reader
	done     chan struct{}
	dir      string
	stderr   *limitedBuffer
}

func NewKernel(options KernelOptions) (*Kernel, error) {
	descriptor, err := ResolveEngine(options.Engine)
	if err != nil {
		return nil, err
	}
	limits := options.Limits.normalized()
	if limits.MaxConcurrentHostCalls < 1 || limits.MaxConcurrentHostCalls > maxOutstandingCalls || limits.HostRequests < 1 || limits.Wall < time.Millisecond || limits.MemoryBytes > math.MaxInt64/4 || limits.OutputBytes < 1 || limits.FrameBytes < 1 || limits.MaxWorkers < 1 {
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
	for _, module := range options.Modules {
		if _, ok := moduleRegistry[module]; !ok {
			return nil, fmt.Errorf("unknown RLM module %q", module)
		}
	}
	return &Kernel{
		engine: descriptor, checkpoints: options.Checkpoints, modules: append([]string(nil), options.Modules...),
		command: command, limits: limits, manager: options.Manager, host: options.Host,
		scratch: options.Scratch, onRestore: options.OnRestore,
		onHostStart: options.OnHostStart, onHostCall: options.OnHostCall,
	}, nil
}

func (kernel *Kernel) Exec(ctx context.Context, code string) (Result, error) {
	kernel.execMu.Lock()
	defer kernel.execMu.Unlock()
	pinned := false
	if lease, _ := ctx.Value(kernelLeaseKey{}).(*kernelTurnLease); lease != nil && lease.kernel == kernel {
		lease.mu.Lock()
		pinned = lease.active
		if pinned {
			defer lease.mu.Unlock()
		} else {
			lease.mu.Unlock()
		}
	}
	release := func() {}
	var restored *RestoreReport
	if !pinned || !kernel.manager.running(kernel) {
		start, acquired, err := kernel.acquire(ctx)
		if err != nil {
			return Result{}, err
		}
		restored = start.Restore
		// A replacement inherits the turn's pin. The turn release owns the
		// current reservation, including replacements, until the turn ends.
		if !pinned {
			release = acquired
		}
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
		kernel.stop()
		return Result{}, err
	} else if report != nil {
		restored = report
	}
	result, err := kernel.evalLocked(ctx, code)
	result.FormatVersion, result.ExecutionEngine, result.Language = 2, kernel.engine.ID, kernel.engine.Language
	result.Restored = restored
	result.Scratch = kernel.snapshotLocked(ctx)
	return result, err
}

// evalLocked runs one cell on the resident worker, serving host requests
// until the result frame arrives. The caller holds kernel.mu.
func (kernel *Kernel) evalLocked(ctx context.Context, code string) (Result, error) {
	if kernel.engine.ID == EngineQuickJS {
		return kernel.evalQuickJSLocked(ctx, code)
	}
	kernel.nextID++
	id := kernel.nextID
	if err := writeFrame(kernel.worker.input, kernel.limits.FrameBytes, frame{Type: "eval", ID: id, Code: code}); err != nil {
		kernel.stop()
		return Result{}, err
	}
	// The wall clock charges Starlark compute only: time the worker spends
	// running code between frames. Time inside a host call (shell, a
	// permission prompt, agents.wait, MCP) is not counted; each host call is
	// bounded by its own limit and by turn cancellation.
	budget := kernel.limits.Wall
	var consumed time.Duration
	var hostCalls uint64
	onUpdate, callID := tools.OnUpdate(ctx), tools.ToolCallID(ctx)
	exhausted := func() (Result, error) {
		kernel.stop()
		return Result{}, fmt.Errorf("RLM cell deadline: %s of Starlark compute exceeded (time inside host calls is not counted)", budget)
	}
	for {
		remaining := budget - consumed
		if remaining <= 0 {
			return exhausted()
		}
		readCtx, cancel := context.WithTimeout(ctx, remaining)
		started := time.Now()
		response, err := kernel.read(readCtx)
		cancel()
		consumed += time.Since(started)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
				return exhausted()
			}
			kernel.stop()
			return Result{}, err
		}
		switch response.Type {
		case "output":
			// The cell's print output so far; the result frame repeats it in full.
			if onUpdate != nil && response.ID == id {
				onUpdate(response.Output)
			}
		case "host_request":
			if err := validateModuleOperation(response.Module, response.Operation); err != nil {
				kernel.stop()
				return Result{}, err
			}
			var value any
			var callErr error
			hostCalls++
			call := HostCall{
				CallID: callID, InvocationID: fmt.Sprintf("%d:%d", id, hostCalls),
				Module: response.Module, Operation: response.Operation,
				Summary: hostCallSummary(response.Arguments),
			}
			callStarted := time.Now()
			if kernel.onHostStart != nil {
				kernel.onHostStart(call)
			}
			if kernel.host == nil {
				callErr = errors.New("RLM host is not bound")
			} else {
				value, callErr = kernel.host.Call(ctx, response.Module, response.Operation, response.Arguments)
			}
			if kernel.onHostCall != nil {
				call.Duration = time.Since(callStarted)
				call.Status = "completed"
				if callErr != nil {
					call.Err = callErr.Error()
					call.Status = "failed"
					if errors.Is(callErr, context.Canceled) {
						call.Status = "cancelled"
					}
				}
				kernel.onHostCall(call)
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
			result := Result{Value: response.Value, Output: response.Output, Steps: response.Steps, HasValue: response.HasValue, Metrics: map[string]uint64{"starlark_steps": response.Steps}}
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

type kernelTurnLease struct {
	kernel *Kernel
	mu     sync.Mutex
	active bool
}

// AcquireTurn pins one worker for an entire model turn. Every rlm_exec call
// made with the returned context shares the same Starlark globals.
func (kernel *Kernel) AcquireTurn(ctx context.Context) (context.Context, TurnStart, func(), error) {
	start, release, err := kernel.acquire(ctx)
	if err != nil {
		return ctx, TurnStart{}, func() {}, err
	}
	lease := &kernelTurnLease{kernel: kernel, active: true}
	return context.WithValue(ctx, kernelLeaseKey{}, lease), start, sync.OnceFunc(func() {
		lease.mu.Lock()
		defer lease.mu.Unlock()
		lease.active = false
		release()
	}), nil
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
		kernel.mu.Lock()
		kernel.stop()
		kernel.mu.Unlock()
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
	if err != nil {
		kernel.stop()
	}
	kernel.mu.Unlock()
	if err != nil {
		kernel.manager.abandon(kernel)
		return TurnStart{}, func() {}, err
	}
	return start, sync.OnceFunc(func() {
		kernel.mu.Lock()
		defer kernel.mu.Unlock()
		if kernel.worker == nil {
			kernel.manager.depart(kernel)
		} else {
			kernel.manager.release(kernel)
		}
	}), nil
}

// restoreLocked revives stored scratch into a worker process that has not
// loaded it yet. It returns nil when nothing is stored. The caller holds
// kernel.mu with a live worker.
func (kernel *Kernel) restoreLocked(ctx context.Context) (*RestoreReport, error) {
	if !kernel.needsRestore {
		return nil, nil //nolint:nilnil // nil report means nothing to report
	}
	if kernel.checkpoints != nil {
		report, found, err := kernel.restoreCheckpointLocked(ctx)
		if err != nil || found {
			return report, err
		}
	}
	if kernel.scratch == nil || kernel.engine.ID != EngineStarlark {
		kernel.needsRestore = false
		return nil, nil //nolint:nilnil // no store configured
	}
	loadCtx, cancel := context.WithTimeout(ctx, kernel.limits.Wall)
	defer cancel()
	snapshot, manifest, err := kernel.scratch.Load(loadCtx)
	if err != nil {
		return nil, err
	}
	if snapshot == "" && manifest.Saved == nil && manifest.Skipped == nil && manifest.Bytes == 0 {
		kernel.needsRestore = false
		return nil, nil //nolint:nilnil // nothing stored yet
	}
	if strings.TrimSpace(snapshot) == "" {
		return nil, errors.New("empty stored scratch snapshot")
	}
	if manifest.Saved == nil || manifest.Bytes < 0 {
		return nil, errors.New("invalid stored scratch manifest")
	}
	response, err := kernel.roundTripLocked(ctx, frame{Type: "restore", Code: snapshot})
	if err != nil {
		return nil, err
	}
	var report RestoreReport
	if response.Value == nil {
		return nil, errors.New("missing scratch restore report")
	}
	if err := decodeFrameValue(response.Value, &report); err != nil {
		return nil, err
	}
	if report.Restored == nil {
		return nil, errors.New("invalid scratch restore report")
	}
	report.Failed = append(report.Failed, manifest.Skipped...)
	kernel.snapshotHash = scratchHash(snapshot, manifest)
	kernel.skippedHash = scratchSkippedHash(manifest.Skipped)
	kernel.needsRestore = false
	if kernel.onRestore != nil {
		kernel.onRestore(ctx, report)
	}
	return &report, nil
}

func scratchHash(snapshot string, manifest SnapshotManifest) [32]byte {
	data, _ := json.Marshal(struct {
		Snapshot string
		Manifest SnapshotManifest
	}{snapshot, manifest})
	return sha256.Sum256(data)
}

func scratchSkippedHash(skipped []SkippedName) [32]byte {
	data, _ := json.Marshal(skipped)
	return sha256.Sum256(data)
}

// snapshotLocked persists changed scratch after a cell. Persistence failures
// preserve the cell result and are retried after the next cell.
func (kernel *Kernel) snapshotLocked(ctx context.Context) *ScratchReport {
	if kernel.checkpoints != nil {
		return kernel.captureCheckpointLocked(ctx)
	}
	if kernel.scratch == nil || kernel.worker == nil {
		return nil
	}
	// Capture a completed cell even if its caller was cancelled. Both operations
	// below have their own deadlines; effects are never replayed to retry a save.
	ctx = context.WithoutCancel(ctx)
	warning := func(err error) *ScratchReport {
		detail := err.Error()
		if len(detail) > 1024 {
			detail = detail[:1024]
		}
		return &ScratchReport{Warning: "Scratch checkpoint failed; the cell result remains valid. Do not replay effects. A later cell will retry checkpointing: " + detail}
	}
	response, err := kernel.roundTripLocked(ctx, frame{Type: "snapshot"})
	if err != nil {
		return warning(err)
	}
	if !json.Valid([]byte(response.Code)) || response.Value == nil {
		return warning(errors.New("invalid scratch snapshot response"))
	}
	var manifest SnapshotManifest
	if err := decodeFrameValue(response.Value, &manifest); err != nil {
		return warning(err)
	}
	if manifest.Saved == nil {
		return warning(errors.New("invalid scratch snapshot manifest"))
	}
	hash := scratchHash(response.Code, manifest)
	if hash == kernel.snapshotHash {
		return nil
	}
	saveCtx, cancel := context.WithTimeout(ctx, kernel.limits.Wall)
	defer cancel()
	if err := kernel.scratch.Save(saveCtx, response.Code, manifest); err != nil {
		return warning(err)
	}
	kernel.snapshotHash = hash
	skippedHash := scratchSkippedHash(manifest.Skipped)
	changed := skippedHash != kernel.skippedHash && (len(manifest.Skipped) > 0 || kernel.skippedHash != [32]byte{})
	kernel.skippedHash = skippedHash
	if changed {
		return &ScratchReport{Skipped: append([]SkippedName{}, manifest.Skipped...)}
	}
	return nil
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
	timeout := kernel.limits.Wall
	if request.Type == "hello" {
		timeout = max(timeout, 5*time.Second)
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
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

func decodeFrameValue(value, target any) error {
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
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case value := <-process.frames:
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
			kernel.stopProcess(false)
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
		"-engine", kernel.engine.ID,
		"-wall-nanos", strconv.FormatInt(int64(kernel.limits.Wall), 10),
		"-steps", strconv.FormatUint(kernel.limits.Steps, 10),
		"-host-requests", strconv.Itoa(kernel.limits.HostRequests),
		"-memory-bytes", strconv.FormatUint(kernel.limits.MemoryBytes, 10),
		"-output-bytes", strconv.Itoa(kernel.limits.OutputBytes),
		"-frame-bytes", strconv.Itoa(kernel.limits.FrameBytes),
	)
	if len(kernel.modules) > 0 {
		args = append(args, "-modules", strings.Join(kernel.modules, ","))
	}
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
		done: make(chan struct{}), readDone: make(chan struct{}), frames: make(chan frameOutcome, 1), dir: dir, stderr: stderr,
	}
	kernel.worker = process
	go func() {
		defer close(process.readDone)
		for {
			value, readErr := readFrame(process.output, kernel.limits.FrameBytes, kernel.engine.ID == EngineQuickJS)
			select {
			case process.frames <- frameOutcome{frame: value, err: readErr}:
			case <-process.done:
				return
			}
			if readErr != nil {
				return
			}
		}
	}()
	kernel.needsRestore = kernel.scratch != nil || kernel.checkpoints != nil
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
			// A running reservation may belong to an acquisition waiting on
			// kernel.mu. Its replacement must keep that slot; release removes
			// it if no replacement was started.
			kernel.manager.mu.Lock()
			if !kernel.manager.resident[kernel].running {
				delete(kernel.manager.resident, kernel)
				kernel.manager.scheduleLocked()
			}
			kernel.manager.mu.Unlock()
		}
		kernel.mu.Unlock()
	}()
	failed = false
	response, handshakeErr := kernel.roundTripLocked(context.Background(), frame{Type: "hello", Engine: kernel.engine.ID, Build: kernel.engine.Build, ABI: kernel.engine.ABI, Profile: kernel.engine.Profile})
	if handshakeErr != nil {
		return handshakeErr
	}
	if response.Engine != kernel.engine.ID || response.Build != kernel.engine.Build || response.ABI != kernel.engine.ABI || response.Profile != kernel.engine.Profile {
		kernel.stop()
		return errors.New("RLM engine handshake mismatch")
	}
	return nil
}

func (kernel *Kernel) stop() { kernel.stopProcess(true) }

func (kernel *Kernel) stopProcess(depart bool) {
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
	<-process.readDone
	_ = os.RemoveAll(process.dir)
	if depart {
		kernel.manager.depart(kernel)
	}
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

// hostCallSummary renders a host call's arguments for the presentation
// stream: identifying keys as key=value, bounded to 80 bytes each, and
// payload keys as their size only, so file contents, message bodies, prompts,
// and code never enter the event log.
func hostCallSummary(arguments map[string]any) string {
	if len(arguments) == 0 {
		return ""
	}
	keys := make([]string, 0, len(arguments))
	for key := range arguments {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		text := fmt.Sprint(arguments[key])
		switch key {
		case "content", "body", "text", "code", "prompt", "prompts", "value", "old", "new", "arguments":
			parts = append(parts, fmt.Sprintf("%s=<%d bytes>", key, len(text)))
		default:
			if len(text) > 80 {
				cut := 80
				for cut > 0 && !utf8.RuneStart(text[cut]) {
					cut--
				}
				text = text[:cut] + "…"
			}
			parts = append(parts, key+"="+text)
		}
	}
	return strings.Join(parts, ", ")
}
