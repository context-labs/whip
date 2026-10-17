//go:build unix

package daemonconn

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
)

const maxUnixSocketPath = 100

var ErrDaemonOwned = errors.New("another daemon owns this runtime")

type RuntimePaths struct {
	Home    string
	Runtime string
	Socket  string
	Lock    string
}

func Paths(home string) (RuntimePaths, error) {
	paths, err := ResolvePaths(home)
	if err != nil {
		return RuntimePaths{}, err
	}
	for _, dir := range []string{paths.Home, paths.Runtime} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return RuntimePaths{}, err
		}
		if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // directories require execute permission and remain owner-only.
			return RuntimePaths{}, err
		}
	}
	return paths, nil
}

// ResolvePaths computes daemon locations without creating or modifying them.
// Discovery must use this instead of Paths so inspecting a stopped installation
// does not initialize a runtime or change existing directory permissions.
func ResolvePaths(home string) (RuntimePaths, error) {
	if home == "" {
		return RuntimePaths{}, errors.New("runtime home is required")
	}
	abs, err := filepath.Abs(filepath.Join(home, "runtime-v2"))
	if err != nil {
		return RuntimePaths{}, err
	}
	runtimeDir := abs
	socket := filepath.Join(runtimeDir, "daemon.sock")
	if len(socket) >= maxUnixSocketPath {
		digest := sha256.Sum256([]byte(abs))
		runtimeDir = filepath.Join(os.TempDir(), fmt.Sprintf("whip-%d-%s", os.Getuid(), hex.EncodeToString(digest[:8])))
		socket = filepath.Join(runtimeDir, "daemon.sock")
	}
	return RuntimePaths{Home: abs, Runtime: runtimeDir, Socket: socket, Lock: filepath.Join(runtimeDir, "daemon.lock")}, nil
}

func DialLocal(paths RuntimePaths, timeout time.Duration) (net.Conn, error) {
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
