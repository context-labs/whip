package tui

import (
	"context"
	"fmt"
	"os"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
)

type setupHost interface {
	ReadConfiguration(context.Context) (daemon.RuntimeConfiguration, error)
	UpdateConfiguration(context.Context, daemon.ConfigurationUpdate) (daemon.RuntimeConfiguration, error)
	SetProviderKey(context.Context, daemon.ProviderKeySetup) (daemon.RuntimeConfiguration, error)
	BeginLogin(context.Context) (daemon.ProviderLoginStatus, error)
	LoginStatus(context.Context, string) (daemon.ProviderLoginStatus, error)
	SelectLoginTeam(context.Context, string, string) (daemon.ProviderLoginStatus, error)
	SelectLoginProject(context.Context, string, string) (daemon.ProviderLoginStatus, error)
	CreateLoginProject(context.Context, string, string) (daemon.ProviderLoginStatus, error)
	Close() error
}

func connectSetupHost(ctx context.Context) (setupHost, error) {
	home, err := config.Dir()
	if err != nil {
		return nil, err
	}
	paths, err := daemon.Paths(home)
	if err != nil {
		return nil, err
	}
	return daemon.EnsureClient(ctx, paths, daemon.InitializeParams{
		ProtocolMajor: daemon.ProtocolMajor, BuildID: Version,
		ClientKind: "tui", ClientID: fmt.Sprintf("setup-%d", os.Getpid()),
	}, func() error { return daemon.LaunchSelfDaemon(paths) })
}
