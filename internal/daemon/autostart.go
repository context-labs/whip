package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type LaunchDaemon func() error

// EnsureClient attaches to a responsive daemon, starting one when the socket
// is missing or stale. Losing launch races simply attach to the winner.
func EnsureClient(ctx context.Context, paths RuntimePaths, initialize InitializeParams, launch LaunchDaemon) (*Client, error) {
	var restartingFrom int64
	for {
		client, err := awaitClient(ctx, paths, initialize, launch)
		if err != nil {
			return nil, err
		}
		observed := client.InitializeResult()
		if restartingFrom != 0 && observed.Generation <= restartingFrom {
			_ = client.Close()
			timer := time.NewTimer(10 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
			continue
		}
		if initialize.BuildID == "" || observed.BuildID == initialize.BuildID {
			return client, nil
		}
		payload, _ := json.Marshal(map[string]string{"build_id": initialize.BuildID})
		digest := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s", observed.Generation, initialize.BuildID)))
		result, err := client.Command(ctx, CommandParams{
			CommandID: "checkpoint-" + hex.EncodeToString(digest[:8]), Scope: "daemon",
			Operation: "daemon.checkpoint", Payload: payload,
		})
		if err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("checkpoint daemon build %q: %w", observed.BuildID, err)
		}
		var notice RestartNotice
		if err := json.Unmarshal([]byte(result.Output), &notice); err != nil || notice.Generation != observed.Generation {
			_ = client.Close()
			return nil, errors.New("daemon returned an invalid restart notice")
		}
		if err := client.RequestRestart(ctx, notice.Generation); err != nil {
			_ = client.Close()
			return nil, err
		}
		restartingFrom = notice.Generation
	}
}

func awaitClient(ctx context.Context, paths RuntimePaths, initialize InitializeParams, launch LaunchDaemon) (*Client, error) {
	client, err := DialClient(ctx, paths, initialize)
	if err == nil {
		return client, nil
	}
	if launch == nil {
		return nil, err
	}
	if launchErr := launch(); launchErr != nil && !errors.Is(launchErr, ErrDaemonOwned) {
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
		delay = min(delay*2, 250*time.Millisecond)
	}
}
