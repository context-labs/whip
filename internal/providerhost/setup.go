package providerhost

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/session"
)

// SetupKey is the explicit CLI-style setup operation: validate one canonical
// provider's credentials by bounded discovery before publishing the route.
// It changes no defaults and never retries discovery or a conflicting CAS.
// A catalog response is not proof that a model can perform inference.
func (s *Service) SetupKey(ctx context.Context, revision, id string, key *KeyPublication, environment bool) (Inventory, error) {
	if revision == "" || (key == nil) != environment || id != "openrouter" && id != "inference-net" {
		return Inventory{}, ErrInvalid
	}
	if key != nil {
		if session.ValidateID(key.ID) != nil || len(key.Key) == 0 || len(key.Key) > 64<<10 {
			return Inventory{}, ErrInvalid
		}
		for _, c := range []byte(key.Key) {
			if c < '!' || c > '~' {
				return Inventory{}, ErrInvalid
			}
		}
	}
	before, err := s.snapshot(ctx)
	if err != nil {
		return Inventory{}, err
	}
	if before.Revision != revision {
		return Inventory{}, config.ErrRevisionConflict
	}
	previous, exists := before.Host.Providers[id]
	provider := config.Provider{Kind: "openai-chat", Models: previous.Models}
	for _, preset := range Presets() {
		if preset.ID == id {
			provider.BaseURL = preset.BaseURL
			provider.CredentialSource, provider.CredentialEnv = "env", preset.Environments[0]
			break
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return Inventory{}, ErrClosed
	}
	if s.active[id] != nil || len(s.active) >= 4 {
		s.mu.Unlock()
		return Inventory{}, ErrBusy
	}
	s.active[id] = cancel
	s.wg.Add(1)
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.active, id); s.mu.Unlock(); s.wg.Done() }()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	var auth authorization
	if key != nil {
		provider.CredentialSource, provider.CredentialEnv = "file", ""
		auth = authorization{key: key.Key, check: func(ctx context.Context) error { return ctx.Err() }}
	} else {
		auth, err = s.capture(ctx, provider)
		if err != nil {
			return Inventory{}, err
		}
	}
	models, discovery, err := s.discover(ctx, id, provider, auth)
	if err := ctx.Err(); err != nil {
		return Inventory{}, err
	}
	if err != nil {
		return Inventory{}, err
	}
	encoded, err := json.Marshal(models)
	if err != nil || len(encoded) > 2<<20 {
		return Inventory{}, ErrDiscovery
	}
	if err := auth.check(ctx); err != nil {
		return Inventory{}, err
	}
	// Validation has completed. save rechecks the exact revision before key
	// publication and again at route publication. A racing writer wins safely.
	result, err := s.save(ctx, Change{Revision: revision, ID: id, Provider: provider, Key: key}, !exists)
	if err != nil {
		return Inventory{}, err
	}
	current, err := s.snapshot(ctx)
	if err != nil {
		return result, nil // Configuration succeeded; absent discovery remains explicit.
	}
	published := current.Host.Providers[id]
	_, identity, err := s.inspect(ctx, published)
	if err != nil || published.BaseURL != provider.BaseURL || published.Kind != provider.Kind || identity.Credential != sha256.Sum256([]byte(auth.key)) {
		return result, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	total, bytes := len(models), len(encoded)
	for name, entry := range s.catalogs {
		if name != id {
			total += len(entry.value.Models)
			bytes += entry.bytes
		}
	}
	if !s.closed && total <= 8192 && bytes <= 16<<20 {
		value := Catalog{Provider: id, State: "cached", ScopeState: "current", Discovery: discovery, FetchedAt: new(time.Now().UTC()), Models: models}
		s.catalogs[id] = catalogEntry{scope: identity, observed: identity, value: value, bytes: len(encoded)}
	}
	return result, nil
}
