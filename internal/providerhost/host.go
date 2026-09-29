// Package providerhost owns host provider setup and bounded discovery caches.
// Routes and defaults live only in config.Authority. Catalogs are observations,
// never credential authority or proof that inference is available.
package providerhost

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/inferenceauth"
	"github.com/context-labs/whip/internal/openaiauth"
	"github.com/context-labs/whip/internal/session"
)

var (
	ErrInvalid     = errors.New("invalid provider setup operation")
	ErrMissing     = errors.New("provider route is not configured")
	ErrDisabled    = errors.New("provider route is disabled")
	ErrExists      = errors.New("provider route already exists")
	ErrClosed      = errors.New("provider host service is closed")
	ErrBusy        = errors.New("provider catalog refresh is already active or at capacity")
	ErrStale       = errors.New("provider route or credentials changed; refresh again")
	ErrCredentials = errors.New("provider credential source is unavailable")
	ErrDiscovery   = errors.New("provider model discovery failed; inference has not been verified")
	ErrStorage     = errors.New("provider configuration persistence needs attention; reread before retrying")
)

type CredentialStatus struct {
	Source      string `json:"source"`
	State       string `json:"state"`
	Environment string `json:"environment"`
	File        string `json:"file"`
}

// Route contains editable nonsecret declarations. Command arguments and raw
// credential captures are intentionally absent from public inspection.
type Route struct {
	ID         string                  `json:"id"`
	Disabled   bool                    `json:"disabled"`
	Kind       string                  `json:"kind"`
	BaseURL    string                  `json:"base_url"`
	Credential CredentialStatus        `json:"credential"`
	Models     map[string]config.Model `json:"models"`
}

type Inventory struct {
	Revision        string                  `json:"revision"`
	Routes          []Route                 `json:"routes"`
	Defaults        session.ModelSelection  `json:"defaults"`
	CompactionModel *session.ModelSelection `json:"compaction_model"`
	PermissionMode  session.PermissionMode  `json:"permission_mode"`
}

// KeyPublication is a transient input, never a saved host declaration. Reuse ID
// and the same Key after an uncertain acknowledgement; new bytes need a new ID.
type KeyPublication struct{ ID, Key string }

type Change struct {
	Revision       string
	ID             string
	Provider       config.Provider
	KeepCredential bool
	Key            *KeyPublication
}

// Defaults changes the complete provider/model/effort/sampling selection at
// once. Optional Settings explicitly publishes pricing and limits for it in
// the same update. A nil selection clears the host's unconfigured default.
// Explicit model names need not appear in an ephemeral catalog: configuration
// remains valid across service restart. Readiness keeps unknown names unknown.
type Defaults struct {
	Selection *session.ModelSelection
	Settings  *config.Model
}

type Service struct {
	ctx       context.Context
	cancel    context.CancelFunc
	config    *config.Authority
	http      *http.Client
	lookup    func(string) (string, bool)
	openAI    *openaiauth.Manager
	inference *inferenceauth.Manager
	mu        sync.Mutex
	wg        sync.WaitGroup
	closed    bool
	active    map[string]context.CancelFunc
	catalogs  map[string]catalogEntry
}

func New(ctx context.Context, authority *config.Authority, client *http.Client, lookup func(string) (string, bool), openAI *openaiauth.Manager, inference *inferenceauth.Manager) (*Service, error) {
	if authority == nil {
		return nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	value := http.Client{Timeout: 20 * time.Second}
	if client != nil {
		value = *client
	}
	if value.Timeout <= 0 || value.Timeout > 20*time.Second {
		value.Timeout = 20 * time.Second
	}
	value.Jar = nil
	value.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	ctx, cancel := context.WithCancel(ctx)
	return &Service{ctx: ctx, cancel: cancel, config: authority, http: &value, lookup: lookup, openAI: openAI, inference: inference, active: map[string]context.CancelFunc{}, catalogs: map[string]catalogEntry{}}, nil
}

func (s *Service) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.ctx.Err() != nil {
		return ErrClosed
	}
	return nil
}

