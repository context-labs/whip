package providerhost

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/openaiauth"
	"github.com/context-labs/whip/internal/session"
)

type Catalog struct {
	Provider   string     `json:"provider"`
	State      string     `json:"state"`
	ScopeState string     `json:"scope_state"`
	Discovery  string     `json:"discovery"`
	FetchedAt  *time.Time `json:"fetched_at"`
	Stale      bool       `json:"stale"`
	Failure    string     `json:"failure"`
	Models     []Model    `json:"models"`
}

type catalogEntry struct {
	scope    scope
	observed scope
	value    Catalog
	bytes    int
}

type Readiness struct {
	Configured      bool   `json:"configured"`
	CredentialState string `json:"credential_state"`
	CatalogState    string `json:"catalog_state"`
	ModelState      string `json:"model_state"`
	InferenceState  string `json:"inference_state"`
}

// Catalog reads only local source state. A command's current output cannot be
// known without executing it; its cached scope remains explicitly unverified.
func (s *Service) Catalog(ctx context.Context, id string) (Catalog, error) {
	snapshot, err := s.snapshot(ctx)
	if err != nil {
		return Catalog{}, err
	}
	p, ok := snapshot.Host.Providers[id]
	if !ok {
		return Catalog{}, ErrMissing
	}
	_, current, inspectErr := s.inspect(ctx, p)
	s.mu.Lock()
	defer s.mu.Unlock()
	value := Catalog{Provider: id, State: "missing", ScopeState: "unverified", Discovery: "not_checked", Models: []Model{}}
	entry, ok := s.catalogs[id]
	if !ok {
		return value, nil
	}
	if inspectErr != nil || entry.scope.Route != current.Route || source(p) != "command" && entry.scope != current || source(p) == "command" && entry.observed != entry.scope {
		value.State = "scope_changed"
		return value, nil
	}
	value = cloneCatalog(entry.value)
	value.State = "cached"
	value.ScopeState = "current"
	if source(p) == "command" {
		value.ScopeState = "unverified"
	}
	value.Stale = time.Since(*value.FetchedAt) > 24*time.Hour
	return value, nil
}

func (s *Service) Readiness(ctx context.Context, selection session.ModelSelection) (Readiness, error) {
	value := Readiness{CredentialState: "missing", CatalogState: "missing", ModelState: "unknown", InferenceState: "not_tested"}
	snapshot, err := s.snapshot(ctx)
	if err != nil {
		return value, err
	}
	p, ok := snapshot.Host.Providers[selection.Provider]
	if !ok {
		return value, nil
	}
	value.Configured = true
	status, _, _ := s.inspect(ctx, p)
	value.CredentialState = status.State
	catalog, err := s.Catalog(ctx, selection.Provider)
	if err != nil {
		return value, err
	}
	value.CatalogState = catalog.State
	if _, ok := p.Models[selection.Name]; ok {
		value.ModelState = "configured"
	} else {
		for _, model := range catalog.Models {
			if model.ID == selection.Name {
				value.ModelState = "catalogued"
			}
		}
	}
	return value, nil
}

