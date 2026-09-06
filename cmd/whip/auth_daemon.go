package main

import (
	"context"
	"errors"

	"github.com/context-labs/whip/internal/daemon"
)

type providerDaemon interface {
	ReadConfiguration(context.Context) (daemon.RuntimeConfiguration, error)
	SetProviderKey(context.Context, daemon.ProviderKeySetup) (daemon.RuntimeConfiguration, error)
	BeginLogin(context.Context) (daemon.ProviderLoginStatus, error)
	LoginStatus(context.Context, string) (daemon.ProviderLoginStatus, error)
	SelectLoginTeam(context.Context, string, string) (daemon.ProviderLoginStatus, error)
	SelectLoginProject(context.Context, string, string) (daemon.ProviderLoginStatus, error)
	CreateLoginProject(context.Context, string, string) (daemon.ProviderLoginStatus, error)
	ProviderStatus(context.Context, string) (daemon.ProviderStatus, error)
	LogoutProvider(context.Context, string) (daemon.ProviderStatus, error)
	RotateProviderKey(context.Context, string) (daemon.ProviderStatus, error)
	Close() error
}

func connectProviderDaemon(ctx context.Context) (providerDaemon, error) {
	connection, err := connectDaemon(ctx, "cli", daemonClientID("auth"), nil)
	if err != nil {
		return nil, err
	}
	client, ok := connection.(providerDaemon)
	if !ok {
		_ = connection.Close()
		return nil, errors.New("daemon does not support provider operations")
	}
	return client, nil
}

func setupProviderCLI(ctx context.Context, provider, key string, environment bool) error {
	client, err := connectProviderDaemon(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	current, err := client.ReadConfiguration(ctx)
	if err != nil {
		return err
	}
	_, err = client.SetProviderKey(ctx, daemon.ProviderKeySetup{Revision: current.Revision, Provider: provider, Key: key, Environment: environment})
	return err
}
