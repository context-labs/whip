package rpc

import (
	"context"
	"encoding/json"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
)

func dispatchExecutionDefaults(ctx context.Context, r *runtime.Runtime, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "host.execution_defaults":
		return decode(raw, func(protocol.EmptyParams) (any, error) {
			value, err := r.HostConfiguration().Snapshot(ctx)
			return executionDefaults(value), err
		})
	case "host.set_execution_defaults":
		return decode(raw, func(p protocol.SetExecutionDefaultsParams) (any, error) {
			d := p.Defaults
			value, err := r.HostConfiguration().SetExecutionDefaults(ctx, p.ExpectedRevision, config.ExecutionDefaults{
				Engine: session.Engine(d.Engine), Effort: d.Effort, CompactionPercent: d.CompactionPercent,
				GoalMaxContinuations: int64(d.GoalMaxContinuations), MaxAttempts: d.MaxAttempts,
			})
			return executionDefaults(value), err
		})
	default:
		return nil, ErrMethod
	}
}

func executionDefaults(value config.Snapshot) protocol.HostExecutionDefaults {
	d := value.Host.ExecutionDefaults()
	return protocol.HostExecutionDefaults{
		Revision: value.Revision,
		Engine:   string(d.Engine), Effort: d.Effort, CompactionPercent: d.CompactionPercent,
		GoalMaxContinuations: protocol.Counter(d.GoalMaxContinuations), MaxAttempts: d.MaxAttempts,
	}
}
