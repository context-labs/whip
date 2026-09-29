// Package config owns explicit host files and provider routes, never session
// persistence. Loading a host does not resolve credentials or start a runtime.
package config

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/lspconfig"
	"github.com/context-labs/whip/internal/session"
)

const (
	FileName = "host.json"
	Version  = 14
)

type Provider struct {
	Kind          string `json:"kind"`
	BaseURL       string `json:"base_url"`
	CredentialEnv string `json:"credential_env"`
	// CredentialSource selects env, file, command, none, or inference-net.
	// Empty retains the explicit CredentialEnv/no-auth shorthand. URLs never
	// select a credential, and resolution never discovers other host files.
	CredentialSource  string             `json:"credential_source,omitempty"`
	CredentialFile    string             `json:"credential_file,omitempty"`
	CredentialCommand *CredentialCommand `json:"credential_command,omitempty"`
	Models            map[string]Model   `json:"models,omitempty"`
}

// Model contains host-side dispatch limits and price evidence, not session
// selection. Missing prices remain unknown. Zero limits select bounded defaults.
type Model struct {
	Prices          session.ModelPrices `json:"prices"`
	MaxOutputTokens int64               `json:"max_output_tokens"`
	TimeoutMillis   int64               `json:"timeout_millis"`
	MaxAttempts     int                 `json:"max_attempts"`
	// ContextWindowTokens is the host-declared provider context maximum, from
	// 1 to 1 billion tokens. It bounds input for reservation, not measured usage.
	// Nil is unknown. The provider must enforce this maximum; actual overages
	// are still recorded, including when the host declaration was inaccurate.
	ContextWindowTokens *int64 `json:"context_window_tokens,omitempty"`
}

func (m Model) Resolve() (Model, error) { return m.resolve(4096) }

// A zero default keeps a subscription's unspecified ceiling unresolved until
// the execution adapter applies its verified model-specific natural bound.
func (m Model) resolve(defaultOutput int64) (Model, error) {
	if m.MaxOutputTokens == 0 {
		m.MaxOutputTokens = defaultOutput
	}
	if m.TimeoutMillis == 0 {
		m.TimeoutMillis = 120000
	}
	if m.MaxAttempts == 0 {
		m.MaxAttempts = 3
	}
	if m.MaxOutputTokens < 0 || m.MaxOutputTokens > 1000000 {
		return Model{}, fmt.Errorf("%w: output token limit must be 1–1000000", session.ErrInvalid)
	}
	if m.ContextWindowTokens != nil {
		if *m.ContextWindowTokens < 1 || *m.ContextWindowTokens > 1000000000 || m.MaxOutputTokens > *m.ContextWindowTokens {
			return Model{}, fmt.Errorf("%w: context window must be 1–1000000000 tokens and at least the output limit", session.ErrInvalid)
		}
		m.ContextWindowTokens = new(*m.ContextWindowTokens)
	}
	if m.TimeoutMillis < 1 || m.TimeoutMillis > 600000 {
		return Model{}, fmt.Errorf("%w: provider timeout must be 1–600000 milliseconds", session.ErrInvalid)
	}
	if m.MaxAttempts < 1 || m.MaxAttempts > 5 {
		return Model{}, fmt.Errorf("%w: model attempts must be 1–5", session.ErrInvalid)
	}
	return m, m.Prices.Validate()
}

type Host struct {
	DefaultPermissionMode session.PermissionMode `json:"default_permission_mode,omitempty"`
	// LSP publishes bounded stdio server declarations without granting session authority.
	LSP map[string]lspconfig.Config `json:"lsp"`
	// ProjectRoots publishes named project directories without granting authority.
	ProjectRoots map[string]string `json:"project_roots"`
	// StandingInstructionsFile explicitly publishes one file; empty disables it.
	StandingInstructionsFile string `json:"standing_instructions_file"`
	// SkillRoots publishes explicit named directories; publication grants no authority.
	SkillRoots map[string]string       `json:"skill_roots"`
	Version    int                     `json:"version"`
	Providers  map[string]Provider     `json:"providers"`
	Defaults   session.Configuration   `json:"defaults"`
	Engine     session.Engine          `json:"engine"`
	Resources  []session.ResourceLimit `json:"resources"`
}

// Default is intentionally unconfigured. Model/provider selection is required
// before resolving a runnable session; initialization invents no credentials.
func Default() Host {
	return Host{Version: Version, LSP: map[string]lspconfig.Config{}, ProjectRoots: map[string]string{}, SkillRoots: map[string]string{}, Providers: map[string]Provider{}, Engine: session.Starlark, Resources: session.DefaultResourceLimits()}
}

