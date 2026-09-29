// whipcode acp is an editor adapter over the same native host as other clients.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	acpsdk "github.com/coder/acp-go-sdk"

	"github.com/context-labs/whip/internal/acp"
	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

func acpCLI(args []string) error {
	input, output := os.Stdin, os.Stdout
	fs := flag.NewFlagSet("acp", flag.ContinueOnError)
	model := fs.String("m", "", "native model name (default: current host default)")
	provider := fs.String("p", "", "native provider route (default: current host default)")
	fs.Usage = func() { fmt.Fprintln(os.Stderr, "usage: whipcode acp [-m model] [-p provider]"); fs.PrintDefaults() }
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("acp accepts flags only")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	c, err := connectNativeRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	var inventory protocol.ProviderInventory
	if err := c.Call(ctx, "providers.list", protocol.EmptyParams{}, &inventory); err != nil {
		return err
	}
	selection := protocol.ModelSelection{}
	if inventory.Defaults != nil {
		selection = *inventory.Defaults
	}
	var override *protocol.ModelSelection
	if *model != "" || *provider != "" {
		if *model != "" {
			selection.Name = *model
		}
		if *provider != "" {
			selection.Provider = protocol.ID(*provider)
		}
		if selection.Provider == "" || selection.Name == "" {
			return errors.New("ACP model override needs both a provider and model, or a configured host default")
		}
		override = &selection
	}
	stdio, err := newACPStdio(input, output, 5*time.Second)
	if err != nil {
		return err
	}
	defer stdio.Close()
	bridge := acp.NewBridge(version, c, acp.Options{Model: override, Vision: acpSupportsVision(ctx, c, selection)})
	connection := acpsdk.NewAgentSideConnection(bridge, stdio, stdio.input)
	bridge.SetAgentConnection(connection)
	select {
	case <-connection.Done():
	case <-ctx.Done():
	}
	// Closing the process-owned stdio releases blocked SDK reads/writes before
	// joining editor observers. It does not cancel any host execution.
	stdio.Close()
	bridge.CloseAll()
	return nil
}

// Startup uses same-scope cached or bundled declarations only. It never probes
// credentials, refreshes a catalog, or executes a provider command.
func acpSupportsVision(ctx context.Context, c *client.Client, selection protocol.ModelSelection) bool {
	if selection.Provider == "" || selection.Name == "" {
		return false
	}
	var cached protocol.ProviderCatalog
	if c.Call(ctx, "providers.catalog", protocol.ProviderParams{Provider: selection.Provider}, &cached) == nil && cached.State == "cached" {
		for _, model := range cached.Models {
			if model.ID == selection.Name && len(model.InputModalities) > 0 {
				return slices.Contains(model.InputModalities, "image")
			}
		}
	}
	var bundled protocol.ProviderModelsResult
	if c.Call(ctx, "providers.bundled", protocol.ProviderParams{Provider: selection.Provider}, &bundled) == nil {
		for _, model := range bundled.Items {
			if model.ID == selection.Name {
				return slices.Contains(model.InputModalities, "image")
			}
		}
	}
	return false
}
