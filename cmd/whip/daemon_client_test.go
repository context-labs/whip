package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/capability"
	daemonclient "github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemonconn"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

func runtimeDBPath(home string) string { return filepath.Join(home, "runtime-v2", "sessions.db") }

func openRuntimeTestStore(t *testing.T, home string) *session.Store {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(runtimeDBPath(home)), 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := session.Open(runtimeDBPath(home), capability.NewWorkspaces())
	if err != nil {
		t.Fatal(err)
	}
	return store
}

// useTestDaemon keeps command tests at the real protocol boundary while
// running the owner in-process; the test binary cannot exec its hidden daemon
// subcommand the way the installed whipcode binary can.
func useTestDaemon(t *testing.T) {
	t.Helper()
	previous := connectDaemon
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	var once sync.Once
	started := false
	connectDaemon = func(callCtx context.Context, clientKind, clientID string, cursors map[string]int64) (daemonclient.RootConnection, error) {
		dir, err := config.Dir()
		if err != nil {
			return nil, err
		}
		paths, err := daemonconn.Paths(dir)
		if err != nil {
			return nil, err
		}
		return daemonclient.EnsureClient(callCtx, paths, protocol.InitializeParams{
			ProtocolMajor: protocol.Major, BuildID: version, ClientKind: clientKind,
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
