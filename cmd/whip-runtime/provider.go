package main

import (
	"context"
	"fmt"
	"os"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

// Host routes and credentials are refreshed at request preparation. The prepared
// body, route and prices stay fixed across that logical call's recorded retries.
func configuredProvider(directory string) model.OpenAI {
	return model.OpenAI{Resolve: func(ctx context.Context, selection session.ModelSelection) (model.ChatRoute, error) {
		if err := ctx.Err(); err != nil {
			return model.ChatRoute{}, err
		}
		host, err := config.Load(directory)
		if err != nil {
			return model.ChatRoute{}, err
		}
		provider, ok := host.Providers[selection.Provider]
		if !ok {
			return model.ChatRoute{}, fmt.Errorf("provider route %q is not configured", selection.Provider)
		}
		settings, err := provider.Models[selection.Name].Resolve()
		if err != nil {
			return model.ChatRoute{}, err
		}
		credential, err := provider.Credential(os.LookupEnv)
		if err != nil {
			return model.ChatRoute{}, err
		}
		return model.ChatRoute{
			URL: provider.BaseURL, Credential: credential, Prices: settings.Prices,
			MaxOutputTokens: settings.MaxOutputTokens, TimeoutMillis: settings.TimeoutMillis,
			MaxAttempts: settings.MaxAttempts,
		}, nil
	}}
}
