package rpc

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
)

func dispatchHostProfiles(ctx context.Context, r *runtime.Runtime, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "host.profiles":
		return decode(raw, func(protocol.EmptyParams) (any, error) {
			value, err := r.HostConfiguration().Snapshot(ctx)
			return hostProfiles(value), err
		})
	case "host.set_profiles":
		return decode(raw, func(p protocol.SetHostProfilesParams) (any, error) {
			profiles := make([]config.RemoteHost, 0, len(p.Profiles))
			for _, profile := range p.Profiles {
				profiles = append(profiles, config.RemoteHost{
					ID: string(profile.ID), Name: profile.Name, URL: profile.URL,
					RuntimeID: string(profile.RuntimeID), ConnectOnLaunch: profile.ConnectOnLaunch,
				})
			}
			value, err := r.HostConfiguration().SetRemoteHosts(ctx, p.ExpectedRevision, profiles)
			return hostProfiles(value), err
		})
	default:
		return nil, ErrMethod
	}
}

func hostProfiles(value config.Snapshot) protocol.HostProfiles {
	result := protocol.HostProfiles{Revision: value.Revision, Profiles: []protocol.HostProfile{}}
	for _, profile := range value.Host.RemoteHosts {
		result.Profiles = append(result.Profiles, protocol.HostProfile{
			ID: protocol.ID(profile.ID), Name: strings.TrimSpace(profile.Name), URL: profile.URL,
			RuntimeID: protocol.ID(profile.RuntimeID), ConnectOnLaunch: profile.ConnectOnLaunch,
		})
	}
	return result
}
