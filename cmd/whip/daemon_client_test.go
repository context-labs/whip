package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/session"
)

func runtimeDBPath(home string) string { return filepath.Join(home, "runtime-v2", "sessions.db") }

func openRuntimeTestStore(t *testing.T, home string) *session.Store {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(runtimeDBPath(home)), 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := session.Open(runtimeDBPath(home))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

// useTestDaemon keeps command tests at the real protocol boundary while
// running the owner in-process; the test binary cannot exec its hidden daemon
// subcommand the way the installed whip binary can.
func useTestDaemon(t *testing.T) {
	t.Helper()
	previous := connectDaemon
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	var once sync.Once
	started := false
	connectDaemon = func(callCtx context.Context, clientKind, clientID string, cursors map[string]int64) (daemon.RootConnection, error) {
		dir, err := config.Dir()
		if err != nil {
			return nil, err
		}
		paths, err := daemon.Paths(dir)
		if err != nil {
			return nil, err
		}
		return daemon.EnsureClient(callCtx, paths, daemon.InitializeParams{
			ProtocolMajor: daemon.ProtocolMajor, BuildID: version, ClientKind: clientKind,
			ClientID: clientID, Capabilities: []string{"commands", "events", "snapshots"}, Cursors: cursors,
		}, func() error {
			once.Do(func() {
				started = true
				go func() { finished <- runDaemon(ctx, nil) }()
			})
			return nil
		})
	}
	t.Cleanup(func() {
		connectDaemon = previous
		cancel()
		if !started {
			return
		}
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("test daemon did not stop")
		}
	})
}
