package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/context-labs/whip/internal/instruction"
	"github.com/context-labs/whip/internal/store"
)

// HostStanding describes the active host publication without exposing its path.
// Human edits do not publish a new source or grant any session instruction access.
type HostStanding struct {
	Published bool
	Revision  *string
	Text      *string
}

func (r *Runtime) HostStandingInstructions(ctx context.Context) (HostStanding, error) {
	if err := r.Err(); err != nil {
		return HostStanding{}, err
	}
	if err := ctx.Err(); err != nil {
		return HostStanding{}, err
	}
	path := r.host.StandingInstructionsFile
	if path == "" {
		return HostStanding{}, nil
	}
	value, err := instruction.ReadStandingFile(ctx, path)
	return standingProjection(value), standingError(err)
}

func (r *Runtime) WriteHostStandingInstructions(ctx context.Context, expected, text string) (HostStanding, error) {
	r.standingMu.Lock()
	defer r.standingMu.Unlock()
	if err := r.Err(); err != nil {
		return HostStanding{}, err
	}
	path := r.host.StandingInstructionsFile
	if path == "" {
		return HostStanding{}, fmt.Errorf("%w: host has not published a standing instruction file", store.ErrNotFound)
	}
	value, err := instruction.WriteStandingFile(ctx, path, expected, text)
	return standingProjection(value), standingError(err)
}

func standingProjection(value instruction.StandingFile) HostStanding {
	return HostStanding{Published: true, Revision: &value.Revision, Text: &value.Text}
}

func standingError(err error) error {
	if errors.Is(err, instruction.ErrStandingChanged) {
		return fmt.Errorf("%w: %w", store.ErrConflict, err)
	}
	return err
}
