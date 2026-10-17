package client

import (
	"context"
	"errors"
	"time"

	"github.com/context-labs/whip/internal/daemonconn"
	"github.com/context-labs/whip/internal/protocol"
)

type LaunchDaemon func() error

// EnsureClient attaches to a responsive daemon, starting one when the socket
// is missing or stale. Losing launch races simply attach to the winner.
func EnsureClient(ctx context.Context, paths daemonconn.RuntimePaths, initialize protocol.InitializeParams, launch LaunchDaemon) (*Client, error) {
	return awaitClient(ctx, paths, initialize, launch)
}

func awaitClient(ctx context.Context, paths daemonconn.RuntimePaths, initialize protocol.InitializeParams, launch LaunchDaemon) (*Client, error) {
	client, err := DialClient(ctx, paths, initialize)
	if err == nil {
		return client, nil
	}
	if _, rejected := errors.AsType[*protocol.RPCError](err); rejected || launch == nil {
		return nil, err
	}
	if launchErr := launch(); launchErr != nil && !errors.Is(launchErr, daemonconn.ErrDaemonOwned) {
		return nil, launchErr
	}
	delay := 10 * time.Millisecond
	for {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, errors.Join(err, ctx.Err())
		case <-timer.C:
		}
		client, err = DialClient(ctx, paths, initialize)
		if err == nil {
			return client, nil
		}
		if _, rejected := errors.AsType[*protocol.RPCError](err); rejected {
			return nil, err
		}
		delay = min(delay*2, 250*time.Millisecond)
	}
}
