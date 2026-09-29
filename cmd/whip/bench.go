package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/context-labs/whip/internal/buildinfo"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/localruntime"
	"github.com/context-labs/whip/internal/session"
)

// The startup benchmark measures declaration loading and selection validation,
// not model preparation, authentication, network discovery, or runtime startup.
// Its explicit initializer publishes only the native host declaration.
func benchCLI(initialize bool, name, provider string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	// This offline operation needs no socket path or live runtime identity.
	directory, err := filepath.Abs(filepath.Join(buildinfo.Home(home), localruntime.Namespace))
	if err != nil {
		return err
	}
	var host config.Host
	if initialize {
		host, err = config.Initialize(directory)
	} else {
		host, err = config.Load(directory)
		if errors.Is(err, os.ErrNotExist) {
			host, err = config.Default(), nil
		}
	}
	if err != nil {
		return err
	}
	selection := host.Defaults.Model
	if name != "" {
		selection.Name = name
	}
	if provider != "" {
		selection.Provider = provider
	}
	if selection.Equal(session.ModelSelection{}) {
		return nil // Deliberately unconfigured: no provider or credential invented.
	}
	if err := selection.Validate(); err != nil {
		return err
	}
	if _, ok := host.Providers[selection.Provider]; !ok {
		return fmt.Errorf("provider route %q is not configured", selection.Provider)
	}
	// Provider.Models declares optional limits/prices, not a model allowlist.
	// Load validated every declaration; a remote model's existence is unknown.
	return nil
}
