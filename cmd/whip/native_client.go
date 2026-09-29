package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/context-labs/whip/internal/buildinfo"
	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/hostcmd"
	"github.com/context-labs/whip/internal/localruntime"
)

func nativeRuntimePaths() (localruntime.Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return localruntime.Paths{}, err
	}
	return localruntime.Resolve(buildinfo.Home(home))
}

// connectNativeRuntime is replaced only by disposable CLI fixtures. Production
// selects the fresh namespace and pins the host identity returned by readiness.
var connectNativeRuntime = func(ctx context.Context) (*client.Client, error) {
	paths, err := nativeRuntimePaths()
	if err != nil {
		return nil, err
	}
	launch, err := nativeRuntimeLaunch()
	if err != nil {
		return nil, err
	}
	ready, err := launchNativeRuntime(ctx, paths, launch)
	if err != nil {
		return nil, err
	}
	if ready.Process == nil {
		return nil, errors.New("native runtime returned no identity")
	}
	return client.Connect(ctx, paths.Socket, &ready.Process.RuntimeID)
}

func nativeRuntimeCLI(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return hostcmd.Run(ctx, args, os.Stdout, os.Stderr)
}
