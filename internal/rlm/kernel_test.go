package rlm

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestWorkerProcess(t *testing.T) {
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		return
	}
	args := os.Args[separator+1:]
	if len(args) >= 2 && args[0] == "-test-protocol-response" {
		switch args[1] {
		case "mismatch":
			_ = writeFrame(os.Stdout, 1<<20, frame{Type: "result", ID: 99})
		case "unexpected":
			_ = writeFrame(os.Stdout, 1<<20, frame{Type: "host_response", ID: 1})
		case "invalid-operation":
			_ = writeFrame(os.Stdout, 1<<20, frame{Type: "host_request", ID: 1, Module: "files", Operation: "missing"})
		case "stderr":
			fmt.Fprint(os.Stderr, "worker diagnostic")
			os.Exit(2)
		}
		return
	}
	if len(args) >= 2 && args[0] == "-test-child-pid" {
		child := exec.CommandContext(context.Background(), "/bin/sh", "-c", "sleep 60")
		if err := child.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if err := os.WriteFile(args[1], []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		args = args[2:]
	}
	if len(args) >= 2 && args[0] == "-test-env-report" {
		if err := os.WriteFile(args[1], []byte(os.Getenv("WHIP_RLM_CANARY")), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		args = args[2:]
	}
	if err := WorkerMain(args, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func testKernel(t *testing.T, limits Limits, host Host) *Kernel {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	kernel, err := NewKernel(KernelOptions{
		Command: []string{executable, "-test.run=TestWorkerProcess", "--"},
		Limits:  limits, Host: host,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(kernel.Close)
	return kernel
}

func testManagedKernel(t *testing.T, manager *Manager) *Kernel {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	kernel, err := NewKernel(KernelOptions{
		Command: []string{executable, "-test.run=TestWorkerProcess", "--"},
		Limits:  DefaultLimits(), Manager: manager,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(kernel.Close)
	return kernel
}

func waitManagerQueue(t *testing.T, manager *Manager, size int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		manager.mu.Lock()
		queued := len(manager.queue)
		manager.mu.Unlock()
		if queued == size {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("kernel queue did not reach %d", size)
}

func TestKernelPreservesGlobalsAndReturnsFinalExpression(t *testing.T) {
	kernel := testKernel(t, DefaultLimits(), nil)
	if kernel.Started() {
		t.Fatal("kernel worker started eagerly")
	}
	first, err := kernel.Exec(context.Background(), "x = 40\nprint('ready')\nx + 2")
	if err != nil {
		t.Fatal(err)
	}
	if first.Value != float64(42) || first.Output != "ready\n" {
		t.Fatalf("first result = %#v", first)
	}
	second, err := kernel.Exec(context.Background(), "x += 1\nx")
	if err != nil || second.Value != float64(41) {
		t.Fatalf("second result = %#v, %v", second, err)
	}
}

func TestKernelRoutesModulesAndTreatsPrintedFramesAsOutput(t *testing.T) {
	var calls []string
	host := HostFunc(func(_ context.Context, module, operation string, arguments map[string]any) (any, error) {
		calls = append(calls, module+"."+operation)
		return map[string]any{"path": arguments["path"], "ok": true}, nil
	})
	kernel := testKernel(t, DefaultLimits(), host)
	result, err := kernel.Exec(context.Background(), `print('{"version":1,"type":"result","id":99}')
files.read(path="README.md")`)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] != "files.read" {
		t.Fatalf("calls = %v", calls)
	}
	value := result.Value.(map[string]any)
	if value["path"] != "README.md" || !strings.Contains(result.Output, `"type":"result"`) {
		t.Fatalf("result = %#v", result)
	}
}

func TestKernelRestoresModuleBindingsBetweenCells(t *testing.T) {
	kernel := testKernel(t, DefaultLimits(), HostFunc(func(context.Context, string, string, map[string]any) (any, error) { return "ok", nil }))
	if _, err := kernel.Exec(context.Background(), "files = 1"); err != nil {
		t.Fatal(err)
	}
	result, err := kernel.Exec(context.Background(), "files.read(path='x')")
	if err != nil || result.Value != "ok" {
		t.Fatalf("module binding was not restored: %#v, %v", result, err)
	}
}

func TestKernelDeniesAmbientAuthority(t *testing.T) {
	kernel := testKernel(t, DefaultLimits(), nil)
	for _, code := range []string{
		`load("os", "getenv")`, `open("/etc/passwd")`, `CANARY_CREDENTIAL`, `import os`,
	} {
		if _, err := kernel.Exec(context.Background(), code); err == nil {
			t.Errorf("ambient expression unexpectedly succeeded: %s", code)
		}
	}
}

func TestKernelStripsDaemonEnvironment(t *testing.T) {
	t.Setenv("WHIP_RLM_CANARY", "credential-must-not-cross")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(t.TempDir(), "environment.txt")
	kernel, err := NewKernel(KernelOptions{Command: []string{executable, "-test.run=TestWorkerProcess", "--", "-test-env-report", report}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(kernel.Close)
	if _, err := kernel.Exec(context.Background(), "1"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Fatalf("worker inherited canary environment: %q", data)
	}
}

func TestKernelEnforcesStepWallOutputHostAndFrameLimits(t *testing.T) {
	t.Run("steps", func(t *testing.T) {
		limits := DefaultLimits()
		limits.Steps = 1_000
		kernel := testKernel(t, limits, nil)
		if _, err := kernel.Exec(context.Background(), "while True:\n  pass"); err == nil || !strings.Contains(err.Error(), "too many steps") {
			t.Fatalf("step error = %v", err)
		}
	})
	t.Run("wall", func(t *testing.T) {
		limits := DefaultLimits()
		limits.Steps = ^uint64(0)
		limits.Wall = 30 * time.Millisecond
		kernel := testKernel(t, limits, nil)
		if _, err := kernel.Exec(context.Background(), "while True:\n  pass"); err == nil || !strings.Contains(err.Error(), "deadline") {
			t.Fatalf("wall error = %v", err)
		}
	})
	t.Run("host calls are not charged", func(t *testing.T) {
		limits := DefaultLimits()
		limits.Wall = 100 * time.Millisecond
		slow := HostFunc(func(context.Context, string, string, map[string]any) (any, error) {
			time.Sleep(3 * limits.Wall)
			return "ok", nil
		})
		kernel := testKernel(t, limits, slow)
		if result, err := kernel.Exec(context.Background(), "files.read(path='a')"); err != nil || result.Value != "ok" {
			t.Fatalf("slow host call tripped the cell clock: %+v err=%v", result, err)
		}
	})
	t.Run("compute after a host call is charged", func(t *testing.T) {
		limits := DefaultLimits()
		limits.Steps = ^uint64(0)
		limits.Wall = 100 * time.Millisecond
		slow := HostFunc(func(context.Context, string, string, map[string]any) (any, error) {
			time.Sleep(limits.Wall / 2)
			return "ok", nil
		})
		kernel := testKernel(t, limits, slow)
		if _, err := kernel.Exec(context.Background(), "files.read(path='a')\nwhile True:\n  pass"); err == nil || !strings.Contains(err.Error(), "Starlark compute exceeded") {
			t.Fatalf("compute after host call error = %v", err)
		}
	})
	t.Run("output", func(t *testing.T) {
		limits := DefaultLimits()
		limits.OutputBytes = 32
		kernel := testKernel(t, limits, nil)
		if _, err := kernel.Exec(context.Background(), `print("x" * 100)`); err == nil || !strings.Contains(err.Error(), "output limit") {
			t.Fatalf("output error = %v", err)
		}
	})
	t.Run("host requests", func(t *testing.T) {
		limits := DefaultLimits()
		limits.HostRequests = 1
		kernel := testKernel(t, limits, HostFunc(func(context.Context, string, string, map[string]any) (any, error) { return "ok", nil }))
		if _, err := kernel.Exec(context.Background(), "files.read(path='a')\nfiles.read(path='b')"); err == nil || !strings.Contains(err.Error(), "host request limit") {
			t.Fatalf("host request error = %v", err)
		}
	})
	t.Run("frame", func(t *testing.T) {
		limits := DefaultLimits()
		limits.FrameBytes = 256
		kernel := testKernel(t, limits, nil)
		if _, err := kernel.Exec(context.Background(), strings.Repeat("x", 512)); !errors.Is(err, ErrFrameLimit) {
			t.Fatalf("frame error = %v", err)
		}
	})
	t.Run("memory", func(t *testing.T) {
		limits := DefaultLimits()
		limits.MemoryBytes = 32 << 20
		if raceEnabled {
			// ThreadSanitizer's baseline exceeds the production stress limit;
			// parent-side RSS enforcement still bounds this race-build worker.
			limits.MemoryBytes = defaultMemoryBytes
		}
		limits.Steps = ^uint64(0)
		limits.Wall = 3 * time.Second
		kernel := testKernel(t, limits, nil)
		_, err := kernel.Exec(context.Background(), "items = []\nwhile True:\n  items.append('x' * 65536)")
		if err == nil {
			t.Fatal("allocation pressure unexpectedly succeeded")
		}
		if _, restartErr := kernel.Exec(context.Background(), "1"); restartErr != nil {
			t.Fatalf("kernel did not restart after memory termination: %v (original %v)", restartErr, err)
		}
	})
}

func TestKernelReservationAndCrashRestart(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxWorkers = 1
	manager := NewManager(1)
	executable, _ := os.Executable()
	newKernel := func() *Kernel {
		kernel, err := NewKernel(KernelOptions{Command: []string{executable, "-test.run=TestWorkerProcess", "--"}, Limits: limits, Manager: manager})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(kernel.Close)
		return kernel
	}
	first, second := newKernel(), newKernel()
	if _, err := first.Exec(context.Background(), "x = 7"); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Exec(context.Background(), "1"); err != nil {
		t.Fatalf("LRU replacement: %v", err)
	}
	if first.Started() || manager.State(first) != KernelCold {
		t.Fatalf("first kernel was not suspended: started=%v state=%s", first.Started(), manager.State(first))
	}
	if _, err := first.Exec(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "undefined") {
		t.Fatalf("globals survived suspension: %v", err)
	}

	first.mu.Lock()
	pid := first.worker.command.Process.Pid
	first.mu.Unlock()
	if err := killProcessGroup(pid); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for manager.Active() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if manager.Active() != 0 {
		t.Fatal("crashed worker retained its daemon reservation")
	}
	if _, err := first.Exec(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "undefined") {
		t.Fatalf("globals survived worker restart: %v", err)
	}
	if _, err := second.Exec(context.Background(), "1"); err != nil {
		t.Fatalf("reservation after restart = %v", err)
	}
}

func TestKernelManagerSchedulesFIFOWithoutEvictingRunningTurns(t *testing.T) {
	manager := NewManager(2)
	t.Cleanup(manager.Close)
	kernels := []*Kernel{
		testManagedKernel(t, manager), testManagedKernel(t, manager),
		testManagedKernel(t, manager), testManagedKernel(t, manager),
	}
	_, _, releaseFirst, err := kernels[0].AcquireTurn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer releaseFirst()
	_, _, releaseSecond, err := kernels[1].AcquireTurn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer releaseSecond()

	type acquisition struct {
		index   int
		release func()
		err     error
	}
	acquired := make(chan acquisition, 2)
	acquire := func(index int) {
		_, _, release, acquireErr := kernels[index].AcquireTurn(t.Context())
		acquired <- acquisition{index: index, release: release, err: acquireErr}
	}
	go acquire(2)
	waitManagerQueue(t, manager, 1)
	go acquire(3)
	waitManagerQueue(t, manager, 2)
	if manager.Active() != 2 {
		t.Fatalf("resident workers=%d, want 2", manager.Active())
	}

	releaseSecond()
	third := <-acquired
	if third.err != nil || third.index != 2 {
		t.Fatalf("first queued acquisition=%+v", third)
	}
	defer third.release()
	if manager.State(kernels[0]) != KernelRunning || manager.State(kernels[1]) != KernelCold || manager.State(kernels[2]) != KernelRunning {
		t.Fatalf("states after first grant=%s,%s,%s", manager.State(kernels[0]), manager.State(kernels[1]), manager.State(kernels[2]))
	}

	releaseFirst()
	fourth := <-acquired
	if fourth.err != nil || fourth.index != 3 {
		t.Fatalf("second queued acquisition=%+v", fourth)
	}
	defer fourth.release()
	if manager.Active() != 2 || manager.State(kernels[2]) != KernelRunning || manager.State(kernels[3]) != KernelRunning {
		t.Fatalf("states after second grant=%s,%s active=%d", manager.State(kernels[2]), manager.State(kernels[3]), manager.Active())
	}
}

func TestKernelManagerEvictsIdleWorkersInLRUOrderAcrossRetainedSessions(t *testing.T) {
	manager := NewManager(2)
	t.Cleanup(manager.Close)
	kernels := make([]*Kernel, 10)
	for index := range kernels {
		kernels[index] = testManagedKernel(t, manager)
	}
	if _, err := kernels[0].Exec(t.Context(), "x = 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := kernels[1].Exec(t.Context(), "x = 2"); err != nil {
		t.Fatal(err)
	}
	if _, err := kernels[0].Exec(t.Context(), "x += 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := kernels[2].Exec(t.Context(), "x = 3"); err != nil {
		t.Fatal(err)
	}
	if manager.State(kernels[1]) != KernelCold || manager.State(kernels[0]) != KernelResident {
		t.Fatalf("LRU states first=%s second=%s", manager.State(kernels[0]), manager.State(kernels[1]))
	}
	for index := 3; index < len(kernels); index++ {
		if _, err := kernels[index].Exec(t.Context(), fmt.Sprintf("x = %d", index)); err != nil {
			t.Fatalf("retained session %d: %v", index, err)
		}
		if manager.Active() > 2 {
			t.Fatalf("resident workers=%d after session %d", manager.Active(), index)
		}
	}
	started := 0
	for _, kernel := range kernels {
		if kernel.Started() {
			started++
		}
	}
	if started != 2 {
		t.Fatalf("resident subprocesses=%d, want 2", started)
	}
}

func TestKernelManagerRemovesCancelledWaitersAndWakesOnShutdown(t *testing.T) {
	manager := NewManager(1)
	first := testManagedKernel(t, manager)
	queued := testManagedKernel(t, manager)
	shutdown := testManagedKernel(t, manager)
	_, _, release, err := first.AcquireTurn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithCancel(t.Context())
	cancelled := make(chan error, 1)
	go func() {
		_, _, _, acquireErr := queued.AcquireTurn(ctx)
		cancelled <- acquireErr
	}()
	waitManagerQueue(t, manager, 1)
	if _, _, _, duplicateErr := queued.AcquireTurn(t.Context()); duplicateErr == nil || !strings.Contains(duplicateErr.Error(), "already queued") {
		t.Fatalf("duplicate waiter error=%v", duplicateErr)
	}
	cancel()
	if err := <-cancelled; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled acquisition=%v", err)
	}
	waitManagerQueue(t, manager, 0)

	closed := make(chan error, 1)
	go func() {
		_, _, _, acquireErr := shutdown.AcquireTurn(t.Context())
		closed <- acquireErr
	}()
	waitManagerQueue(t, manager, 1)
	manager.Close()
	if err := <-closed; !errors.Is(err, ErrManagerClosed) {
		t.Fatalf("shutdown acquisition=%v", err)
	}
}

func TestKernelTurnLeasePreservesScratchAndSuspendRejectsRunning(t *testing.T) {
	manager := NewManager(1)
	t.Cleanup(manager.Close)
	kernel := testManagedKernel(t, manager)
	ctx, start, release, err := kernel.AcquireTurn(t.Context())
	if err != nil || start.Restarted {
		t.Fatalf("first lease start=%+v err=%v", start, err)
	}
	if err := kernel.Suspend(); !errors.Is(err, ErrKernelRunning) {
		t.Fatalf("suspend running kernel=%v", err)
	}
	if _, err := kernel.Exec(ctx, "scratch = 41"); err != nil {
		t.Fatal(err)
	}
	result, err := kernel.Exec(ctx, "scratch + 1")
	if err != nil || result.Value != float64(42) {
		t.Fatalf("same-turn scratch=%#v err=%v", result, err)
	}
	release()
	if err := kernel.Suspend(); err != nil {
		t.Fatal(err)
	}
	ctx, start, release, err = kernel.AcquireTurn(t.Context())
	if err != nil || !start.Restarted || start.Restore != nil {
		t.Fatalf("second lease start=%+v err=%v", start, err)
	}
	defer release()
	if _, err := kernel.Exec(ctx, "scratch"); err == nil || !strings.Contains(err.Error(), "undefined") {
		t.Fatalf("scratch survived suspension: %v", err)
	}
}

func TestKernelDeadlineReapsProcessGroup(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	pidPath := filepath.Join(t.TempDir(), "child.pid")
	limits := DefaultLimits()
	limits.Wall = 50 * time.Millisecond
	limits.Steps = ^uint64(0)
	kernel, err := NewKernel(KernelOptions{Command: []string{executable, "-test.run=TestWorkerProcess", "--", "-test-child-pid", pidPath}, Limits: limits})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(kernel.Close)
	if _, err := kernel.Exec(context.Background(), "while True:\n  pass"); err == nil {
		t.Fatal("infinite cell did not hit wall limit")
	}
	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("worker descendant %d survived process-group kill", pid)
}

func TestKernelSerializesConcurrentCells(t *testing.T) {
	kernel := testKernel(t, DefaultLimits(), nil)
	var wait sync.WaitGroup
	for range 4 {
		wait.Go(func() {
			if _, err := kernel.Exec(context.Background(), "value = 1"); err != nil {
				t.Errorf("Exec: %v", err)
			}
		})
	}
	wait.Wait()
}

func TestKernelRejectsMalformedWorkerProtocol(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		response string
		want     string
	}{
		{response: "mismatch", want: "mismatched RLM evaluation result"},
		{response: "unexpected", want: "unexpected RLM worker frame"},
		{response: "invalid-operation", want: "unknown RLM operation"},
		{response: "stderr", want: "worker diagnostic"},
	}
	for _, test := range tests {
		t.Run(test.response, func(t *testing.T) {
			kernel, err := NewKernel(KernelOptions{Command: []string{executable, "-test.run=TestWorkerProcess", "--", "-test-protocol-response", test.response}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(kernel.Close)
			if _, err := kernel.Exec(context.Background(), "1"); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("protocol error = %v", err)
			}
		})
	}
}

func TestKernelLifecycleAndDiagnosticsBoundaries(t *testing.T) {
	for _, limits := range []Limits{
		{HostRequests: -1},
		{Wall: time.Nanosecond},
		{MemoryBytes: math.MaxInt64},
		{OutputBytes: -1},
		{FrameBytes: -1},
		{MaxWorkers: -1},
	} {
		if _, err := NewKernel(KernelOptions{Command: []string{"unused"}, Limits: limits}); err == nil {
			t.Fatalf("invalid limits accepted: %+v", limits)
		}
	}
	manager := NewManager(0)
	if manager.Active() != 0 {
		t.Fatalf("new manager active count = %d", manager.Active())
	}
	kernel, err := NewKernel(KernelOptions{Command: []string{"/path/that/does/not/exist"}, Manager: manager})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := kernel.Exec(context.Background(), "1"); err == nil {
		t.Fatal("missing worker executable succeeded")
	}
	if manager.Active() != 0 {
		t.Fatal("failed worker start leaked a manager reservation")
	}
	kernel.Close()
	kernel.Close()
	if _, err := kernel.Exec(context.Background(), "1"); !errors.Is(err, ErrKernelClosed) {
		t.Fatalf("closed kernel error = %v", err)
	}

	buffer := &limitedBuffer{limit: 4}
	if written, err := buffer.Write([]byte("abcdef")); err != nil || written != 6 || buffer.String() != "abcd" {
		t.Fatalf("limited buffer = %q, %d, %v", buffer.String(), written, err)
	}
	if written, err := buffer.Write([]byte("z")); err != nil || written != 1 || buffer.String() != "abcd" {
		t.Fatalf("full limited buffer = %q, %d, %v", buffer.String(), written, err)
	}
	if err := killProcessGroup(0); err != nil {
		t.Fatalf("zero process group: %v", err)
	}
}

type memoryScratch struct {
	mu       sync.Mutex
	program  string
	manifest SnapshotManifest
	saves    int
}

func (store *memoryScratch) Load(context.Context) (string, SnapshotManifest, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.program, store.manifest, nil
}

func (store *memoryScratch) Save(_ context.Context, program string, manifest SnapshotManifest) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.program, store.manifest = program, manifest
	store.saves++
	return nil
}

func (store *memoryScratch) count() int {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.saves
}

func TestKernelScratchSurvivesSuspensionAndMidTurnRestart(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(1)
	t.Cleanup(manager.Close)
	limits := DefaultLimits()
	limits.Wall = 300 * time.Millisecond
	limits.Steps = ^uint64(0)
	store := &memoryScratch{}
	kernel, err := NewKernel(KernelOptions{
		Command: []string{executable, "-test.run=TestWorkerProcess", "--"},
		Limits:  limits, Manager: manager, Scratch: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(kernel.Close)

	ctx, start, release, err := kernel.AcquireTurn(t.Context())
	if err != nil || start.Restarted || start.Restore != nil {
		t.Fatalf("first lease start=%+v err=%v", start, err)
	}
	if _, err := kernel.Exec(ctx, "scratch = 41\ndef bump(x):\n    return x + 1"); err != nil {
		t.Fatal(err)
	}
	if store.count() != 1 {
		t.Fatalf("saves after first cell = %d", store.count())
	}
	if result, err := kernel.Exec(ctx, "bump(scratch)"); err != nil || result.Value != float64(42) || result.Restored != nil {
		t.Fatalf("same-turn cell = %+v err=%v", result, err)
	}
	if store.count() != 1 {
		t.Fatalf("unchanged scratch was saved again: %d", store.count())
	}
	release()
	if err := kernel.Suspend(); err != nil {
		t.Fatal(err)
	}

	ctx, start, release, err = kernel.AcquireTurn(t.Context())
	if err != nil || !start.Restarted || start.Restore == nil {
		t.Fatalf("second lease start=%+v err=%v", start, err)
	}
	if !slices.Equal(start.Restore.Restored, []string{"scratch", "bump"}) || len(start.Restore.Failed) != 0 {
		t.Fatalf("restore report = %+v", start.Restore)
	}
	if result, err := kernel.Exec(ctx, "bump(scratch)"); err != nil || result.Value != float64(42) {
		t.Fatalf("restored scratch = %+v err=%v", result, err)
	}
	// A cell that hits the wall clock kills the worker mid-turn; the next
	// cell runs on a replacement that revived the scratch first.
	if _, err := kernel.Exec(ctx, "while True:\n  pass"); err == nil {
		t.Fatal("infinite cell did not hit the wall limit")
	}
	result, err := kernel.Exec(ctx, "bump(scratch)")
	if err != nil || result.Value != float64(42) || result.Restored == nil || !slices.Contains(result.Restored.Restored, "scratch") {
		t.Fatalf("mid-turn restore = %+v err=%v", result, err)
	}
	release()
}