func (s *Service) snapshot(ctx context.Context) (config.Snapshot, error) {
	if err := s.check(ctx); err != nil {
		return config.Snapshot{}, err
	}
	value, err := s.config.Snapshot(ctx)
	if err != nil {
		return config.Snapshot{}, safeConfigError(err)
	}
	return value, nil
}

func (s *Service) List(ctx context.Context) (Inventory, error) {
	value, err := s.snapshot(ctx)
	if err != nil {
		return Inventory{}, err
	}
	result := Inventory{Revision: value.Revision, Routes: []Route{}, Defaults: value.Host.Defaults.Model.Clone(), CompactionModel: value.Host.Defaults.Compaction.Model}
	result.PermissionMode, _ = session.ResolvePermissionMode(value.Host.DefaultPermissionMode)
	for id, provider := range value.Host.Providers {
		status, _, _ := s.inspect(ctx, provider)
		result.Routes = append(result.Routes, Route{ID: id, Disabled: provider.Disabled, Kind: provider.Kind, BaseURL: provider.BaseURL, Credential: status, Models: provider.Models})
	}
	slices.SortFunc(result.Routes, func(a, b Route) int { return strings.Compare(a.ID, b.ID) })
	s.mu.Lock()
	for id := range s.catalogs {
		if _, exists := value.Host.Providers[id]; !exists {
			delete(s.catalogs, id)
		}
	}
	s.mu.Unlock()
	return result, nil
}

func (s *Service) Create(ctx context.Context, change Change) (Inventory, error) {
	return s.save(ctx, change, true)
}

func (s *Service) Update(ctx context.Context, change Change) (Inventory, error) {
	return s.save(ctx, change, false)
}

func (s *Service) save(ctx context.Context, change Change, creating bool) (Inventory, error) {
	if session.ValidateID(change.ID) != nil || change.Revision == "" {
		return Inventory{}, ErrInvalid
	}
	before, err := s.snapshot(ctx)
	if err != nil {
		return Inventory{}, err
	}
	if before.Revision != change.Revision {
		return Inventory{}, config.ErrRevisionConflict
	}
	previous, exists := before.Host.Providers[change.ID]
	if creating && exists {
		return Inventory{}, ErrExists
	}
	if !creating && !exists {
		return Inventory{}, ErrMissing
	}
	provider := change.Provider
	if exists {
		provider.Disabled = previous.Disabled
		provider.CredentialEpoch = previous.CredentialEpoch
	}
	if change.KeepCredential {
		if creating || change.Key != nil || previous.BaseURL != provider.BaseURL || previous.Kind != provider.Kind {
			return Inventory{}, ErrInvalid
		}
		provider.CredentialSource, provider.CredentialEnv, provider.CredentialFile, provider.CredentialCommand = previous.CredentialSource, previous.CredentialEnv, previous.CredentialFile, previous.CredentialCommand
	}
	// Validate all route fields before publishing any secret. The temporary
	// placeholder is never saved and satisfies the file-source declaration.
	if change.Key != nil {
		if change.KeepCredential || provider.Kind == "openai-codex" || provider.CredentialSource != "file" || provider.CredentialEnv != "" || provider.CredentialCommand != nil || provider.CredentialFile != "" {
			return Inventory{}, ErrInvalid
		}
		provider.CredentialFile = "/private/provider-key"
	}
	if before.Host.Providers == nil {
		before.Host.Providers = map[string]config.Provider{}
	}
	before.Host.Providers[change.ID] = provider
	if before.Host.Validate() != nil {
		return Inventory{}, ErrInvalid
	}
	if change.Key != nil {
		path, err := s.config.PublishKey(ctx, change.Key.ID, change.Key.Key)
		if err != nil {
			return Inventory{}, safeConfigError(err)
		}
		provider.CredentialFile = path
	}
	_, err = s.config.Update(ctx, change.Revision, func(host *config.Host) error {
		_, exists := host.Providers[change.ID]
		if creating && exists {
			return ErrExists
		}
		if !creating && !exists {
			return ErrMissing
		}
		if host.Providers == nil {
			host.Providers = map[string]config.Provider{}
		}
		host.Providers[change.ID] = provider
		return nil
	})
	if err != nil {
		return Inventory{}, safeConfigError(err)
	}
	return s.List(ctx)
}

