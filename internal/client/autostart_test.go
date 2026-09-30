//go:build unix

package client

import (
	"context"
	"errors"
	"testing"

	"github.com/context-labs/whip/internal/daemonconn"
	"github.com/context-labs/whip/internal/protocol"
)

func TestEnsureClientReportsLaunchAndContextFailures(t *testing.T) {
	paths, err := daemonconn.Paths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	launchErr := errors.New("launch failed")
	if _, err := EnsureClient(context.Background(), paths, protocol.InitializeParams{}, nil); err == nil {
		t.Fatal("missing daemon without launcher succeeded")
	}
	if _, err := EnsureClient(context.Background(), paths, protocol.InitializeParams{}, func() error { return launchErr }); !errors.Is(err, launchErr) {
		t.Fatalf("launch failure = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := EnsureClient(ctx, paths, protocol.InitializeParams{}, func() error { return daemonconn.ErrDaemonOwned }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled autostart = %v", err)
	}
}
