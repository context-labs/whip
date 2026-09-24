package daemon

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/openaiauth"
)

func (s *ProviderService) loginOpenAI(flow *providerLoginFlow) {
	generation := s.openAI.Generation()
	credentials, err := s.openAI.Snapshot()
	needsLogin := credentials.AccessToken == ""
	if err == nil {
		cfg, configErr := config.Load()
		if configErr != nil {
			err = configErr
		} else {
			_, configured := cfg.Providers[openaiauth.Provider]
			needsLogin = needsLogin || configured
		}
	} else if credentials.AccessToken != "" {
		// A revoked login can be replaced. Malformed local storage must be
		// repaired first, avoiding a browser login that cannot be saved.
		needsLogin, err = true, nil
	}
	if err == nil && needsLogin {
		credentials, err = s.openAILogin(flow.ctx, func(url, code string) {
			s.mu.Lock()
			defer s.mu.Unlock()
			if flow.status.State == "authorizing" {
				flow.status.VerificationURL, flow.status.UserCode = url, code
			}
		})
	}
	s.mu.Lock()
	if flow.ctx.Err() != nil || flow.status.State != "authorizing" {
		s.mu.Unlock()
		return
	}
	if err != nil {
		s.failOpenAILogin(flow, err)
		s.mu.Unlock()
		return
	}
	flow.status.State, flow.status.Email = "provisioning", credentials.Email
	s.mu.Unlock()

	s.provisionMu.Lock()
	err = s.openAI.Install(flow.ctx, generation, credentials)
	if err == nil {
		_, _, err = config.UpdateVersioned("", func(cfg *config.Config) error {
			if err := flow.ctx.Err(); err != nil {
				return err
			}
			if err := cfg.UpsertOpenAICodex(); err != nil {
				return err
			}
			cfg.EnableProvider(openaiauth.Provider)
			return nil
		})
		if err != nil {
			err = errors.New("signed in to OpenAI, but configuration could not be saved; start sign-in again to finish setup")
		}
	}
	if err == nil {
		err = config.DeleteCatalog(openaiauth.Provider)
	}
	s.provisionMu.Unlock()
	var catalogErr error
	if err == nil {
		catalogCtx, cancel := context.WithTimeout(flow.ctx, 30*time.Second)
		catalogErr = s.refreshModels(catalogCtx, openaiauth.Provider, config.Provider{
			API: openaiauth.Provider, BaseURL: openaiauth.BaseURL,
		})
		cancel()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if flow.ctx.Err() != nil || flow.status.State != "provisioning" {
		return
	}
	if err != nil {
		s.failOpenAILogin(flow, err)
		return
	}
	flow.status.State = "succeeded"
	if catalogErr != nil {
		flow.status.Error = "Signed in. Model discovery failed; refresh the model catalog to retry."
	}
	flow.status.UserCode = ""
	flow.cancel()
}

func (s *ProviderService) failOpenAILogin(flow *providerLoginFlow, err error) {
	s.failLogin(flow)
	// openaiauth returns bounded errors without upstream bodies or credentials.
	flow.status.Error = err.Error()
	flow.status.UserCode = ""
}

func (s *ProviderService) openAIStatus() (ProviderStatus, error) {
	if s.openAIErr != nil {
		return ProviderStatus{}, s.openAIErr
	}
	cfg, err := config.Load()
	if err != nil {
		return ProviderStatus{}, err
	}
	return s.openAIStatusFromConfig(cfg)
}

func (s *ProviderService) openAIStatusFromConfig(cfg *config.Config) (ProviderStatus, error) {
	if s.openAIErr != nil {
		return ProviderStatus{}, s.openAIErr
	}
	entry, configured := cfg.Providers[openaiauth.Provider]
	status := ProviderStatus{
		Provider: openaiauth.Provider, Configured: configured, KeySource: "subscription",
		AuthMethod: "chatgpt", AuthState: "signed_out", Warnings: []string{},
	}
	credentials, authErr := s.openAI.Snapshot()
	if authErr != nil {
		status.AuthState = "sign_in_required"
		status.Warnings = append(status.Warnings, authErr.Error())
	} else if credentials.AccessToken != "" {
		status.AuthState = "connected"
		if !configured {
			status.AuthState = "setup_required"
		}
	}
	status.Email, status.AccountID, status.Plan = credentials.Email, credentials.AccountID, credentials.Plan
	if configured {
		if err := entry.ValidateOpenAICodex(); err != nil {
			status.AuthState = "configuration_error"
			status.Warnings = append(status.Warnings, err.Error())
		}
	}
	status.Disabled = slices.Contains(cfg.DisabledProviders, openaiauth.Provider)
	status.Available = new(status.AuthState == "connected" && !status.Disabled)
	return status, nil
}

func (s *ProviderService) logoutOpenAI(ctx context.Context) (ProviderStatus, error) {
	if s.openAIErr != nil {
		return ProviderStatus{}, s.openAIErr
	}
	s.mu.Lock()
	for _, flow := range s.flows {
		if flow.status.Provider == openaiauth.Provider && loginActive(flow.status.State) {
			flow.status.State, flow.status.UserCode = "interrupted", ""
			flow.cancel()
		}
	}
	s.mu.Unlock()
	s.provisionMu.Lock()
	defer s.provisionMu.Unlock()
	return s.logoutOpenAILocked(ctx)
}

func (s *ProviderService) logoutOpenAILocked(ctx context.Context) (ProviderStatus, error) {
	if err := ctx.Err(); err != nil {
		return ProviderStatus{}, err
	}
	if err := s.openAI.Logout(); err != nil {
		return ProviderStatus{}, err
	}
	if err := config.DeleteCatalog(openaiauth.Provider); err != nil {
		return ProviderStatus{}, errors.New("signed out of OpenAI, but the cached model catalog could not be removed")
	}
	return s.openAIStatus()
}
