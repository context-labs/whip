package rpc

import (
	"context"
	"encoding/json"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
)

func dispatchReload(ctx context.Context, r *runtime.Runtime, method string, raw json.RawMessage) (any, error) {
	if method == "sessions.reload" {
		return decode(raw, func(p protocol.ReloadSessionParams) (any, error) {
			value, err := r.ReloadSession(ctx, session.ReloadRequest{ID: string(p.EditID), SessionID: session.SessionID(p.SessionID), ExpectedRevision: session.Revision(p.ExpectedRevision)})
			if err != nil {
				return nil, err
			}
			return protocol.ReloadEditFromDomain(value)
		})
	}
	return decode(raw, func(p protocol.ReloadEditParams) (any, error) {
		var value session.ReloadEdit
		var err error
		switch method {
		case "sessions.reload_edit":
			value, err = r.ReloadEdit(ctx, session.SessionID(p.SessionID), string(p.EditID))
		case "sessions.cancel_reload":
			value, err = r.CancelReload(ctx, session.SessionID(p.SessionID), string(p.EditID))
		default:
			return nil, ErrMethod
		}
		if err != nil {
			return nil, err
		}
		return protocol.ReloadEditFromDomain(value)
	})
}
