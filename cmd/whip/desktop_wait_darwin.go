package main

import (
	"errors"

	"golang.org/x/sys/unix"
)

type desktopProcessWatcher struct {
	fd   int
	done bool
}

func desktopWatchProcess(pid int) (*desktopProcessWatcher, error) {
	if pid <= 0 {
		return nil, errors.New("invalid SSH process ID")
	}
	fd, err := unix.Kqueue()
	if err != nil {
		return nil, err
	}
	unix.CloseOnExec(fd)
	change := []unix.Kevent_t{{
		Ident: uint64(pid), Filter: unix.EVFILT_PROC,
		Flags: unix.EV_ADD | unix.EV_ENABLE | unix.EV_ONESHOT, Fflags: unix.NOTE_EXIT,
	}}
	_, err = unix.Kevent(
		fd,
		change,
		nil,
		nil,
	)
	// An already-exited child still has its unreaped PID reserved for us.
	if errors.Is(err, unix.ESRCH) {
		return &desktopProcessWatcher{fd: fd, done: true}, nil
	}
	if err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	return &desktopProcessWatcher{fd: fd}, nil
}

func (w *desktopProcessWatcher) exited() (bool, error) {
	if w.done {
		return true, nil
	}
	events := make([]unix.Kevent_t, 1)
	n, err := unix.Kevent(
		w.fd,
		nil,
		events,
		&unix.Timespec{},
	)
	if errors.Is(err, unix.EINTR) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if n == 1 {
		if events[0].Flags&unix.EV_ERROR != 0 {
			code := events[0].Data
			if code < 0 {
				return false, errors.New("invalid SSH process watcher error")
			}
			return false, unix.Errno(code)
		}
		w.done = events[0].Fflags&unix.NOTE_EXIT != 0
	}
	return w.done, nil
}

func (w *desktopProcessWatcher) close() { _ = unix.Close(w.fd) }
