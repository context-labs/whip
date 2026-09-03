//go:build darwin || linux

package capability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const processHelper = "WHIP_PROCESS_TEST_HELPER"

func TestProcessHelper(t *testing.T) {
	switch os.Getenv(processHelper) {
	case "inspect":
		cwd, err := os.Getwd()
		if err != nil {
			os.Exit(2)
		}
		_, fdErr := os.NewFile(99, "canary").Stat()
		_ = json.NewEncoder(os.Stdout).Encode(struct {
			Cwd    string
			Env    []string
			FDOpen bool
		}{cwd, os.Environ(), fdErr == nil})
		os.Exit(0)
	case "sleep":
		time.Sleep(30 * time.Second)
		os.Exit(0)
	case "spawn":
		cmd := exec.CommandContext(context.Background(), os.Args[0], "-test.run=^TestProcessHelper$")
		cmd.Env = replaceEnvironment(os.Environ(), processHelper, "sleep")
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Start(); err != nil {
			os.Exit(2)
		}
		fmt.Fprintln(os.Stdout, cmd.Process.Pid)
		os.Exit(0)
	}
}

func TestProcessEnvironmentCwdAndDescriptors(t *testing.T) {
	t.Setenv("HOME", "/snapshot-home")
	t.Setenv("PATH", os.Getenv("PATH"))
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("LC_TEST", "kept")
	t.Setenv("WHIP_SECRET", "daemon-whip")
	t.Setenv("PROVIDER_API_KEY", "daemon-provider")
	t.Setenv("SSH_AUTH_SOCK", "/daemon-agent")
	t.Setenv("RANDOM_DAEMON_SECRET", "daemon-random")
	m := NewProcessManager()
	t.Setenv("HOME", "/changed-after-snapshot")

	f, err := os.CreateTemp(t.TempDir(), "descriptor")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Dup2(int(f.Fd()), 99); err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(99)
	if _, _, errno := syscall.Syscall(syscall.SYS_FCNTL, 99, syscall.F_SETFD, 0); errno != 0 {
		t.Fatal(errno)
	}

	cwd := t.TempDir()
	var stdout, stderr bytes.Buffer
	p, err := m.Start(context.Background(), "root", os.Args[0], []string{"-test.run=^TestProcessHelper$"}, ProcessOptions{
		Cwd:    cwd,
		Env:    map[string]string{processHelper: "inspect", "WHIP_SECRET": "explicit"},
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Wait(); err != nil {
		t.Fatalf("child failed: %v: %s", err, stderr.String())
	}
	var got struct {
		Cwd    string
		Env    []string
		FDOpen bool
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode child output: %v: %q", err, stdout.String())
	}
	env := envMap(got.Env)
	for name, want := range map[string]string{
		"HOME":        "/snapshot-home",
		"LANG":        "en_US.UTF-8",
		"LC_TEST":     "kept",
		"WHIP_SECRET": "explicit",
	} {
		if env[name] != want {
			t.Errorf("%s = %q, want %q", name, env[name], want)
		}
	}
	for _, name := range []string{"PROVIDER_API_KEY", "SSH_AUTH_SOCK", "RANDOM_DAEMON_SECRET"} {
		if _, ok := env[name]; ok {
			t.Errorf("daemon environment leaked %s", name)
		}
	}
	canonicalCwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if got.Cwd != canonicalCwd {
		t.Errorf("cwd = %q, want %q", got.Cwd, canonicalCwd)
	}
	if got.FDOpen {
		t.Error("unintended descriptor 99 was inherited")
	}
	if built, err := m.environment(nil); err != nil || !slices.IsSorted(built) {
		t.Errorf("environment not deterministically sorted: %v, %v", built, err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestProcessValidation(t *testing.T) {
	m := NewProcessManager()
	valid := ProcessOptions{Cwd: t.TempDir(), Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		root string
		cmd  string
		opts ProcessOptions
	}{
		{"root", "", os.Args[0], valid},
		{"command", "root", "", valid},
		{"cwd", "root", os.Args[0], ProcessOptions{Stdin: valid.Stdin, Stdout: valid.Stdout, Stderr: valid.Stderr}},
		{"cwd-missing", "root", os.Args[0], ProcessOptions{Cwd: filepath.Join(t.TempDir(), "missing"), Stdin: valid.Stdin, Stdout: valid.Stdout, Stderr: valid.Stderr}},
		{"cwd-file", "root", os.Args[0], ProcessOptions{Cwd: file, Stdin: valid.Stdin, Stdout: valid.Stdout, Stderr: valid.Stderr}},
		{"stdio", "root", os.Args[0], ProcessOptions{Cwd: valid.Cwd}},
		{"name", "root", os.Args[0], withEnv(valid, map[string]string{"BAD=NAME": "x"})},
		{"name-prefix", "root", os.Args[0], withEnv(valid, map[string]string{"1BAD": "x"})},
		{"value", "root", os.Args[0], withEnv(valid, map[string]string{"GOOD": "bad\x00value"})},
		{"path-override", "root", os.Args[0], withEnv(valid, map[string]string{"PATH": "/tmp"})},
		{"loader-override", "root", os.Args[0], withEnv(valid, map[string]string{"LD_PRELOAD": "/tmp/inject.so"})},
		{"shell-override", "root", os.Args[0], withEnv(valid, map[string]string{"BASH_ENV": "/tmp/inject.sh"})},
		{"git-override", "root", os.Args[0], withEnv(valid, map[string]string{"GIT_SSH_COMMAND": "inject"})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := m.Start(context.Background(), tc.root, tc.cmd, nil, tc.opts); err == nil {
				t.Fatal("Start accepted invalid input")
			}
		})
	}
	if err := m.StopRoot(""); err == nil {
		t.Fatal("StopRoot accepted an empty root ID")
	}
	if _, err := m.RegisterStop("", func() error { return nil }); err == nil {
		t.Fatal("RegisterStop accepted an empty root ID")
	}
	if _, err := m.RegisterStop("root", nil); err == nil {
		t.Fatal("RegisterStop accepted a nil callback")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Start(ctx, "root", os.Args[0], nil, valid); !errors.Is(err, context.Canceled) {
		t.Fatalf("Start with canceled context = %v", err)
	}
	if env, err := m.ChildEnvironment(nil); err != nil || !slices.IsSorted(env) {
		t.Fatalf("ChildEnvironment = %v, %v", env, err)
	}

	finished := &Process{groupClosed: true}
	if !finished.groupGone() {
		t.Fatal("closed process group was reported active")
	}
	missing := &Process{cmd: &exec.Cmd{Process: &os.Process{Pid: 1 << 30}}}
	if err := missing.Kill(); err != nil {
		t.Fatalf("killing an absent process group = %v", err)
	}
}

func TestProcessManagerRejectsStoppedAndClosedRoots(t *testing.T) {
	m := NewProcessManager()
	stopEntered := make(chan struct{})
	releaseStop := make(chan struct{})
	release := sync.OnceFunc(func() { close(releaseStop) })
	t.Cleanup(func() {
		release()
		_ = m.Close()
	})
	if _, err := m.RegisterStop("root", func() error {
		close(stopEntered)
		<-releaseStop
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	stopDone := make(chan error, 1)
	go func() { stopDone <- m.StopRoot("root") }()
	<-stopEntered

	if _, err := m.RegisterStop("root", func() error { return nil }); !errors.Is(err, ErrRootStopped) {
		t.Fatalf("RegisterStop error = %v, want ErrRootStopped", err)
	}
	if _, err := m.Start(context.Background(), "root", os.Args[0], []string{"-test.run=^TestProcessHelper$"}, processOptions(t, io.Discard, io.Discard)); !errors.Is(err, ErrRootStopped) {
		t.Fatalf("Start error = %v, want ErrRootStopped", err)
	}
	release()
	if err := <-stopDone; err != nil {
		t.Fatal(err)
	}

	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := m.RegisterStop("other", func() error { return nil }); !errors.Is(err, ErrProcessManagerClosed) {
		t.Fatalf("RegisterStop after Close = %v, want ErrProcessManagerClosed", err)
	}
	if _, err := m.Start(context.Background(), "other", os.Args[0], []string{"-test.run=^TestProcessHelper$"}, processOptions(t, io.Discard, io.Discard)); !errors.Is(err, ErrProcessManagerClosed) {
		t.Fatalf("Start after Close = %v, want ErrProcessManagerClosed", err)
	}
}

func TestProcessRootIsolationAndClose(t *testing.T) {
	m := NewProcessManager()
	p1 := startSleepingProcess(t, m, context.Background(), "one")
	p2 := startSleepingProcess(t, m, context.Background(), "two")
	if err := m.StopRoot("one"); err != nil {
		t.Fatal(err)
	}
	if err := p1.Wait(); err == nil {
		t.Fatal("stopped process exited successfully, want signal error")
	}
	if err := syscall.Kill(p2.PID(), 0); err != nil {
		t.Fatalf("stopping root one killed root two: %v", err)
	}
	p3, err := m.Start(context.Background(), "one", os.Args[0], []string{"-test.run=^TestProcessHelper$"}, ProcessOptions{
		Cwd: t.TempDir(), Env: map[string]string{processHelper: "sleep"}, Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard,
	})
	if err != nil {
		t.Fatalf("reactivate stopped root: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if err := p2.Wait(); err == nil {
		t.Fatal("Close did not kill the remaining process")
	}
	if err := p3.Wait(); err == nil {
		t.Fatal("Close did not kill the reactivated root")
	}
	if _, err := m.Start(context.Background(), "three", os.Args[0], nil, processOptions(t, io.Discard, io.Discard)); err == nil {
		t.Fatal("closed manager accepted a process")
	}
}

func TestProcessRootStopsRegisteredDependency(t *testing.T) {
	m := NewProcessManager()
	stopped := make(chan struct{})
	unregister, err := m.RegisterStop("root", func() error {
		close(stopped)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.StopRoot("root"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	default:
		t.Fatal("registered dependency was not stopped")
	}
	unregister()
	if unregister2, err := m.RegisterStop("root", func() error { return nil }); err != nil {
		t.Fatalf("reactivate root: %v", err)
	} else {
		unregister2()
	}
}

func TestProcessRootContainsDependencyStopPanic(t *testing.T) {
	m := NewProcessManager()
	called := false
	if _, err := m.RegisterStop("root", func() error { panic("stop exploded") }); err != nil {
		t.Fatal(err)
	}
	if _, err := m.RegisterStop("root", func() error { called = true; return nil }); err != nil {
		t.Fatal(err)
	}
	err := m.StopRoot("root")
	if err == nil || !strings.Contains(err.Error(), "stop exploded") {
		t.Fatalf("stop panic error=%v", err)
	}
	if !called {
		t.Fatal("one stop panic prevented remaining cleanup")
	}
}

func TestProcessContextCancellation(t *testing.T) {
	m := NewProcessManager()
	ctx, cancel := context.WithCancel(context.Background())
	p := startSleepingProcess(t, m, ctx, "root")
	cancel()
	waitProcess(t, p)
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestProcessPipes(t *testing.T) {
	m := NewProcessManager()
	process, stdin, stdout, err := m.StartPiped(context.Background(), "root", "sh", []string{"-c", "read value; printf 'got:%s' \"$value\""}, ProcessOptions{
		Cwd:    t.TempDir(),
		Stderr: io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(stdin, "hello\n"); err != nil {
		t.Fatal(err)
	}
	stdin.Close()
	got, err := io.ReadAll(stdout)
	if err != nil {
		t.Fatal(err)
	}
	stdout.Close()
	if err := process.Wait(); err != nil {
		t.Fatal(err)
	}
	if string(got) != "got:hello" {
		t.Fatalf("stdout = %q", got)
	}
}

func TestProcessKillStopsItsGroup(t *testing.T) {
	m := NewProcessManager()
	p := startSleepingProcess(t, m, context.Background(), "root")
	if err := p.Kill(); err != nil {
		t.Fatal(err)
	}
	waitProcess(t, p)
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestProcessStopRootKillsDescendantGroup(t *testing.T) {
	m := NewProcessManager()
	out, err := os.CreateTemp(t.TempDir(), "descendant-pid")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	p, err := m.Start(context.Background(), "root", os.Args[0], []string{"-test.run=^TestProcessHelper$"}, ProcessOptions{
		Cwd:    t.TempDir(),
		Env:    map[string]string{processHelper: "spawn"},
		Stdin:  strings.NewReader(""),
		Stdout: out,
		Stderr: out,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Wait(); err != nil {
		t.Fatal(err)
	}
	if _, err := out.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	var pid int
	if _, err := fmt.Fscan(out, &pid); err != nil {
		t.Fatalf("parse descendant pid: %v", err)
	}
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("descendant exited before StopRoot: %v", err)
	}
	if err := m.StopRoot("root"); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("descendant remains after StopRoot: %v", err)
	}
}

func TestProcessRacingStopRejectsStart(t *testing.T) {
	for i := range 25 {
		m := NewProcessManager()
		root := fmt.Sprintf("root-%d", i)
		start := make(chan *Process, 1)
		startErr := make(chan error, 1)
		go func() {
			p, err := m.Start(context.Background(), root, os.Args[0], []string{"-test.run=^TestProcessHelper$"}, ProcessOptions{
				Cwd:    t.TempDir(),
				Env:    map[string]string{processHelper: "sleep"},
				Stdin:  strings.NewReader(""),
				Stdout: io.Discard,
				Stderr: io.Discard,
			})
			start <- p
			startErr <- err
		}()
		stopErr := make(chan error, 1)
		go func() { stopErr <- m.StopRoot(root) }()
		p, err := <-start, <-startErr
		if stopErr := <-stopErr; stopErr != nil {
			t.Fatal(stopErr)
		}
		if err == nil {
			if err := m.StopRoot(root); err != nil {
				t.Fatal(err)
			}
			waitProcess(t, p)
		}
		restarted, err := m.Start(context.Background(), root, os.Args[0], []string{"-test.run=^TestProcessHelper$"}, ProcessOptions{
			Cwd: t.TempDir(), Env: map[string]string{processHelper: "exit"}, Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard,
		})
		if err != nil {
			t.Fatalf("reactivate root after StopRoot: %v", err)
		}
		_ = restarted.Wait()
	}
}

func startSleepingProcess(t *testing.T, m *ProcessManager, ctx context.Context, root string) *Process {
	t.Helper()
	p, err := m.Start(ctx, root, os.Args[0], []string{"-test.run=^TestProcessHelper$"}, ProcessOptions{
		Cwd:    t.TempDir(),
		Env:    map[string]string{processHelper: "sleep"},
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func waitProcess(t *testing.T, p *Process) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- p.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("killed process exited successfully")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for killed process")
	}
}

func processOptions(t *testing.T, stdout, stderr io.Writer) ProcessOptions {
	t.Helper()
	return ProcessOptions{Cwd: t.TempDir(), Stdin: strings.NewReader(""), Stdout: stdout, Stderr: stderr}
}

func withEnv(opts ProcessOptions, env map[string]string) ProcessOptions {
	opts.Env = env
	return opts
}

func envMap(values []string) map[string]string {
	result := make(map[string]string, len(values))
	for _, value := range values {
		name, value, ok := strings.Cut(value, "=")
		if ok {
			result[name] = value
		}
	}
	return result
}

func replaceEnvironment(env []string, name, value string) []string {
	prefix := name + "="
	for i := range env {
		if strings.HasPrefix(env[i], prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}
