package rpc

import (
	"context"
	"encoding/json"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
)

func dispatchBrowserDriver(ctx context.Context, r *runtime.Runtime, method string, raw json.RawMessage) (any, error) {
	project := func(v runtime.BrowserDriverSelection, err error) (any, error) {
		return protocol.HostBrowserDriver{Revision: v.Revision, ConfiguredDriver: v.ConfiguredDriver, Driver: v.Driver, Pinned: v.Pinned}, err
	}
	switch method {
	case "host.browser_driver":
		return decode(raw, func(protocol.EmptyParams) (any, error) { return project(r.BrowserDriver(ctx)) })
	case "host.set_browser_driver":
		return decode(raw, func(p protocol.SetBrowserDriverParams) (any, error) {
			return project(r.SetBrowserDriver(ctx, p.ExpectedRevision, p.Driver))
		})
	default:
		return nil, ErrMethod
	}
}
