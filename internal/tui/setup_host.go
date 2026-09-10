package tui

import (
	"context"

	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/protocol"
)

type setupHost interface {
	ReadConfiguration(context.Context) (daemon.RuntimeConfiguration, error)
	UpdateConfiguration(context.Context, daemon.ConfigurationUpdate) (daemon.RuntimeConfiguration, error)
	ReadProvider(context.Context, string) (protocol.ProviderConfiguration, error)
	CreateProvider(context.Context, protocol.ProviderCreateParams) (protocol.ProviderConfiguration, error)
	UpdateProvider(context.Context, protocol.ProviderUpdateParams) (protocol.ProviderConfiguration, error)
	RemoveProvider(context.Context, protocol.ProviderRemoveParams) (protocol.ProviderRemoveResult, error)
	DisconnectProvider(context.Context, protocol.ProviderDisconnectParams) (protocol.ProviderStatus, error)
	SetProviderKey(context.Context, daemon.ProviderKeySetup) (daemon.RuntimeConfiguration, error)
	ListProvidersFor(context.Context, string, string) (protocol.ProviderList, error)
	DiscoverProviders(context.Context, string, string) (protocol.ProviderList, error)
	ProviderCatalogsFor(context.Context, string, bool) (protocol.ProviderCatalogsResult, error)
	ListLogins(context.Context) (daemon.ProviderLoginList, error)
	BeginProviderLogin(context.Context, string) (daemon.ProviderLoginStatus, error)
	CancelLogin(context.Context, string) (daemon.ProviderLoginStatus, error)
	LoginStatus(context.Context, string) (daemon.ProviderLoginStatus, error)
	SelectLoginTeam(context.Context, string, string) (daemon.ProviderLoginStatus, error)
	SelectLoginProject(context.Context, string, string) (daemon.ProviderLoginStatus, error)
	CreateLoginProject(context.Context, string, string) (daemon.ProviderLoginStatus, error)
}
