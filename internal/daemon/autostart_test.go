//go:build unix

package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

type runningServer struct {
	server *Server
	served chan error
}

func TestEnsureClientStartsDaemonAcrossStaleSocket(t *testing.T) {
	home := t.TempDir()
	paths, err := Paths(home)
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
	paths, err := Paths(t.TempDir())
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
	if _, err := EnsureClient(ctx, paths, InitializeParams{}, func() error { return ErrDaemonOwned }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled autostart = %v", err)
	}
}

func TestLaunchDaemonProcessUsesOwnerOnlyLog(t *testing.T) {
	paths, err := Paths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := launchDaemonProcess(paths, "/usr/bin/true"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(paths.Home, "daemon.log"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("daemon log mode = %v, %v", info, err)
	}
}

func TestSelfLaunchAndRestartUseCurrentExecutable(t *testing.T) {
	previousExecutable, previousReplace := selfExecutable, replaceProcess
	selfExecutable = func() (string, error) { return "/usr/bin/true", nil }
	var replaced bool

	replaceProcess = func(path string, args, _ []string) error {
		replaced = path == "/usr/bin/true" && len(args) == 2 && args[1] == "_daemon"
		return errors.New("exec stopped for test")
	}
	t.Cleanup(func() { selfExecutable, replaceProcess = previousExecutable, previousReplace })
	paths, err := Paths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := LaunchSelfDaemon(paths); err != nil {
		t.Fatal(err)
	}
	if err := RestartSelfDaemon(); err == nil || !replaced {
		t.Fatalf("restart replacement = %v, called=%t", err, replaced)
	}
}

func TestSelfLaunchAndRestartReportExecutableFailures(t *testing.T) {
	previousExecutable := selfExecutable
	want := errors.New("executable unavailable")
	selfExecutable = func() (string, error) { return "", want }
	t.Cleanup(func() { selfExecutable = previousExecutable })
	paths, err := Paths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := LaunchSelfDaemon(paths); !errors.Is(err, want) {
		t.Fatalf("launch executable error = %v", err)
	}
	if err := RestartSelfDaemon(); !errors.Is(err, want) {
		t.Fatalf("restart executable error = %v", err)
	}
	if err := launchDaemonProcess(paths, filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing daemon executable launched")
	}
}

func TestEnsureClientAttachesAcrossBuildsWithoutRestart(t *testing.T) {
	home := t.TempDir()
	paths, err := Paths(home)
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
	paths, err := Paths(home)
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
	for _, major := range []int{1, 2, 3} {
		_, err = EnsureClient(ctx, paths, InitializeParams{
			ProtocolMajor: major, ClientID: "old", ClientKind: "test",
		}, func() error { t.Error("protocol mismatch triggered a launch"); return nil })
		failure, ok := errors.AsType[*RPCError](err)
		if !ok || failure.Code != -32001 {
			t.Fatalf("protocol %d rejection = %v", major, err)
		}
	}
}

func startTestServer(path string, paths RuntimePaths, buildID string, generation int64, restart func()) (runningServer, error) {
	store, err := session.Open(path)
	if err != nil {
		return runningServer{}, err
	}
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
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
	for range 100 {
		if _, err := os.Lstat(paths.Socket); err == nil {
			return runningServer{server: server, served: served}, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			_ = server.Close()
			return runningServer{}, err
		}
		time.Sleep(time.Millisecond)
	}
	_ = server.Close()
	return runningServer{}, errors.New("test daemon did not publish its socket")
}
