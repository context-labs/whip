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
func configuredProvider(directory string, auth model.SubscriptionAuth, inference model.InferenceAuth) model.OpenAI {
	return model.OpenAI{Auth: auth, InferenceAuth: inference, Resolve: func(ctx context.Context, selection session.ModelSelection) (model.Route, error) {
		if err := ctx.Err(); err != nil {
			return model.Route{}, err
		}
		host, err := config.Load(directory)
		if err != nil {
			return model.Route{}, err
		}
		provider, ok := host.Providers[selection.Provider]
		if !ok {
			return model.Route{}, fmt.Errorf("provider route %q is not configured", selection.Provider)
		}
		settings := provider.Models[selection.Name]
		if settings.MaxAttempts == 0 {
			settings.MaxAttempts = host.ExecutionDefaults().MaxAttempts
		}
		if provider.Kind == "openai-codex" {
			if provider.CredentialEnv != "" || provider.BaseURL != "" {
				return model.Route{}, fmt.Errorf("%w: subscription routes cannot configure an endpoint or API credential", session.ErrInvalid)
			}
			ceiling := model.SubscriptionOutputLimit(selection.Name)
			if ceiling == 0 || settings.MaxOutputTokens < 0 || settings.MaxOutputTokens > 0 && settings.MaxOutputTokens < ceiling {
				return model.Route{}, fmt.Errorf("%w: subscription model requires its verified natural output ceiling", session.ErrInvalid)
			}
			settings.MaxOutputTokens = ceiling
		}
		settings, err = settings.Resolve()
		if err != nil {
			return model.Route{}, err
		}
		credential, err := provider.Credential(ctx, os.LookupEnv)
		if err != nil {
			return model.Route{}, err
		}
		return model.Route{
			Kind: provider.Kind, URL: provider.BaseURL, Credential: credential, Prices: settings.Prices,
			ManagedInference: provider.CredentialSource == "inference-net",
			MaxOutputTokens:  settings.MaxOutputTokens, TimeoutMillis: settings.TimeoutMillis,
			MaxAttempts: settings.MaxAttempts, ContextWindowTokens: settings.ContextWindowTokens,
		}, nil
	}}
}
