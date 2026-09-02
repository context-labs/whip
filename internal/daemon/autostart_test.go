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
