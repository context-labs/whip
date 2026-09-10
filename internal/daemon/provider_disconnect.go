package daemon

import (
	"context"
	"errors"
	"slices"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/openaiauth"
	"github.com/context-labs/whip/internal/protocol"
)

// DisconnectProvider removes WHIP-owned credentials and opts out of fallback
// discovery. Legacy account logout remains available without changing its contract.
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
		return ProviderStatus{}, errors.New("credentials are managed externally; disable this provider instead")
	}
	_, _, err = config.UpdateVersioned(p.Revision, func(cfg *config.Config) error {
		if route, ok := cfg.Providers[p.Provider]; ok && status.KeySource == "literal" {
			route.APIKey = ""
			cfg.Providers[p.Provider] = route
		}
		if !slices.Contains(cfg.DisabledProviders, p.Provider) {
			cfg.DisabledProviders = append(cfg.DisabledProviders, p.Provider)
		}
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
		return ProviderStatus{}, errors.New("provider disabled, but its model cache could not be removed")
	}
	if p.Provider == config.InferenceNetProvider && status.KeySource == "machine" {
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
