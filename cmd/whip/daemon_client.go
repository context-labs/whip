package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
)

var connectDaemon = func(ctx context.Context, clientKind, clientID string, cursors map[string]int64) (daemon.RootConnection, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	paths, err := daemon.Paths(dir)
	if err != nil {
		return nil, err
	}
	return daemon.EnsureClient(ctx, paths, daemon.InitializeParams{
		ProtocolMajor: daemon.ProtocolMajor,
		BuildID:       version,
		ClientKind:    clientKind,
		ClientID:      clientID,
		Capabilities:  []string{"commands", "events", "snapshots"},
		Cursors:       cursors,
	}, func() error { return daemon.LaunchSelfDaemon(paths) })
}

func daemonConnector(clientKind, clientID string) daemon.RootConnector {
	return func(ctx context.Context, cursors map[string]int64) (daemon.RootConnection, error) {
		return connectDaemon(ctx, clientKind, clientID, cursors)
	}
}

func daemonClientID(kind string) string {
	return fmt.Sprintf("%s-%d", kind, os.Getpid())
}

var daemonCommandIDs atomic.Uint64

var daemonCommandInstance = strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + strconv.Itoa(os.Getpid())

func daemonCommandID(clientID, operation string) string {
	return clientID + "-" + daemonCommandInstance + "-" + operation + "-" + strconv.FormatUint(daemonCommandIDs.Add(1), 10)
}
