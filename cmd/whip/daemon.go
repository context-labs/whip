package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"

	"github.com/context-labs/whip/internal/buildinfo"
	"github.com/context-labs/whip/internal/capability"
	providersvc "github.com/context-labs/whip/internal/provider"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/rlm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/webgateway"
)

var (
	restartDaemonBinary = daemon.RestartSelfDaemon
	daemonKernelCommand []string
)

func daemonCLI(args []string) error {
	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runDaemon(signals, args)
}

func runDaemon(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("_daemon", flag.ContinueOnError)
	maintenanceFD := fs.Int("maintenance-fd", 0, "inherited backend maintenance descriptor")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("hidden daemon mode does not accept arguments")
	}
	network, err := daemonNetworkEnvironment()
	if err != nil {
		return err
	}
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	paths, err := daemon.Paths(dir)
	if err != nil {
		return err
	}
	startup, err := daemon.AcquireStartup(paths, *maintenanceFD)
	if err != nil {
		return err
	}
	owner, err := daemon.AcquireOwner(paths.Lock)
	_ = startup.Close()
	if err != nil {
		return err
	}
	defer func() { _ = owner.Close() }()

	// The database is not opened or inspected until cross-process ownership
	// is established above.
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	store, err := session.Open(filepath.Join(paths.Home, "sessions.db"))
	if err != nil {
		return err
	}
	processes := capability.NewProcessManager()
	store.SetGlobalPermissionRules(cfg.Permissions.Allow)
	generation, err := store.BeginDaemonGeneration(context.Background(), version)
	if err != nil {
		_ = processes.Close()
		_ = store.Close()
		return err
	}
	limits := rlmLimits(cfg.RLM)
	providers := providersvc.NewProviderService(ctx, strconv.FormatInt(generation, 10))
	defer providers.Close()
	if discovered, discoveryErr := providers.DiscoverProviders(ctx, "", ""); discoveryErr != nil {
		config.LogEvent("provider.discovery", discoveryErr.Error())
	} else if discovered.DiscoveryError != "" {
		config.LogEvent("provider.discovery", discovered.DiscoveryError)
	}
	kernels := rlm.NewManager(limits.MaxWorkers)
	defer kernels.Close()
	factory := daemonRuntimeFactory(store, providers, kernels, limits)
	ownerDaemon, err := daemon.New(store, processes, factory, providers)
	if err != nil {
		_ = processes.Close()
		_ = store.Close()
		return err
	}
	lifecycleRequested := make(chan bool, 1)
	var lifecycleOnce sync.Once
	requestLifecycle := func(restart bool) {
		lifecycleOnce.Do(func() { lifecycleRequested <- restart })
	}
	server, err := daemon.NewServer(ownerDaemon, daemon.ServerOptions{
		BuildID: version, Generation: generation, RuntimeDir: paths.Runtime, NetworkTerminals: network.Terminals,
		Restart: func() { requestLifecycle(true) }, Stop: func() { requestLifecycle(false) },
	})
	if err != nil {
		_ = ownerDaemon.Close()
		return err
	}
	defer func() {
		_ = server.Close()
		_ = os.Remove(paths.Socket)
	}()
	gatewayCtx, stopGateway := context.WithCancel(ctx)
	gatewayDone := make(chan struct{})
	if network.Enabled {
		server.SetGatewayStatus(protocol.GatewayStatus{State: "starting"})
		go func() {
			defer close(gatewayDone)
			manageGateway(gatewayCtx, paths, generation, server.SetGatewayStatus)
		}()
	} else {
		close(gatewayDone)
	}
	closeGateway := func() { stopGateway(); <-gatewayDone }
	defer closeGateway()
	served := make(chan error, 1)
	go func() { defer close(served); served <- server.ListenAndServe(paths) }()
	defer func() { _ = server.Close(); <-served }()
	select {
	case err := <-served:
		return err
	case restart := <-lifecycleRequested:
		closeGateway()
		status := "stopping"
		if restart {
			status = "restarting"
		}
		_ = store.SetDaemonStatus(context.Background(), generation, status)
		if err := server.Close(); err != nil {
			return err
		}
		_ = os.Remove(paths.Socket)
		if err := owner.Close(); err != nil {
			return err
		}
		if restart {
			return restartDaemonBinary()
		}
		return nil
	case <-ctx.Done():
		_ = store.SetDaemonStatus(context.Background(), generation, "stopping")
		return server.Close()
	}
}

// Automatic gateway startup is opt-in. Terminal authority remains daemon-owned
// and is independent of whether this daemon owns a gateway child.
type daemonNetworkOptions struct {
	webgateway.Options
	Enabled   bool
	Terminals bool
}

func daemonNetworkEnvironment() (daemonNetworkOptions, error) {
	options := daemonNetworkOptions{Options: gatewayEnvironment()}
	for _, setting := range []struct {
		name   string
		target *bool
	}{
		{"NETWORK", &options.Enabled}, {"NETWORK_TERMINALS", &options.Terminals},
	} {
		if value := os.Getenv(buildinfo.Env(setting.name)); value != "" {
			enabled, err := strconv.ParseBool(value)
			if err != nil {
				return daemonNetworkOptions{}, fmt.Errorf("%s must be a boolean: %w", buildinfo.Env(setting.name), err)
			}
			*setting.target = enabled
		}
	}
	return options, nil
}
