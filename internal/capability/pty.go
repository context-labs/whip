//go:build darwin || linux

package capability

import (
	"errors"
	"os"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

// OpenPTY returns a slave and a master whose blocked reads and writes are
// interrupted by Close. Use SyscallConn for later master ioctls: File.Fd can
// restore blocking mode and prevent an owning process service from joining.
func OpenPTY() (*os.File, *os.File, error) {
	master, slave, err := pty.Open()
	if err != nil {
		return nil, nil, err
	}
	raw, err := master.SyscallConn()
	fd := -1
	var operationErr error
	if err == nil {
		err = raw.Control(func(value uintptr) {
			fd, operationErr = unix.FcntlInt(value, unix.F_DUPFD_CLOEXEC, 0)
			if operationErr == nil {
				operationErr = unix.SetNonblock(fd, true)
			}
		})
	}
	err = errors.Join(err, operationErr)
	_ = master.Close()
	if err != nil {
		if fd >= 0 {
			_ = unix.Close(fd)
		}
		_ = slave.Close()
		return nil, nil, err
	}
	return os.NewFile(uintptr(fd), "pty-master"), slave, nil
}
