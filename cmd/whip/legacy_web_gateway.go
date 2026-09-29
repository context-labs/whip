package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/legacy/protocol"
	"github.com/context-labs/whip/internal/webgateway"
)

// Retained managed-gateway clients keep their original runner until cutover.
// The public web command uses the native gateway in web.go.
func runGateway(ctx context.Context, paths daemon.RuntimePaths, expected *gatewayReady, ready func(gatewayReady) error) error {
	options := gatewayEnvironment()
	options.SocketPath = paths.Socket
	options.Open = func(ctx context.Context) (webgateway.Client, error) {
		client, err := dialGatewayClient(ctx, paths)
		if err != nil {
			return nil, err
		}
		if expected != nil {
			init := client.InitializeResult()
			if init.RuntimeID != expected.RuntimeID || init.Generation != expected.Generation {
				_ = client.Close()
				return nil, errors.New("daemon generation changed; start the gateway again")
			}
		}
		return client, nil
	}
	// Check compatibility before assets, so an old/stopped runtime has an
	// actionable error even in an unpackaged development binary.
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	client, err := options.Open(checkCtx)
	cancel()
	if err != nil {
		return err
	}
	initializedCheck := client.InitializeResult()
	_ = client.Close()
	if expected == nil {
		expected = &gatewayReady{RuntimeID: initializedCheck.RuntimeID, Generation: initializedCheck.Generation}
	}
	if !gatewayAssetsAvailable() {
		return errors.New("this executable was built without web assets; run `npm ci && task build`, then run `whipcode web` again")
	}
	server, err := webgateway.Start(ctx, options)
	if err != nil {
		return err
	}
	defer func() { _ = server.Close() }()
	initialized := server.InitializeResult()
	if err := ready(gatewayReady{Endpoint: server.Endpoint(), RuntimeID: initialized.RuntimeID, Generation: initialized.Generation}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return nil
	case <-server.Done():
		if ctx.Err() != nil {
			return nil
		}
		return server.Err()
	}
}

func dialGatewayClient(ctx context.Context, paths daemon.RuntimePaths) (*daemon.Client, error) {
	client, err := daemon.DialClient(ctx, paths, daemon.InitializeParams{
		ProtocolMajor: daemon.ProtocolMajor, BuildID: version,
		ClientID: daemonClientID("web-gateway"), ClientKind: "automation",
		Capabilities: []string{protocol.NetworkClientCapability},
	})
	if err != nil {
		return nil, fmt.Errorf("connect to running daemon: %w; inspect `whipcode daemon status` or run `whipcode daemon start` explicitly", err)
	}
	if !slices.Contains(client.InitializeResult().NegotiatedCapabilities, protocol.NetworkClientCapability) {
		_ = client.Close()
		return nil, errors.New("the running daemon does not support the network-client safety capability; update it and explicitly run `whipcode daemon restart` (interrupts active work)")
	}
	return client, nil
}
