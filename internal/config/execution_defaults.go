package config

import (
	"context"
	"fmt"

	"github.com/context-labs/whip/internal/session"
)

// ExecutionDefaults projects only execution preferences. Compaction zero selects
// 50 percent. Attempts include the first request; continuations exclude the
// initial goal input. Neither setting authorizes replay of an uncertain effect.
type ExecutionDefaults struct {
	Engine               session.Engine
	Effort               string
	CompactionPercent    int
	GoalMaxContinuations int64
	MaxAttempts          int
}

func (h Host) ExecutionDefaults() ExecutionDefaults {
	continuations := int64(100)
	if h.GoalMaxContinuations != nil {
		continuations = *h.GoalMaxContinuations
	}
	attempts := h.MaxAttempts
	if attempts == 0 {
		attempts = 3
	}
	return ExecutionDefaults{
		Engine: h.Engine, Effort: h.Defaults.Model.Effort,
		CompactionPercent:    h.Defaults.Compaction.ThresholdPercent,
		GoalMaxContinuations: continuations, MaxAttempts: attempts,
	}
}

func (d ExecutionDefaults) Validate() error {
	if err := d.Engine.Validate(); err != nil {
		return err
	}
	if d.MaxAttempts < 1 || d.MaxAttempts > session.MaxModelAttempts || d.GoalMaxContinuations < 0 {
		return fmt.Errorf("%w: attempts must be a positive exact integer and additional goal continuations must be nonnegative", session.ErrInvalid)
	}
	if err := (session.CompactionPolicy{ThresholdPercent: d.CompactionPercent}).Validate(); err != nil {
		return err
	}
	return (session.ModelSelection{Provider: "validation", Name: "validation", Effort: d.Effort}).Validate()
}

// SetExecutionDefaults shares the host revision with all other configuration
// edits. Existing sessions, goals, and prepared provider calls remain unchanged.
func (a *Authority) SetExecutionDefaults(ctx context.Context, expected string, values ExecutionDefaults) (Snapshot, error) {
	if err := values.Validate(); err != nil {
		return Snapshot{}, err
	}
	return a.Update(ctx, expected, func(host *Host) error {
		if values == host.ExecutionDefaults() {
			return nil
		}
		host.Engine = values.Engine
		host.Defaults.Model.Effort = values.Effort
		host.Defaults.Compaction.ThresholdPercent = values.CompactionPercent
		host.GoalMaxContinuations = new(values.GoalMaxContinuations)
		host.MaxAttempts = values.MaxAttempts
		return nil
	})
}
