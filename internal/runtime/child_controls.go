package runtime

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tool"
)

func (r *Runtime) prepareChildControl(current session.Session, call tool.Invocation) (tool.Prepared, error) {
	var request any
	switch call.Name {
	case "submit":
		request = &session.ChildSubmit{}
	case "inspect":
		request = &session.ChildInspect{}
	case "list":
		request = &session.ChildList{}
	case "stop", "delete":
		request = &session.ChildTarget{}
	default:
		return tool.Prepared{}, session.ErrInvalid
	}
	if err := decodeArguments(call.Arguments, request); err != nil {
		return tool.Prepared{}, err
	}
	arguments, err := json.Marshal(request)
	if err != nil {
		return tool.Prepared{}, err
	}
	return tool.Prepared{Capability: "agents." + call.Name, Resource: string(current.TreeID), Arguments: arguments, Apply: func(ctx context.Context, id session.OperationID) (any, error) {
		result, err := r.store.ApplyChildControl(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, turn := range result.CancelTurns {
			r.cancelTurn(turn)
		}
		if call.Name == "stop" {
			if err := r.cleanupShellOwners(ctx); err != nil {
				return nil, fmt.Errorf("child stop committed; shell cleanup failed: %w", err)
			}
		}
		if result.Deleted {
			if err := r.cleanupDeletedKernels(ctx); err != nil {
				return nil, fmt.Errorf("child deletion committed; kernel cleanup failed: %w", err)
			}
		}
		r.Wake()
		return result.Value, nil
	}}, nil
}
