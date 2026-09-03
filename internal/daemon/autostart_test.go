//go:build unix

package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	client, err := EnsureClient(ctx, paths, InitializeParams{ProtocolMajor: 1, BuildID: "current", ClientID: "client", ClientKind: "test"}, launch)
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

func TestEnsureClientReplacesMismatchedBuildAndGeneration(t *testing.T) {
	home := t.TempDir()
	paths, err := Paths(home)
	if err != nil {
		t.Fatal(err)
	}
	restart := make(chan struct{})
	old, err := startTestServer(filepath.Join(home, "sessions.db"), paths, "old", 4, func() { close(restart) })
	if err != nil {
		t.Fatal(err)
	}
	replaced := make(chan runningServer, 1)
	replaceErr := make(chan error, 1)
	go func() {
		<-restart
		if err := old.server.Close(); err != nil {
			replaceErr <- err
			return
		}
		if err := <-old.served; err != nil {
			replaceErr <- err
			return
		}
		next, err := startTestServer(filepath.Join(home, "sessions.db"), paths, "new", 5, nil)
		if err != nil {
			replaceErr <- err
			return
		}
		replaced <- next
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := EnsureClient(ctx, paths, InitializeParams{ProtocolMajor: 1, BuildID: "new", ClientID: "stable", ClientKind: "test"}, func() error {
		return ErrDaemonOwned
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := client.InitializeResult(); got.BuildID != "new" || got.Generation != 5 {
		t.Fatalf("replacement initialize = %+v", got)
	}
	_ = client.Close()
	select {
	case err := <-replaceErr:
		t.Fatal(err)
	case next := <-replaced:
		if err := next.server.Close(); err != nil {
			t.Fatal(err)
		}
		if err := <-next.served; err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestEnsureClientRejectsInvalidRestartNotice(t *testing.T) {
	home := t.TempDir()
	paths, err := Paths(home)
	if err != nil {
		t.Fatal(err)
	}
	store, err := session.Open(filepath.Join(home, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]string{"build_id": "new"})
	commandHash := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s", 4, "new")))
	commandID := "checkpoint-" + hex.EncodeToString(commandHash[:8])
	digest, err := requestDigest("daemon", "", "daemon.checkpoint", payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitCommand(context.Background(), session.CommandAdmission{
		ClientID: "stable", CommandID: commandID, Scope: session.CommandScopeDaemon, RequestDigest: digest,
		Payload: session.RuntimePayload{Data: payload},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FinishCommand(context.Background(), "stable", commandID, "succeeded", session.RuntimePayload{Data: []byte("invalid")}); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(value, ServerOptions{BuildID: "old", Generation: 4})
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- server.ListenAndServe(paths) }()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Lstat(paths.Socket); err == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := EnsureClient(ctx, paths, InitializeParams{ProtocolMajor: 1, BuildID: "new", ClientID: "stable", ClientKind: "test"}, func() error { return ErrDaemonOwned }); err == nil || !strings.Contains(err.Error(), "invalid restart notice") {
		t.Fatalf("invalid restart notice = %v", err)
	}
	_ = server.Close()
	if err := <-served; err != nil {
		t.Fatal(err)
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
