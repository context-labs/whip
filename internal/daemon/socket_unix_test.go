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
	if _, err := AcquireOwner(paths.Lock); !errors.Is(err, ErrDaemonOwned) {
		t.Fatalf("second owner error = %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	lock, err = AcquireOwner(paths.Lock)
	if err != nil {
		t.Fatalf("released owner lock was not reusable: %v", err)
	}
	_ = lock.Close()
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
