//go:build unix

package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const maxUnixSocketPath = 100

var ErrDaemonOwned = errors.New("another daemon owns this runtime")

type RuntimePaths struct {
	Home    string
	Runtime string
	Socket  string
	Lock    string
}

type OwnerLock struct{ file *os.File }

func Paths(home string) (RuntimePaths, error) {
	if home == "" {
		return RuntimePaths{}, errors.New("runtime home is required")
	}
	abs, err := filepath.Abs(home)
	if err != nil {
		return RuntimePaths{}, err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return RuntimePaths{}, err
	}
	if err := os.Chmod(abs, 0o700); err != nil { //nolint:gosec // directories require execute permission and remain owner-only.
		return RuntimePaths{}, err
	}
	runtimeDir := abs
	socket := filepath.Join(runtimeDir, "daemon.sock")
	if len(socket) >= maxUnixSocketPath {
		digest := sha256.Sum256([]byte(abs))
		runtimeDir = filepath.Join(os.TempDir(), fmt.Sprintf("whip-%d-%s", os.Getuid(), hex.EncodeToString(digest[:8])))
		if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
			return RuntimePaths{}, err
		}
		if err := os.Chmod(runtimeDir, 0o700); err != nil { //nolint:gosec // directories require execute permission and remain owner-only.
			return RuntimePaths{}, err
		}
		socket = filepath.Join(runtimeDir, "daemon.sock")
	}
	return RuntimePaths{Home: abs, Runtime: runtimeDir, Socket: socket, Lock: filepath.Join(runtimeDir, "daemon.lock")}, nil
}

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
			return nil, ErrDaemonOwned
		}
		return nil, err
	}
	return &OwnerLock{file: file}, nil
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

func listenLocal(paths RuntimePaths) (net.Listener, error) {
	if conn, err := (&net.Dialer{Timeout: 150 * time.Millisecond}).DialContext(context.Background(), "unix", paths.Socket); err == nil {
		_ = conn.Close()
		return nil, ErrDaemonOwned
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

func dialLocal(paths RuntimePaths, timeout time.Duration) (net.Conn, error) {
	info, err := os.Lstat(paths.Socket)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0o600 {
		return nil, errors.New("daemon socket is not owner-only")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() {
		return nil, errors.New("daemon socket has a different owner")
	}
	return (&net.Dialer{Timeout: timeout}).DialContext(context.Background(), "unix", paths.Socket)
}
