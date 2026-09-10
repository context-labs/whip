package daemon

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/openaiauth"
	"github.com/context-labs/whip/internal/protocol"
)

var providerIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
var providerEnvironmentPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,255}$`)

func providerPreset(id string) (config.Provider, bool) {
	for _, preset := range config.ProviderPresets() {
		if preset.ID == id {
			return preset.Provider, true
		}
	}
	return config.Provider{}, false
}

func configuredProvider(cfg *config.Config, id string) (config.Provider, bool) {
	if provider, ok := cfg.Providers[id]; ok {
		return provider, true
	}
	return providerPreset(id)
}

func providerRemovalBlockers(cfg *config.Config, id string) []string {
	blockers := []string{}
	if _, builtin := providerPreset(id); builtin || id == "inference" {
		blockers = append(blockers, "Built-in providers can be disabled, not removed.")
	}
	if cfg.DefaultProvider == id {
		blockers = append(blockers, "The default provider references this connection.")
	}
	if cfg.CompactProvider == id {
		blockers = append(blockers, "The compaction provider references this connection.")
	}
	if _, exists := cfg.Providers[id]; exists && len(cfg.Providers) == 1 && len(cfg.Models) == 0 {
		blockers = append(blockers, "Add another connection before removing this one, or disable it instead.")
	}
	for alias, model := range cfg.Models {
		if slices.Contains(model.Providers, id) {
			blockers = append(blockers, fmt.Sprintf("Model alias %q references this connection.", alias))
		}
	}
	slices.Sort(blockers)
	return blockers
}

// ReadProvider returns editable metadata without resolving secret commands or
// contacting an endpoint. Browser credentials remain in their existing managers.
func (s *ProviderService) ReadProvider(id string) (protocol.ProviderConfiguration, error) {
	cfg, revision, err := config.ReadVersioned()
	if err != nil {
		return protocol.ProviderConfiguration{}, err
	}
	return s.providerConfiguration(cfg, revision, id)
}

func (s *ProviderService) providerConfiguration(cfg *config.Config, revision, id string) (protocol.ProviderConfiguration, error) {
	provider, ok := configuredProvider(cfg, id)
	if !ok {
		return protocol.ProviderConfiguration{}, errors.New("unknown provider")
	}
	_, builtin := providerPreset(id)
	status, err := s.providerStatus(cfg, id)
	if err != nil {
		return protocol.ProviderConfiguration{}, err
	}
	mode := status.KeySource
	switch {
	case provider.Auth == "none":
		mode = "none"
	case mode == "literal":
		mode = "api_key"
	case mode == "env_file" || mode == "key_file":
		mode = "environment"
	case mode == "none":
		mode = "api_key"
	}
	result := protocol.ProviderConfiguration{
		Revision: revision, Provider: id, Custom: !builtin,
		Definition: redactedProviderDefinition(id, provider),
		Credential: protocol.ProviderCredentialSummary{
			Mode: mode, Configured: provider.Auth == "none" || status.KeySource != "none",
			EnvironmentVariable: status.EnvironmentVariable, Available: status.Available,
			CredentialPath: status.CredentialPath,
		},
		Models: []protocol.ProviderConfiguredModel{}, RemovalBlockers: providerRemovalBlockers(cfg, id),
	}
	for alias, model := range cfg.Models {
		if slices.Contains(model.Providers, id) {
			result.Models = append(result.Models, protocol.ProviderConfiguredModel{
				Alias: alias, ID: cmp.Or(model.ID, alias), Context: model.Context, MaxOutput: model.MaxOut,
			})
		}
	}
	slices.SortFunc(result.Models, func(a, b protocol.ProviderConfiguredModel) int { return cmp.Compare(a.Alias, b.Alias) })
	return result, nil
}

// Invalid file-authored URLs can contain credentials; the editable metadata
// never echoes userinfo, query parameters, or fragments back to a client.
func redactedProviderDefinition(id string, provider config.Provider) protocol.ProviderDefinition {
	definition := protocol.ProviderDefinition{Name: cmp.Or(provider.Name, id), API: provider.API}
	if endpoint, err := url.Parse(provider.BaseURL); err == nil {
		endpoint.User, endpoint.RawQuery, endpoint.Fragment, endpoint.RawFragment = nil, "", "", ""
		endpoint.ForceQuery = false
		definition.BaseURL = endpoint.String()
	}
	return definition
}

func validateProviderDefinition(provider config.Provider) error {
	if strings.TrimSpace(provider.Name) == "" || len(provider.Name) > 256 || strings.IndexFunc(provider.Name, unicode.IsControl) >= 0 {
		return errors.New("provider name is required and must be at most 256 characters")
	}
	if provider.API != "" && provider.API != "openai-completions" {
		return errors.New("custom providers require the OpenAI-compatible Chat Completions API")
	}
	endpoint, err := url.Parse(provider.BaseURL)
	if err != nil || len(provider.BaseURL) > 2048 || endpoint.Hostname() == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != "" {
		return errors.New("base URL must be an HTTP(S) API root without credentials, query parameters, or fragments")
	}
	if strings.HasSuffix(strings.TrimRight(endpoint.Path, "/"), "/chat/completions") {
		return errors.New("base URL must be the API root, not a chat completions endpoint")
	}
	return provider.ValidateAuth()
}

func applyProviderCredential(provider config.Provider, credential protocol.ProviderCredential, creating bool) (config.Provider, error) {
	switch credential.Mode {
	case "keep":
		if creating || credential.Key != "" || credential.EnvironmentVariable != "" {
			return config.Provider{}, errors.New("keep existing credential is available only when editing")
		}
	case "api_key":
		key := config.TrimKey(credential.Key)
		if key == "" || len(key) > 16384 || strings.ContainsAny(key, "\x00\r\n") || credential.EnvironmentVariable != "" {
			return config.Provider{}, errors.New("a valid literal API key is required")
		}
		if strings.HasPrefix(key, "!") || config.IsWholeRef(key) {
			return config.Provider{}, errors.New("API key must be literal; choose environment variable or retain the existing file-authored reference")
		}
		provider.APIKey, provider.APIKeyEnv, provider.Auth = key, "", ""
	case "environment":
		if !providerEnvironmentPattern.MatchString(credential.EnvironmentVariable) || credential.Key != "" {
			return config.Provider{}, errors.New("a valid environment variable name is required")
		}
		provider.APIKey, provider.APIKeyEnv, provider.Auth = "", credential.EnvironmentVariable, ""
	case "none":
		if credential.Key != "" || credential.EnvironmentVariable != "" {
			return config.Provider{}, errors.New("no authentication cannot include a credential")
		}
		provider.APIKey, provider.APIKeyEnv, provider.Auth = "", "", "none"
	default:
		return config.Provider{}, errors.New("choose API key, environment variable, or no authentication")
	}
	return provider, nil
}

func applyManualProviderModel(cfg *config.Config, id string, manual *protocol.ProviderManualModel, creating bool) error {
	if manual == nil {
		return nil
	}
	if strings.TrimSpace(manual.ID) != manual.ID || manual.ID == "" || len(manual.ID) > 512 || strings.IndexFunc(manual.ID, unicode.IsControl) >= 0 || strings.TrimSpace(manual.Alias) != manual.Alias || manual.Alias == "" || len(manual.Alias) > 768 || strings.IndexFunc(manual.Alias, unicode.IsControl) >= 0 {
		return errors.New("manual model requires a valid alias and exact API model ID")
	}
	if manual.Context < 0 || manual.MaxOutput < 0 {
		return errors.New("model context and maximum output must be positive when set")
	}
	model, exists := cfg.Models[manual.Alias]
	if exists && (creating || len(model.Providers) != 1 || model.Providers[0] != id || cmp.Or(model.ID, manual.Alias) != manual.ID) {
		return errors.New("model alias already exists; choose a unique alias")
	}
	model.ID, model.Providers, model.Context, model.MaxOut = manual.ID, []string{id}, manual.Context, manual.MaxOutput
	model.MaxTokens = 0
	if cfg.Models == nil {
		cfg.Models = make(map[string]config.Model)
	}
	cfg.Models[manual.Alias] = model
	return nil
}

func readProviderRevision(revision string) (*config.Config, error) {
	if revision == "" {
		return nil, errors.New("configuration revision is required")
	}
	cfg, current, err := config.ReadVersioned()
	if err != nil {
		return nil, err
	}
	if current != revision {
		return nil, config.ErrRevisionConflict
	}
	return cfg, nil
}

func (s *ProviderService) CreateProvider(ctx context.Context, p protocol.ProviderCreateParams) (protocol.ProviderConfiguration, error) {
	if !providerIDPattern.MatchString(p.Provider) {
		return protocol.ProviderConfiguration{}, errors.New("provider ID must use lowercase letters, digits, dots, underscores, or hyphens and start with a letter or digit")
	}
	if _, builtin := providerPreset(p.Provider); builtin || p.Provider == "inference" {
		return protocol.ProviderConfiguration{}, errors.New("provider ID is reserved for a built-in connection")
	}
	s.provisionMu.Lock()
	defer s.provisionMu.Unlock()
	cfg, err := readProviderRevision(p.Revision)
	if err != nil {
		return protocol.ProviderConfiguration{}, err
	}
	if _, exists := cfg.Providers[p.Provider]; exists {
		return protocol.ProviderConfiguration{}, errors.New("provider ID already exists; refresh before editing it")
	}
	provider := config.Provider{Name: strings.TrimSpace(p.Definition.Name), BaseURL: strings.TrimRight(strings.TrimSpace(p.Definition.BaseURL), "/"), API: cmp.Or(p.Definition.API, "openai-completions")}
	provider, err = applyProviderCredential(provider, p.Credential, true)
	if err != nil {
		return protocol.ProviderConfiguration{}, err
	}
	if err := validateProviderDefinition(provider); err != nil {
		return protocol.ProviderConfiguration{}, err
	}
	if err := applyManualProviderModel(cfg, p.Provider, p.ManualModel, true); err != nil {
		return protocol.ProviderConfiguration{}, err
	}
	return s.saveProvider(ctx, p.Revision, p.Provider, provider, p.ManualModel, true, true, p.AllowUnverified)
}

func (s *ProviderService) UpdateProvider(ctx context.Context, p protocol.ProviderUpdateParams) (protocol.ProviderConfiguration, error) {
	s.provisionMu.Lock()
	defer s.provisionMu.Unlock()
	cfg, err := readProviderRevision(p.Revision)
	if err != nil {
		return protocol.ProviderConfiguration{}, err
	}
	provider, exists := configuredProvider(cfg, p.Provider)
	if !exists {
		return protocol.ProviderConfiguration{}, errors.New("unknown provider")
	}
	original := provider
	if p.Name != nil {
		provider.Name = strings.TrimSpace(*p.Name)
		if provider.Name == "" {
			return protocol.ProviderConfiguration{}, errors.New("provider name is required")
		}
	}
	if p.BaseURL != nil {
		provider.BaseURL = strings.TrimRight(strings.TrimSpace(*p.BaseURL), "/")
	}
	endpointChanged := provider.BaseURL != strings.TrimRight(original.BaseURL, "/")
	if endpointChanged {
		if _, builtin := providerPreset(p.Provider); builtin {
			return protocol.ProviderConfiguration{}, errors.New("built-in endpoints cannot be changed here; add a custom provider")
		}
		if p.Credential == nil || p.Credential.Mode == "keep" {
			return protocol.ProviderConfiguration{}, errors.New("changing an endpoint requires an explicit credential choice before contacting the new URL")
		}
	}
	if p.Credential != nil {
		provider, err = applyProviderCredential(provider, *p.Credential, false)
		if err != nil {
			return protocol.ProviderConfiguration{}, err
		}
	}
	if provider.API == openaiauth.Provider {
		err = provider.ValidateOpenAICodex()
	} else {
		// File-authored entries may omit the display name.
		provider.Name = cmp.Or(provider.Name, p.Provider)
		err = validateProviderDefinition(provider)
	}
	if err != nil {
		return protocol.ProviderConfiguration{}, err
	}
	if err := applyManualProviderModel(cfg, p.Provider, p.ManualModel, false); err != nil {
		return protocol.ProviderConfiguration{}, err
	}
	check := endpointChanged || p.Credential != nil && p.Credential.Mode != "keep"
	return s.saveProvider(ctx, p.Revision, p.Provider, provider, p.ManualModel, false, check, p.AllowUnverified)
}

func patchProviderConfiguration(cfg *config.Config, id string, provider config.Provider, manual *protocol.ProviderManualModel, creating, enable bool) error {
	if creating {
		if _, exists := cfg.Providers[id]; exists {
			return errors.New("provider ID already exists")
		}
	}
	if err := applyManualProviderModel(cfg, id, manual, creating); err != nil {
		return err
	}
	if cfg.Providers == nil {
		cfg.Providers = make(map[string]config.Provider)
	}
	cfg.Providers[id] = provider
	if enable {
		cfg.EnableProvider(id)
	}
	return nil
}

// saveProvider runs with provisionMu held, never with the configuration lock
// across discovery. The final revision check protects against intervening edits.
func (s *ProviderService) saveProvider(ctx context.Context, revision, id string, provider config.Provider, manual *protocol.ProviderManualModel, creating, check, allowUnverified bool) (protocol.ProviderConfiguration, error) {
	discovery := protocol.ProviderDiscovery{Status: "not_checked"}
	var models []llm.ModelInfo
	var key string
	if err := ctx.Err(); err != nil {
		return protocol.ProviderConfiguration{}, err
	}
	if check {
		current, err := config.Load()
		if err != nil {
			return protocol.ProviderConfiguration{}, err
		}
		credentials := config.DiscoverCredentials(current, &config.Config{Providers: map[string]config.Provider{id: provider}})
		key, err = credentials.ResolveKey(provider)
		if err != nil || key == "" && provider.Auth != "none" {
			if !allowUnverified || manual == nil || credentials.KeyStatus(provider).Environment == "" {
				return protocol.ProviderConfiguration{}, errors.New("provider key is unavailable on the execution host; choose a manual model and save without verification to configure it for later")
			}
			discovery = protocol.ProviderDiscovery{Status: "unverified", Message: "Saved for later: the environment variable is unavailable on the execution host."}
		} else {
			validationCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			models, discovery, err = s.discoverProviderModels(validationCtx, id, provider, key)
			cancel()
			if ctx.Err() != nil {
				return protocol.ProviderConfiguration{}, ctx.Err()
			}
			if err != nil || len(models) == 0 {
				var response *llm.HTTPError
				if errors.As(err, &response) && (strings.HasPrefix(response.Status, "401") || strings.HasPrefix(response.Status, "403")) {
					return protocol.ProviderConfiguration{}, providerValidationError(err)
				}
				if !allowUnverified || manual == nil {
					return protocol.ProviderConfiguration{}, errors.New("model discovery is unavailable; check the connection or enter a manual model and explicitly save without verification")
				}
				discovery = protocol.ProviderDiscovery{Status: "unverified", Message: "Saved without verification; no model call has been tested."}
				models = nil
			}
		}
	}
	cfg, committed, err := config.UpdateVersioned(revision, func(cfg *config.Config) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return patchProviderConfiguration(cfg, id, provider, manual, creating, check)
	})
	if err != nil {
		return protocol.ProviderConfiguration{}, err
	}
	if check {
		s.interruptProviderLogins(id)
		cacheErr := config.DeleteCatalog(id)
		// Environment values can change independently of the configuration revision.
		if len(models) > 0 {
			current, loadErr := config.Load()
			var currentKey string
			var keyErr error
			if loadErr == nil {
				currentKey, keyErr = provider.ResolveKey(current)
			}
			if keyErr == nil && loadErr == nil && current.Providers[id] == provider && currentKey == key {
				cacheErr = config.UpdateCatalog(id, config.Catalog{FetchedAt: time.Now(), BaseURL: provider.BaseURL, Models: modelInfoLites(models)})
			} else {
				cacheErr = errors.New("credentials changed")
			}
		}
		if cacheErr != nil {
			discovery.Message += " Configuration saved, but the model cache needs to be refreshed."
		}
	}
	result, err := s.providerConfiguration(cfg, committed, id)
	if err != nil {
		// Accepted configuration must not be reported as a failed write due to
		// an unrelated account metadata read after persistence.
		return protocol.ProviderConfiguration{Revision: committed, Provider: id, Definition: redactedProviderDefinition(id, provider), Models: []protocol.ProviderConfiguredModel{}, RemovalBlockers: providerRemovalBlockers(cfg, id), Discovery: &discovery}, nil
	}
	result.Discovery = &discovery
	return result, nil
}

func (s *ProviderService) RemoveProvider(ctx context.Context, p protocol.ProviderRemoveParams) (protocol.ProviderRemoveResult, error) {
	if p.Revision == "" {
		return protocol.ProviderRemoveResult{}, errors.New("configuration revision is required")
	}
	s.provisionMu.Lock()
	defer s.provisionMu.Unlock()
	_, revision, err := config.UpdateVersioned(p.Revision, func(cfg *config.Config) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if blockers := providerRemovalBlockers(cfg, p.Provider); len(blockers) != 0 {
			return fmt.Errorf("cannot remove provider: %s Disable it instead", strings.Join(blockers, " "))
		}
		if _, exists := cfg.Providers[p.Provider]; !exists {
			return errors.New("unknown custom provider")
		}
		delete(cfg.Providers, p.Provider)
		cfg.EnableProvider(p.Provider)
		return nil
	})
	if err != nil {
		return protocol.ProviderRemoveResult{}, err
	}
	s.interruptProviderLogins(p.Provider)
	result := protocol.ProviderRemoveResult{Revision: revision}
	if err := config.DeleteCatalog(p.Provider); err != nil {
		result.Warning = "Provider removed, but its model cache could not be removed."
	}
	return result, nil
}
