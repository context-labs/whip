//go:build unix

package daemon

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRuntimePathsAndOwnerLock(t *testing.T) {
	if _, err := Paths(""); err == nil {
		t.Fatal("empty runtime home was accepted")
	}
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Paths(blocked); err == nil {
		t.Fatal("regular file was accepted as runtime home")
	}
	if _, err := AcquireOwner(filepath.Join(t.TempDir(), "missing", "daemon.lock")); err == nil {
		t.Fatal("owner lock was created through a missing directory")
	}
	var empty *OwnerLock
	if err := empty.Close(); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(t.TempDir(), strings.Repeat("long", 40))
	paths, err := Paths(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths.Socket) >= maxUnixSocketPath || paths.Runtime == paths.Home {
		t.Fatalf("long-path fallback = %+v", paths)
	}
	lock, err := AcquireOwner(paths.Lock)
	if err != nil {
		t.Fatal(err)
	}
	pid, active, err := ActiveOwnerPID(paths.Lock)
	if err != nil || !active || pid != os.Getpid() {
		t.Fatalf("active owner = pid %d, active %t, err %v", pid, active, err)
	}
	if _, err := AcquireOwner(paths.Lock); !errors.Is(err, ErrDaemonOwned) {
		t.Fatalf("second owner error = %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if pid, active, err := ActiveOwnerPID(paths.Lock); err != nil || active || pid != 0 {
		t.Fatalf("released owner = pid %d, active %t, err %v", pid, active, err)
	}
	lock, err = AcquireOwner(paths.Lock)
	if err != nil {
		t.Fatalf("released owner lock was not reusable: %v", err)
	}
	_ = lock.Close()
}

func TestResolvePathsDoesNotCreateRuntime(t *testing.T) {
	t.Parallel()
	root, err := os.MkdirTemp("/tmp", "whip-paths-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	for _, test := range []struct {
		name     string
		home     string
		fallback bool
	}{
		{name: "short path", home: filepath.Join(root, "home")},
		{name: "long path", home: filepath.Join(root, strings.Repeat("long", 40)), fallback: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			paths, err := ResolvePaths(test.home)
			if err != nil {
				t.Fatal(err)
			}
			if (paths.Runtime != paths.Home) != test.fallback {
				t.Fatalf("runtime fallback = %+v", paths)
			}
			for _, path := range []string{test.home, paths.Home, paths.Runtime} {
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("resolving paths touched %s: %v", path, err)
				}
			}
			created, err := Paths(test.home)
			if test.fallback {
				t.Cleanup(func() { _ = os.RemoveAll(paths.Runtime) })
			}
			if err != nil || created != paths {
				t.Fatalf("startup and discovery resolved different paths: %+v, %+v, %v", paths, created, err)
			}
		})
	}
}

func TestActiveOwnerPIDDoesNotCreateOrModifyLock(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		present bool
	}{
		{name: "missing parent"},
		{name: "stale PID", present: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "runtime", "daemon.lock")
			if test.present {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("12345\n"), 0o400); err != nil {
					t.Fatal(err)
				}
			}
			if pid, owned, err := ActiveOwnerPID(path); err != nil || owned || pid != 0 {
				t.Fatalf("unowned lock = pid %d, owned %t, err %v", pid, owned, err)
			}
			if !test.present {
				if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("owner inspection created runtime files: %v", err)
				}
				return
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0o400 {
				t.Fatalf("owner inspection changed lock permissions: %v, %v", info, err)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "12345\n" {
				t.Fatalf("owner inspection changed lock metadata: %q, %v", data, err)
			}
		})
	}
}

func TestActiveOwnerPIDRejectsMissingOwnerMetadata(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "daemon.lock")
	owner, err := AcquireOwner(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	if err := os.WriteFile(path, []byte("invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if pid, owned, err := ActiveOwnerPID(path); err == nil || !owned || pid != 0 {
		t.Fatalf("invalid owned lock = pid %d, owned %t, err %v", pid, owned, err)
	}
}

func TestOwnerOnlySocketRefusesUnsafeState(t *testing.T) {
	paths, err := Paths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	listener, err := listenLocal(paths)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close(); _ = os.Remove(paths.Socket) })
	if _, err := listenLocal(paths); !errors.Is(err, ErrDaemonOwned) {
		t.Fatalf("responsive daemon replacement = %v", err)
	}
	conn, err := dialLocal(paths, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if err := os.Chmod(paths.Socket, 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := dialLocal(paths, time.Second); err == nil {
		t.Fatal("world-accessible socket should be rejected")
	}
	if conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(context.Background(), "unix", paths.Socket); err != nil {
		t.Fatalf("the underlying socket should still be live: %v", err)
	} else {
		_ = conn.Close()
	}
}

func TestListenLocalReportsStalePathFailures(t *testing.T) {
	paths, err := Paths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(paths.Socket, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.Socket, "entry"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := listenLocal(paths); err == nil {
		t.Fatal("non-removable stale socket path was accepted")
	}

	paths.Socket = filepath.Join(t.TempDir(), "missing", "daemon.sock")
	if _, err := listenLocal(paths); err == nil {
		t.Fatal("socket was created through a missing directory")
	}
	if _, err := dialLocal(paths, time.Millisecond); err == nil {
		t.Fatal("missing socket was dialed")
	}
}
