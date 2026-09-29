package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"time"

	"github.com/context-labs/whip/internal/protocol"
)

const (
	openRouterEnvironment = "OPENROUTER_API_KEY"
	inferenceEnvironment  = "INFERENCE_API_KEY"
	inferenceProvider     = "inference-net"
)

func setupProviderCLI(ctx context.Context, provider, key string, environment bool) error {
	client, err := connectNativeRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	var current protocol.ProviderInventory
	if err := client.Call(ctx, "providers.list", protocol.EmptyParams{}, &current); err != nil {
		return err
	}
	params := protocol.ProviderKeySetup{Revision: current.Revision, Provider: provider, Environment: environment}
	if !environment {
		params.Key = &protocol.ProviderKeyPublication{ID: protocol.ID(rand.Text()), Key: key}
	}
	var result protocol.ProviderInventory
	return client.Call(ctx, "providers.setup_key", params, &result)
}

func accountText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func accountWarning(value *string) {
	if value != nil {
		fmt.Fprintln(os.Stderr, "whipcode:", *value)
	}
}

func accountPoll(ctx context.Context) error {
	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func showApproval(url, code, previous *string) {
	if url == nil || *url == *previous {
		return
	}
	*previous = *url
	fmt.Printf("Approve in your browser:\n  %s\n  Code: %s\n", *url, accountText(code))
	openBrowser(*url)
}