func (h Host) Validate() error {
	if _, err := session.ResolvePermissionMode(h.DefaultPermissionMode); err != nil {
		return err
	}
	if err := lspconfig.ValidateConfig(h.LSP); err != nil {
		return fmt.Errorf("%w: %w", session.ErrInvalid, err)
	}
	if h.Version != Version {
		return fmt.Errorf("%w: unsupported host version %d", session.ErrInvalid, h.Version)
	}
	if h.StandingInstructionsFile != "" {
		if err := session.ValidateText(h.StandingInstructionsFile, 4096); err != nil {
			return err
		}
		path := h.StandingInstructionsFile
		if !utf8.ValidString(path) || !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) == string(filepath.Separator) || strings.ContainsRune(filepath.Base(path), '\\') {
			return fmt.Errorf("%w: standing instruction file must be a clean absolute path with a basename", session.ErrInvalid)
		}
	}
	if len(h.ProjectRoots) > session.MaxProjectRoots {
		return fmt.Errorf("%w: too many host project roots", session.ErrInvalid)
	}
	for id, path := range h.ProjectRoots {
		if err := session.ValidateID(id); err != nil {
			return err
		}
		if err := session.ValidateText(path, 4096); err != nil {
			return err
		}
		if !utf8.ValidString(path) || !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return fmt.Errorf("%w: host project root must be a clean absolute path", session.ErrInvalid)
		}
	}
	if len(h.SkillRoots) > session.MaxSkillRoots {
		return fmt.Errorf("%w: too many host skill roots", session.ErrInvalid)
	}
	for id, path := range h.SkillRoots {
		if err := session.ValidateID(id); err != nil {
			return err
		}
		if err := session.ValidateText(path, 4096); err != nil {
			return err
		}
		if !utf8.ValidString(path) || !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return fmt.Errorf("%w: host skill root must be a clean absolute path", session.ErrInvalid)
		}
	}
	if err := h.Engine.Validate(); err != nil {
		return err
	}
	if _, err := session.ResolveResourceLimits(h.Resources, nil); err != nil {
		return err
	}
	if len(h.Providers) > 128 {
		return fmt.Errorf("%w: too many provider routes", session.ErrInvalid)
	}
	for name, provider := range h.Providers {
		if err := session.ValidateID(name); err != nil {
			return err
		}
		if err := provider.validateCredentialSource(); err != nil {
			return err
		}
		if provider.Kind == "openai-codex" {
			if provider.BaseURL != "" {
				return fmt.Errorf("%w: subscription routes cannot configure an endpoint or API credential", session.ErrInvalid)
			}
		} else {
			route, err := url.Parse(provider.BaseURL)
			if err != nil || len(provider.BaseURL) > 4000 || route.Hostname() == "" || (route.Scheme != "https" && route.Scheme != "http") ||
				route.User != nil || route.RawQuery != "" || route.Fragment != "" || (provider.Kind != "openai-chat" && provider.Kind != "openai-responses") {
				return fmt.Errorf("%w: invalid provider route %q", session.ErrInvalid, name)
			}
		}
		if len(provider.Models) > 1024 {
			return fmt.Errorf("%w: too many configured provider models", session.ErrInvalid)
		}
		for modelName, settings := range provider.Models {
			if err := session.ValidateText(modelName, 256); err != nil {
				return err
			}
			defaultOutput := int64(4096)
			if provider.Kind == "openai-codex" {
				defaultOutput = 0
			}
			if _, err := settings.resolve(defaultOutput); err != nil {
				return err
			}
		}
	}
	if h.Defaults.Compaction.Model != nil {
		if _, ok := h.Providers[h.Defaults.Compaction.Model.Provider]; !ok {
			return fmt.Errorf("%w: compaction provider route is absent", session.ErrInvalid)
		}
	}
	if h.Defaults.Model.Equal(session.ModelSelection{}) {
		// Validate declarations while permitting the deliberate unconfigured state.
		value := h.Defaults.Clone()
		value.Model = session.ModelSelection{Provider: "unconfigured", Name: "unconfigured"}
		return value.Validate()
	}
	if _, ok := h.Providers[h.Defaults.Model.Provider]; !ok {
		return fmt.Errorf("%w: default provider route is absent", session.ErrInvalid)
	}
	return h.Defaults.Validate()
}
