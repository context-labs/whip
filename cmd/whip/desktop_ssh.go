//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const desktopSSHGrace = time.Second

type desktopSSHStreams struct {
	parent *os.File
	stdout *os.File
	stderr *os.File
}

func desktopSSHCLI(args []string) int {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
		syscall.SIGHUP,
		syscall.SIGQUIT,
	)
	defer stop()
	err := desktopSSH(ctx, args, desktopSSHStreams{parent: os.Stdin, stdout: os.Stdout, stderr: os.Stderr})
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() > 0 {
		return exitErr.ExitCode()
	}
	// Never include SSH arguments, environment, or prompt text in diagnostics.
	fmt.Fprintln(os.Stderr, "whip desktop: SSH process ended or could not be started")
	return 1
}

// desktopSSH owns only the fixed SSH executable and its process group. Stdin
// belongs to the desktop parent: it is never forwarded to SSH or its descendants.
func desktopSSH(ctx context.Context, args []string, streams desktopSSHStreams) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := streams.parent.Stat()
	if err != nil {
		return err
	}
	if info.Mode()&(os.ModeNamedPipe|os.ModeSocket) == 0 {
		return errors.New("SSH supervision requires a parent lifetime pipe")
	}
	parentFD := int(streams.parent.Fd())
	if ended, err := desktopParentEnded(parentFD, 0); ended || err != nil {
		return errors.New("SSH parent connection is closed")
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	command := exec.Command("/usr/bin/ssh", args...) //nolint:noctx,gosec // Supervise cancellation below; the native desktop passes validated SSH options.
	command.Env = append(os.Environ(), "WHIP_DESKTOP_ASKPASS=1", "SSH_ASKPASS="+executable)
	command.Stdout, command.Stderr = streams.stdout, streams.stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return err
	}

	watcher, err := desktopWatchProcess(command.Process.Pid)
	if err != nil {
		_ = unix.Kill(-command.Process.Pid, unix.SIGKILL)
		desktopReapUnobserved(command.Process)
		return errors.New("could not observe SSH process lifetime")
	}
	defer watcher.close()

	var interrupted error
	for {
		exited, err := watcher.exited()
		if err != nil {
			interrupted = err
			break
		}
		if exited {
			break
		}
		if err := ctx.Err(); err != nil {
			interrupted = err
			break
		}
		ended, err := desktopParentEnded(parentFD, 25*time.Millisecond)
		if ended || err != nil {
			interrupted = errors.New("SSH parent connection closed")
			break
		}
	}

	// Observe exit without reaping first. Keeping the leader's PID reserved until
	// the last group signal prevents PID reuse from targeting an unrelated group.
	_ = unix.Kill(-command.Process.Pid, unix.SIGTERM)
	if interrupted != nil {
		_ = desktopAwaitExit(watcher, desktopSSHGrace)
	}
	// Once the leader has exited, remaining ProxyJump/askpass processes cannot
	// complete this request. Kill them before reaping the leader, including after
	// a normal SSH exit. Never signal a remote daemon or another process group.
	killErr := unix.Kill(-command.Process.Pid, unix.SIGKILL)
	if err := desktopAwaitExit(watcher, 2*time.Second); err != nil {
		_ = command.Process.Release()
		return errors.New("SSH process cleanup timed out")
	}
	waitErr := command.Wait()
	if interrupted != nil {
		return interrupted
	}
	// Darwin reports EPERM when the reserved leader is the group's only member
	// and is already a zombie. The exit observation and Wait above confirm it.
	emptyDarwinGroup := runtime.GOOS == "darwin" && errors.Is(killErr, unix.EPERM)
	if killErr != nil && !errors.Is(killErr, unix.ESRCH) && !emptyDarwinGroup {
		return errors.New("could not stop remaining SSH processes")
	}
	return waitErr
}

// If installing an exit watcher failed (for example, due to a descriptor
// limit), still reap the child after the final kill, without an unbounded Wait.
func desktopReapUnobserved(process *os.Process) {
	defer func() { _ = process.Release() }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		var status unix.WaitStatus
		pid, err := unix.Wait4(process.Pid, &status, unix.WNOHANG, nil)
		if pid == process.Pid || errors.Is(err, unix.ECHILD) {
			return
		}
		if err != nil && !errors.Is(err, unix.EINTR) {
			return
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func desktopParentEnded(fd int, wait time.Duration) (bool, error) {
	if fd < 0 || fd > math.MaxInt32 {
		return false, errors.New("invalid SSH parent descriptor")
	}
	poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	_, err := unix.Poll(poll, int(wait/time.Millisecond))
	if errors.Is(err, unix.EINTR) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if poll[0].Revents&(unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) != 0 {
		return true, nil
	}
	if poll[0].Revents&unix.POLLIN != 0 {
		var discard [256]byte
		n, err := unix.Read(fd, discard[:])
		if errors.Is(err, unix.EINTR) {
			return false, nil
		}
		return n == 0, err
	}
	return false, nil
}

func desktopAwaitExit(watcher *desktopProcessWatcher, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		exited, err := watcher.exited()
		if err != nil || exited {
			return err
		}
		if time.Now().After(deadline) {
			return errors.New("SSH exit deadline exceeded")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
