//go:build unix

package daemon

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/daemonconn"
	"golang.org/x/sys/unix"
)

type OwnerLock struct{ file *os.File }

// AcquireOwner must run before opening or inspecting sessions.db.
func AcquireOwner(path string) (*OwnerLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // path comes from validated RuntimePaths.
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, daemonconn.ErrDaemonOwned
		}
		return nil, err
	}
	if err := file.Truncate(0); err != nil {
		_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
		_ = file.Close()
		return nil, err
	}
	if _, err := file.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0); err != nil {
		_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
		_ = file.Close()
		return nil, err
	}
	if err := file.Sync(); err != nil {
		_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
		_ = file.Close()
		return nil, err
	}
	return &OwnerLock{file: file}, nil
}

// ActiveOwnerPID returns the PID recorded by the process currently holding
// the daemon owner lock without creating or modifying the lock file. A missing
// lock is unowned; stale PID text is ignored once the lock is free.
func ActiveOwnerPID(path string) (int, bool, error) {
	file, err := os.Open(path) //nolint:gosec // path comes from validated RuntimePaths.
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	defer func() { _ = file.Close() }()
	if err := unix.Flock(int(file.Fd()), unix.LOCK_SH|unix.LOCK_NB); err == nil {
		_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
		return 0, false, nil
	} else if !errors.Is(err, unix.EWOULDBLOCK) {
		return 0, false, err
	}
	data, err := io.ReadAll(io.LimitReader(file, 64))
	if err != nil {
		return 0, true, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, true, errors.New("daemon lock is owned but has no valid PID metadata")
	}
	return pid, true, nil
}

func (l *OwnerLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := unix.Flock(int(l.file.Fd()), unix.LOCK_UN)
	err = errors.Join(err, l.file.Close())
	l.file = nil
	return err
}

func listenLocal(paths daemonconn.RuntimePaths) (net.Listener, error) {
	if conn, err := (&net.Dialer{Timeout: 150 * time.Millisecond}).DialContext(context.Background(), "unix", paths.Socket); err == nil {
		_ = conn.Close()
		return nil, daemonconn.ErrDaemonOwned
	}
	if err := os.Remove(paths.Socket); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", paths.Socket)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(paths.Socket, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(paths.Socket)
		return nil, err
	}
	return listener, nil
}
