package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/context-labs/whip/internal/buildinfo"
	"github.com/context-labs/whip/internal/tui"
)

func nativeTUI(options tui.NativeOptions) (id string, err error) {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	options.ClientHome, err = filepath.Abs(buildinfo.Home(home))
	if err != nil {
		return "", err
	}
	options.WorkingDirectory, err = os.Getwd()
	if err != nil {
		return "", err
	}
	connecting, cancel := context.WithTimeout(ctx, 60*time.Second)
	connection, err := connectNativeRuntime(connecting)
	cancel()
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, connection.Close()) }()
	// This connection came from our localruntime launcher/readiness path.
	options.KnownLocalFilesystem = true
	return tui.RunNative(ctx, connection, options)
}
