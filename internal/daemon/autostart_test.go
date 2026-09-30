//go:build unix

package daemon

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/daemonconn"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

type runningServer struct {
	server *Server
	served chan error
}

func TestEnsureClientStartsDaemonAcrossStaleSocket(t *testing.T) {
	home := t.TempDir()
	paths, err := daemonconn.Paths(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Socket, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	var running runningServer
	var launchErr error
	launch := func() error {
		once.Do(func() {
			running, launchErr = startTestServer(filepath.Join(home, "sessions.db"), paths, "current", 1, nil)
		})
		return launchErr
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := EnsureClient(ctx, paths, InitializeParams{ProtocolMajor: ProtocolMajor, BuildID: "current", ClientID: "client", ClientKind: "test"}, launch)
	if err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	if running.server == nil {
		t.Fatal("daemon was not launched")
	}
	if err := running.server.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-running.served; err != nil {
		t.Fatal(err)
	}
}

func TestEnsureClientReportsLaunchAndContextFailures(t *testing.T) {
	paths, err := daemonconn.Paths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	launchErr := errors.New("launch failed")
	if _, err := EnsureClient(context.Background(), paths, InitializeParams{}, nil); err == nil {
		t.Fatal("missing daemon without launcher succeeded")
	}
	if _, err := EnsureClient(context.Background(), paths, InitializeParams{}, func() error { return launchErr }); !errors.Is(err, launchErr) {
		t.Fatalf("launch failure = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := EnsureClient(ctx, paths, InitializeParams{}, func() error { return daemonconn.ErrDaemonOwned }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled autostart = %v", err)
	}
}

func TestEnsureClientAttachesAcrossBuildsWithoutRestart(t *testing.T) {
	home := t.TempDir()
	paths, err := daemonconn.Paths(home)
	if err != nil {
		t.Fatal(err)
	}
	restarted := make(chan struct{}, 1)
	running, err := startTestServer(filepath.Join(home, "sessions.db"), paths, "old", 4, func() { restarted <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer running.server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	client, err := EnsureClient(ctx, paths, InitializeParams{ProtocolMajor: ProtocolMajor, BuildID: "new", ClientID: "stable", ClientKind: "test"}, func() error { t.Error("responsive daemon triggered a launch"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if got := client.InitializeResult(); got.BuildID != "old" || got.Generation != 4 {
		t.Fatalf("unexpected runtime replacement: %+v", got)
	}
	select {
	case <-restarted:
		t.Fatal("build mismatch restarted runtime")
	default:
	}
}

func TestEnsureClientRejectsOldProtocolWithoutLaunching(t *testing.T) {
	home := t.TempDir()
	paths, err := daemonconn.Paths(home)
	if err != nil {
		t.Fatal(err)
	}
	running, err := startTestServer(filepath.Join(home, "sessions.db"), paths, "current", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer running.server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	for _, major := range []int{1, 2, 3, 4} {
		_, err = EnsureClient(ctx, paths, InitializeParams{
			ProtocolMajor: major, ClientID: "old", ClientKind: "test",
		}, func() error { t.Error("protocol mismatch triggered a launch"); return nil })
		failure, ok := errors.AsType[*RPCError](err)
		if !ok || failure.Code != -32001 {
			t.Fatalf("protocol %d rejection = %v", major, err)
		}
	}
}

func startTestServer(path string, paths daemonconn.RuntimePaths, buildID string, generation int64, restart func()) (runningServer, error) {
	store, err := session.Open(path, capability.NewWorkspaces())
	if err != nil {
		return runningServer{}, err
	}
	processes := capability.NewProcessManager()
	value, err := New(store, processes, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		_ = processes.Close()
		_ = store.Close()
		return runningServer{}, err
	}
	server, err := NewServer(value, ServerOptions{BuildID: buildID, Generation: generation, RuntimeDir: paths.Runtime, Restart: restart})
	if err != nil {
		_ = value.Close()
		return runningServer{}, err
	}
	served := make(chan error, 1)
	go func() { served <- server.ListenAndServe(paths) }()
	dialer := net.Dialer{Timeout: 100 * time.Millisecond}
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		// bind creates the socket before listen makes it connectable.
		if conn, err := dialer.DialContext(context.Background(), "unix", paths.Socket); err == nil {
			_ = conn.Close()
			return runningServer{server: server, served: served}, nil
		}
		time.Sleep(time.Millisecond)
	}
	_ = server.Close()
	return runningServer{}, errors.New("test daemon did not publish its socket")
}
