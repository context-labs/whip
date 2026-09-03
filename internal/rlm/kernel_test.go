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
	if _, err := second.Exec(context.Background(), "1"); !errors.Is(err, ErrWorkerLimit) {
		t.Fatalf("reservation error = %v", err)
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
	if _, err := second.Exec(context.Background(), "1"); !errors.Is(err, ErrWorkerLimit) {
		// first owns the sole slot again after its restart.
		t.Fatalf("reservation after restart = %v", err)
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
