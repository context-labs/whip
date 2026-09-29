package rpc

import (
	"context"
	"encoding/json"

	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/browserconfig"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
)

func dispatchExternalBrowser(ctx context.Context, r *runtime.Runtime, method string, raw json.RawMessage) (any, error) {
	project := func(v runtime.ExternalBrowserStatus, err error) (any, error) {
		c := v.Configuration
		return protocol.ExternalBrowserStatus{Revision: v.Revision, Configuration: protocol.ExternalBrowserConfiguration{Mode: c.Mode, Executable: c.Executable, LiveEndpoint: c.LiveEndpoint, LiveProfile: c.LiveProfile, AllowPrivateURLs: c.AllowPrivateURLs}, Driver: v.Driver, DriverPinned: v.DriverPinned}, err
	}
	switch method {
	case "host.external_browser":
		return decode(raw, func(protocol.EmptyParams) (any, error) { return project(r.ExternalBrowserStatus(ctx)) })
	case "host.set_external_browser":
		return decode(raw, func(p protocol.ConfigureExternalBrowserParams) (any, error) {
			c := p.Configuration
			return project(r.ConfigureExternalBrowser(ctx, p.ExpectedRevision, browserconfig.Config{Mode: c.Mode, Executable: c.Executable, LiveEndpoint: c.LiveEndpoint, LiveProfile: c.LiveProfile, AllowPrivateURLs: c.AllowPrivateURLs}))
		})
	case "browser.external_sessions":
		return decode(raw, func(p protocol.SessionParams) (any, error) {
			entries, err := r.ExternalBrowserSessions(ctx, session.SessionID(p.SessionID))
			items := make([]protocol.ExternalBrowserSession, 0, len(entries))
			for _, entry := range entries {
				items = append(items, externalBrowserSession(entry))
			}
			return protocol.ExternalBrowserSessions{Items: items}, err
		})
	case "browser.reconnect_external", "browser.disconnect_external":
		return decode(raw, func(p protocol.ExternalBrowserConnectionParams) (any, error) {
			value, err := r.ChangeExternalBrowserConnection(ctx, session.SessionID(p.RootID), p.Name, string(p.Generation), method == "browser.reconnect_external")
			return externalBrowserSession(value), err
		})
	default:
		return nil, ErrMethod
	}
}

func externalBrowserSession(v browser.NativeDescription) protocol.ExternalBrowserSession {
	return protocol.ExternalBrowserSession{RootID: protocol.ID(v.RootID), Name: v.Name, Mode: v.Mode, Driver: v.Driver, Generation: protocol.ID(v.Generation), Resource: v.Resource, State: v.State}
}
