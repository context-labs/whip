//go:build darwin || linux

package terminal

import (
	"errors"
	"os"
	"syscall"
	"time"

	"github.com/context-labs/whip/internal/capability"
	"golang.org/x/sys/unix"
)

// Rewrap the master in nonblocking mode so Go owns its read/write deadlines.
// All later ioctls use SyscallConn, never File.Fd (which switches to blocking).
func openPTY(cols, rows uint16) (*os.File, *os.File, error) {
	master, slave, err := capability.OpenPTY()
	if err != nil {
		return nil, nil, err
	}
	if err := setSize(master, cols, rows); err != nil {
		_ = master.Close()
		_ = slave.Close()
		return nil, nil, err
	}
	return master, slave, nil
}

func setSize(master *os.File, cols, rows uint16) error {
	raw, err := master.SyscallConn()
	if err != nil {
		return err
	}
	var ioctlErr error
	err = raw.Control(func(fd uintptr) {
		ioctlErr = unix.IoctlSetWinsize(int(fd), unix.TIOCSWINSZ, &unix.Winsize{Col: cols, Row: rows})
	})
	return errors.Join(err, ioctlErr)
}

// The foreground pgid comes from this private controlling terminal. The kernel
// restricts it to this terminal's session; it is never a caller-supplied pid.
func foregroundGroup(master *os.File, shellPID int) int {
	raw, err := master.SyscallConn()
	if err != nil {
		return 0
	}
	group := 0
	_ = raw.Control(func(fd uintptr) { group, _ = unix.IoctlGetInt(int(fd), unix.TIOCGPGRP) })
	if group <= 1 || group == shellPID || group == syscall.Getpgrp() {
		return 0
	}
	return group
}

func killForeground(group int) {
	if group > 1 {
		_ = syscall.Kill(-group, syscall.SIGKILL)
	}
}

func joinForeground(group int) {
	if group <= 1 {
		return
	}
	for !errors.Is(syscall.Kill(-group, 0), syscall.ESRCH) {
		killForeground(group)
		time.Sleep(10 * time.Millisecond)
	}
}