// Remove preserves credentials and rejects dangling defaults. Replacement can
// explicitly replace/clear the main default in the same CAS; compaction still
// referencing this route must be changed explicitly before removal.
func (s *Service) Remove(ctx context.Context, revision, id string, replacement *Defaults) (Inventory, error) {
	if err := s.check(ctx); err != nil {
		return Inventory{}, err
	}
	_, err := s.config.Update(ctx, revision, func(host *config.Host) error {
		if _, exists := host.Providers[id]; !exists {
			return ErrMissing
		}
		if replacement != nil {
			if err := applyDefaults(host, *replacement, false); err != nil {
				return err
			}
		}
		if host.Defaults.Model.Provider == id || host.Defaults.Compaction.Model != nil && host.Defaults.Compaction.Model.Provider == id {
			return ErrInvalid
		}
		delete(host.Providers, id)
		return nil
	})
	if err != nil {
		return Inventory{}, safeConfigError(err)
	}
	return s.List(ctx)
}

func (s *Service) SetDefaults(ctx context.Context, revision string, value Defaults) (Inventory, error) {
	if err := s.check(ctx); err != nil {
		return Inventory{}, err
	}
	_, err := s.config.Update(ctx, revision, func(host *config.Host) error { return applyDefaults(host, value, false) })
	if err != nil {
		return Inventory{}, safeConfigError(err)
	}
	return s.List(ctx)
}

func (s *Service) SetCompactionModel(ctx context.Context, revision string, value Defaults) (Inventory, error) {
	if err := s.check(ctx); err != nil {
		return Inventory{}, err
	}
	_, err := s.config.Update(ctx, revision, func(host *config.Host) error { return applyDefaults(host, value, true) })
	if err != nil {
		return Inventory{}, safeConfigError(err)
	}
	return s.List(ctx)
}

func applyDefaults(host *config.Host, value Defaults, compaction bool) error {
	if value.Selection == nil {
		if value.Settings != nil {
			return ErrInvalid
		}
		if compaction {
			host.Defaults.Compaction.Model = nil
		} else {
			host.Defaults.Model = session.ModelSelection{}
		}
		return nil
	}
	selection := value.Selection.Clone()
	if selection.Validate() != nil {
		return ErrInvalid
	}
	provider, ok := host.Providers[selection.Provider]
	if !ok {
		return ErrMissing
	}
	if value.Settings != nil {
		if provider.Models == nil {
			provider.Models = map[string]config.Model{}
		}
		provider.Models[selection.Name] = *value.Settings
		host.Providers[selection.Provider] = provider
	}
	if compaction {
		host.Defaults.Compaction.Model = &selection
	} else {
		host.Defaults.Model = selection
	}
	return nil
}

func safeConfigError(err error) error {
	for _, known := range []error{config.ErrRevisionConflict, config.ErrKeyConflict, config.ErrKeyStoragePending, config.ErrKeyStorage, ErrInvalid, ErrMissing, ErrExists, context.Canceled, context.DeadlineExceeded} {
		if errors.Is(err, known) {
			return known
		}
	}
	if errors.Is(err, session.ErrInvalid) {
		return ErrInvalid
	}
	return ErrStorage
}

// Close joins refreshes before the command closes either borrowed manager.
func (s *Service) Close() {
	s.cancel()
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.wg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.catalogs)
}

// ClearDiscovery cancels work accepted before Disconnect and removes its local
// observations. The authority's changed credential epoch rejects late writers.
func (s *Service) ClearDiscovery(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel := s.active[id]; cancel != nil {
		cancel()
	}
	delete(s.catalogs, id)
}
