//go:build unix

package daemon

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// LaunchSelfDaemon starts this executable's hidden daemon mode detached from
// the client terminal. Readiness is established only by protocol initialize.
func LaunchSelfDaemon(paths RuntimePaths) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	logPath := filepath.Join(paths.Home, "daemon.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // paths.Home is the validated owner-only whip runtime.
	if err != nil {
		return err
	}
	if err := logFile.Chmod(0o600); err != nil {
		_ = logFile.Close()
		return err
	}
	command := exec.CommandContext(context.Background(), executable, "_daemon")
	command.Stdin = nil
	command.Stdout = logFile
	command.Stderr = logFile
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		return err
	}
	err = errors.Join(command.Process.Release(), logFile.Close())
	return err
}

func RestartSelfDaemon() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	return syscall.Exec(executable, []string{executable, "_daemon"}, os.Environ())
}
