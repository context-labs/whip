package daemon

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/inferencenet"
	"github.com/context-labs/whip/internal/protocol"
)

type ProviderLoginList = protocol.ProviderLoginList
type ProviderNameParams = protocol.ProviderNameParams
type ProviderStatus = protocol.ProviderStatus

// ListLogins recovers flow identities after a lost begin acknowledgement.
// The same bounded retained flow set backs status queries.
func (s *ProviderService) ListLogins() ProviderLoginList {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := ProviderLoginList{Flows: []ProviderLoginStatus{}}
	for _, flow := range s.flows {
		result.Flows = append(result.Flows, loginSnapshot(flow))
	}
	slices.SortFunc(result.Flows, func(a, b ProviderLoginStatus) int { return cmp.Compare(a.FlowID, b.FlowID) })
	return result
}

func (s *ProviderService) ProviderStatus(provider string) (ProviderStatus, error) {
	return readProviderStatus(provider)
}

func readProviderStatus(provider string) (ProviderStatus, error) {
	if provider != config.InferenceNetProvider && provider != "openrouter" {
		return ProviderStatus{}, errors.New("unsupported provider")
	}
	cfg, err := config.Load()
	if err != nil {
		return ProviderStatus{}, err
	}
	entry, ok := cfg.Providers[provider]
	result := ProviderStatus{Provider: provider, Configured: ok, Warnings: []string{}}
	switch {
	case !ok:
		result.KeySource = "none"
	case entry.APIKeyEnv != "":
		result.KeySource = "environment"
	case entry.APIKey != "":
		result.KeySource = "literal"
	default:
		result.KeySource = "machine"
	}
	if provider == config.InferenceNetProvider {
		auth, err := inferencenet.LoadAuth()
		if err != nil {
			return ProviderStatus{}, errors.New("could not read provider account on execution host")
		}
		result.Email, result.ProjectID, result.ProjectName, result.MachineKeyName = auth.UserEmail, auth.ProjectID, auth.ProjectName, auth.MachineKeyName
	}
	return result, nil
}

func (s *ProviderService) LogoutProvider(ctx context.Context, provider string) (ProviderStatus, error) {
	if provider != config.InferenceNetProvider {
		return ProviderStatus{}, errors.New("provider does not support account logout")
	}
	s.mu.Lock()
	for _, flow := range s.flows {
		if loginActive(flow.status.State) {
			flow.status.State = "interrupted"
			flow.token = ""
			flow.cancel()
		}
	}
	s.mu.Unlock()
	s.provisionMu.Lock()
	defer s.provisionMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	auth, err := inferencenet.LoadAuth()
	if err != nil {
		return ProviderStatus{}, errors.New("could not read provider account on execution host")
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
		return ProviderStatus{}, errors.New("could not remove provider account on execution host")
	}
	result, err := readProviderStatus(provider)
	result.Warnings = warnings
	return result, err
}

func (s *ProviderService) RotateProviderKey(ctx context.Context, provider string) (ProviderStatus, error) {
	if provider != config.InferenceNetProvider {
		return ProviderStatus{}, errors.New("provider does not support machine key rotation")
	}
	s.provisionMu.Lock()
	defer s.provisionMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	auth, err := inferencenet.LoadAuth()
	if err != nil {
		return ProviderStatus{}, errors.New("could not read provider account on execution host")
	}
	if !auth.SignedIn() {
		return ProviderStatus{}, errors.New("run provider login before rotating the machine key")
	}
	if _, err := auth.Rotate(ctx); err != nil {
		return ProviderStatus{}, errors.New("provider key rotation interrupted; inspect account status before retrying")
	}
	if err := inferencenet.SaveAuth(auth); err != nil {
		return ProviderStatus{}, errors.New("could not save rotated provider key on execution host")
	}
	return readProviderStatus(provider)
}
