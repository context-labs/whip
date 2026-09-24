package openaiauth

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type refresh struct {
	done        chan struct{}
	credentials Credentials
	err         error
}

// Manager owns one host account and coalesces refreshes across all model clients.
// Close cancels and joins owned refresh work. Network calls never hold mu; the
// short state/storage critical section orders logout and rotating-token writes.
type Manager struct {
	ctx        context.Context
	cancel     context.CancelFunc
	http       *http.Client
	issuer     string
	path       string
	mu         sync.Mutex
	wg         sync.WaitGroup
	closed     bool
	loaded     bool
	dirty      bool
	generation uint64
	credential Credentials
	loadErr    error
	terminal   error
	flight     *refresh
	save       func(string, Credentials) error
}

func New(ctx context.Context, directory string) *Manager {
	ctx, cancel := context.WithCancel(ctx)
	return &Manager{
		ctx: ctx, cancel: cancel, issuer: issuer,
		path: filepath.Join(directory, "openai-codex.json"), save: saveCredentials,
		http: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	}
}

func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	m.cancel()
	m.mu.Unlock()
	m.wg.Wait()
}

func (m *Manager) load() error {
	if !m.loaded {
		m.credential, m.loadErr = readCredentials(m.path)
		m.loaded = m.loadErr == nil
	}
	return m.loadErr
}

// Snapshot returns private saved state without refreshing it. Callers must build
// an explicit safe account projection before returning any status to a client.
func (m *Manager) Snapshot() (Credentials, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.load(); err != nil {
		return Credentials{}, err
	}
	return m.credential, m.terminal
}

func (m *Manager) Generation() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.generation
}

// Install publishes a completed login only if no logout/replacement intervened.
func (m *Manager) Install(ctx context.Context, generation uint64, credentials Credentials) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.closed || m.ctx.Err() != nil || generation != m.generation {
		return ErrLoginChanged
	}
	if err := m.load(); err != nil {
		return err
	}
	if err := m.save(m.path, credentials); err != nil {
		return err
	}
	m.generation++
	m.credential, m.terminal, m.dirty = credentials, nil, false
	return nil
}

func (m *Manager) Logout() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.generation++
	m.credential, m.terminal, m.loadErr = Credentials{}, nil, nil
	m.loaded, m.dirty = true, false
	if err := os.Remove(m.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.New("could not remove OpenAI credentials; logout is not durable until the file is removed")
	}
	return nil
}

func (m *Manager) Credentials(ctx context.Context) (Credentials, error) {
	return m.credentials(ctx, "")
}

// Refresh replaces a rejected access token. A newer token already obtained by
// another request is reused, preventing a second refresh of a rotating token.
func (m *Manager) Refresh(ctx context.Context, rejectedToken string) (Credentials, error) {
	return m.credentials(ctx, rejectedToken)
}

func (m *Manager) credentials(ctx context.Context, rejectedToken string) (Credentials, error) {
	if err := ctx.Err(); err != nil {
		return Credentials{}, err
	}
	m.mu.Lock()
	if m.closed || m.ctx.Err() != nil {
		m.mu.Unlock()
		return Credentials{}, context.Canceled
	}
	if err := m.load(); err != nil {
		m.mu.Unlock()
		return Credentials{}, err
	}
	if m.terminal != nil || m.credential.AccessToken == "" {
		err := m.terminal
		if err == nil {
			err = ErrSignInRequired
		}
		m.mu.Unlock()
		return Credentials{}, err
	}
	if m.dirty {
		if err := m.save(m.path, m.credential); err != nil {
			m.mu.Unlock()
			return Credentials{}, err
		}
		m.dirty = false
	}
	if time.Until(m.credential.ExpiresAt) > time.Minute &&
		(rejectedToken == "" || rejectedToken != m.credential.AccessToken) {
		credentials := m.credential
		m.mu.Unlock()
		return credentials, nil
	}
	flight := m.flight
	if flight == nil {
		flight = &refresh{done: make(chan struct{})}
		m.flight = flight
		credentials, generation := m.credential, m.generation
		m.wg.Add(1)
		go m.runRefresh(flight, generation, credentials)
	}
	m.mu.Unlock()
	select {
	case <-ctx.Done():
		return Credentials{}, ctx.Err()
	case <-flight.done:
		return flight.credentials, flight.err
	}
}

func (m *Manager) runRefresh(flight *refresh, generation uint64, previous Credentials) {
	defer m.wg.Done()
	ctx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
	defer cancel()
	credentials, err := m.exchange(ctx, url.Values{
		"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {previous.RefreshToken},
	}, previous)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || generation != m.generation || m.ctx.Err() != nil {
		err = ErrLoginChanged
	} else if err == nil {
		// Keep a rotated token in memory if persistence fails. A subsequent call
		// retries saving it instead of reusing the now-invalid old refresh token.
		m.credential = credentials
		err = m.save(m.path, credentials)
		m.dirty = err != nil
	} else {
		var rejected *authError
		if errors.As(err, &rejected) && rejected.requiresLogin() {
			m.terminal = err
		}
	}
	if err == nil {
		flight.credentials = credentials
	}
	flight.err = err
	m.flight = nil
	close(flight.done)
}
