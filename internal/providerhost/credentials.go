package providerhost

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"time"

	"github.com/context-labs/whip/internal/config"
)

// scope never leaves the process. Credential digests distinguish observable
// file/environment replacement without storing credentials in the cache.
type scope struct {
	Route      [32]byte
	Credential [32]byte
	Generation uint64
	Account    string
}

type authorization struct {
	scope                   scope
	key, account, residency string
	check                   func(context.Context) error
	refresh                 func(context.Context) (string, error)
}

func source(p config.Provider) string {
	if p.Kind == "openai-codex" {
		return "openai-codex"
	}
	if p.CredentialSource != "" {
		return p.CredentialSource
	}
	if p.CredentialEnv != "" {
		return "env"
	}
	return "none"
}

func routeScope(p config.Provider) scope {
	p.Models = nil
	raw, _ := json.Marshal(p)
	return scope{Route: sha256.Sum256(raw)}
}

// inspect never refreshes management tokens or executes secret commands. An
// expired subscription token is a refresh requirement, not a proof of logout.
func (s *Service) inspect(ctx context.Context, p config.Provider) (CredentialStatus, scope, error) {
	state := CredentialStatus{Source: source(p), State: "unavailable", Environment: p.CredentialEnv, File: p.CredentialFile}
	identity := routeScope(p)
	switch state.Source {
	case "command":
		state.State = "unchecked"
		return state, identity, nil
	case "none":
		state.State = "not_required"
		return state, identity, nil
	case "openai-codex":
		if s.openAI == nil {
			return state, identity, ErrCredentials
		}
		generation := s.openAI.Generation()
		value, err := s.openAI.Snapshot()
		if err != nil || generation != s.openAI.Generation() {
			return state, identity, ErrCredentials
		}
		identity.Generation, identity.Account = generation, value.AccountID
		if value.AccessToken == "" {
			state.State = "missing"
			return state, identity, ErrCredentials
		}
		state.State = "available"
		if !value.ExpiresAt.After(time.Now()) {
			state.State = "refresh_required"
		}
		return state, identity, nil
	case "inference-net":
		if s.inference == nil {
			return state, identity, ErrCredentials
		}
		generation := s.inference.Generation()
		value, err := s.inference.Snapshot()
		if err != nil || generation != s.inference.Generation() {
			return state, identity, ErrCredentials
		}
		identity.Generation, identity.Account = generation, value.Scope.TeamID+"\x00"+value.Scope.ProjectID
		if value.MachineKey.Value == "" {
			state.State = "missing"
			return state, identity, ErrCredentials
		}
		identity.Credential = sha256.Sum256([]byte(value.MachineKey.Value))
		state.State = "available"
		return state, identity, nil
	default:
		key, err := p.Credential(ctx, s.lookup)
		if err != nil {
			if state.Source == "env" {
				state.State = "missing"
			}
			return state, identity, ErrCredentials
		}
		identity.Credential = sha256.Sum256([]byte(key))
		state.State = "available"
		return state, identity, nil
	}
}

func (s *Service) capture(ctx context.Context, p config.Provider) (authorization, error) {
	value := authorization{scope: routeScope(p)}
	switch source(p) {
	case "openai-codex":
		if s.openAI == nil {
			return value, ErrCredentials
		}
		captured, err := s.openAI.Capture(ctx)
		if err != nil {
			return value, ErrCredentials
		}
		value.key, value.account, value.residency = captured.Credentials.AccessToken, captured.Credentials.AccountID, captured.Credentials.ComputeResidency
		value.scope.Generation, value.scope.Account = captured.Generation, captured.Credentials.AccountID
		value.check = func(ctx context.Context) error { return s.openAI.Check(ctx, captured) }
		value.refresh = func(ctx context.Context) (string, error) {
			next, err := s.openAI.RefreshCaptured(ctx, captured)
			if err != nil {
				return "", ErrCredentials
			}
			captured = next
			return next.Credentials.AccessToken, nil
		}
	case "inference-net":
		if s.inference == nil {
			return value, ErrCredentials
		}
		captured, err := s.inference.Capture(ctx)
		if err != nil {
			return value, ErrCredentials
		}
		value.key = captured.Key
		value.scope.Generation, value.scope.Account = captured.Generation, captured.TeamID+"\x00"+captured.ProjectID
		value.scope.Credential = sha256.Sum256([]byte(captured.Key))
		value.check = func(ctx context.Context) error { return s.inference.Check(ctx, captured) }
	default:
		key, err := p.Credential(ctx, s.lookup)
		if err != nil {
			return value, ErrCredentials
		}
		value.key = key
		if source(p) != "none" {
			value.scope.Credential = sha256.Sum256([]byte(key))
		}
		if source(p) == "command" {
			// Re-running an administrator's command would be another effect.
			value.check = func(ctx context.Context) error { return ctx.Err() }
		} else {
			captured := value.scope
			value.check = func(ctx context.Context) error {
				_, current, err := s.inspect(ctx, p)
				if err != nil || current != captured {
					return ErrStale
				}
				return nil
			}
		}
	}
	return value, nil
}
