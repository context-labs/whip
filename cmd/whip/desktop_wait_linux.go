package main

import (
	"errors"

	"golang.org/x/sys/unix"
)

type desktopProcessWatcher struct {
	pid int
}

func desktopWatchProcess(pid int) (*desktopProcessWatcher, error) {
	return &desktopProcessWatcher{pid: pid}, nil
}

func (w *desktopProcessWatcher) exited() (bool, error) {
	var info unix.Siginfo
	err := unix.Waitid(
		unix.P_PID,
		w.pid,
		&info,
		unix.WEXITED|unix.WNOWAIT|unix.WNOHANG,
		nil,
	)
	if errors.Is(err, unix.EINTR) {
		return false, nil
	}
	return info.Signo != 0, err
}

func (w *desktopProcessWatcher) close() {}
