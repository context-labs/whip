package config

import (
	"context"

	"github.com/context-labs/whip/internal/session"
)

// SetDefaultPermissionMode edits only the default for future roots and forks.
// Existing trees retain their SQL policy; a stale host revision never writes.
func (a *Authority) SetDefaultPermissionMode(ctx context.Context, expected string, mode session.PermissionMode) (Snapshot, error) {
	if err := mode.Validate(); err != nil {
		return Snapshot{}, err
	}
	return a.Update(ctx, expected, func(host *Host) error {
		host.DefaultPermissionMode = mode
		return nil
	})
}
