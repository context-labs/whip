package tui

import (
	"context"

	"github.com/context-labs/whip/internal/protocol"
)

type setupHost interface {
	ReadConfiguration(context.Context) (protocol.RuntimeConfiguration, error)
	UpdateConfiguration(context.Context, protocol.ConfigurationUpdate) (protocol.RuntimeConfiguration, error)
	ReadProvider(context.Context, string) (protocol.ProviderConfiguration, error)
	CreateProvider(context.Context, protocol.ProviderCreateParams) (protocol.ProviderConfiguration, error)
	UpdateProvider(context.Context, protocol.ProviderUpdateParams) (protocol.ProviderConfiguration, error)
	RemoveProvider(context.Context, protocol.ProviderRemoveParams) (protocol.ProviderRemoveResult, error)
	DisconnectProvider(context.Context, protocol.ProviderDisconnectParams) (protocol.ProviderStatus, error)
	SetProviderKey(context.Context, protocol.ProviderKeySetup) (protocol.RuntimeConfiguration, error)
	ListProvidersFor(context.Context, string, string) (protocol.ProviderList, error)
	DiscoverProviders(context.Context, string, string) (protocol.ProviderList, error)
	ProviderCatalogsFor(context.Context, string, bool) (protocol.ProviderCatalogsResult, error)
	ListLogins(context.Context) (protocol.ProviderLoginList, error)
	BeginProviderLogin(context.Context, string) (protocol.ProviderLoginStatus, error)
	CancelLogin(context.Context, string) (protocol.ProviderLoginStatus, error)
	LoginStatus(context.Context, string) (protocol.ProviderLoginStatus, error)
	SelectLoginTeam(context.Context, string, string) (protocol.ProviderLoginStatus, error)
	SelectLoginProject(context.Context, string, string) (protocol.ProviderLoginStatus, error)
	CreateLoginProject(context.Context, string, string) (protocol.ProviderLoginStatus, error)
}
