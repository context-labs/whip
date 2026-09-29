package providerhost

import (
	"context"
	"slices"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/session"
)

// Candidate reports only credential availability from the fixed setup presets.
// It never returns credential contents or publishes a provider declaration.
type Candidate struct {
	Provider        string `json:"provider"`
	Source          string `json:"source"`
	Environment     string `json:"environment"`
	CredentialState string `json:"credential_state"`
}

type Candidates struct {
	Revision string      `json:"revision"`
	Items    []Candidate `json:"items"`
}

func (s *Service) Candidates(ctx context.Context) (Candidates, error) {
	snapshot, err := s.snapshot(ctx)
	if err != nil {
		return Candidates{}, err
	}
	result := Candidates{Revision: snapshot.Revision, Items: []Candidate{}}
	for _, preset := range Presets() {
		if _, configured := snapshot.Host.Providers[preset.ID]; configured {
			continue
		}
		for _, environment := range preset.Environments {
			provider := config.Provider{Kind: preset.Kind, BaseURL: preset.BaseURL, CredentialSource: "env", CredentialEnv: environment}
			status, _, err := s.inspect(ctx, provider)
			if err == nil && status.State == "available" {
				result.Items = append(result.Items, Candidate{Provider: preset.ID, Source: "env", Environment: environment, CredentialState: status.State})
			}
		}
		if preset.ID == "inference-net" || preset.ID == "openai-codex" {
			provider := config.Provider{Kind: preset.Kind, BaseURL: preset.BaseURL}
			if preset.ID == "inference-net" {
				provider.CredentialSource = "inference-net"
			}
			status, _, err := s.inspect(ctx, provider)
			if err == nil && (status.State == "available" || status.State == "refresh_required") {
				result.Items = append(result.Items, Candidate{Provider: preset.ID, Source: status.Source, CredentialState: status.State})
			}
		}
	}
	return result, ctx.Err()
}

// UseCandidate is an explicit publication, not discovery or inference. It
// rechecks the chosen approved source before publishing under the supplied CAS.
func (s *Service) UseCandidate(ctx context.Context, revision string, candidate Candidate) (Inventory, error) {
	var provider config.Provider
	for _, preset := range Presets() {
		if preset.ID != candidate.Provider {
			continue
		}
		provider = config.Provider{Kind: preset.Kind, BaseURL: preset.BaseURL}
		switch {
		case candidate.Source == "env" && slices.Contains(preset.Environments, candidate.Environment):
			provider.CredentialSource, provider.CredentialEnv = "env", candidate.Environment
		case candidate.Source == "inference-net" && preset.ID == "inference-net" && candidate.Environment == "":
			provider.CredentialSource = "inference-net"
		case candidate.Source == "openai-codex" && preset.ID == "openai-codex" && candidate.Environment == "":
		default:
			return Inventory{}, ErrInvalid
		}
		status, _, err := s.inspect(ctx, provider)
		if err != nil || status.State != "available" && status.State != "refresh_required" {
			return Inventory{}, ErrCredentials
		}
		return s.Create(ctx, Change{Revision: revision, ID: preset.ID, Provider: provider})
	}
	return Inventory{}, ErrInvalid
}

func (s *Service) SetEnabled(ctx context.Context, revision, id string, enabled bool) (Inventory, error) {
	if err := s.check(ctx); err != nil {
		return Inventory{}, err
	}
	_, err := s.config.Update(ctx, revision, func(host *config.Host) error {
		provider, ok := host.Providers[id]
		if !ok {
			return ErrMissing
		}
		provider.Disabled = !enabled
		host.Providers[id] = provider
		return nil
	})
	if err != nil {
		return Inventory{}, safeConfigError(err)
	}
	return s.List(ctx)
}

// SetPreferences publishes the Providers form in one validated host revision.
func (s *Service) SetPreferences(ctx context.Context, revision string, defaults Defaults, mode session.PermissionMode) (Inventory, error) {
	if err := s.check(ctx); err != nil {
		return Inventory{}, err
	}
	if mode != session.PermissionPrompt && mode != session.PermissionAutomatic {
		return Inventory{}, ErrInvalid
	}
	_, err := s.config.Update(ctx, revision, func(host *config.Host) error {
		if err := applyDefaults(host, defaults, false); err != nil {
			return err
		}
		host.DefaultPermissionMode = mode
		return nil
	})
	if err != nil {
		return Inventory{}, safeConfigError(err)
	}
	return s.List(ctx)
}

// ExecutionPreferences keeps raw default intent distinct from resolved values.
// Nil continuations selects 100; explicit zero disables them. Attempts zero
// selects three total attempts. Import preferences never connect an MCP server.
type ExecutionPreferences struct {
	Engine               session.Engine
	CompactionPercent    int
	CompactionModel      Defaults
	GoalMaxContinuations *int64
	MaxAttempts          int
	ImportClaude         bool
	ImportCodex          bool
}

func (s *Service) SetExecutionPreferences(ctx context.Context, revision string, values ExecutionPreferences) (config.Snapshot, error) {
	if err := s.check(ctx); err != nil {
		return config.Snapshot{}, err
	}
	result, err := s.config.Update(ctx, revision, func(host *config.Host) error {
		if err := applyDefaults(host, values.CompactionModel, true); err != nil {
			return err
		}
		host.Engine = values.Engine
		host.Defaults.Compaction.ThresholdPercent = values.CompactionPercent
		host.GoalMaxContinuations = values.GoalMaxContinuations
		host.MaxAttempts = values.MaxAttempts
		host.SetAgentImportPreferences(values.ImportClaude, values.ImportCodex)
		return nil
	})
	if err != nil {
		return config.Snapshot{}, safeConfigError(err)
	}
	return result, nil
}
