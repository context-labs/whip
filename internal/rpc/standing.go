package rpc

import (
	"context"
	"encoding/json"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
)

func dispatchStanding(ctx context.Context, r *runtime.Runtime, method string, raw json.RawMessage) (any, error) {
	if method == "host.standing.read" {
		value, err := r.HostStandingInstructions(ctx)
		return protocol.HostStandingInstructions(value), err
	}
	return decode(raw, func(p protocol.WriteHostStandingInstructionsParams) (any, error) {
		value, err := r.WriteHostStandingInstructions(ctx, p.ExpectedRevision, p.Text)
		return protocol.HostStandingInstructions(value), err
	})
}