// Refresh owns one explicit, bounded read-only discovery. Neither cache misses
// nor listing start it. Failures retain the last catalog for its exact scope;
// successful empty membership replaces that scope with an empty catalog.
func (s *Service) Refresh(ctx context.Context, id string) (Catalog, error) {
	snapshot, err := s.snapshot(ctx)
	if err != nil {
		return Catalog{}, err
	}
	p, ok := snapshot.Host.Providers[id]
	if !ok {
		return Catalog{}, ErrMissing
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return Catalog{}, ErrClosed
	}
	if s.active[id] || len(s.active) >= 4 {
		s.mu.Unlock()
		return Catalog{}, ErrBusy
	}
	s.active[id] = true
	s.wg.Add(1)
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.active, id); s.mu.Unlock(); s.wg.Done() }()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	auth, err := s.capture(ctx, p)
	if err != nil {
		if ctx.Err() != nil {
			return Catalog{}, ctx.Err()
		}
		return Catalog{}, err
	}
	s.mu.Lock()
	if entry, ok := s.catalogs[id]; ok {
		entry.observed = auth.scope
		s.catalogs[id] = entry
	}
	s.mu.Unlock()
	models, discovery, fetchErr := s.discover(ctx, id, p, auth)
	// Bound the encoded public value as well as raw HTTP input and model count.
	// Escaping can expand otherwise bounded provider-supplied strings.
	encoded, encodeErr := json.Marshal(models)
	if encodeErr != nil || len(encoded) > 2<<20 {
		fetchErr = ErrDiscovery
	}
	if err := ctx.Err(); err != nil {
		return Catalog{}, err
	}
	current, err := s.snapshot(ctx)
	if err != nil {
		return Catalog{}, err
	}
	now, exists := current.Host.Providers[id]
	if !exists || routeScope(now).Route != auth.scope.Route {
		return Catalog{}, ErrStale
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Catalog{}, ErrClosed
	}
	if auth.check(ctx) != nil {
		return Catalog{}, ErrStale
	}
	if fetchErr != nil {
		result := Catalog{Provider: id, State: "missing", ScopeState: "current", Discovery: "failed", Models: []Model{}, Failure: ErrDiscovery.Error()}
		if entry, exists := s.catalogs[id]; exists && entry.scope == auth.scope {
			entry.value.Failure = ErrDiscovery.Error()
			s.catalogs[id] = entry
			result = cloneCatalog(entry.value)
			result.State = "cached"
		}
		return result, ErrDiscovery
	}
	total := len(models)
	totalBytes := len(encoded)
	for name, entry := range s.catalogs {
		if _, exists := current.Host.Providers[name]; !exists {
			delete(s.catalogs, name)
			continue
		}
		if name != id {
			total += len(entry.value.Models)
			totalBytes += entry.bytes
		}
	}
	if total > 8192 || totalBytes > 16<<20 {
		return Catalog{}, ErrDiscovery
	}
	value := Catalog{Provider: id, State: "cached", ScopeState: "current", Discovery: discovery, FetchedAt: new(time.Now().UTC()), Models: models}
	if source(p) == "command" {
		value.ScopeState = "unverified"
	}
	s.catalogs[id] = catalogEntry{scope: auth.scope, observed: auth.scope, value: value, bytes: len(encoded)}
	return cloneCatalog(value), nil
}

func (s *Service) discover(ctx context.Context, id string, p config.Provider, auth authorization) ([]Model, string, error) {
	base := strings.TrimRight(p.BaseURL, "/")
	if p.Kind == "openai-codex" {
		base = openaiauth.BaseURL
	}
	if canonical(id, p) && id == "openrouter" {
		if _, err := s.request(ctx, base+"/key", auth); err != nil {
			return nil, "failed", err
		}
	}
	path := base + "/models"
	if p.Kind == "openai-codex" {
		path += "?client_version=0.153.4"
	}
	raw, err := s.request(ctx, path, auth)
	if err != nil {
		return nil, "failed", err
	}
	models, err := decodeModels(raw, id, p)
	if err != nil {
		return nil, "failed", err
	}
	discovery := "catalog_response"
	if p.Kind == "openai-codex" {
		discovery = "account_catalog"
	}
	if canonical(id, p) && id == "openrouter" {
		discovery = "authenticated_catalog"
	}
	if source(p) == "none" || canonical(id, p) && id == "deepinfra" {
		discovery = "public_catalog"
	}
	return models, discovery, nil
}

func (s *Service) request(ctx context.Context, url string, auth authorization) ([]byte, error) {
	if auth.check(ctx) != nil {
		return nil, ErrStale
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, ErrDiscovery
	}
	request.Header.Set("Accept", "application/json")
	if auth.key != "" {
		request.Header.Set("Authorization", "Bearer "+auth.key)
	}
	if auth.account != "" {
		request.Header.Set("Chatgpt-Account-Id", auth.account)
		request.Header.Set("User-Agent", "whip")
		request.Header.Set("Originator", "whip")
		if auth.residency != "" {
			request.Header.Set("X-Openai-Internal-Codex-Residency", auth.residency)
		}
	}
	response, err := s.http.Do(request)
	if err != nil {
		return nil, ErrDiscovery
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusUnauthorized && auth.refresh != nil {
		// This read-only request may refresh once, pinned to the captured login.
		_ = response.Body.Close()
		key, err := auth.refresh(ctx)
		if err != nil {
			return nil, ErrDiscovery
		}
		auth.key, auth.refresh = key, nil
		return s.request(ctx, url, auth)
	}
	if response.StatusCode != http.StatusOK {
		return nil, ErrDiscovery
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	if err != nil || len(raw) > 8<<20 {
		return nil, ErrDiscovery
	}
	return raw, nil
}

func cloneCatalog(value Catalog) Catalog {
	value.FetchedAt = copyPointer(value.FetchedAt)
	models := make([]Model, len(value.Models))
	for i, model := range value.Models {
		models[i] = cloneModel(model)
	}
	value.Models = models
	return value
}
