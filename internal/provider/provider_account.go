package provider

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/inferencenet"
	"github.com/context-labs/whip/internal/openaiauth"
	"github.com/context-labs/whip/internal/protocol"
)

// ListLogins recovers flow identities after a lost begin acknowledgement.
// The same bounded retained flow set backs status queries.
func (s *ProviderService) ListLogins() protocol.ProviderLoginList {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := protocol.ProviderLoginList{Flows: []protocol.ProviderLoginStatus{}}
	for _, flow := range s.flows {
		result.Flows = append(result.Flows, loginSnapshot(flow))
	}
	slices.SortFunc(result.Flows, func(a, b protocol.ProviderLoginStatus) int { return cmp.Compare(a.FlowID, b.FlowID) })
	return result
}

func (s *ProviderService) ProviderStatus(provider string) (protocol.ProviderStatus, error) {
	cfg, err := config.Load()
	if err != nil {
		return protocol.ProviderStatus{}, err
	}
	return s.providerStatus(cfg, provider)
}

func readProviderStatus(provider string) (protocol.ProviderStatus, error) {
	cfg, err := config.Load()
	if err != nil {
		return protocol.ProviderStatus{}, err
	}
	return providerKeyStatus(cfg, provider)
}

func (s *ProviderService) LogoutProvider(ctx context.Context, provider string) (protocol.ProviderStatus, error) {
	if provider == openaiauth.Provider {
		return s.logoutOpenAI(ctx)
	}
	if provider != config.InferenceNetProvider {
		return protocol.ProviderStatus{}, errors.New("provider does not support account logout")
	}
	s.mu.Lock()
	for _, flow := range s.flows {
		if flow.status.Provider != openaiauth.Provider && loginActive(flow.status.State) {
			flow.status.State = "interrupted"
			flow.token = ""
			flow.cancel()
		}
	}
	s.mu.Unlock()
	s.provisionMu.Lock()
	defer s.provisionMu.Unlock()
	return s.logoutInferenceNetLocked(ctx)
}

func (s *ProviderService) logoutInferenceNetLocked(ctx context.Context) (protocol.ProviderStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	auth, err := inferencenet.LoadAuth()
	if err != nil {
		return protocol.ProviderStatus{}, errors.New("could not read provider account on execution host")
	}
	warnings := []string{}
	if auth.HasMachineKey() {
		if err := auth.ArchiveMachineKey(ctx); err != nil {
			warnings = append(warnings, "could not disable the remote machine key")
		}
	}
	if auth.SignedIn() {
		if err := inferencenet.SignOut(ctx, auth.SessionToken); err != nil {
			warnings = append(warnings, "could not close the remote provider session")
		}
	}
	if err := inferencenet.ClearAuth(); err != nil {
		return protocol.ProviderStatus{}, errors.New("could not remove provider account on execution host")
	}
	if err := config.DeleteCatalog(config.InferenceNetProvider); err != nil {
		return protocol.ProviderStatus{}, errors.New("signed out, but the provider model cache could not be removed")
	}
	result, err := readProviderStatus(config.InferenceNetProvider)
	result.Warnings = warnings
	return result, err
}

func (s *ProviderService) RotateProviderKey(ctx context.Context, provider string) (protocol.ProviderStatus, error) {
	if provider != config.InferenceNetProvider {
		return protocol.ProviderStatus{}, errors.New("provider does not support machine key rotation")
	}
	s.provisionMu.Lock()
	defer s.provisionMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	auth, err := inferencenet.LoadAuth()
	if err != nil {
		return protocol.ProviderStatus{}, errors.New("could not read provider account on execution host")
	}
	if !auth.SignedIn() {
		return protocol.ProviderStatus{}, errors.New("run provider login before rotating the machine key")
	}
	if _, err := auth.Rotate(ctx); err != nil {
		return protocol.ProviderStatus{}, errors.New("provider key rotation interrupted; inspect account status before retrying")
	}
	if err := inferencenet.SaveAuth(auth); err != nil {
		return protocol.ProviderStatus{}, errors.New("could not save rotated provider key on execution host")
	}
	if err := config.DeleteCatalog(provider); err != nil {
		return protocol.ProviderStatus{}, errors.New("key rotated, but the provider model cache could not be removed")
	}
	return readProviderStatus(provider)
}
