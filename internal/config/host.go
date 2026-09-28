// Package config owns explicit host files and provider routes, never session
// persistence. Loading a host does not resolve credentials or start a runtime.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
)

const (
	FileName = "host.json"
	Version  = 7
)

var environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type Provider struct {
	Kind          string           `json:"kind"`
	BaseURL       string           `json:"base_url"`
	CredentialEnv string           `json:"credential_env"`
	Models        map[string]Model `json:"models,omitempty"`
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
	return Host{Version: Version, ProjectRoots: map[string]string{}, SkillRoots: map[string]string{}, Providers: map[string]Provider{}, Engine: session.Starlark, Resources: session.DefaultResourceLimits()}
}

func (h Host) Validate() error {
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
		if provider.Kind == "openai-codex" {
			if provider.BaseURL != "" || provider.CredentialEnv != "" {
				return fmt.Errorf("%w: subscription routes cannot configure an endpoint or API credential", session.ErrInvalid)
			}
		} else {
			route, err := url.Parse(provider.BaseURL)
			if err != nil || len(provider.BaseURL) > 4000 || route.Hostname() == "" || (route.Scheme != "https" && route.Scheme != "http") ||
				route.User != nil || route.RawQuery != "" || route.Fragment != "" || (provider.Kind != "openai-chat" && provider.Kind != "openai-responses") {
				return fmt.Errorf("%w: invalid provider route %q", session.ErrInvalid, name)
			}
			if provider.CredentialEnv != "" && !environmentName.MatchString(provider.CredentialEnv) {
				return fmt.Errorf("%w: invalid credential environment reference", session.ErrInvalid)
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
	if h.Defaults.Model == (session.ModelSelection{}) {
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

// Credential resolves an environment reference only when the execution layer
// constructs a client. Passing the lookup keeps tests and callers explicit.
func (p Provider) Credential(lookup func(string) (string, bool)) (string, error) {
	if p.Kind == "openai-codex" && p.CredentialEnv != "" {
		return "", fmt.Errorf("%w: subscription routes cannot use API credentials", session.ErrInvalid)
	}
	if p.CredentialEnv == "" {
		return "", nil
	}
	if lookup == nil {
		return "", fmt.Errorf("%w: credential lookup is required", session.ErrInvalid)
	}
	value, ok := lookup(p.CredentialEnv)
	if !ok || value == "" {
		return "", fmt.Errorf("credential environment variable %q is unset", p.CredentialEnv)
	}
	return value, nil
}

func Load(directory string) (Host, error) {
	if directory == "" {
		return Host{}, fmt.Errorf("%w: configuration directory is required", session.ErrInvalid)
	}
	//nolint:gosec // The host explicitly selects its configuration directory; the filename is fixed.
	file, err := os.Open(filepath.Join(directory, FileName))
	if err != nil {
		return Host{}, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, session.MaxDocumentBytes+1))
	if err != nil {
		return Host{}, err
	}
	if len(raw) > session.MaxDocumentBytes {
		return Host{}, fmt.Errorf("%w: host configuration exceeds size limit", session.ErrInvalid)
	}
	var host Host
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&host); err != nil {
		return Host{}, fmt.Errorf("decode host configuration: %w", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return Host{}, fmt.Errorf("%w: trailing host configuration data", session.ErrInvalid)
	}
	if err := host.Validate(); err != nil {
		return Host{}, err
	}
	host.Resources, err = session.ResolveResourceLimits(host.Resources, nil)
	if err != nil {
		return Host{}, err
	}
	return host, nil
}

// Initialize publishes one complete default file, or loads the existing file.
// The directory is explicit; old filenames and installed runtimes are ignored.
func Initialize(directory string) (Host, error) {
	if directory == "" {
		return Host{}, fmt.Errorf("%w: configuration directory is required", session.ErrInvalid)
	}
	if err := write(directory, Default(), true); err != nil && !errors.Is(err, os.ErrExist) {
		return Host{}, err
	}
	return Load(directory)
}
func Save(directory string, host Host) error { return write(directory, host, false) }
func write(directory string, host Host, onlyNew bool) (err error) {
	if directory == "" {
		return fmt.Errorf("%w: configuration directory is required", session.ErrInvalid)
	}
	if err := host.Validate(); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(host, "", "  ")
	if err != nil {
		return err
	}
	if len(raw)+1 > session.MaxDocumentBytes {
		return fmt.Errorf("%w: host configuration exceeds size limit", session.ErrInvalid)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".host-*")
	if err != nil {
		return err
	}
	defer func() {
		if removeErr := os.Remove(file.Name()); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, removeErr)
		}
	}()
	if _, err := file.Write(append(raw, '\n')); err != nil {
		return errors.Join(err, file.Close())
	}
	if err := file.Sync(); err != nil {
		return errors.Join(err, file.Close())
	}
	if err := file.Close(); err != nil {
		return err
	}
	target := filepath.Join(directory, FileName)
	if onlyNew {
		err = os.Link(file.Name(), target)
	} else {
		err = os.Rename(file.Name(), target)
	}
	if err != nil {
		return err
	}
	//nolint:gosec // Open the caller-selected host directory to sync the configuration file publication.
	dir, err := os.Open(directory)
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}
