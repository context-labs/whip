package runtime

import (
	"context"
	"encoding/json"
	"time"

	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tool"
)

func (r *Runtime) preparePermissionInspection(current session.Session, call tool.Invocation) (tool.Prepared, error) {
	var args struct {
		ID session.OperationID `json:"id"`
	}
	if err := decodeArguments(call.Arguments, &args); err != nil {
		return tool.Prepared{}, err
	}
	request := session.PermissionInspection{SessionID: current.ID, ConfigRevision: current.ConfigRevision, Action: call.Name, OperationID: args.ID}
	if err := request.Validate(); err != nil {
		return tool.Prepared{}, err
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return tool.Prepared{}, err
	}
	return tool.Prepared{
		Capability: "permissions.inspect", Resource: string(current.ID), Arguments: raw,
		Acquire: func(ctx context.Context) (func(), error) { return func() {}, ctx.Err() },
		Run: func(ctx context.Context, _ session.OperationID) (any, error) {
			if request.Action == "request" {
				return map[string]any{"status": "invoke_operation", "message": "invoke the exact operation to create a durable permission request"}, nil
			}
			permission, err := r.store.Permission(ctx, current.ID, request.OperationID)
			if err != nil {
				return nil, err
			}
			return struct {
				OperationID session.OperationID     `json:"operation_id"`
				State       session.PermissionState `json:"state"`
				CreatedAt   time.Time               `json:"created_at"`
				ResolvedAt  *time.Time              `json:"resolved_at"`
			}{permission.OperationID, permission.State, permission.CreatedAt, permission.ResolvedAt}, nil
		},
	}, nil
}
