package rpc

import (
	"context"
	"encoding/json"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
)

func dispatchControls(ctx context.Context, r *runtime.Runtime, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "workspace.inspect":
		return decode(raw, func(p protocol.SessionParams) (any, error) {
			value, err := r.Session(ctx, session.SessionID(p.SessionID))
			return protocol.WorkspaceInspection{SessionID: protocol.ID(value.ID), WorkingDirectory: value.WorkingDirectory, ConfigurationRevision: protocol.Counter(value.ConfigRevision)}, err
		})
	case "workspace.set":
		return decode(raw, func(p protocol.WorkspaceSetParams) (any, error) {
			value, err := r.SetWorkingDirectory(ctx, session.WorkspaceSetRequest{ID: string(p.ID), SessionID: session.SessionID(p.SessionID), ExpectedRevision: session.Revision(p.ExpectedRevision), Path: p.Path})
			if err != nil {
				return nil, err
			}
			return protocol.ControlEditFromDomain(value)
		})
	case "run.configure":
		return decode(raw, func(p protocol.RunConfigureParams) (any, error) {
			value, err := r.ConfigureRun(ctx, session.RunConfigureRequest{ID: string(p.ID), SessionID: session.SessionID(p.SessionID), ExpectedRevision: session.Revision(p.ExpectedRevision), Configuration: p.Configuration.Domain()})
			if err != nil {
				return nil, err
			}
			return protocol.ControlEditFromDomain(value)
		})
	default:
		return nil, ErrMethod
	}
}
