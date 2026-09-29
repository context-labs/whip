package rpc

import (
	"context"
	"encoding/json"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
)

func dispatchPermissionMode(ctx context.Context, r *runtime.Runtime, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "permissions.policy":
		return decode(raw, func(p protocol.SessionParams) (any, error) {
			value, err := r.PermissionPolicy(ctx, session.SessionID(p.SessionID))
			return protocol.PermissionPolicyFromDomain(value), err
		})
	case "permissions.set_mode":
		return decode(raw, func(p protocol.SetPermissionModeParams) (any, error) {
			value, err := r.SetPermissionMode(ctx, session.PermissionModeRequest{ID: session.PermissionModeEditID(p.EditID), SessionID: session.SessionID(p.SessionID), ExpectedRevision: session.Revision(p.ExpectedRevision), Mode: session.PermissionMode(p.Mode)})
			return protocol.PermissionModeEditFromDomain(value), err
		})
	case "permissions.mode_edit":
		return decode(raw, func(p protocol.PermissionModeEditParams) (any, error) {
			value, err := r.PermissionModeEdit(ctx, session.SessionID(p.SessionID), session.PermissionModeEditID(p.EditID))
			return protocol.PermissionModeEditFromDomain(value), err
		})
	case "host.permission_default":
		value, err := r.HostConfiguration().Snapshot(ctx)
		if err != nil {
			return nil, err
		}
		return defaultPermissionMode(value)
	case "host.set_permission_default":
		return decode(raw, func(p protocol.SetDefaultPermissionModeParams) (any, error) {
			value, err := r.HostConfiguration().SetDefaultPermissionMode(ctx, p.ExpectedRevision, session.PermissionMode(p.Mode))
			if err != nil {
				return nil, err
			}
			return defaultPermissionMode(value)
		})
	default:
		return nil, ErrMethod
	}
}

func defaultPermissionMode(value config.Snapshot) (protocol.DefaultPermissionMode, error) {
	mode, err := session.ResolvePermissionMode(value.Host.DefaultPermissionMode)
	return protocol.DefaultPermissionMode{Mode: string(mode), Revision: value.Revision}, err
}
