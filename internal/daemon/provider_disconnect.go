package daemon

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/openaiauth"
	"github.com/context-labs/whip/internal/protocol"
)

// DisconnectProvider clears WHIP-owned credentials and restores normal provider
// setup. Credentials managed outside WHIP remain in their original sources.
func (s *ProviderService) DisconnectProvider(ctx context.Context, p protocol.ProviderDisconnectParams) (ProviderStatus, error) {
	if p.Revision == "" {
		return ProviderStatus{}, errors.New("configuration revision is required")
	}
	s.provisionMu.Lock()
	defer s.provisionMu.Unlock()
	if err := ctx.Err(); err != nil {
		return ProviderStatus{}, err
	}
	cfg, revision, err := config.ReadVersioned()
	if err != nil {
		return ProviderStatus{}, err
	}
	if revision != p.Revision {
		return ProviderStatus{}, config.ErrRevisionConflict
	}
	status, err := s.providerStatus(cfg, p.Provider)
	if err != nil {
		return ProviderStatus{}, err
	}
	switch status.KeySource {
	case "literal", "machine", "subscription":
	default:
		if !status.Disabled {
			return ProviderStatus{}, errors.New("credentials are managed outside Whip; remove them at their source to disconnect")
		}
	}
	inferenceAccount := p.Provider == config.InferenceNetProvider && status.KeySource == "machine"
	if route, ok := cfg.Providers[p.Provider]; ok && p.Provider == config.InferenceNetProvider && strings.TrimRight(route.BaseURL, "/") == config.InferenceNetBaseURL {
		inferenceAccount = true
	}
	_, _, err = config.UpdateVersioned(p.Revision, func(cfg *config.Config) error {
		if route, ok := cfg.Providers[p.Provider]; ok && status.KeySource == "literal" {
			route.APIKey = ""
			if route.APIKeyEnv == "" {
				for _, preset := range config.ProviderPresetPolicy() {
					if preset.ID == p.Provider && strings.TrimRight(route.BaseURL, "/") == strings.TrimRight(preset.Provider.BaseURL, "/") && (route.API == "" || route.API == preset.Provider.API) {
						route.APIKeyEnv = preset.Provider.APIKeyEnv
						break
					}
				}
			}
			cfg.Providers[p.Provider] = route
		}
		cfg.DisabledProviders = slices.DeleteFunc(cfg.DisabledProviders, func(name string) bool { return name == p.Provider })
		return nil
	})
	if err != nil {
		return ProviderStatus{}, err
	}
	// Cancel only after the revision is accepted, so stale UI cannot interrupt login.
	s.interruptProviderLogins(p.Provider)
	if p.Provider == openaiauth.Provider {
		return s.logoutOpenAILocked(ctx)
	}
	if err := config.DeleteCatalog(p.Provider); err != nil {
		return ProviderStatus{}, errors.New("saved credentials removed, but the provider model cache could not be removed")
	}
	if inferenceAccount {
		return s.logoutInferenceNetLocked(ctx)
	}
	return s.ProviderStatus(p.Provider)
}

// Call with provisionMu held so a cancelled login cannot publish credentials.
func (s *ProviderService) interruptProviderLogins(provider string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, flow := range s.flows {
		if flow.status.Provider == provider && loginActive(flow.status.State) {
			flow.status.State, flow.status.UserCode, flow.token = "interrupted", "", ""
			flow.cancel()
		}
	}
}
