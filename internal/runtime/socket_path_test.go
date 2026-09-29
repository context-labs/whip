package runtime

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/runtimepath"
)

func longSocketDirectory(t *testing.T) (string, string) {
	t.Helper()
	directory := filepath.Join(t.TempDir(), strings.Repeat("long", 40))
	socket := runtimepath.Socket(directory)
	t.Cleanup(func() { _ = os.Remove(socket); _ = os.Remove(filepath.Dir(socket)) })
	return directory, socket
}

func TestLongSocketOwnerRejectsUnsafeFallback(t *testing.T) {
	for _, mode := range []string{"symlink", "public", "file"} {
		t.Run(mode, func(t *testing.T) {
			directory, socket := longSocketDirectory(t)
			parent := filepath.Dir(socket)
			switch mode {
			case "symlink":
				if err := os.Symlink(t.TempDir(), parent); err != nil {
					t.Fatal(err)
				}
			case "public":
				if err := os.Mkdir(parent, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(parent, 0o755); err != nil {
					t.Fatal(err)
				}
			case "file":
				if err := os.Mkdir(parent, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(socket, []byte("preserve occupant"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if r, err := Open(t.Context(), directory, model.Scripted{}, Options{}); err == nil {
				_ = r.Close()
				t.Fatal("unsafe fallback accepted")
			}
			if mode == "file" {
				raw, err := os.ReadFile(socket)
				if err != nil || string(raw) != "preserve occupant" {
					t.Fatal("foreign occupant modified", err)
				}
			}
		})
	}
}

func TestLongSocketOwnerReclaimsOnlyStaleSocketUnderDurableLock(t *testing.T) {
	directory, socket := longSocketDirectory(t)
	parent := filepath.Dir(socket)
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Net: "unix", Name: socket})
	if err != nil {
		t.Fatal(err)
	}
	listener.SetUnlinkOnClose(false)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := Open(t.Context(), directory, model.Scripted{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	if r.SocketPath() != socket {
		t.Fatal("discovery and owner disagree")
	}
	if _, err := os.Lstat(socket); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("stale socket retained", err)
	}
	if err := os.WriteFile(socket, []byte("sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	if second, err := Open(t.Context(), directory, model.Scripted{}, Options{}); !errors.Is(err, ErrOwned) {
		if second != nil {
			_ = second.Close()
		}
		t.Fatal("second owner admitted", err)
	}
	raw, err := os.ReadFile(socket)
	if err != nil || string(raw) != "sentinel" {
		t.Fatal("competing owner touched socket", err)
	}
	if err := os.Remove(socket); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(parent); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("empty fallback not released", err)
	}
}
