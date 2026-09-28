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

// CapturedCredentials couples private credentials to the manager's login
// generation. Token rotation preserves the generation; installation and logout
// invalidate it. Captures are ephemeral host values, never public projections.
type CapturedCredentials struct {
	Credentials Credentials
	Generation  uint64
}

type refresh struct {
	done     chan struct{}
	captured CapturedCredentials
	err      error
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
	read       func(string) (Credentials, error)
	save       func(string, Credentials) (bool, error)
	remove     func(string) error
}

func New(ctx context.Context, directory string) *Manager {
	ctx, cancel := context.WithCancel(ctx)
	return &Manager{
		ctx: ctx, cancel: cancel, issuer: issuer,
		path: filepath.Join(directory, "openai-codex.json"),
		read: func(path string) (Credentials, error) { return readCredentials(path, (*os.File).Sync) },
		save: func(path string, credentials Credentials) (bool, error) {
			return saveCredentials(path, credentials, (*os.File).Sync)
		},
		remove: func(path string) error { return removeCredentials(path, os.Remove, (*os.File).Sync) },
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
		m.credential, m.loadErr = m.read(m.path)
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
	if m.dirty {
		return m.credential, ErrPersistence
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
	published, err := m.save(m.path, credentials)
	if published {
		m.generation++
		m.credential, m.terminal, m.dirty = credentials, nil, err != nil
	}
	return err
}

func (m *Manager) Logout() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.generation++
	m.credential, m.terminal, m.loadErr = Credentials{}, nil, nil
	m.loaded, m.dirty = true, false
	// Revocation is immediate even when removal fails. Keep that failure visible
	// without reloading credentials that may still exist on disk.
	m.terminal = m.remove(m.path)
	return m.terminal
}

// Capture returns credentials and their generation from one locked state. Any
// required refresh finishes and persists before the capture is published.
func (m *Manager) Capture(ctx context.Context) (CapturedCredentials, error) {
	return m.credentials(ctx, "", nil)
}

// PersistPending confirms only local credential persistence. It never refreshes
// tokens and cannot complete a pending logout, which requires an explicit retry.
func (m *Manager) PersistPending(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(ctx, nil); err != nil {
		return err
	}
	return m.persistPending()
}

// persistPending requires mu and retains rotated tokens on every failed save.
func (m *Manager) persistPending() error {
	if m.dirty {
		if _, err := m.save(m.path, m.credential); err != nil {
			return err
		}
		m.dirty = false
	}
	return nil
}

// Check proves that the captured login is authorized at this check's locked
// linearization point. It is not atomic with a later HTTP request; callers must
// check again at execution after any intervening admission wait. Token rotation
// within the same login does not invalidate a capture. Check performs no refresh.
func (m *Manager) Check(ctx context.Context, captured CapturedCredentials) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.check(ctx, &captured); err != nil {
		return err
	}
	if m.dirty {
		return ErrPersistence
	}
	return nil
}

// RefreshCaptured replaces a rejected token only for its captured login. It may
// reuse a newer token from a concurrent refresh, but never a replacement login.
func (m *Manager) RefreshCaptured(ctx context.Context, captured CapturedCredentials) (CapturedCredentials, error) {
	if captured.Credentials.AccessToken == "" {
		return CapturedCredentials{}, ErrLoginChanged
	}
	return m.credentials(ctx, captured.Credentials.AccessToken, &captured)
}

func (m *Manager) Credentials(ctx context.Context) (Credentials, error) {
	captured, err := m.Capture(ctx)
	return captured.Credentials, err
}

// Refresh replaces a rejected access token. A newer token already obtained by
// another request is reused, preventing a second refresh of a rotating token.
func (m *Manager) Refresh(ctx context.Context, rejectedToken string) (Credentials, error) {
	captured, err := m.credentials(ctx, rejectedToken, nil)
	return captured.Credentials, err
}

// check requires mu. Check the generation before loading so stale captures
// cannot read or refresh a replacement account's credentials.
func (m *Manager) check(ctx context.Context, captured *CapturedCredentials) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.closed || m.ctx.Err() != nil {
		return context.Canceled
	}
	if captured != nil && captured.Generation != m.generation {
		return ErrLoginChanged
	}
	if err := m.load(); err != nil {
		return err
	}
	if captured != nil && (captured.Credentials.AccountID == "" || captured.Credentials.AccountID != m.credential.AccountID) {
		return ErrLoginChanged
	}
	if m.terminal != nil {
		return m.terminal
	}
	if m.credential.AccessToken == "" {
		return ErrSignInRequired
	}
	return nil
}

func (m *Manager) credentials(ctx context.Context, rejectedToken string, captured *CapturedCredentials) (CapturedCredentials, error) {
	if err := ctx.Err(); err != nil {
		return CapturedCredentials{}, err
	}
	m.mu.Lock()
	if err := m.check(ctx, captured); err != nil {
		m.mu.Unlock()
		return CapturedCredentials{}, err
	}
	if err := m.persistPending(); err != nil {
		m.mu.Unlock()
		return CapturedCredentials{}, err
	}
	if time.Until(m.credential.ExpiresAt) > time.Minute &&
		(rejectedToken == "" || rejectedToken != m.credential.AccessToken) {
		result := CapturedCredentials{Credentials: m.credential, Generation: m.generation}
		m.mu.Unlock()
		return result, nil
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
		return CapturedCredentials{}, ctx.Err()
	case <-flight.done:
		if err := ctx.Err(); err != nil {
			return CapturedCredentials{}, err
		}
		if flight.err != nil {
			return CapturedCredentials{}, flight.err
		}
		// The login can change after the refresh publishes but before this
		// waiter resumes. Revalidate at the capture's own linearization point.
		if err := m.Check(ctx, flight.captured); err != nil {
			return CapturedCredentials{}, err
		}
		return flight.captured, nil
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
		_, err = m.save(m.path, credentials)
		m.dirty = err != nil
	} else {
		var rejected *authError
		if errors.As(err, &rejected) && rejected.requiresLogin() {
			m.terminal = err
		}
	}
	if err == nil {
		flight.captured = CapturedCredentials{Credentials: credentials, Generation: m.generation}
	}
	flight.err = err
	m.flight = nil
	close(flight.done)
}
